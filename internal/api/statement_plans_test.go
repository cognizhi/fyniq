//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A P&L beside its budget and its forecast (FR-RPT-02; p1-planning design D6), worked by hand on TestBudgetVsActualAndHome's
// company: the first financial year runs from October to December 2026, its budget is subscriptions of 10,000.00 a
// month, advertising of 2,000.00 for Sales and rent of 3,000.00, and the books hold October and November. October and
// November's P&L: subscriptions 18,000.00 against 20,000.00, advertising 4,000.00 against 4,000.00, rent 6,500.00 against
// 6,000.00, net profit 7,500.00 (41.7% of revenue) against 10,000.00 (50.0%). Sales' P&L has advertising alone: −4,000.00
// against −4,000.00. October to December: subscriptions 18,000.00 against 30,000.00. The forecast starts in December,
// after November, the last actual month; its December is the forecast's own, services of 5,000.00 its only revenue.
// req: FR-RPT-02
// req: QA-RPT-01
func TestStatementComparatives(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	e.clock.Set(time.Date(2026, 12, 5, 9, 0, 0, 0, time.UTC))
	c := e.budgetChart()
	e.plannedBooks(c)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error { // December, the year's last month, with no books yet
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(12, 1), EndsOn: porttest.Day(12, 31)})
		return err
	})
	accountant := f.as(f.ahkow, authz.Accountant)

	read := func(query string) FinancialStatement {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/reports/statements/pl?"+query, e.as(authz.Auditor), nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", query, r.Status, r.Raw)
		}
		var s FinancialStatement
		if err := json.Unmarshal([]byte(r.Raw), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	block := func(s FinancialStatement, key string) FinancialBlock {
		t.Helper()
		for _, b := range s.Blocks {
			if b.Key == key {
				return b
			}
		}
		t.Fatalf("no block %s", key)
		return FinancialBlock{}
	}
	line := func(s FinancialStatement, code string) FinancialLine {
		t.Helper()
		for _, b := range s.Blocks {
			for _, g := range b.Groups {
				for _, l := range g.Lines {
					if l.Code == code {
						return l
					}
				}
			}
		}
		t.Fatalf("no line %s", code)
		return FinancialLine{}
	}
	amounts := func(ms []Money) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.Amount)
		}
		return out
	}
	same := func(what string, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: %v, want %v", what, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: %v, want %v", what, got, want)
				return
			}
		}
	}

	// Without a recorded budget, the column is left out, as a prior year the books do not have.
	if s := read("from=2026-10&to=2026-11&compare=budget"); len(s.Columns) != 1 {
		t.Fatalf("a budget column before any budget: %+v", s.Columns)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/reports/statements/bs?to=2026-11&compare=budget", accountant, nil), "getFinancialStatement", 422, CodeInvalid)

	f.approvedBudget(c)
	s := read("from=2026-10&to=2026-11&compare=budget")
	if len(s.Columns) != 2 || s.Columns[1].Key != "budget" || s.Columns[1].Label != "Budget" || s.Columns[1].From != "2026-10-01" || s.Columns[1].To != "2026-11-30" {
		t.Fatalf("the columns: %+v", s.Columns)
	}
	same("revenue", amounts(block(s, "revenue").Totals), "18000.00", "20000.00")
	same("advertising", amounts(line(s, "6110").Amounts), "4000.00", "4000.00")
	same("rent", amounts(line(s, "6200").Amounts), "6500.00", "6000.00")
	np := block(s, "net_profit")
	same("net profit", amounts(np.Totals), "7500.00", "10000.00")
	if np.Percents[0] != "41.7" || np.Percents[1] != "50.0" {
		t.Errorf("net profit's %% of revenue: %v", np.Percents)
	}
	// Sales' P&L beside Sales' budget lines.
	sales := read("from=2026-10&to=2026-11&compare=budget&dimension_value_id=" + c.sales)
	same("Sales' advertising", amounts(line(sales, "6110").Amounts), "4000.00", "4000.00")
	same("Sales' net profit", amounts(block(sales, "net_profit").Totals), "-4000.00", "-4000.00")
	// A quarter: December has a budget and no books yet.
	q := read("from=2026-10&to=2026-12&compare=budget")
	same("the quarter's revenue", amounts(block(q, "revenue").Totals), "18000.00", "30000.00")
	// By month, there is no comparative.
	if m := read("from=2026-10&to=2026-11&compare=budget&columns=month"); len(m.Columns) != 3 {
		t.Errorf("by month: %+v", m.Columns)
	}

	// The forecast: none recorded, then December's from the recorded forecast, and none for the months before it.
	if s := read("from=2026-12&to=2026-12&compare=forecast"); s.Forecast != nil || len(s.Columns) != 1 {
		t.Fatalf("a forecast before any is recorded: %+v", s.Forecast)
	}
	d := e.call(http.MethodPost, "/v1/forecasts", accountant, forecastPlan())
	if rec := e.call(http.MethodPost, "/v1/forecasts/"+d.str("id")+"/record", accountant, nil, "If-Match", d.Header.Get("ETag")); rec.Status != http.StatusOK {
		t.Fatalf("recording the forecast: %d %s", rec.Status, rec.Raw)
	}
	fc := read("from=2026-12&to=2026-12&compare=forecast").Forecast
	if fc == nil || fc.ForecastID != d.str("id") || fc.Scenario != "Base" || len(fc.Sections) != 6 {
		t.Fatalf("December's forecast: %+v", fc)
	}
	r := e.call(http.MethodGet, "/v1/forecasts/"+d.str("id")+"/results", accountant, nil)
	var res ForecastResults
	if err := json.Unmarshal([]byte(r.Raw), &res); err != nil {
		t.Fatal(err)
	}
	dec := res.Runs[0].Months[0]
	if dec.Month != "2026-12" || dec.Revenue != "5000.00" {
		t.Fatalf("the forecast's December: %+v", dec)
	}
	var opex apd.Decimal
	gp, _, _ := apd.NewFromString(dec.GrossProfit)
	ebitda, _, _ := apd.NewFromString(dec.EBITDA)
	if _, err := apd.BaseContext.Sub(&opex, gp, ebitda); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"revenue": dec.Revenue, "cost_of_sales": dec.DirectCosts, "gross_profit": dec.GrossProfit,
		"operating_expenses": opex.Text('f'), "profit_before_tax": dec.ProfitBeforeTax, "net_profit": dec.NetProfit}
	for _, x := range fc.Sections {
		if x.Amount.Amount != want[x.Key] {
			t.Errorf("the forecast's %s: %s, want %s", x.Key, x.Amount.Amount, want[x.Key])
		}
	}
	if s := read("from=2026-10&to=2026-12&compare=forecast"); s.Forecast != nil {
		t.Errorf("a forecast for months it does not cover: %+v", s.Forecast)
	}
	if s := read("from=2026-12&to=2026-12&compare=forecast&dimension_value_id=" + c.sales); s.Forecast != nil {
		t.Errorf("a forecast beside Sales' P&L: %+v", s.Forecast)
	}
}
