package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/project"
	"github.com/cognizhi/fyniq/internal/domain/projectbilling"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ billing a project's work (FR-PRJ-10; p1-projects task 5.2)

// UnbilledTime is an approved, billable, unbilled time entry and its value at the billing rate of its day.
type UnbilledTime struct {
	EntryID    string `json:"entry_id" format:"uuid" doc:"The time entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e71"`
	UserID     string `json:"user_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	PersonName string `json:"person_name" doc:"Their name." example:"Priya Nair"`
	WorkDate   string `json:"work_date" format:"date" doc:"The day worked." example:"2026-10-13"`
	Minutes    int    `json:"minutes" doc:"Net minutes; a reversing entry is negative." example:"150"`
	Rate       string `json:"rate,omitempty" doc:"The billing rate an hour on that day, in the project's currency; absent when no billing rate covers the day, and then the entry cannot be invoiced." example:"150.00"`
	Value      string `json:"value,omitempty" doc:"RND-TIME of the minutes at the rate, per entry." example:"375.00"`
}

// UnbilledExpense is a posted billable cost of a bill or a claim, and what it is billed at.
type UnbilledExpense struct {
	DocumentID    string `json:"document_id" format:"uuid" doc:"The posted bill or claim." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e81"`
	Line          int    `json:"line" minimum:"0" doc:"The line's place in its document, from 0." example:"0"`
	Source        string `json:"source" enum:"bill,claim" doc:"What the cost is on." example:"bill"`
	Number        string `json:"number,omitempty" doc:"The document's number." example:"BILL-0007"`
	Date          string `json:"date,omitempty" format:"date" doc:"The cost's date." example:"2026-10-14"`
	Label         string `json:"label,omitempty" doc:"What it is." example:"Venue hire"`
	Cost          Money  `json:"cost" doc:"The cost before GST, in the document's currency."`
	MarkupPercent string `json:"markup_percent,omitempty" doc:"The markup on it." example:"10"`
	Markup        Money  `json:"markup" doc:"RND-MARKUP of the cost at the markup percentage."`
	Billable      Money  `json:"billable" doc:"The cost plus the markup: what is billed."`
}

// ProjectUnbilled is what a project can bill now.
type ProjectUnbilled struct {
	ProjectID     string            `json:"project_id" format:"uuid" doc:"The project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	BillingMethod string            `json:"billing_method" enum:"time_and_materials,fixed_fee,retainer,internal" doc:"How the project bills: only a time-and-materials project bills its time; a fixed fee bills by milestone, a retainer draws time down, and an internal project bills nothing." example:"time_and_materials"`
	Currency      string            `json:"currency" doc:"The project's currency." example:"SGD"`
	HasCustomer   bool              `json:"has_customer" doc:"false when the project has no customer, so no invoice can be drafted." example:"true"`
	Time          []UnbilledTime    `json:"time" nullable:"false" doc:"Priced approved billable time not yet on a posted invoice, by day."`
	Unpriced      []UnbilledTime    `json:"unpriced_time" nullable:"false" doc:"Approved billable time that no billing rate covers: enter a rate from the day (setBillingRate) to bill it."`
	Expenses      []UnbilledExpense `json:"expenses" nullable:"false" doc:"Posted billable costs in the project's currency, at cost plus markup."`
	OtherCurrency []UnbilledExpense `json:"expenses_in_other_currencies" nullable:"false" doc:"Billable costs in another currency: they are not on the project's invoice."`
	TimeValue     Money             `json:"time_value" doc:"The priced time's value."`
	ExpenseValue  Money             `json:"expense_value" doc:"The expenses' billable total."`
	Total         Money             `json:"total" doc:"What an invoice of it all comes to before GST."`
}

type projectUnbilledOutput struct{ Body ProjectUnbilled }

// DraftProjectInvoiceBody is what the AR specialist chooses: the revenue account and the GST code the invoice's lines carry.
type DraftProjectInvoiceBody struct {
	ProjectID        string                `json:"project_id" format:"uuid" doc:"The project to bill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	RevenueAccountID string                `json:"revenue_account_id" format:"uuid" doc:"The revenue account the invoice's lines earn." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d62"`
	GSTCode          string                `json:"gst_code,omitempty" pattern:"^[A-Z]{2,4}$" doc:"The GST code of the invoice's lines. Left out, SR, standard-rated; choose OS or ZR where it applies [verify with the tax agent]." example:"SR"`
	Date             string                `json:"date,omitempty" format:"date" doc:"The invoice's date. Left out, the organisation's today." example:"2026-10-31"`
	TimeEntryIDs     []TimeEntryRef        `json:"time_entry_ids,omitempty" maxItems:"5000" doc:"The priced time entries to bill. Left out, all of them."`
	Expenses         []DraftProjectExpense `json:"expenses,omitempty" maxItems:"2000" doc:"The costs to bill. Left out, all of those in the project's currency."`
}

