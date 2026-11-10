//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Budget vs Actual and Home's planning tiles through the API (FR-RPT-03, FR-KPI-01, FR-PLN-03), worked by hand. The books
// start on 1 Oct 2026, so October to December is the first financial year. Its budget: subscriptions of 10,000.00 a
// month, advertising of 2,000.00 for Sales and rent of 3,000.00. The books: subscriptions of 12,000.00 in October and
// 6,000.00 in November; advertising for Sales of 1,500.00 and 2,500.00; rent of 3,000.00 and 3,500.00. On 5 Dec 2026,
// October and November: subscriptions 18,000.00 against 20,000.00, -2,000.00 or -10.0%, unfavourable; advertising
// 4,000.00 against 4,000.00, 0.0%, favourable; rent 6,500.00 against 6,000.00, +500.00 or 8.3%, unfavourable. Income
// -10.0%, costs 10,500.00 against 10,000.00 or 5.0%, net 7,500.00 against 10,000.00 or -25.0%. Without a recorded
// forecast, Home's forecast tile says so; with the forecast of TestForecastsThroughTheAPI's shape recorded, it shows its
// cash-out month without the funding planned.
// req: FR-RPT-03
// req: FR-KPI-01
// req: FR-PLN-03
// req: QA-RPT-01
func TestBudgetVsActualAndHome(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	e.clock.Set(time.Date(2026, 12, 5, 9, 0, 0, 0, time.UTC))
	c := e.budgetChart()
	accountant := f.as(f.ahkow, authz.Accountant)

	// Home before any budget or forecast.
	h := e.call(http.MethodGet, "/v1/planning/summary", accountant, nil)
	if h.Status != http.StatusOK || h.str("month") != "2026-11" || h.str("budget_missing") != "No approved budget for the year from 1 Oct 2026" ||
		h.str("forecast_missing") != "No forecast is recorded yet" {
		t.Fatalf("Home with nothing planned: %d %s", h.Status, h.Raw)
	}

	b := f.approvedBudget(c)
	e.plannedBooks(c)

	read := func(path string) BudgetVsActual {
		t.Helper()
		r := e.call(http.MethodGet, path, e.as(authz.Auditor), nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, r.Status, r.Raw)
		}
		var out BudgetVsActual
		if err := json.Unmarshal([]byte(r.Raw), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	vs := read("/v1/reports/budget-vs-actual")
	if vs.From != "2026-10" || vs.To != "2026-11" || vs.BudgetID != b.str("id") || len(vs.Rows) != 3 {
		t.Fatalf("the report: %+v", vs)
	}
	for i, want := range []struct {
		code, actual, budget, variance, pct string
		fav                                 bool
	}{
		{"4010", "18000.00", "20000.00", "-2000.00", "-10.0", false},
		{"6110", "4000.00", "4000.00", "0.00", "0.0", true},
		{"6200", "6500.00", "6000.00", "500.00", "8.3", false},
	} {
		r := vs.Rows[i]
		if r.Code != want.code || string(r.Actual) != want.actual || string(r.Budget) != want.budget || string(r.Variance) != want.variance ||
			r.Percent != want.pct || r.Favourable != want.fav {
			t.Errorf("row %d: %+v, want %+v", i, r, want)
		}
	}
	if vs.Income.Percent != "-10.0" || string(vs.Costs.Actual) != "10500.00" || vs.Costs.Percent != "5.0" || string(vs.Net.Actual) != "7500.00" ||
		string(vs.Net.Budget) != "10000.00" || vs.Net.Percent != "-25.0" {
		t.Errorf("totals: income %+v, costs %+v, net %+v", vs.Income, vs.Costs, vs.Net)
	}
	// By dimension: advertising's row is Sales'; one month alone.
	dim := read("/v1/reports/budget-vs-actual?by=dimension&from=2026-11&to=2026-11")
	for _, r := range dim.Rows {
		if r.Code == "6110" && (len(r.Dimensions) != 1 || string(r.Dimensions[0]) != c.sales || string(r.Actual) != "2500.00" || string(r.Budget) != "2000.00") {
			t.Errorf("advertising for Sales in November: %+v", r)
		}
	}
	e.problemIs(e.call(http.MethodGet, "/v1/reports/budget-vs-actual?from=2026-12&to=2027-01", accountant, nil), "getBudgetVsActual", 422, "BUDGET-10")
	e.problemIs(e.call(http.MethodGet, "/v1/reports/budget-vs-actual", e.as(authz.Bookkeeper), nil), "getBudgetVsActual", 403, "API-AUTHZ-01")

	// Home: the year to date against the budget, and the recorded forecast without its funding.
	h = e.call(http.MethodGet, "/v1/planning/summary", accountant, nil)
	if h.str("budget", "revenue", "variance_percent") != "-10.0" || h.str("budget", "costs", "actual") != "10500.00" || h.str("budget", "from") != "2026-10" {
		t.Errorf("Home's budget tiles: %s", h.Raw)
	}
	plan := forecastPlan()
	plan["opex"] = []any{map[string]any{"label": "Rent", "base": "30000.00", "growth": "0", "percent": "0"}}
	d := e.call(http.MethodPost, "/v1/forecasts", accountant, plan)
	if rec := e.call(http.MethodPost, "/v1/forecasts/"+d.str("id")+"/record", accountant, nil, "If-Match", d.Header.Get("ETag")); rec.Status != http.StatusOK {
		t.Fatalf("recording the forecast: %d %s", rec.Status, rec.Raw)
	}
	h = e.call(http.MethodGet, "/v1/planning/summary", accountant, nil)
	if h.str("forecast", "forecast_id") != d.str("id") || h.str("forecast", "scenario") != "Base" || h.str("forecast", "note") == "" {
		t.Fatalf("Home's forecast tile: %s", h.Raw)
	}
	// Cash at November's end is 12,000 + 6,000 − 1,500 − 2,500 − 3,000 − 3,500 = 7,500.00; the plan spends far more than
	// it earns, so it runs out in its first month, December, and the planned equity in March does not save it.
	if h.str("forecast", "cash_out_month") != "2026-12" || h.Body["forecast"].(map[string]any)["runway_months"] != float64(0) {
		t.Errorf("the cash-out month without funding: %s", h.Raw)
	}
}

// approvedBudget drafts the first financial year's budget, October to December 2026, and has it approved: subscriptions
// of 10,000.00 a month, advertising of 2,000.00 for Sales and rent of 3,000.00.
func (f founders) approvedBudget(c budgetChart) response {
	e := f.e
	e.t.Helper()
	accountant := f.as(f.ahkow, authz.Accountant)
	b := e.call(http.MethodPost, "/v1/budgets", accountant, map[string]any{"name": "FY2026 budget", "kind": "original", "financial_year": "2026-10-01",
		"lines": []any{
			map[string]any{"account_id": c.revenue, "amounts": []string{"10000.00", "10000.00", "10000.00"}},
			map[string]any{"account_id": c.ads, "dimension_value_ids": []string{c.sales}, "amounts": []string{"2000.00", "2000.00", "2000.00"}},
			map[string]any{"account_id": c.rent, "amounts": []string{"3000.00", "3000.00", "3000.00"}},
		}})
	asked := e.call(http.MethodPost, "/v1/budgets/"+b.str("id")+"/approval", accountant, nil, "If-Match", b.Header.Get("ETag"))
	if r := f.decide(f.as(f.weiling, authz.Owner), asked.str("id"), "approve", hashOf(asked)); r.Status != http.StatusOK {
		e.t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	return b
}

// plannedBooks posts October's and November's books: subscriptions of 12,000.00 and 6,000.00, advertising for Sales of
// 1,500.00 and 2,500.00, and rent of 3,000.00 and 3,500.00, all through the bank.
func (e *env) plannedBooks(c budgetChart) {
	e.t.Helper()
	post := func(month, day int, dr, cr, amount string, values ...string) {
		e.plannedLine(dr, cr, porttest.Day(time.Month(month), day), amount, values...)
	}
	post(10, 5, c.cash, c.revenue, "12000.00")
	post(10, 10, c.ads, c.cash, "1500.00", c.sales)
	post(10, 20, c.rent, c.cash, "3000.00")
	post(11, 5, c.cash, c.revenue, "6000.00")
	post(11, 10, c.ads, c.cash, "2500.00", c.sales)
	post(11, 20, c.rent, c.cash, "3500.00")
}

// plannedLine posts amount from cr to dr on date straight to the ledger, the debit carrying values.
func (e *env) plannedLine(dr, cr string, date time.Time, amount string, values ...string) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	id := uuid.NewString()
	var dims []uuid.UUID
	for _, v := range values {
		dims = append(dims, uuid.MustParse(v))
	}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: date, PostingIdentity: ledger.SourceManualJournal + ":" + id + ":v1:test@1",
			RuleVersion: "test@1", Source: storage.Source{Type: ledger.SourceManualJournal, ID: id, Version: 1}, Lines: []storage.NewLine{
				{AccountID: uuid.MustParse(dr), Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount), DimensionValues: dims},
				{AccountID: uuid.MustParse(cr), Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
			}})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
}

// matrixBudgetReports are the authorisation matrix's rows for Budget vs Actual and the planning summary.
var matrixBudgetReports = map[string]func(e *env, tok string) (response, func() bool){
	"getBudgetVsActual": func(e *env, tok string) (response, func() bool) {
		matrixBudget(e)
		return e.call(http.MethodGet, "/v1/reports/budget-vs-actual?from=2027-01&to=2027-03", tok, nil), nil
	},
	"getPlanningSummary": func(e *env, tok string) (response, func() bool) {
		matrixBudget(e)
		return e.call(http.MethodGet, "/v1/planning/summary?month=2027-03", tok, nil), nil
	},
}
