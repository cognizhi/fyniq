package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectrate"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ rates and time at cost (FR-PRJ-06; p1-projects task 3.3)
//
// A person's cost rate is payroll data (FR-PAY-13; the owner's decision of 19 Nov 2026): the Owner and the Payroll administrator
// enter and read it, through their own Appendix D row. Time at cost is computed here, for the viewer's tier, and a rate is never
// sent to be hidden by a screen: everyone else is given hours, and the cost at the role-average rate of at least three people,
// else the standard rate, saying which (G4-050).

// CostRate is a person's cost of an hour from a day, or the organisation's standard rate when it names no person.
type CostRate struct {
	ID            string `json:"id" format:"uuid" doc:"The rate's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5201"`
	UserID        string `json:"user_id,omitempty" format:"uuid" doc:"The person; absent for the organisation's standard rate." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	PersonName    string `json:"person_name,omitempty" doc:"Their name." example:"Priya Nair"`
	Role          string `json:"role" doc:"The role the person's rate is grouped under, for role-average rates; Standard for the standard rate." example:"Consultant"`
	EffectiveFrom string `json:"effective_from" format:"date" doc:"The first day it applies." example:"2026-01-01"`
	HourlyRate    string `json:"hourly_rate" doc:"The cost of an hour in the base currency, an exact decimal with at most 6 decimals." example:"62.500000"`
	Currency      string `json:"currency" doc:"The base currency." example:"SGD"`
}

// CostRateDraft enters a cost rate.
type CostRateDraft struct {
	UserID        string `json:"user_id,omitempty" format:"uuid" doc:"The person, an active member; left out, the organisation's standard rate (the Owner's fallback for viewers who may not see a person's own)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	Role          string `json:"role,omitempty" maxLength:"80" doc:"The role the person is grouped under for role-average rates (at least three people share a role); required with a person." example:"Consultant"`
	EffectiveFrom string `json:"effective_from" format:"date" doc:"The first day it applies; a person's rate of a day is the latest starting on or before it. A rate never changes: enter the new one from its day." example:"2026-01-01"`
	HourlyRate    string `json:"hourly_rate" doc:"The cost of an hour in the base currency: payroll cost divided by standard hours, or an agreed rate; a plain decimal, at most 6 decimals." example:"62.50"`
}