// TimeEntryRef is a time entry named by its ID.
type TimeEntryRef string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (TimeEntryRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A time entry's ID (a row of getProjectUnbilled).", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e71"}}
}

// DraftProjectExpense names one cost to bill.
type DraftProjectExpense struct {
	DocumentID string `json:"document_id" format:"uuid" doc:"The posted bill or claim." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e81"`
	Line       int    `json:"line" minimum:"0" doc:"The line's place in its document, from 0." example:"0"`
}

type draftProjectInvoiceInput struct {
	KeyHeader
	PreferHeader
	Body DraftProjectInvoiceBody
}

func unbilledTimeOut(t projectbilling.Time, names map[uuid.UUID]string) UnbilledTime {
	o := UnbilledTime{EntryID: t.EntryID.String(), UserID: t.UserID.String(), PersonName: names[t.UserID], WorkDate: t.Date.Format(time.DateOnly), Minutes: t.Minutes}
	if t.Rate != nil {
		o.Rate = t.Rate.String()
	}
	if t.Value != nil {
		o.Value = t.Value.String()
	}
	return o
}

func unbilledExpenseOut(x projectbilling.Expense) UnbilledExpense {
	o := UnbilledExpense{DocumentID: x.DocumentID.String(), Line: x.Line, Source: x.Source, Number: x.Number, Label: x.Label, Cost: moneyOut(x.Cost),
		MarkupPercent: x.MarkupPercent, Markup: moneyOut(x.Markup), Billable: moneyOut(x.Billable)}
	if !x.Date.IsZero() {
		o.Date = x.Date.Format(time.DateOnly)
	}
	return o
}

