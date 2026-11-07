//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// budgetChart is the accounts and the Department dimension the budget tests read.
type budgetChart struct {
	revenue, ads, rent, cash, equity string
	sales, eng                       string
}

func (e *env) budgetChart() budgetChart {
	e.t.Helper()
	var c budgetChart
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []struct {
			into *string
			n    storage.NewAccount
		}{
			{&c.revenue, storage.NewAccount{Code: "4010", Name: "Subscriptions", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Subscription revenue"}}},
			{&c.ads, storage.NewAccount{Code: "6110", Name: "Advertising", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Sales & marketing", Function: "S&M"}}},
			{&c.rent, storage.NewAccount{Code: "6200", Name: "Office rent", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Premises & office", Function: "G&A"}}},
			{&c.cash, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{CashFlowLine: "Cash & cash equivalents"}}},
			{&c.equity, storage.NewAccount{Code: "3010", Name: "Share capital", Type: storage.Equity, Role: storage.RoleShareCapital}},
		} {
			x, err := u.Accounts().Create(ctx, a.n)
			if err != nil {
				return err
			}
			*a.into = x.ID.String()
		}
		d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"})
		if err != nil {
			return err
		}
		s, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: "SALES", Name: "Sales"})
		if err != nil {
			return err
		}
		g, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: "ENG", Name: "Engineering"})
		c.sales, c.eng = s.ID.String(), g.ID.String()
		return err
	})
	return c
}

func months12(amount string) []string {
	out := make([]string, 12)
	for i := range out {
		out[i] = amount
	}
	return out
}

func budgetBody(c budgetChart) map[string]any {
	return map[string]any{"name": "FY2027 budget", "kind": "original", "financial_year": "2027-01-01", "lines": []any{
		map[string]any{"account_id": c.revenue, "amounts": months12("10000.00")},
		map[string]any{"account_id": c.ads, "dimension_value_ids": []string{c.sales}, "amounts": months12("2000.00")},
		map[string]any{"account_id": c.rent, "amounts": months12("3000.00")},
	}}
}