// CostRatePage is a page of cost rates.
type CostRatePage struct {
	Items      []CostRate      `json:"items" nullable:"false"`
	People     []ProjectPerson `json:"people" nullable:"false" doc:"The active members a rate can be entered for, by name: the Payroll administrator may not read the members list, so it comes with the rates."`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// BillingRate is the price of an hour from a day.
type BillingRate struct {
	ID            string `json:"id" format:"uuid" doc:"The rate's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5301"`
	ProjectID     string `json:"project_id,omitempty" format:"uuid" doc:"The project it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	CustomerID    string `json:"customer_id,omitempty" format:"uuid" doc:"The customer whose projects it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	UserID        string `json:"user_id,omitempty" format:"uuid" doc:"The person it is for; absent for a role or everyone." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	Role          string `json:"role,omitempty" doc:"The role it is for." example:"Consultant"`
	EffectiveFrom string `json:"effective_from" format:"date" doc:"The first day it applies." example:"2026-01-01"`
	HourlyRate    string `json:"hourly_rate" doc:"The price of an hour in the currency." example:"150.00"`
	Currency      string `json:"currency" doc:"The project's currency." example:"SGD"`
}

// BillingRateDraft enters a billing rate.
type BillingRateDraft struct {
	ProjectID     string `json:"project_id,omitempty" format:"uuid" doc:"The project it is for, in the project's currency; give this or customer_id." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	CustomerID    string `json:"customer_id,omitempty" format:"uuid" doc:"A customer, for all their projects, in the base currency; give this or project_id." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	UserID        string `json:"user_id,omitempty" format:"uuid" doc:"One person; not with role." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	Role          string `json:"role,omitempty" maxLength:"80" doc:"One role; not with user_id. Neither: everyone on it." example:"Consultant"`
	EffectiveFrom string `json:"effective_from" format:"date" doc:"The first day it applies." example:"2026-01-01"`
	HourlyRate    string `json:"hourly_rate" doc:"The price of an hour, a plain decimal with at most 6 decimals." example:"150.00"`
}

// BillingRatePage is a page of billing rates.
type BillingRatePage struct {
	Items      []BillingRate `json:"items" nullable:"false"`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// TimeCostPerson is a person's approved time on the project; the cost is given only to a viewer who may see a person's rate.
type TimeCostPerson struct {
	UserID     string `json:"user_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	PersonName string `json:"person_name" doc:"Their name." example:"Priya Nair"`
	Minutes    int    `json:"minutes" doc:"Their approved net minutes." example:"2250"`
	Billable   int    `json:"billable_minutes" doc:"Of them, the billable minutes." example:"2100"`
	Cost       string `json:"cost,omitempty" doc:"Their time at their own cost rate, in the base currency: present only for the Owner and the Payroll administrator (payroll data, FR-PAY-13)." example:"2343.75"`
}

// TimeCost is a project's approved time and what it costs, at the viewer's tier.
type TimeCost struct {
	ProjectID string           `json:"project_id" format:"uuid" doc:"The project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	From      string           `json:"from" format:"date" doc:"The first day." example:"2026-11-01"`
	To        string           `json:"to" format:"date" doc:"The last day." example:"2026-11-30"`
	Minutes   int              `json:"minutes" doc:"The approved net minutes: a reversing entry takes away what it reverses." example:"4500"`
	Billable  int              `json:"billable_minutes" doc:"Of them, the billable minutes." example:"4200"`
	Cost      string           `json:"cost,omitempty" doc:"The time at cost in the base currency (RND-TIME per entry): at each person's rate for the Owner and the Payroll administrator, else at the role-average rate of at least three people, else the standard rate; absent when no rate may be used." example:"4687.50"`
	Basis     string           `json:"rate_basis" enum:"person,role_average,standard,mixed,none" doc:"Where the rates came from: person (each person's own), role_average, standard, mixed when more than one applied, none (hours only)." example:"role_average"`
	Currency  string           `json:"currency" doc:"The base currency." example:"SGD"`
	People    []TimeCostPerson `json:"people" nullable:"false" doc:"Each person's hours; their cost only at the payroll tier."`
}

type costRateListInput struct{ PageQuery }
type costRateListOutput struct{ Body CostRatePage }

type setCostRateInput struct {
	KeyHeader
	PreferHeader
	Body CostRateDraft
}

type costRateOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CostRate
}

type billingRateListInput struct {
	ProjectPath
	PageQuery
}
type billingRateListOutput struct{ Body BillingRatePage }

type setBillingRateInput struct {
	KeyHeader
	PreferHeader
	Body BillingRateDraft
}

type billingRateOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BillingRate
}

type timeCostInput struct {
	ProjectPath
	From string `query:"from" format:"date" required:"true" doc:"The first day." example:"2026-11-01"`
	To   string `query:"to" format:"date" required:"true" doc:"The last day, on or after the first, at most 366 days after it." example:"2026-11-30"`
}
type timeCostOutput struct{ Body TimeCost }

// RateCodes are the codes a write of a rate may answer.
var RateCodes = []string{CodeProjectsOff, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRate, storage.CodeDuplicate, storage.CodeRetry, storage.CodeLockTimeout}

// costTier is what the caller may see of cost rates: each person's own for the Owner and the Payroll administrator (the Appendix D
// row "Project cost rates, person level"), none for a role that sees hours only, the role average for the rest.
func costTier(p authn.Principal) projectrate.Tier {
	if authz.Decide(authz.Policy{Operation: "view", Capability: authz.ProjectCostRates, Need: authz.View}, p.Roles, p.AI()).Allowed {
		return projectrate.Payroll
	}
	for _, r := range []authz.Role{authz.Controller, authz.Accountant, authz.Auditor, authz.TaxAgent, authz.Approver} {
		if slices.Contains(p.Roles, r) {
			return projectrate.Averaged
		}
	}
	return projectrate.Hours
}

func rateDay(field, s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, invalid([]FieldProblem{{Code: CodeInvalid, Field: field, Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"}})
	}
	return t, nil
}

func registerProjectRates(a *API) {
	register(a, authz.ListCostRates, huma.Operation{
		OperationID: "listCostRates", Method: http.MethodGet, Path: "/v1/cost-rates", Tags: []string{"Projects"},
		Summary: "List the cost rates",
		Description: "Every person's cost rate and the organisation's standard rate, oldest first, a page at a time. Payroll data (FR-PAY-13): only the Owner and the " +
			"Payroll administrator, through the Appendix D row \"Project cost rates, person level\"; never the AI. Refused with PRJ-09 while Projects and time are " +
			"switched off. Read-only; no side effects.",
	}, []string{CodeCursor, CodeProjectsOff}, func(ctx context.Context, in *costRateListInput) (*costRateListOutput, error) {
		p, _ := authn.From(ctx)
		out := costRateListOutput{Body: CostRatePage{Items: []CostRate{}, People: []ProjectPerson{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			rs, err := u.Rates().CostRates(ctx)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			ms, err := u.Members().List(ctx, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			for _, m := range ms {
				out.Body.People = append(out.Body.People, ProjectPerson{UserID: m.UserID.String(), DisplayName: m.DisplayName})
			}
			from, to, next, err := pageAt(len(rs), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, r := range rs[from:to] {
				x := CostRate{ID: r.ID.String(), Role: r.Role, EffectiveFrom: r.From.Format(time.DateOnly), HourlyRate: string(r.Hourly), Currency: r.Currency}
				if r.UserID != nil {
					x.UserID, x.PersonName = r.UserID.String(), names[*r.UserID]
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.SetCostRate, huma.Operation{
		OperationID: "setCostRate", Method: http.MethodPost, Path: "/v1/cost-rates", Tags: []string{"Projects"},
		DefaultStatus: http.StatusCreated, Summary: "Enter a cost rate",
		Description: "Enters a person's cost rate from a day, or the standard rate when no person is named (FR-PRJ-06; the owner's decision of 19 Nov 2026). Until payroll " +
			"supplies it the rate is typed in; a payroll-derived rate replaces it later. A rate never changes: a later day's replaces it from that day, and the same " +
			"day twice is refused. Only the Owner and the Payroll administrator, never the AI. Side effects: the rate, and its audit event, in which the rate is proved " +
			"by fingerprint and never kept (AUD-02). Refused with PRJ-09 while Projects and time are switched off. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, RateCodes...), func(ctx context.Context, in *setCostRateInput) (*costRateOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CostRate, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return CostRate{}, err
			}
			org, err := u.Organisation(ctx)
			if err != nil {
				return CostRate{}, err
			}
			cur, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return CostRate{}, err
			}
			var probs []FieldProblem
			from, derr := rateDay("body.effective_from", in.Body.EffectiveFrom)
			if derr != nil {
				probs = append(probs, FieldProblem{Code: CodeInvalid, Field: "body.effective_from", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
			}
			rate, rerr := money.ParseUnitPrice(in.Body.HourlyRate, cur, "hourly_rate")
			if rerr != nil {
				probs = append(probs, fieldOf(rerr, "body.hourly_rate"))
			}
			n := storage.NewCostRate{From: from, Hourly: storage.Decimal(rate.Decimal().Text('f')), Currency: cur.Code, Role: strings.TrimSpace(in.Body.Role)}
			names := map[uuid.UUID]string{}
			if in.Body.UserID == "" {
				n.Role = storage.StandardRole
			} else {
				id, perr := uuid.Parse(in.Body.UserID)
				if perr != nil {
					probs = append(probs, FieldProblem{Code: CodeInvalid, Field: "body.user_id", Message: "is not an ID", Fix: "Send the member's user ID"})
				} else if names, err = memberNames(ctx, u, a.deps.Clock.Now()); err != nil {
					return CostRate{}, err
				} else if _, ok := names[id]; !ok {
					probs = append(probs, FieldProblem{Code: CodeInvalid, Field: "body.user_id", Message: "is not an active member of the organisation", Fix: "Choose a member"})
				} else {
					n.UserID = &id
				}
				if n.Role == "" {
					probs = append(probs, FieldProblem{Code: CodeInvalid, Field: "body.role", Message: "is empty: a person's rate is grouped under a role", Fix: "Name the role, such as Consultant"})
				}
			}
			if len(probs) > 0 {
				return CostRate{}, invalid(probs)
			}
			r, err := u.Rates().AddCostRate(ctx, n)
			if err != nil {
				return CostRate{}, err
			}
			out := CostRate{ID: r.ID.String(), Role: r.Role, EffectiveFrom: r.From.Format(time.DateOnly), HourlyRate: string(r.Hourly), Currency: r.Currency}
			if r.UserID != nil {
				out.UserID, out.PersonName = r.UserID.String(), names[*r.UserID]
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &costRateOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ListBillingRates, huma.Operation{
		OperationID: "listBillingRates", Method: http.MethodGet, Path: "/v1/projects/{project_id}/billing-rates", Tags: []string{"Projects"},
		Summary: "List a project's billing rates",
		Description: "The project's own billing rates and its customer's, oldest first (FR-PRJ-06). A person who reads only the projects they are on gets 404 for another one. " +
			"Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, CodeProjectsOff}, func(ctx context.Context, in *billingRateListInput) (*billingRateListOutput, error) {
		id, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := billingRateListOutput{Body: BillingRatePage{Items: []BillingRate{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			visible, err := a.visibleProjects(ctx, u, p)
			if err != nil {
				return err
			}
			if !visible(id) {
				return ports.ErrNotFound
			}
			pr, err := u.Projects().Get(ctx, id)
			if err != nil {
				return err
			}
			rs, err := u.Rates().BillingRates(ctx, id, pr.CustomerID)
			if err != nil {
				return err
			}
			from, to, next, err := pageAt(len(rs), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, r := range rs[from:to] {
				out.Body.Items = append(out.Body.Items, billingRateOf(r))
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.SetBillingRate, huma.Operation{
		OperationID: "setBillingRate", Method: http.MethodPost, Path: "/v1/billing-rates", Tags: []string{"Projects"},
		DefaultStatus: http.StatusCreated, Summary: "Enter a billing rate",
		Description: "Enters the price of an hour from a day for a project, in its currency, or for a customer's projects in the base currency; for one person, one role or " +
			"everyone (FR-PRJ-06). The rate in force for an entry is the project's for the person, then their role, then everyone, then the customer's the same way. A rate " +
			"never changes: a later day's replaces it. Side effects: the rate and its audit event. Refused with PRJ-09 while Projects and time are switched off. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, RateCodes...), func(ctx context.Context, in *setBillingRateInput) (*billingRateOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BillingRate, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return BillingRate{}, err
			}
			var probs []FieldProblem
			bad := func(field, msg, fix string) {
				probs = append(probs, FieldProblem{Code: CodeInvalid, Field: "body." + field, Message: msg, Fix: fix})
			}
			n := storage.NewBillingRate{Role: strings.TrimSpace(in.Body.Role)}
			org, err := u.Organisation(ctx)
			if err != nil {
				return BillingRate{}, err
			}
			cur := org.BaseCurrency
			switch {
			case (in.Body.ProjectID == "") == (in.Body.CustomerID == ""):
				bad("project_id", "give a project or a customer, not both or neither", "Name the project, or the customer for all their projects")
			case in.Body.ProjectID != "":
				id, perr := uuid.Parse(in.Body.ProjectID)
				if perr != nil {
					bad("project_id", "is not an ID", "Send the project's ID")
					break
				}
				pr, gerr := u.Projects().Get(ctx, id)
				if gerr != nil {
					return BillingRate{}, gerr
				}
				if !pr.Configured || pr.BillingMethod == storage.BillingInternal {
					bad("project_id", "is not a project that is billed", "Choose a billed project")
				}
				n.ProjectID, cur = &id, pr.Currency
			default:
				id, perr := uuid.Parse(in.Body.CustomerID)
				if perr != nil {
					bad("customer_id", "is not an ID", "Send the customer's ID")
					break
				}
				c, gerr := u.Contacts().Get(ctx, id)
				if gerr != nil {
					return BillingRate{}, gerr
				}
				if !c.Customer {
					bad("customer_id", "is not a customer", "Choose a contact marked as a customer")
				}
				n.CustomerID = &id
			}
			if in.Body.UserID != "" {
				id, perr := uuid.Parse(in.Body.UserID)
				if perr != nil {
					bad("user_id", "is not an ID", "Send the member's user ID")
				}
				n.UserID = &id
			}
			if n.UserID != nil && n.Role != "" {
				bad("role", "is given with a person: a rate is for one person or one role", "Leave out one of them")
			}
			from, derr := rateDay("body.effective_from", in.Body.EffectiveFrom)
			if derr != nil {
				bad("effective_from", "is not a date", "Send the day as YYYY-MM-DD")
			}
			if c, cerr := money.Lookup(cur); cerr == nil {
				if rate, rerr := money.ParseUnitPrice(in.Body.HourlyRate, c, "hourly_rate"); rerr != nil {
					probs = append(probs, fieldOf(rerr, "body.hourly_rate"))
				} else {
					n.Hourly = storage.Decimal(rate.Decimal().Text('f'))
				}
			}
			if len(probs) > 0 {
				return BillingRate{}, invalid(probs)
			}
			n.From, n.Currency = from, cur
			r, err := u.Rates().AddBillingRate(ctx, n)
			return billingRateOf(r), err
		})
		if err != nil {
			return nil, err
		}
		return &billingRateOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.GetProjectTimeCost, huma.Operation{
		OperationID: "getProjectTimeCost", Method: http.MethodGet, Path: "/v1/projects/{project_id}/time-cost", Tags: []string{"Projects"},
		Summary: "Read a project's approved time and what it costs",
		Description: "The approved net minutes on the project between two days, each person's hours, and the time at cost (FR-PRJ-06, FR-PRJ-12), valued per entry by RND-TIME at the " +
			"caller's tier and never otherwise: the Owner and the Payroll administrator see each person's own rate and cost; the Controller, the Accountant, the Auditor, the " +
			"Tax agent and the Approver (own projects) see the cost at the role-average rate of at least three people, else the standard rate, and no per-person cost; the " +
			"AR specialist sees hours only. rate_basis says which. The AI never reads it. Time posts nothing: it is a management view. Refused with PRJ-09 while Projects " +
			"and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeInvalid, CodeProjectsOff}, func(ctx context.Context, in *timeCostInput) (*timeCostOutput, error) {
		id, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		from, ferr := rateDay("query.from", in.From)
		to, terr := rateDay("query.to", in.To)
		if ferr != nil || terr != nil || to.Before(from) || to.After(from.AddDate(0, 0, 366)) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.to", Message: "is not a day on or after from, within 366 days", Fix: "Ask for at most a year"}})
		}
		p, _ := authn.From(ctx)
		out := timeCostOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			visible, err := a.visibleProjects(ctx, u, p)
			if err != nil {
				return err
			}
			if !visible(id) {
				return ports.ErrNotFound
			}
			if _, err := u.Projects().Get(ctx, id); err != nil {
				return err
			}
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			cur, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return err
			}
			rs, err := u.Rates().CostRates(ctx)
			if err != nil {
				return err
			}
			es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{ProjectID: id, From: from, To: to, Status: storage.TimeApproved, Limit: 20000})
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			tier := costTier(p)
			body := TimeCost{ProjectID: id.String(), From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Currency: cur.Code, People: []TimeCostPerson{}}
			total := money.Zero(cur)
			bases := map[projectrate.Basis]bool{}
			costed := false
			per := map[uuid.UUID]*TimeCostPerson{}
			own := map[uuid.UUID]money.Money{}
			for _, e := range es {
				x, ok := per[e.UserID]
				if !ok {
					x = &TimeCostPerson{UserID: e.UserID.String(), PersonName: names[e.UserID]}
					per[e.UserID], own[e.UserID] = x, money.Zero(cur)
				}
				x.Minutes += e.Minutes
				body.Minutes += e.Minutes
				if e.Billable {
					x.Billable += e.Minutes
					body.Billable += e.Minutes
				}
				rate, basis, err := projectrate.Rate(rs, tier, e.UserID, e.WorkDate, cur)
				if err != nil {
					return err
				}
				bases[basis] = true
				if basis == projectrate.BasisNone {
					continue
				}
				c, err := rounding.Time(e.Minutes, rate)
				if err != nil {
					return err
				}
				costed = true
				if total, err = total.Add(c); err != nil {
					return err
				}
				if own[e.UserID], err = own[e.UserID].Add(c); err != nil {
					return err
				}
			}
			if costed {
				body.Cost = total.String()
			}
			body.Basis = basisOf(bases)
			for uid, x := range per {
				if tier == projectrate.Payroll && !own[uid].IsZero() {
					x.Cost = own[uid].String()
				}
				body.People = append(body.People, *x)
			}
			slices.SortFunc(body.People, func(a, b TimeCostPerson) int { return strings.Compare(a.PersonName+a.UserID, b.PersonName+b.UserID) })
			out.Body = body
			return nil
		})
		return &out, err
	})
}

// basisOf says where the rates of a total came from: the one basis, mixed, or none.
func basisOf(b map[projectrate.Basis]bool) string {
	var used []projectrate.Basis
	for _, x := range []projectrate.Basis{projectrate.BasisPerson, projectrate.BasisRoleAverage, projectrate.BasisStandard} {
		if b[x] {
			used = append(used, x)
		}
	}
	switch {
	case len(used) == 0:
		return string(projectrate.BasisNone)
	case len(used) == 1 && !b[projectrate.BasisNone]:
		return string(used[0])
	}
	return "mixed"
}

func billingRateOf(r storage.BillingRate) BillingRate {
	out := BillingRate{ID: r.ID.String(), Role: r.Role, EffectiveFrom: r.From.Format(time.DateOnly), HourlyRate: string(r.Hourly), Currency: r.Currency}
	if r.ProjectID != nil {
		out.ProjectID = r.ProjectID.String()
	}
	if r.CustomerID != nil {
		out.CustomerID = r.CustomerID.String()
	}
	if r.UserID != nil {
		out.UserID = r.UserID.String()
	}
	return out
}

// roundingTime is RND-TIME for a cost, kept apart so the report and the time cost value time the same way.
func roundingTime(minutes int, rate money.UnitPrice) (money.Money, error) {
	return rounding.Time(minutes, rate)
}