func registerProjectBilling(a *API) {
	register(a, authz.GetProjectUnbilled, huma.Operation{
		OperationID: "getProjectUnbilled", Method: http.MethodGet, Path: "/v1/projects/{project_id}/unbilled", Tags: []string{"Projects"},
		Summary: "What a project can bill now",
		Description: "Approved, billable time not yet on a posted invoice, valued at the billing rate of each day (RND-TIME per entry), and posted billable " +
			"costs of its bills and claims at cost plus markup (RND-MARKUP), with the totals (FR-PRJ-10). Only a time-and-materials project bills its " +
			"time: a fixed fee bills by milestone, a retainer draws time down, and an internal project bills nothing. Time without a billing rate is " +
			"listed apart. Hours and billing rates only, never a cost rate. A person who reads only the projects they manage gets 404 for another. " +
			"Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeProjectsOff}, func(ctx context.Context, in *ProjectPathInput) (*projectUnbilledOutput, error) {
		id, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := projectUnbilledOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			pr, err := a.visibleProject(ctx, u, p, id)
			if err != nil {
				return err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			un, err := projectbilling.Of(ctx, u, pr, base)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			out.Body = ProjectUnbilled{ProjectID: pr.ID.String(), BillingMethod: pr.BillingMethod, Currency: pr.Currency, HasCustomer: pr.CustomerID != nil,
				Time: []UnbilledTime{}, Unpriced: []UnbilledTime{}, Expenses: []UnbilledExpense{}, OtherCurrency: []UnbilledExpense{},
				TimeValue: moneyOut(un.TimeValue), ExpenseValue: moneyOut(un.ExpenseValue), Total: moneyOut(un.Total)}
			for _, t := range un.Time {
				out.Body.Time = append(out.Body.Time, unbilledTimeOut(t, names))
			}
			for _, t := range un.Unpriced {
				out.Body.Unpriced = append(out.Body.Unpriced, unbilledTimeOut(t, names))
			}
			for _, x := range un.Expenses {
				out.Body.Expenses = append(out.Body.Expenses, unbilledExpenseOut(x))
			}
			for _, x := range un.OtherCurrency {
				out.Body.OtherCurrency = append(out.Body.OtherCurrency, unbilledExpenseOut(x))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.DraftProjectInvoice, huma.Operation{
		OperationID: "draftProjectInvoice", Method: http.MethodPost, Path: "/v1/project-invoice-drafts", Tags: []string{"Projects"}, DefaultStatus: http.StatusCreated,
		Summary: "Draft an invoice from a project's unbilled work",
		Description: "Makes a draft invoice to the project's customer through the ordinary invoice command (createInvoice): one line a person and rate for the time, " +
			"one line a cost for the costs at cost plus markup, each carrying the project, on the revenue account and GST code given (FR-PRJ-10). The draft " +
			"remembers what it bills: posting the invoice marks those time entries and cost lines billed, and voiding it releases them, so a piece of work " +
			"is billed once. It needs the right to create invoices and to see project reports. Refused with PRJ-12 for an internal project, PRJ-13 when " +
			"there is nothing to bill or the project has no customer, and PRJ-09 while Projects and time are switched off. Side effects: the draft invoice " +
			"and its audit event; nothing posts until the invoice is issued. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeInvalid, CodeProjectsOff, project.CodeLine, projectbilling.CodeInternal, projectbilling.CodeNothing, projectbilling.CodeAccount,
		CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeBilled,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency}, InvoiceCodes...),
		func(ctx context.Context, in *draftProjectInvoiceInput) (*invoiceOutput, error) {
			id, err := parseID("body.project_id", in.Body.ProjectID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Invoice, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return Invoice{}, err
				}
				pr, err := a.visibleProject(ctx, u, p, id)
				if err != nil {
					return Invoice{}, err
				}
				v, err := a.invoiceView(ctx, u)
				if err != nil {
					return Invoice{}, err
				}
				d, err := a.projectInvoiceDraft(ctx, u, pr, v.base, in.Body)
				if err != nil {
					return Invoice{}, err
				}
				doc, err := a.invoices.Create(ctx, p.Organisation, d)
				if err != nil {
					return Invoice{}, err
				}
				return a.invoiceOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &invoiceOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &invoiceOutput{Status: w.Status, ETag: invoiceETag(w.Body), Location: "/v1/invoices/" + w.Body.ID, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}

// ProjectPathInput is a project's path alone.
type ProjectPathInput struct{ ProjectPath }

// visibleProject is the project the caller may read, ports.ErrNotFound for one they may not (a manager reads only theirs).
func (a *API) visibleProject(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Project, error) {
	visible, err := a.visibleProjects(ctx, u, p)
	if err != nil {
		return storage.Project{}, err
	}
	if !visible(id) {
		return storage.Project{}, ports.ErrNotFound
	}
	pr, err := u.Projects().Get(ctx, id)
	if err != nil {
		return storage.Project{}, err
	}
	if !pr.Configured {
		return storage.Project{}, ports.ErrNotFound
	}
	return pr, nil
}

// projectInvoiceDraft is the draft invoice of a project's unbilled work: time a person and a rate a line, a cost a line, each carrying the
// project, and what it bills for the database to mark when the invoice is posted.
func (a *API) projectInvoiceDraft(ctx context.Context, u storage.UnitOfWork, pr storage.Project, base money.Currency, in DraftProjectInvoiceBody) (invoice.Draft, error) {
	if pr.BillingMethod == storage.BillingInternal {
		return invoice.Draft{}, problem(projectbilling.CodeInternal, "An internal project is never billed.", "Bill a customer's project instead")
	}
	if pr.CustomerID == nil {
		return invoice.Draft{}, problem(projectbilling.CodeNothing, "The project has no customer to bill.", "Set the project's customer first")
	}
	account, err := uuid.Parse(in.RevenueAccountID)
	if err != nil {
		return invoice.Draft{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.revenue_account_id", Message: "is not an ID", Fix: "Send the revenue account's ID"}})
	}
	code := in.GSTCode
	if code == "" {
		code = "SR"
	}
	un, err := projectbilling.Of(ctx, u, pr, base)
	if err != nil {
		return invoice.Draft{}, err
	}
	cur := un.Currency
	day, err := orgToday(ctx, a, u)
	if err != nil {
		return invoice.Draft{}, err
	}
	if in.Date != "" {
		if day, err = time.Parse(time.DateOnly, in.Date); err != nil {
			return invoice.Draft{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
	}
	// The entries and costs to bill: those asked for, or all that can be.
	wantTime, wantExp := map[uuid.UUID]bool{}, map[string]bool{}
	for i, s := range in.TimeEntryIDs {
		t, err := uuid.Parse(string(s))
		if err != nil {
			return invoice.Draft{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.time_entry_ids[" + strconv.Itoa(i) + "]", Message: "is not an ID", Fix: "Send the time entry's ID"}})
		}
		wantTime[t] = true
	}
	for i, x := range in.Expenses {
		t, err := uuid.Parse(x.DocumentID)
		if err != nil {
			return invoice.Draft{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.expenses[" + strconv.Itoa(i) + "].document_id", Message: "is not an ID", Fix: "Send the bill's or claim's ID"}})
		}
		wantExp[fmt.Sprintf("%s/%d", t, x.Line)] = true
	}
	names, err := memberNames(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return invoice.Draft{}, err
	}
	one, err := money.ParseQuantity("1", "quantity")
	if err != nil {
		return invoice.Draft{}, err
	}
	d := invoice.Draft{CustomerID: *pr.CustomerID, Date: day, Currency: cur, Reference: pr.Code}
	billing := &invoice.ProjectBilling{ProjectID: pr.ID}
	line := func(desc string, amount money.Money) error {
		price, err := money.ParseUnitPrice(amount.String(), cur, "unit_price")
		if err != nil {
			return err
		}
		d.Lines = append(d.Lines, invoice.Line{Description: desc, Quantity: one, UnitPrice: price, AccountID: account, GSTCode: code, Values: []uuid.UUID{pr.ID}})
		return nil
	}
	// Time: a line a person and a rate, in date order, its amount the sum of its entries' RND-TIME values.
	type group struct {
		user    uuid.UUID
		rate    string
		minutes int
		from    time.Time
		to      time.Time
		value   money.Money
	}
	var groups []*group
	for _, t := range un.Time {
		if len(wantTime) > 0 && !wantTime[t.EntryID] {
			continue
		}
		var g *group
		for _, x := range groups {
			if x.user == t.UserID && x.rate == t.Rate.String() {
				g = x
			}
		}
		if g == nil {
			g = &group{user: t.UserID, rate: t.Rate.String(), from: t.Date, to: t.Date, value: money.Zero(cur)}
			groups = append(groups, g)
		}
		g.minutes += t.Minutes
		g.to = t.Date
		if g.value, err = g.value.Add(*t.Value); err != nil {
			return d, err
		}
		billing.TimeEntries = append(billing.TimeEntries, t.EntryID)
	}
	for _, g := range groups {
		desc := fmt.Sprintf("%s, %s to %s: %s at %s %s an hour", names[g.user], g.from.Format(time.DateOnly), g.to.Format(time.DateOnly), hoursText(g.minutes), g.rate, cur.Code)
		if err := line(desc, g.value); err != nil {
			return d, err
		}
	}
	for _, x := range un.Expenses {
		if len(wantExp) > 0 && !wantExp[fmt.Sprintf("%s/%d", x.DocumentID, x.Line)] {
			continue
		}
		desc := x.Label
		if x.MarkupPercent != "" && x.MarkupPercent != "0" {
			desc += fmt.Sprintf(" (cost plus %s%% markup)", x.MarkupPercent)
		}
		if err := line(strings.TrimSpace(desc), x.Billable); err != nil {
			return d, err
		}
		billing.Expenses = append(billing.Expenses, invoice.BilledExpense{DocumentID: x.DocumentID, Line: x.Line})
	}
	if len(d.Lines) == 0 {
		return d, problem(projectbilling.CodeNothing, "Nothing is ready to bill on the project.", "Approve its time, enter a billing rate for it, or mark costs billable")
	}
	slices.SortStableFunc(billing.TimeEntries, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	d.ProjectBilling = billing
	return d, nil
}

// hoursText is minutes as "9 h 30 min".
func hoursText(minutes int) string {
	neg := minutes < 0
	if neg {
		minutes = -minutes
	}
	s := fmt.Sprintf("%d h %02d min", minutes/60, minutes%60)
	if neg {
		return "-" + s
	}
	return s
}
