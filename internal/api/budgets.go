package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// ------------------------------------------------------------------ budgets (FR-PLN-01, C.1 #35)
//
// A budget is a financial year's monthly amounts by income and expense account and dimension value, in versions
// (original, revised; p1-planning design D6). It is drafted by whoever prepares budgets, copied with growth or imported
// from XLSX, and approved by a person who holds A on "Budgets, forecasts", never the one who asked: approving it records
// and locks it, and the year's budget is its latest recorded version. Nothing posts.

// BudgetCodes are the codes a budget answers besides the common ones.
var BudgetCodes = []string{budget.CodeBudget, budget.CodeRecorded, storage.CodeBudget}

// BudgetTotals are a budget's totals, month by month and for the year, as the server adds them (RND-DISPLAY: the web
// app never adds amounts itself).
type BudgetTotals struct {
	Income     []BudgetAmount `json:"income" nullable:"false" doc:"The income lines' total, month by month."`
	Costs      []BudgetAmount `json:"costs" nullable:"false" doc:"The expense lines' total, month by month."`
	Net        []BudgetAmount `json:"net" nullable:"false" doc:"Income less costs, month by month."`
	IncomeYear BudgetAmount   `json:"income_year" doc:"The year's income."`
	CostsYear  BudgetAmount   `json:"costs_year" doc:"The year's costs."`
	NetYear    BudgetAmount   `json:"net_year" doc:"The year's income less its costs."`
}

// BudgetAmount is an amount of a budget, a decimal string in the base currency.
type BudgetAmount string

// Schema implements huma.SchemaProvider.
func (BudgetAmount) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An amount in the base currency, as a decimal string.", Examples: []any{"12500.00"}}
}

// BudgetMonth is a month of a budget's year, YYYY-MM.
type BudgetMonth string

// Schema implements huma.SchemaProvider.
func (BudgetMonth) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A month of the financial year, YYYY-MM.", Examples: []any{"2027-01"}}
}

// DimensionValueID is a dimension value's ID.
type DimensionValueID string

// Schema implements huma.SchemaProvider.
func (DimensionValueID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A value of one of the organisation's dimensions.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c40"}}
}

// BudgetDraft is a budget version as the API takes and gives it.
type BudgetDraft struct {
	Name    string            `json:"name" minLength:"1" maxLength:"80" doc:"The version's name." example:"FY2027 budget"`
	Kind    string            `json:"kind" enum:"original,revised" doc:"original: the year's first budget; revised: a later version that revises the year's recorded one." example:"original"`
	Year    string            `json:"financial_year" format:"date" doc:"The financial year's first day." example:"2027-01-01"`
	Revises string            `json:"revises,omitempty" format:"uuid" doc:"The recorded budget a revised version revises." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	Lines   []BudgetLineDraft `json:"lines" maxItems:"5000" nullable:"false" doc:"The amounts by account and dimension values: one line per account and set of values."`
}

// BudgetLineDraft is one account's and set of dimension values' amounts, month by month.
type BudgetLineDraft struct {
	AccountID  string             `json:"account_id" format:"uuid" doc:"A postable income or expense account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b20"`
	Dimensions []DimensionValueID `json:"dimension_value_ids,omitempty" doc:"The values of the organisation's dimensions the line is for, at most one per dimension; none for the whole company."`
	Amounts    []BudgetAmount     `json:"amounts" minItems:"1" maxItems:"24" doc:"One amount for each month of the year, in order, signed as the P&L shows it: income and costs above zero for what is expected."`
}

func budgetIn(d BudgetDraft) budget.Budget {
	b := budget.Budget{Name: d.Name, Kind: d.Kind, Year: d.Year, Revises: d.Revises, Lines: []budget.BudgetLine{}}
	for _, l := range d.Lines {
		x := budget.BudgetLine{AccountID: l.AccountID, Dimensions: []string{}, Amounts: []string{}}
		for _, v := range l.Dimensions {
			x.Dimensions = append(x.Dimensions, string(v))
		}
		for _, a := range l.Amounts {
			x.Amounts = append(x.Amounts, string(a))
		}
		b.Lines = append(b.Lines, x)
	}
	return b
}

func budgetOut(b budget.Budget) BudgetDraft {
	d := BudgetDraft{Name: b.Name, Kind: b.Kind, Year: b.Year, Revises: b.Revises, Lines: []BudgetLineDraft{}}
	for _, l := range b.Lines {
		x := BudgetLineDraft{AccountID: l.AccountID, Amounts: []BudgetAmount{}}
		for _, v := range l.Dimensions {
			x.Dimensions = append(x.Dimensions, DimensionValueID(v))
		}
		for _, a := range l.Amounts {
			x.Amounts = append(x.Amounts, BudgetAmount(a))
		}
		d.Lines = append(d.Lines, x)
	}
	return d
}

// BudgetView is a budget version, drafted or recorded, with its totals.
type BudgetView struct {
	ID         string         `json:"id" format:"uuid" doc:"The budget's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	Status     string         `json:"status" enum:"draft,awaiting_approval,recorded,superseded" doc:"draft: being prepared; awaiting_approval: asked for approval; recorded: approved and locked, the year's budget; superseded: a later version of the year is recorded." example:"recorded"`
	Version    int            `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Currency   string         `json:"currency" doc:"The base currency of its amounts." example:"SGD"`
	Budget     BudgetDraft    `json:"budget" doc:"Its name, kind, year and lines."`
	Months     []BudgetMonth  `json:"months" nullable:"false" doc:"The year's months, in the order of each line's amounts."`
	LineTotals []BudgetAmount `json:"line_totals" nullable:"false" doc:"Each line's total for the year, in the order of its lines."`
	Totals     BudgetTotals   `json:"totals" doc:"Its totals."`
	ApprovalID string         `json:"approval_id,omitempty" format:"uuid" doc:"The approval request while one waits." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a80"`
	RecordedBy *Person        `json:"recorded_by,omitempty" doc:"Who asked for its approval, once recorded."`
	RecordedAt *time.Time     `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-11-20T09:30:00Z"`
}