// Budgets through the API (FR-PLN-01, C.1 #35; p1-planning design D6), worked by hand. The books start on 1 Oct 2026, so
// the first financial year is October to December 2026 and the second 2027. The 2027 budget has subscriptions of
// 10,000.00 a month, advertising of 2,000.00 for Sales and rent of 3,000.00: income 120,000.00 and costs 60,000.00 for
// the year, 5,000.00 net a month. The Accountant drafts it and asks; the Owner approves it, which records and locks it.
// A revised version copied with 5% growth has subscriptions of 10,500.00, advertising of 2,100.00 and rent of 3,150.00;
// approved, it is the year's budget and the original is superseded. A 2028 copy at -10% has 9,450.00 of subscriptions.
// An XLSX sheet drafts the same lines; a row naming an asset and a cell that is not an amount are refused with their
// row and column. C.1 #35 warns of 2026, a year with postings and no budget, until its budget is approved.
// req: FR-PLN-01
// req: C.1 #35
// req: ENG-23
// req: QA-RPT-01
// asvs: V8.2.1
func TestBudgetsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	c := e.budgetChart()
	accountant := f.as(f.ahkow, authz.Accountant)
	owner := f.as(f.weiling, authz.Owner)

	d := e.call(http.MethodPost, "/v1/budgets", accountant, budgetBody(c))
	if d.Status != http.StatusCreated || d.str("status") != "draft" {
		t.Fatalf("draft: %d %s", d.Status, d.Raw)
	}
	var v BudgetView
	if err := json.Unmarshal([]byte(d.Raw), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Months) != 12 || v.Months[0] != "2027-01" || v.Totals.IncomeYear != "120000.00" || v.Totals.CostsYear != "60000.00" ||
		v.Totals.NetYear != "60000.00" || v.Totals.Net[3] != "5000.00" || v.LineTotals[1] != "24000.00" || v.Currency != "SGD" {
		t.Errorf("the draft's totals: %+v", v)
	}

	// Every problem at once, with its field.
	bad := budgetBody(c)
	lines := bad["lines"].([]any)
	lines[0].(map[string]any)["account_id"] = c.cash
	lines[1].(map[string]any)["dimension_value_ids"] = []string{c.sales, c.eng}
	lines[2].(map[string]any)["amounts"] = append(months12("3000.00")[:11], "3000.001")
	bad["lines"] = append(lines, map[string]any{"account_id": c.rent, "amounts": months12("1.00")})
	r := e.call(http.MethodPost, "/v1/budgets", accountant, bad)
	e.problemIs(r, "createBudget", 422, CodeInvalid)
	for _, field := range []string{"body.lines[0].account_id", "body.lines[1].dimension_value_ids[1]", "body.lines[2].amounts[11]", "body.lines[3]"} {
		if !strings.Contains(r.Raw, `"`+field+`"`) || !strings.Contains(r.Raw, `"BUDGET-10"`) {
			t.Errorf("the refusal names no %s: %s", field, r.Raw)
		}
	}
	notYear := budgetBody(c)
	notYear["financial_year"] = "2027-02-01"
	e.problemIs(e.call(http.MethodPost, "/v1/budgets", accountant, notYear), "createBudget", 422, CodeInvalid)
	revised := budgetBody(c)
	revised["kind"] = "revised"
	if r := e.call(http.MethodPost, "/v1/budgets", accountant, revised); !strings.Contains(r.Raw, `"body.revises"`) {
		t.Errorf("a revised budget naming nothing: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/budgets", e.as(authz.Auditor), budgetBody(c)), "createBudget", 403, "API-AUTHZ-01")

	// Asked and approved by another person: recorded and locked.
	id := d.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/budgets/"+id+"/approval", accountant, nil), "requestBudgetApproval", 428, CodeIfMatchRequired)
	asked := e.call(http.MethodPost, "/v1/budgets/"+id+"/approval", accountant, nil, "If-Match", d.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectBudget {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/budgets/"+id, accountant, nil); g.str("status") != "awaiting_approval" || g.str("approval_id") != asked.str("id") {
		t.Errorf("waiting: %s", g.Raw)
	}
	e.problemIs(f.decide(accountant, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); a.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	g := e.call(http.MethodGet, "/v1/budgets/"+id, accountant, nil)
	if g.str("status") != "recorded" || g.str("recorded_at") == "" {
		t.Fatalf("recorded: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/budgets/"+id, accountant, budgetBody(c), "If-Match", g.Header.Get("ETag")), "replaceBudget", 409, "BUDGET-11")

	// A revised version copied with 5% growth supersedes it once approved.
	cp := e.call(http.MethodPost, "/v1/budgets:copy", accountant, map[string]any{"from": id, "name": "FY2027 budget, revised", "kind": "revised",
		"financial_year": "2027-01-01", "growth": "0.05"})
	if cp.Status != http.StatusCreated || cp.str("budget", "revises") != id {
		t.Fatalf("copy: %d %s", cp.Status, cp.Raw)
	}
	var rv BudgetView
	_ = json.Unmarshal([]byte(cp.Raw), &rv)
	if rv.Budget.Lines[0].Amounts[0] != "10500.00" || rv.Budget.Lines[1].Amounts[11] != "2100.00" || rv.Budget.Lines[2].Amounts[5] != "3150.00" ||
		rv.Budget.Lines[1].Dimensions[0] != DimensionValueID(c.sales) {
		t.Errorf("the copy's amounts: %+v", rv.Budget.Lines)
	}
	asked2 := e.call(http.MethodPost, "/v1/budgets/"+cp.str("id")+"/approval", accountant, nil, "If-Match", cp.Header.Get("ETag"))
	if a := f.decide(owner, asked2.str("id"), "approve", hashOf(asked2)); a.Status != http.StatusOK {
		t.Fatalf("approving the revision: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/budgets/"+id, accountant, nil); g.str("status") != "superseded" {
		t.Errorf("the original after the revision: %s", g.str("status"))
	}
	next := e.call(http.MethodPost, "/v1/budgets:copy", accountant, map[string]any{"from": cp.str("id"), "name": "FY2028 budget", "kind": "original",
		"financial_year": "2028-01-01", "growth": "-0.1"})
	if next.Status != http.StatusCreated || !strings.Contains(next.Raw, `"9450.00"`) {
		t.Errorf("a 2028 copy at -10%%: %d %s", next.Status, next.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/budgets:copy", accountant, map[string]any{"from": id, "name": "FY2026", "kind": "original",
		"financial_year": "2026-10-01", "growth": "0"}), "copyBudget", 422, "BUDGET-10") // 12 months into a year of 3
	if l := e.call(http.MethodGet, "/v1/budgets?financial_year=2027-01-01", e.as(authz.Auditor), nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("2027's budgets: %s", l.Raw)
	}

	// From XLSX: the same lines, and every row's problem at once.
	xlsx := func(rows ...[]sheet.Cell) []byte {
		t.Helper()
		header := []string{"Account", "Department"}
		for m := 1; m <= 12; m++ {
			header = append(header, time.Date(2027, time.Month(m), 1, 0, 0, 0, 0, time.UTC).Format("2006-01"))
		}
		s := sheet.Sheet{Name: "Budget", Header: header}
		for _, cells := range rows {
			s.Rows = append(s.Rows, sheet.Row{Cells: cells})
		}
		b, err := sheet.XLSX([]sheet.Sheet{s}, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	row := func(account, dept, amount string) []sheet.Cell {
		cells := []sheet.Cell{sheet.Text(account), sheet.Text(dept)}
		for range 12 {
			cells = append(cells, sheet.Number(amount, 2))
		}
		return cells
	}
	fields := map[string]string{"name": "FY2027 from the sheet", "kind": "original", "financial_year": "2027-01-01"}
	im := e.upload("/v1/budgets:import", accountant, fields, "budget.xlsx", xlsx(row("4010 Subscriptions", "", "10000.00"), row("6110", "SALES", "2000.00"), row("6200", "", "3000.00")))
	if im.Status != http.StatusCreated || !strings.Contains(im.Raw, `"income_year":"120000.00"`) || !strings.Contains(im.Raw, c.sales) {
		t.Fatalf("import: %d %s", im.Status, im.Raw)
	}
	wrong := row("6200", "", "3000.00")
	wrong[5] = sheet.Text("three thousand")
	r = e.upload("/v1/budgets:import", accountant, fields, "budget.xlsx", xlsx(row("1010", "", "1.00"), row("6110", "OPS", "2000.00"), wrong))
	e.problemIs(r, "importBudget", 422, CodeInvalid)
	for _, field := range []string{"row 2, column A", "row 3, column B", "row 4, column F"} {
		if !strings.Contains(r.Raw, field) || !strings.Contains(r.Raw, `"BUDGET-12"`) {
			t.Errorf("the import's refusal names no %s: %s", field, r.Raw)
		}
	}
	r = e.upload("/v1/budgets:import", accountant, fields, "budget.xlsx", []byte("not a workbook at all"))
	if r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a file that is no workbook: %d %s", r.Status, r.Raw)
	}

	// C.1 #35: 2026 has postings and no budget, until one is approved; 2027 has its own.
	post := func(dr, cr, amount string) {
		t.Helper()
		id := uuid.NewString()
		ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 5), PostingIdentity: ledger.SourceOpeningBalance + ":" + id + ":v1:test@1",
				RuleVersion: "test@1", Source: storage.Source{Type: ledger.SourceOpeningBalance, ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: uuid.MustParse(dr), Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
					{AccountID: uuid.MustParse(cr), Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
				}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	post(c.cash, c.equity, "50000.00")
	ch := e.checked("planning.budget")
	wantStatus(t, ch, "warning")
	if it := itemNamed(ch, "2026-10-01"); it == nil {
		t.Fatalf("C.1 #35 names no 2026: %s", ch.Raw)
	}
	fy26 := e.call(http.MethodPost, "/v1/budgets", accountant, map[string]any{"name": "FY2026 budget", "kind": "original", "financial_year": "2026-10-01",
		"lines": []any{map[string]any{"account_id": c.rent, "amounts": []string{"3000.00", "3000.00", "3000.00"}}}})
	asked3 := e.call(http.MethodPost, "/v1/budgets/"+fy26.str("id")+"/approval", accountant, nil, "If-Match", fy26.Header.Get("ETag"))
	if a := f.decide(owner, asked3.str("id"), "approve", hashOf(asked3)); a.Status != http.StatusOK {
		t.Fatalf("approving 2026's budget: %d %s", a.Status, a.Raw)
	}
	wantStatus(t, e.checked("planning.budget"), "ok")
}

type budgetFixture struct{ budget string }

var matrixBudgetOf = map[*env]budgetFixture{}

func matrixBudget(e *env) budgetFixture {
	e.t.Helper()
	if f, ok := matrixBudgetOf[e]; ok {
		return f
	}
	e.booksFrom()
	c := e.budgetChart()
	d := e.call(http.MethodPost, "/v1/budgets", e.as(authz.Accountant), budgetBody(c))
	if d.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's budget: %d %s", d.Status, d.Raw)
	}
	out := budgetFixture{budget: d.str("id")}
	matrixBudgetOf[e] = out
	return out
}

// matrixBudgetState is what a refused call must leave as it was: the draft and the list.
func matrixBudgetState(e *env, f budgetFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/budgets/" + f.budget, "/v1/budgets"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

func matrixBudgetBody(e *env, f budgetFixture) map[string]any {
	g := e.call(http.MethodGet, "/v1/budgets/"+f.budget, e.as(authz.Accountant), nil)
	return g.Body["budget"].(map[string]any)
}

// matrixBudgets are the authorisation matrix's rows for budgets (FR-PLN-01).
var matrixBudgets = map[string]func(e *env, tok string) (response, func() bool){
	"listBudgets": func(e *env, tok string) (response, func() bool) {
		matrixBudget(e)
		return e.call(http.MethodGet, "/v1/budgets", tok, nil), nil
	},
	"createBudget": func(e *env, tok string) (response, func() bool) {
		f := matrixBudget(e)
		was := matrixBudgetState(e, f)
		r := e.call(http.MethodPost, "/v1/budgets", tok, matrixBudgetBody(e, f), "Prefer", "return=preview")
		return r, func() bool { return matrixBudgetState(e, f) == was }
	},
	"getBudget": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/budgets/"+matrixBudget(e).budget, tok, nil), nil
	},
	"replaceBudget": func(e *env, tok string) (response, func() bool) {
		f := matrixBudget(e)
		was := matrixBudgetState(e, f)
		r := e.call(http.MethodPut, "/v1/budgets/"+f.budget, tok, matrixBudgetBody(e, f), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixBudgetState(e, f) == was }
	},
	"copyBudget": func(e *env, tok string) (response, func() bool) {
		f := matrixBudget(e)
		was := matrixBudgetState(e, f)
		r := e.call(http.MethodPost, "/v1/budgets:copy", tok, map[string]any{"from": f.budget, "name": "FY2028", "kind": "original",
			"financial_year": "2028-01-01", "growth": "0.05"}, "Prefer", "return=preview")
		return r, func() bool { return matrixBudgetState(e, f) == was }
	},
	"importBudget": func(e *env, tok string) (response, func() bool) {
		f := matrixBudget(e)
		was := matrixBudgetState(e, f)
		header := []string{"Account"}
		cells := []sheet.Cell{sheet.Text("6200")}
		for m := 1; m <= 12; m++ {
			header = append(header, time.Date(2027, time.Month(m), 1, 0, 0, 0, 0, time.UTC).Format("2006-01"))
			cells = append(cells, sheet.Number("1.00", 2))
		}
		b, err := sheet.XLSX([]sheet.Sheet{{Name: "Budget", Header: header, Rows: []sheet.Row{{Cells: cells}}}}, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC))
		if err != nil {
			e.t.Fatal(err)
		}
		r := e.upload("/v1/budgets:import", tok, map[string]string{"name": "FY2027", "kind": "original", "financial_year": "2027-01-01"},
			"budget.xlsx", b, "Prefer", "return=preview")
		return r, func() bool { return matrixBudgetState(e, f) == was }
	},
	"requestBudgetApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixBudget(e)
		was := matrixBudgetState(e, f)
		r := e.call(http.MethodPost, "/v1/budgets/"+f.budget+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixBudgetState(e, f) == was }
	},
}
