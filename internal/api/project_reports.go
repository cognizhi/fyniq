package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/project"
	"github.com/cognizhi/fyniq/internal/domain/projectrate"
	"github.com/cognizhi/fyniq/internal/domain/projectreport"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ project reports (FR-PRJ-12; p1-projects task 4.2)

// ProjectReportRow is a project's profitability in a window.
type ProjectReportRow struct {
	ProjectID     string `json:"project_id" format:"uuid" doc:"The project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Code          string `json:"code" doc:"Its code." example:"ACME-01"`
	Name          string `json:"name" doc:"Its name." example:"Acme rollout"`
	CustomerName  string `json:"customer_name,omitempty" doc:"Its customer." example:"Acme Pte Ltd"`
	Revenue       string `json:"revenue" doc:"Revenue posted to the project in the months, from posted ledger lines on revenue accounts, in the base currency." example:"30000.00"`
	DirectCosts   string `json:"direct_costs,omitempty" doc:"Project-tagged posted costs on expense accounts WITHOUT the payroll-cost flag (FR-COA-02), in the base currency." example:"420.00"`
	PayrollTagged string `json:"payroll_tagged,omitempty" doc:"Project-tagged lines on payroll-cost accounts: listed to reconcile, never in direct costs, so salary tagged to a project is not counted twice (G4-049)." example:"5000.00"`
	TimeCost      string `json:"time_cost,omitempty" doc:"Approved time at the viewer's cost rate (RND-TIME per entry); absent when no rate may be used." example:"12000.00"`
	Margin        string `json:"margin,omitempty" doc:"Revenue less time at cost less direct costs; absent when the time has no cost to use." example:"17580.00"`
	Minutes       int    `json:"minutes" doc:"Approved net minutes." example:"13500"`
	Billable      int    `json:"billable_minutes" doc:"Of them, the billable minutes." example:"12000"`
	Standard      string `json:"standard_value,omitempty" doc:"Billable time at billing rates, in the base currency, when the project is billed in it." example:"36000.00"`
	Realisation   string `json:"realisation,omitempty" doc:"Billed (revenue) over standard value, a percentage to one decimal." example:"83.3"`
	Basis         string `json:"rate_basis" enum:"person,role_average,standard,mixed,none" doc:"Where the time cost's rates came from." example:"role_average"`
	BudgetMinutes *int   `json:"budget_minutes,omitempty" doc:"The hours budget in minutes." example:"24000"`
	BudgetUsed    string `json:"budget_used,omitempty" doc:"Approved minutes over the hours budget, a percentage to one decimal." example:"56.3"`
}

// ProjectReportPerson is a person's time in a window.
type ProjectReportPerson struct {
	UserID      string `json:"user_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	PersonName  string `json:"person_name" doc:"Their name." example:"Priya Nair"`
	Minutes     int    `json:"minutes" doc:"Approved net minutes." example:"9000"`
	Billable    int    `json:"billable_minutes" doc:"Billable minutes." example:"8000"`
	Available   int    `json:"available_minutes" doc:"The weekdays of the window less public holidays, at 8 hours a day (a working pattern and leave join with FR-PPL-07)." example:"10080"`
	Utilisation string `json:"utilisation" doc:"Billable over available, a percentage to one decimal." example:"79.4"`
	Cost        string `json:"cost,omitempty" doc:"Their time at their own cost rate: only for the Owner and the Payroll administrator (FR-PAY-13)." example:"9000.00"`
}

// ProjectReport is what projects earn and cost.
type ProjectReport struct {
	From         string                `json:"from" format:"date" doc:"The first month's first day." example:"2026-11-01"`
	To           string                `json:"to" format:"date" doc:"The last month's first day." example:"2026-11-01"`
	Currency     string                `json:"currency" doc:"The base currency." example:"SGD"`
	Projects     []ProjectReportRow    `json:"projects" nullable:"false" doc:"Each project the caller may see."`
	People       []ProjectReportPerson `json:"people" nullable:"false" doc:"Each person's utilisation."`
	Untagged     *ProjectReportRow     `json:"untagged,omitempty" doc:"Revenue and costs of lines tagged with no project: every project plus these equal the revenue and expense accounts' totals (QA-RPT-01). Only for those who see every project, as it is company-wide."`
	CostsVisible bool                  `json:"costs_visible" doc:"false when the caller may see hours only." example:"true"`
}