// BudgetPage is a page of budgets: each year's latest first.
type BudgetPage struct {
	Items      []BudgetView `json:"items" nullable:"false"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// BudgetCopy is a copy of a budget with growth.
type BudgetCopy struct {
	From   string `json:"from" format:"uuid" doc:"The budget copied: any version." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	Name   string `json:"name" minLength:"1" maxLength:"80" doc:"The new version's name." example:"FY2028 budget"`
	Kind   string `json:"kind" enum:"original,revised" doc:"original for another year; revised for the same year, revising the budget copied, which must be recorded." example:"original"`
	Year   string `json:"financial_year" format:"date" doc:"The financial year's first day: the same year, or one with as many months." example:"2028-01-01"`
	Growth string `json:"growth" doc:"Each amount × (1 + growth), rounded once (RND-PLAN), as a fraction: 0.05 for 5%, -0.1 for 10% less." example:"0.05"`
}

// BudgetUpload is a budget imported from XLSX.
type BudgetUpload struct {
	File    huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"The budget: an .xlsx workbook, up to 10 MB, whose first sheet has a header row of Account, a column per dimension by its name and a column per month of the year (YYYY-MM), then a row per line: the account's code, each dimension's value code and the month's amounts. It is checked and scanned for malware as evidence is."`
	Name    string        `form:"name" required:"true" minLength:"1" maxLength:"80" doc:"The new version's name." example:"FY2027 budget"`
	Kind    string        `form:"kind" required:"true" enum:"original,revised" doc:"original or revised." example:"original"`
	Year    string        `form:"financial_year" required:"true" format:"date" doc:"The financial year's first day." example:"2027-01-01"`
	Revises string        `form:"revises" required:"false" format:"uuid" doc:"The recorded budget a revised version revises." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
}

type (
	BudgetPath struct {
		BudgetID string `path:"budget_id" format:"uuid" doc:"The budget." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	}
	budgetListInput struct {
		PageQuery
		Year string `query:"financial_year" format:"date" doc:"Only one financial year's budgets, by its first day." example:"2027-01-01"`
	}
	budgetListOutput struct{ Body BudgetPage }
	budgetDraftInput struct {
		KeyHeader
		PreferHeader
		Body BudgetDraft
	}
	budgetReviseInput struct {
		BudgetPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body BudgetDraft
	}
	budgetCopyInput struct {
		KeyHeader
		PreferHeader
		Body BudgetCopy
	}
	budgetImportInput struct {
		KeyHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[BudgetUpload]
	}
	budgetAskInput struct {
		BudgetPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	budgetReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body BudgetView
	}
	budgetWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     BudgetView
	}
)

// budgetSums adds a budget's amounts, exactly; they are at the base currency's minor units already.
func budgetSums(b budget.Budget, accounts map[uuid.UUID]storage.Account, months int) (lines []BudgetAmount, t BudgetTotals, err error) {
	c := apd.BaseContext.WithPrecision(60)
	income, costs := make([]apd.Decimal, months), make([]apd.Decimal, months)
	var incomeYear, costsYear apd.Decimal
	for _, l := range b.Lines {
		var total apd.Decimal
		id, _ := uuid.Parse(l.AccountID)
		isIncome := accounts[id].Type == storage.Revenue
		for k, a := range l.Amounts {
			x, _, perr := apd.NewFromString(a)
			if perr != nil || k >= months {
				return nil, t, fmt.Errorf("budget amount %q: %v", a, perr)
			}
			c.Add(&total, &total, x)
			if isIncome {
				c.Add(&income[k], &income[k], x)
				c.Add(&incomeYear, &incomeYear, x)
			} else {
				c.Add(&costs[k], &costs[k], x)
				c.Add(&costsYear, &costsYear, x)
			}
		}
		lines = append(lines, BudgetAmount(total.Text('f')))
	}
	t = BudgetTotals{Income: []BudgetAmount{}, Costs: []BudgetAmount{}, Net: []BudgetAmount{}}
	for k := range months {
		var net apd.Decimal
		c.Sub(&net, &income[k], &costs[k])
		t.Income = append(t.Income, BudgetAmount(income[k].Text('f')))
		t.Costs = append(t.Costs, BudgetAmount(costs[k].Text('f')))
		t.Net = append(t.Net, BudgetAmount(net.Text('f')))
	}
	var net apd.Decimal
	c.Sub(&net, &incomeYear, &costsYear)
	t.IncomeYear, t.CostsYear, t.NetYear = BudgetAmount(incomeYear.Text('f')), BudgetAmount(costsYear.Text('f')), BudgetAmount(net.Text('f'))
	if lines == nil {
		lines = []BudgetAmount{}
	}
	return lines, t, nil
}

// budgetOf is a budget document as the API shows it. latest maps each year to its recorded budget's ID; asked the
// budgets waiting for approval.
func (a *API) budgetOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, latest map[string]uuid.UUID, asked map[string]string) (BudgetView, error) {
	b, err := budget.Read(doc)
	if err != nil {
		return BudgetView{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return BudgetView{}, err
	}
	out := BudgetView{ID: doc.ID.String(), Status: "draft", Version: doc.Version, Currency: o.BaseCurrency, Budget: budgetOut(b), Months: []BudgetMonth{}}
	months := 0
	if cal, err := period.CalendarOf(o.Profile); err == nil {
		if y, ok := budget.YearOf(cal, b.Year); ok {
			for _, m := range y.Months {
				out.Months = append(out.Months, BudgetMonth(m.StartsOn.Format("2006-01")))
			}
			months = len(y.Months)
		}
	}
	chart, err := budget.ChartIn(ctx, u)
	if err != nil {
		return BudgetView{}, err
	}
	if out.LineTotals, out.Totals, err = budgetSums(b, chart.Accounts, months); err != nil {
		return BudgetView{}, err
	}
	switch {
	case doc.Status == storage.DocumentPosted:
		out.Status = "superseded"
		if latest[b.Year] == doc.ID {
			out.Status = "recorded"
		}
		out.RecordedAt = doc.PostedAt
	case asked[doc.ID.String()] != "":
		out.Status, out.ApprovalID = "awaiting_approval", asked[doc.ID.String()]
	}
	return out, nil
}

// latestBudgets maps each year to its latest recorded budget.
func latestBudgets(ctx context.Context, u storage.UnitOfWork) (map[string]uuid.UUID, error) {
	docs, err := budget.Recorded(ctx, u)
	if err != nil {
		return nil, err
	}
	out := map[string]uuid.UUID{}
	for _, doc := range docs {
		b, err := budget.Read(doc)
		if err != nil {
			return nil, err
		}
		if _, ok := out[b.Year]; !ok {
			out[b.Year] = doc.ID
		}
	}
	return out, nil
}

// shownBudget is a budget by ID: not found for any other document.
func (a *API) shownBudget(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (BudgetView, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return BudgetView{}, err
	}
	if doc.Type != storage.DocumentBudget {
		return BudgetView{}, ports.ErrNotFound
	}
	latest, err := latestBudgets(ctx, u)
	if err != nil {
		return BudgetView{}, err
	}
	asked, err := a.waiting(ctx, u, approval.ObjectBudget)
	if err != nil {
		return BudgetView{}, err
	}
	return a.budgetOf(ctx, u, doc, latest, asked)
}

func budgetStatus(v BudgetView) string {
	if v.Status == "recorded" || v.Status == "superseded" {
		return "posted"
	}
	return "draft"
}

func budgetAnswer(w written[BudgetView]) *budgetWriteOutput {
	if w.Previewed {
		return &budgetWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &budgetWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, budgetStatus(w.Body)), Location: "/v1/budgets/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// budgetDependencies are a budget's envelope values: its status and its year.
func budgetDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	b, err := budget.Read(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "financial_year": b.Year}, nil
}

// budgetSheetProblems turns a sheet that cannot be read into the import's refusal (BUDGET-12).
func budgetSheetProblems(err error) error {
	var re *sheet.ReadError
	if errors.As(err, &re) {
		field := "body.file"
		if re.Row > 0 {
			field = fmt.Sprintf("body.file (row %d)", re.Row)
		}
		return invalid([]FieldProblem{{Code: budget.CodeImport, Field: field, Message: re.Message, Fix: re.Fix}})
	}
	var lp ledger.Problems
	if errors.As(err, &lp) {
		fields := make([]FieldProblem, 0, len(lp))
		for _, x := range lp {
			field := "body." + x.Field
			if x.Code == budget.CodeImport {
				field = "body.file (" + x.Field + ")"
			}
			fields = append(fields, FieldProblem{Code: x.Code, Field: field, Message: x.Message, Fix: x.Fix})
		}
		return invalid(fields)
	}
	return err
}

func registerBudgets(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListBudgets, huma.Operation{
		OperationID: "listBudgets", Method: http.MethodGet, Path: "/v1/budgets", Tags: []string{"Planning"},
		Summary:     "List the budgets",
		Description: "Every budget version, the latest first, with its status and totals, a page at a time; financial_year keeps one year's (FR-PLN-01). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *budgetListInput) (*budgetListOutput, error) {
		p, _ := authn.From(ctx)
		out := budgetListOutput{Body: BudgetPage{Items: []BudgetView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			latest, err := latestBudgets(ctx, u)
			if err != nil {
				return err
			}
			asked, err := a.waiting(ctx, u, approval.ObjectBudget)
			if err != nil {
				return err
			}
			var all []BudgetView
			for _, status := range []storage.DocumentStatus{storage.DocumentPosted, storage.DocumentDraft} {
				f := storage.DocumentFilter{Type: storage.DocumentBudget, Status: status, Limit: 200}
				for {
					page, err := u.Documents().Page(ctx, f)
					if err != nil {
						return err
					}
					for _, doc := range page {
						v, err := a.budgetOf(ctx, u, doc, latest, asked)
						if err != nil {
							return err
						}
						if in.Year == "" || v.Budget.Year == in.Year {
							all = append(all, v)
						}
					}
					if len(page) < f.Limit {
						break
					}
					f.AfterDate, f.AfterID = page[len(page)-1].Date, &page[len(page)-1].ID
				}
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x BudgetView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []BudgetView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateBudget, huma.Operation{
		OperationID: "createBudget", Method: http.MethodPost, Path: "/v1/budgets", Tags: []string{"Planning"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a budget",
		Description: "Drafts a budget version for a financial year (FR-PLN-01, design D6): monthly amounts by postable income and expense " +
			"account and dimension value, one amount for each month of the year, signed as the P&L shows it. Ask for its approval with " +
			"requestBudgetApproval: approved, it is recorded and locked. It posts nothing. Side effects: the draft, audit events. Every " +
			"problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), BudgetCodes...), func(ctx context.Context, in *budgetDraftInput) (*budgetWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BudgetView, error) {
			doc, err := budget.DraftIn(ctx, u, budgetIn(in.Body))
			if err != nil {
				return BudgetView{}, equityProblems(err)
			}
			return a.shownBudget(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return budgetAnswer(w), nil
	})

	register(a, authz.GetBudget, huma.Operation{
		OperationID: "getBudget", Method: http.MethodGet, Path: "/v1/budgets/{budget_id}", Tags: []string{"Planning"},
		Summary:     "Get a budget",
		Description: "A budget version with its status, its year's months, each line's total and the totals (FR-PLN-01). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *BudgetPath) (*budgetReadOutput, error) {
		id, err := parseID("budget_id", in.BudgetID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out budgetReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.shownBudget(ctx, u, id)
			out.Body, out.ETag = v, etag(v.Version, budgetStatus(v))
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceBudget, huma.Operation{
		OperationID: "replaceBudget", Method: http.MethodPut, Path: "/v1/budgets/{budget_id}", Tags: []string{"Planning"},
		Summary: "Change a drafted budget",
		Description: "Saves a new version of a budget not recorded yet (FR-PLN-01); a recorded one is locked (BUDGET-11): copy it into a " +
			"revised version instead. A change while its approval waits makes that approval stale. Side effects: the new version, " +
			"audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), BudgetCodes...),
		func(ctx context.Context, in *budgetReviseInput) (*budgetWriteOutput, error) {
			id, err := parseID("budget_id", in.BudgetID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BudgetView, error) {
				doc, err := budget.Drafted(ctx, u, id)
				if err != nil {
					return BudgetView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return BudgetView{}, err
				}
				if _, err := budget.ReviseIn(ctx, u, id, budgetIn(in.Body)); err != nil {
					return BudgetView{}, equityProblems(err)
				}
				return a.shownBudget(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return budgetAnswer(w), nil
		})

	register(a, authz.CopyBudget, huma.Operation{
		OperationID: "copyBudget", Method: http.MethodPost, Path: "/v1/budgets:copy", Tags: []string{"Planning"},
		DefaultStatus: http.StatusCreated, Summary: "Copy a budget with growth",
		Description: "Drafts a new budget version from another (FR-PLN-01): each amount × (1 + growth), rounded once (RND-PLAN), month by " +
			"month in order, for the same year as a revised version or another year of as many months as an original. Side effects: " +
			"the draft, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), BudgetCodes...), func(ctx context.Context, in *budgetCopyInput) (*budgetWriteOutput, error) {
		from, err := parseID("body.from", in.Body.From)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BudgetView, error) {
			src, err := u.Documents().Get(ctx, from)
			if err != nil {
				return BudgetView{}, err
			}
			if src.Type != storage.DocumentBudget {
				return BudgetView{}, ports.ErrNotFound
			}
			b, err := budget.Read(src)
			if err != nil {
				return BudgetView{}, err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return BudgetView{}, err
			}
			cur, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return BudgetView{}, err
			}
			cal, err := period.CalendarOf(o.Profile)
			if err != nil {
				return BudgetView{}, err
			}
			y, ok := budget.YearOf(cal, in.Body.Year)
			if !ok {
				return BudgetView{}, invalid([]FieldProblem{{Code: budget.CodeBudget, Field: "body.financial_year",
					Message: fmt.Sprintf("%q is not the first day of a financial year", in.Body.Year), Fix: "Name the financial year by its first day, such as 2028-01-01"}})
			}
			lines, err := budget.Copy(b, in.Body.Growth, len(y.Months), cur)
			if err != nil {
				return BudgetView{}, err
			}
			nb := budget.Budget{Name: in.Body.Name, Kind: in.Body.Kind, Year: in.Body.Year, Lines: lines}
			if nb.Kind == budget.KindRevised {
				nb.Revises = from.String()
			}
			doc, err := budget.DraftIn(ctx, u, nb)
			if err != nil {
				return BudgetView{}, equityProblems(err)
			}
			return a.shownBudget(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return budgetAnswer(w), nil
	})

	register(a, authz.ImportBudget, huma.Operation{
		OperationID: "importBudget", Method: http.MethodPost, Path: "/v1/budgets:import", Tags: []string{"Planning"},
		DefaultStatus: http.StatusCreated, Summary: "Import a budget from XLSX",
		Description: "Drafts a budget version from an .xlsx workbook, as multipart/form-data (FR-PLN-01, design D6). The file is an " +
			"untrusted upload: its type is decided from its content and it is scanned for malware, as evidence is (FR-DOC-08), then " +
			"read by a bounded reader. Its first sheet has a header row of Account, a column per dimension by its name and a column per " +
			"month of the year, then a row per line. Every problem comes back together with its row and column (BUDGET-12), and nothing " +
			"is drafted while any remains. The file is not kept. Side effects: the draft, audit events. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeUnavailable, budget.CodeImport}, writes...), BudgetCodes...), func(ctx context.Context, in *budgetImportInput) (*budgetWriteOutput, error) {
		up := in.RawBody.Data()
		if up == nil || up.File.File == nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the budget in the form field file"}})
		}
		defer up.File.Close()
		if a.deps.Scanner == nil {
			return nil, problem(CodeUnavailable, "Budgets cannot be imported here: no malware scanner is configured.", "Ask your administrator to configure it.")
		}
		data, err := io.ReadAll(io.LimitReader(up.File, 10<<20+1))
		if err != nil || len(data) > 10<<20 {
			return nil, problem(CodeTooLarge, "The file is larger than 10 MB or could not be read in full.", "Send a smaller workbook.")
		}
		file, problems, err := evidence.Check(ctx, a.deps.Scanner, up.File.Filename, data)
		if err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The malware scan is not available, so the budget was not imported.", "Try again in a few minutes.")
		}
		if len(problems) > 0 {
			var bad []FieldProblem
			for _, x := range problems {
				bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
			}
			return nil, invalid(bad)
		}
		g, err := sheet.ReadXLSX(file.Data, budget.MaxImportRows)
		if err != nil {
			return nil, budgetSheetProblems(err)
		}
		request := struct{ Name, Kind, Year, Revises, SHA256 string }{up.Name, up.Kind, up.Year, up.Revises, file.OriginalSHA256}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BudgetView, error) {
			o, err := u.Organisation(ctx)
			if err != nil {
				return BudgetView{}, err
			}
			cal, err := period.CalendarOf(o.Profile)
			if err != nil {
				return BudgetView{}, err
			}
			y, ok := budget.YearOf(cal, up.Year)
			if !ok {
				return BudgetView{}, invalid([]FieldProblem{{Code: budget.CodeBudget, Field: "body.financial_year",
					Message: fmt.Sprintf("%q is not the first day of a financial year", up.Year), Fix: "Name the financial year by its first day, such as 2027-01-01"}})
			}
			chart, err := budget.ChartIn(ctx, u)
			if err != nil {
				return BudgetView{}, err
			}
			lines, probs := budget.FromSheet(g, y, chart)
			if probs != nil {
				return BudgetView{}, budgetSheetProblems(probs)
			}
			doc, err := budget.DraftIn(ctx, u, budget.Budget{Name: up.Name, Kind: up.Kind, Year: up.Year, Revises: up.Revises, Lines: lines})
			if err != nil {
				return BudgetView{}, equityProblems(err)
			}
			return a.shownBudget(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return budgetAnswer(w), nil
	})

	if op := a.OpenAPI().Paths["/v1/budgets:import"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"PK… (the workbook's bytes)"}
		}
	}

	register(a, authz.RequestBudgetApproval, huma.Operation{
		OperationID: "requestBudgetApproval", Method: http.MethodPost, Path: "/v1/budgets/{budget_id}/approval", Tags: []string{"Planning"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval of a budget",
		Description: "Asks another person to approve a drafted budget at the version read (FR-PLN-01, FM-1). Approving it checks it again, " +
			"then records and locks it: the year's budget from then on, its earlier version superseded. The answer is 202 with an " +
			"approval request, which a person who holds A on \"Budgets, forecasts\" approves, stepped up, never the one who asked. Needs a " +
			"person signed in who passed step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired}, writes...), BudgetCodes...),
		func(ctx context.Context, in *budgetAskInput) (*equityAskOutput, error) {
			id, err := parseID("budget_id", in.BudgetID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := budget.Drafted(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				b, err := budget.ReadyIn(ctx, u, doc)
				if err != nil {
					return Approval{}, equityProblems(err)
				}
				v, err := a.shownBudget(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "financial_year", After: b.Year},
					{Field: "kind", After: b.Kind}, {Field: "lines", After: strconv.Itoa(len(b.Lines))},
					{Field: "income_year", After: string(v.Totals.IncomeYear) + " " + v.Currency},
					{Field: "costs_year", After: string(v.Totals.CostsYear) + " " + v.Currency}}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectBudget, ObjectKey: doc.ID.String(),
					Operation: approval.OpBudgetRecord, Summary: fmt.Sprintf("Approve the budget %s for the year from %s", b.Name, b.Year),
					Execute: map[string]any{"budget_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