type projectReportInput struct {
	From string `query:"from" format:"date" required:"true" doc:"A day in the first month." example:"2026-11-01"`
	To   string `query:"to" format:"date" required:"true" doc:"A day in the last month, at most 12 months after the first." example:"2026-11-30"`
}
type projectReportOutput struct{ Body ProjectReport }

type timeEntriesInput struct {
	ProjectPath
	PageQuery
	From string `query:"from" format:"date" required:"true" doc:"The first day." example:"2026-11-01"`
	To   string `query:"to" format:"date" required:"true" doc:"The last day, at most 366 days after the first." example:"2026-11-30"`
}

// TimeEntryList is a project's approved entries.
type TimeEntryList struct {
	Items      []TimeEntry `json:"items" nullable:"false" doc:"The approved entries by day; hours only, never a rate or a cost."`
	NextCursor string      `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type timeEntriesOutput struct{ Body TimeEntryList }

func moneyText(m *money.Money) string {
	if m == nil {
		return ""
	}
	return m.String()
}

func registerProjectReports(a *API) {
	register(a, authz.GetProjectReport, huma.Operation{
		OperationID: "getProjectReport", Method: http.MethodGet, Path: "/v1/reports/projects", Tags: []string{"Projects"},
		Summary: "Project profitability, utilisation and realisation",
		Description: "For each project the caller may see, between two months: revenue and direct costs from the posted ledger through the balance service (payroll-cost " +
			"accounts excluded and listed apart), the time at cost at the caller's tier (RND-TIME) and the margin = revenue - time at cost - direct costs, approved hours, " +
			"billing at standard value and realisation, and budget use; each person's utilisation (billable over available hours); and the lines tagged with no project, so " +
			"every project plus these tie to the ledger (FR-PRJ-12, QA-RPT-01). Time posts nothing. A person's own cost appears only for the Owner and the Payroll " +
			"administrator; the AR specialist is given hours and revenue only; the AI never reads it. Refused with PRJ-09 while Projects and time are switched off. " +
			"Read-only; no side effects.",
	}, []string{CodeInvalid, CodeProjectsOff}, func(ctx context.Context, in *projectReportInput) (*projectReportOutput, error) {
		from, ferr := rateDay("query.from", in.From)
		to, terr := rateDay("query.to", in.To)
		if ferr != nil || terr != nil || to.Before(from) || to.After(from.AddDate(1, 0, 0)) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.to", Message: "is not a day on or after from, within 12 months", Fix: "Ask for at most a year"}})
		}
		p, _ := authn.From(ctx)
		out := projectReportOutput{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			body, err := a.projectReportOf(ctx, u, p, from, to)
			out.Body = body
			return err
		})
		return &out, err
	})

	register(a, authz.ListProjectTimeEntries, huma.Operation{
		OperationID: "listProjectTimeEntries", Method: http.MethodGet, Path: "/v1/projects/{project_id}/time-entries", Tags: []string{"Projects"},
		Summary: "The approved time entries behind a project's figures",
		Description: "The project's approved entries between two days, reversing entries included, by day: the drill-down of its hours and cost (FR-PRJ-12). Hours only, never " +
			"a rate or a cost. A person who reads only the projects they are on gets 404 for another one. Refused with PRJ-09 while Projects and time are switched off. " +
			"Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, CodeInvalid, CodeProjectsOff}, func(ctx context.Context, in *timeEntriesInput) (*timeEntriesOutput, error) {
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
		out := timeEntriesOutput{Body: TimeEntryList{Items: []TimeEntry{}}}
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
			es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{ProjectID: id, From: from, To: to, Status: storage.TimeApproved, Limit: 5000})
			if err != nil {
				return err
			}
			from2, to2, next, err := pageAt(len(es), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, e := range es[from2:to2] {
				out.Body.Items = append(out.Body.Items, timeEntryOf(e))
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})
}

// projectReportOf works the report out for the caller's tier.
func (a *API) projectReportOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, from, to time.Time) (ProjectReport, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return ProjectReport{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return ProjectReport{}, err
	}
	first := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := last.AddDate(0, 1, -1)
	tier := costTier(p)
	body := ProjectReport{From: first.Format(time.DateOnly), To: last.Format(time.DateOnly), Currency: base.Code, Projects: []ProjectReportRow{}, People: []ProjectReportPerson{}, CostsVisible: tier != projectrate.Hours}
	ledger, untagged, err := projectreport.LedgerByProject(ctx, u, base, first, last)
	if err != nil {
		return body, err
	}
	visible, err := a.visibleProjects(ctx, u, p)
	if err != nil {
		return body, err
	}
	costRates, err := u.Rates().CostRates(ctx)
	if err != nil {
		return body, err
	}
	names, err := memberNames(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return body, err
	}
	ps, err := u.Projects().List(ctx)
	if err != nil {
		return body, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return body, err
	}
	holidays, err := holidaysOf(ctx, u, today)
	if err != nil {
		return body, err
	}
	type tally struct {
		minutes, billable int
		cost              money.Money
	}
	people := map[uuid.UUID]*tally{}
	for _, pr := range ps {
		if !pr.Configured || !visible(pr.ID) {
			continue
		}
		billing, err := u.Rates().BillingRates(ctx, pr.ID, pr.CustomerID)
		if err != nil {
			return body, err
		}
		es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{ProjectID: pr.ID, From: first, To: end, Status: storage.TimeApproved, Limit: 20000})
		if err != nil {
			return body, err
		}
		t, err := projectreport.TimeOf(es, costRates, billing, pr, tier, base)
		if err != nil {
			return body, err
		}
		l, ok := ledger[pr.ID]
		if !ok {
			l = projectreport.Ledger{Revenue: money.Zero(base), DirectCosts: money.Zero(base), PayrollTagged: money.Zero(base)}
		}
		row := ProjectReportRow{ProjectID: pr.ID.String(), Code: pr.Code, Name: pr.Name, Revenue: l.Revenue.String(), DirectCosts: l.DirectCosts.String(), PayrollTagged: l.PayrollTagged.String(),
			Minutes: t.Minutes, Billable: t.Billable, Basis: basisOf(t.Basis), BudgetMinutes: pr.BudgetMinutes}
		if tier == projectrate.Hours { // the AR specialist: revenue and hours (Appendix D)
			row.DirectCosts, row.PayrollTagged = "", ""
		} else {
			row.TimeCost = moneyText(t.Cost)
			if m, err := projectreport.Margin(l, t); err != nil {
				return body, err
			} else {
				row.Margin = moneyText(m)
			}
			row.Standard = moneyText(t.Standard)
			if row.Realisation, err = projectreport.Realisation(l.Revenue, t); err != nil {
				return body, err
			}
		}
		if pr.BudgetMinutes != nil && *pr.BudgetMinutes > 0 {
			row.BudgetUsed, err = projectreport.Utilisation(t.Minutes, *pr.BudgetMinutes, base)
			if err != nil {
				return body, err
			}
		}
		if pr.CustomerID != nil {
			if c, err := u.Contacts().Get(ctx, *pr.CustomerID); err == nil {
				row.CustomerName = c.LegalName
			}
		}
		body.Projects = append(body.Projects, row)
		for _, e := range es {
			x, ok := people[e.UserID]
			if !ok {
				x = &tally{cost: money.Zero(base)}
				people[e.UserID] = x
			}
			x.minutes += e.Minutes
			if e.Billable {
				x.billable += e.Minutes
			}
			if tier == projectrate.Payroll {
				if rate, basis, err := projectrate.Rate(costRates, tier, e.UserID, e.WorkDate, base); err == nil && basis == projectrate.BasisPerson {
					if c, err := roundingTime(e.Minutes, rate); err == nil {
						x.cost, _ = x.cost.Add(c)
					}
				}
			}
		}
	}
	avail := projectreport.Available(first, end, holidays)
	for uid, x := range people {
		util, err := projectreport.Utilisation(x.billable, avail, base)
		if err != nil {
			return body, err
		}
		row := ProjectReportPerson{UserID: uid.String(), PersonName: names[uid], Minutes: x.minutes, Billable: x.billable, Available: avail, Utilisation: util}
		if tier == projectrate.Payroll && !x.cost.IsZero() {
			row.Cost = x.cost.String()
		}
		body.People = append(body.People, row)
	}
	slices.SortFunc(body.People, func(a, b ProjectReportPerson) int {
		return strings.Compare(a.PersonName+a.UserID, b.PersonName+b.UserID)
	})
	slices.SortFunc(body.Projects, func(a, b ProjectReportRow) int { return strings.Compare(a.Code, b.Code) })
	if seesAllProjects(p) {
		body.Untagged = &ProjectReportRow{Code: "", Name: "Tagged with no project", Revenue: untagged.Revenue.String(), DirectCosts: untagged.DirectCosts.String(), PayrollTagged: untagged.PayrollTagged.String(), Basis: "none"}
	}
	return body, nil
}

// billingOn is a cost line's project billing, as the checks read it.
type billingOn struct {
	Values   []uuid.UUID
	Billable bool
	Markup   string
}

// checkBillableLines refuses a bill's line that is billable or marked up without a project of the organisation among its dimension values,
// or with a markup Fyniq cannot read (FR-PRJ-08).
func (a *API) checkBillableLines(ctx context.Context, u storage.UnitOfWork, lines []bill.Line) error {
	on := make([]billingOn, len(lines))
	for i, l := range lines {
		on[i] = billingOn{l.Values, l.Billable, l.Markup}
	}
	return checkBilling(ctx, u, on)
}

// checkBillableClaimLines is the same for an expense claim's lines, receipts and trips alike.
func (a *API) checkBillableClaimLines(ctx context.Context, u storage.UnitOfWork, lines []claim.Line) error {
	on := make([]billingOn, len(lines))
	for i, l := range lines {
		on[i] = billingOn{l.Values, l.Billable, l.Markup}
	}
	return checkBilling(ctx, u, on)
}

func checkBilling(ctx context.Context, u storage.UnitOfWork, lines []billingOn) error {
	var projects map[uuid.UUID]bool
	var bad []FieldProblem
	for i, l := range lines {
		if !l.Billable && l.Markup == "" {
			continue
		}
		if projects == nil {
			ps, err := u.Projects().List(ctx)
			if err != nil {
				return err
			}
			projects = map[uuid.UUID]bool{}
			for _, p := range ps {
				projects[p.ID] = p.Status != storage.ProjectClosed
			}
		}
		has := false
		for _, v := range l.Values {
			has = has || projects[v]
		}
		if p := project.CheckBillableLine("body.lines["+strconv.Itoa(i)+"]", l.Billable, l.Markup, has); p != nil {
			bad = append(bad, FieldProblem{Code: p.Code, Field: p.Field, Message: p.Message, Fix: p.Fix})
		}
	}
	if len(bad) > 0 {
		return invalid(bad)
	}
	return nil
}

// checkOrderProject refuses a purchase order for a project that is not one of the organisation's, or is closed, or while the
// feature is off (FR-PRJ-08).
func (a *API) checkOrderProject(ctx context.Context, u storage.UnitOfWork, d purchaseorder.Draft) error {
	if d.ProjectID == uuid.Nil {
		return nil
	}
	if err := requireProjects(ctx, a, u); err != nil {
		return err
	}
	p, err := u.Projects().Get(ctx, d.ProjectID)
	if err != nil || p.Status == storage.ProjectClosed {
		return invalid([]FieldProblem{{Code: project.CodeLine, Field: "body.project_id", Message: "is not a project of this organisation that is open",
			Fix: "Choose a project that is planned, active or on hold"}})
	}
	return nil
}
