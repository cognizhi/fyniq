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
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// forecastPlan is a small plan, worked by hand in TestForecastsThroughTheAPI: 12 months of services at 5,000.00 a
// month collected the same month, rent of 3,000.00 and advertising of 1,000.00 a month, an engineer on 7,600.00 with
// CPF, and 50,000.00 of equity planned for March 2027.
func forecastPlan() map[string]any {
	return map[string]any{
		"name": "FY2027 plan", "horizon": 12, "cash_adjustment": "0.00",
		"revenue": map[string]any{"customers": "0", "new_customers": "0", "new_growth": "0", "churn": "0", "arpa": "0.00",
			"price_increase": "0", "services": "5000.00", "services_growth": "0", "other": "0.00",
			"collection": map[string]any{"this_month": "1", "next_month": "0", "month_after": "0"}},
		"costs": map[string]any{"hosting": "0", "ai": "0", "contractors": "0", "fees": "0", "salary_increase": "0.04"},
		"opex": []any{
			map[string]any{"label": "Rent", "report_group": "Premises & office", "base": "3000.00", "growth": "0", "percent": "0"},
			map[string]any{"label": "Advertising", "report_group": "Sales & marketing", "base": "1000.00", "growth": "0", "percent": "0"}},
		"headcount": []any{map[string]any{"role": "Engineer", "function": "R&D", "start": "2026-01", "salary": "7600.00", "cpf": true}},
		"capex":     []any{},
		"funding":   []any{map[string]any{"type": "Equity", "month": "2027-03", "amount": "50000.00"}},
		"tax":       map[string]any{"start_up_years": 3, "losses_brought_forward": "0.00", "payment_lag": 9},
	}
}

// The forecast through the API (FR-PLN-02, FR-PLN-03, FR-PLN-08; p1-planning design D3 to D5), worked by hand. On 5 Dec
// 2026 the books hold: in October, 60,000.00 of share capital in the bank, 6,000.00 of subscriptions invoiced and
// 1,000.00 of advertising paid; in November, 2,000.00 received, rent of 3,000.00 on a bill not paid and 2,000.00 of
// advertising paid. So at November's end, the last actual month, cash is 59,000.00, receivables 4,000.00 and payables
// 3,000.00; over the two months Sales & marketing averaged 1,500.00 and Premises & office 1,500.00.
//
// The plan starts in December 2026. Payroll is 7,600.00 + 17% CPF 1,292.00 + SDL at its 11.25 maximum = 8,903.25 a
// month. Month 1 collects 5,000.00 and 60% of the 4,000.00 owed, pays the 4,000.00 of expenses and the 3,000.00 owed:
// operating cash flow 7,400.00 − 7,000.00 − 8,903.25 = −8,503.25, cash 50,496.75. Month 2 collects 40% of the owed
// too: −6,303.25, cash 44,193.50. Then −7,903.25 a month. Base with its funding: 50,000.00 in March, month 4, cash
// 78,387.00, at its lowest 15,161.00 in November 2027, no cash-out. Without it, cash falls below zero in July 2027, a
// runway of 7 months, to −34,839.00 in November. The first 12 months' net burn is 93,839.00, 7,819.92 a month on
// average; the funding gap for 6 months' buffer is 46,919.50 − 15,161.00 = 31,758.50 with the funding, and
// 46,919.50 + 34,839.00 = 81,758.50 without. Year 1 loses 94,839.00, so it pays no tax and carries the loss. Worst moves
// the equity three months to June and raises 60% of it, 30,000.00.
// req: FR-PLN-02
// req: FR-PLN-03
// req: FR-PLN-08
// req: QA-RPT-01
// asvs: V8.2.1
func TestForecastsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(time.Date(2026, 12, 5, 9, 0, 0, 0, time.UTC))
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	acc := map[string]storage.Account{}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []storage.NewAccount{
			{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{ReportGroup: "Cash & bank", CashFlowLine: "Cash & cash equivalents"}},
			{Code: "1100", Name: "Trade receivables", Type: storage.Asset, Role: "accounts-receivable", AccountAttributes: storage.AccountAttributes{ReportGroup: "Trade receivables", CashFlowLine: "Receivables", Control: true}},
			{Code: "2010", Name: "Trade payables", Type: storage.Liability, Role: "accounts-payable", AccountAttributes: storage.AccountAttributes{ReportGroup: "Trade payables", CashFlowLine: "Payables & accruals", Control: true}},
			{Code: "3010", Name: "Share capital", Type: storage.Equity, Role: storage.RoleShareCapital, AccountAttributes: storage.AccountAttributes{ReportGroup: "Share capital", CashFlowLine: "Share capital"}},
			{Code: "4010", Name: "Subscriptions", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Subscription revenue"}},
			{Code: "6110", Name: "Advertising", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Sales & marketing", Function: "S&M"}},
			{Code: "6200", Name: "Office rent", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Premises & office", Function: "G&A"}},
		} {
			x, err := u.Accounts().Create(ctx, a)
			if err != nil {
				return err
			}
			acc[a.Code] = x
		}
		return nil
	})
	post := func(source string, month, day int, dr, cr, amount string) {
		t.Helper()
		id := uuid.NewString()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(time.Month(month), day), PostingIdentity: source + ":" + id + ":v1:test@1",
				RuleVersion: "test@1", Source: storage.Source{Type: source, ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: acc[dr].ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
					{AccountID: acc[cr].ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
				}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	mj := ledger.SourceManualJournal
	post(ledger.SourceOpeningBalance, 10, 1, "1010", "3010", "60000.00")
	post(mj, 10, 10, "1100", "4010", "6000.00")
	post(mj, 10, 20, "6110", "1010", "1000.00")
	post(mj, 11, 10, "1010", "1100", "2000.00")
	post(mj, 11, 15, "6200", "2010", "3000.00")
	post(mj, 11, 20, "6110", "1010", "2000.00")
	entries := func() int {
		n := 0
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			rev, err := u.Ledger().Revision(ctx)
			n = int(rev)
			return err
		})
		return n
	}
	before := entries()

	accountant := e.as(authz.Accountant)
	d := e.call(http.MethodPost, "/v1/forecasts", accountant, forecastPlan())
	if d.Status != http.StatusCreated || d.str("status") != "draft" {
		t.Fatalf("draft: %d %s", d.Status, d.Raw)
	}
	id := d.str("id")
	if n := len(d.Body["plan"].(map[string]any)["scenarios"].([]any)); n != 3 {
		t.Errorf("a plan without scenarios gets Base, Best and Worst; got %d", n)
	}
	results := func(path string) ForecastResults {
		t.Helper()
		r := e.call(http.MethodGet, path, e.as(authz.Auditor), nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, r.Status, r.Raw)
		}
		var out ForecastResults
		if err := json.Unmarshal([]byte(r.Raw), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	res := results("/v1/forecasts/" + id + "/results?scenario=Base")
	b := res.Books
	if b.LastActual != "2026-11" || b.Start != "2026-12" || b.Cash != "59000.00" || b.Receivables != "4000.00" || b.Payables != "3000.00" {
		t.Errorf("the books: %+v", b)
	}
	hints := map[string]string{}
	for _, h := range b.Hints {
		hints[h.ReportGroup] = h.Average
	}
	if hints["Sales & marketing"] != "1500.00" || hints["Premises & office"] != "1500.00" {
		t.Errorf("the hints: %v", hints)
	}
	if strings.Join(func() []string {
		var s []string
		for _, p := range res.Packs {
			s = append(s, string(p))
		}
		return s
	}(), ",") != "sg-cit@2026.1,sg-cpf@2026.1,sg-sdl@2026.1" || res.Currency != "SGD" || res.Sandbox == "" {
		t.Errorf("packs %v, currency %s, sandbox %q", res.Packs, res.Currency, res.Sandbox)
	}
	if len(res.Runs) != 2 || !res.Runs[0].WithFunding || res.Runs[1].WithFunding {
		t.Fatalf("Base with and then without its funding: %d runs", len(res.Runs))
	}
	with, without := res.Runs[0], res.Runs[1]
	m := with.Months
	for _, c := range []struct{ what, got, want string }{
		{"m1 revenue", m[0].Revenue, "5000.00"}, {"m1 payroll", m[0].PayrollRD, "8903.25"}, {"m1 collections", m[0].Collections, "7400.00"},
		{"m1 suppliers", m[0].Suppliers, "-7000.00"}, {"m1 operating", m[0].Operating, "-8503.25"}, {"m1 cash", m[0].Cash, "50496.75"},
		{"m2 collections", m[1].Collections, "6600.00"}, {"m2 cash", m[1].Cash, "44193.50"}, {"m3 cash", m[2].Cash, "36290.25"},
		{"m4 equity", m[3].Equity, "50000.00"}, {"m4 cash", m[3].Cash, "78387.00"}, {"m1 S&M for CAC", m[0].SalesMarketing, "1000.00"},
		{"m12 cash", m[11].Cash, "15161.00"}, {"minimum", with.Key.MinimumCash, "15161.00"}, {"minimum month", with.Key.MinimumMonth, "2027-11"},
		{"average burn", with.Key.AverageBurn, "7819.92"}, {"funding gap", with.Key.FundingGap, "31758.50"},
		{"without: cash out", without.Key.CashOutMonth, "2027-07"}, {"without: minimum", without.Key.MinimumCash, "-34839.00"},
		{"without: funding gap", without.Key.FundingGap, "81758.50"}, {"year 1 tax", with.Taxes[0].Tax, "0.00"},
		{"year 1 losses", with.Taxes[0].LossesCarriedForward, "94839.00"}, {"revenue next 12", with.Key.RevenueNext12, "60000.00"},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %s, want %s", c.what, c.got, c.want)
		}
	}
	if !with.Key.RunwayBeyond || without.Key.RunwayBeyond || without.Key.RunwayMonths != 7 || with.Key.BufferMonths != 6 {
		t.Errorf("runway: with %d (beyond %v), without %d (beyond %v), buffer %d", with.Key.RunwayMonths, with.Key.RunwayBeyond,
			without.Key.RunwayMonths, without.Key.RunwayBeyond, with.Key.BufferMonths)
	}
	if m[0].CAC != "" || m[0].GrossMargin != "100.0" || m[0].Headcount != 1 {
		t.Errorf("m1: CAC %q (no new customers: blank), gross margin %q, headcount %d", m[0].CAC, m[0].GrossMargin, m[0].Headcount)
	}
	// Worst moves the equity three months and raises 60% of it.
	worst := results("/v1/forecasts/" + id + "/results?scenario=worst")
	if got := worst.Runs[0].Months[6].Equity; got != "30000.00" || worst.Runs[0].Months[3].Equity != "0.00" {
		t.Errorf("worst: equity in June %s, in March %s", got, worst.Runs[0].Months[3].Equity)
	}
	// The comparison: every scenario, with and without its funding, without the months.
	cmp := results("/v1/forecasts/" + id + "/comparison")
	if len(cmp.Runs) != 6 || cmp.Runs[0].Months != nil || cmp.Runs[2].Scenario != "Best" || cmp.Runs[0].Key.FundingGap != "31758.50" {
		t.Errorf("comparison: %d runs, months %v", len(cmp.Runs), cmp.Runs[0].Months != nil)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/forecasts/"+id+"/results?scenario=Nope", accountant, nil), "getForecastResults", 422, "PLAN-10")

	// A run saves nothing: the same figures, and no new document.
	run := e.call(http.MethodPost, "/v1/forecasts:run?scenario=Base", accountant, forecastPlan())
	if run.Status != http.StatusOK || !strings.Contains(run.Raw, `"funding_gap":"31758.50"`) || strings.Contains(run.Raw, `"forecast_id"`) {
		t.Errorf("run: %d %s", run.Status, run.Raw[:min(300, len(run.Raw))])
	}
	if l := e.call(http.MethodGet, "/v1/forecasts", accountant, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("after a run the list holds %d forecasts, want 1", len(l.Body["items"].([]any)))
	}

	// The schema refuses a horizon outside 12 to 60; the plan's own rules refuse every other problem at once, with its
	// field, a report group the chart lacks included.
	short := forecastPlan()
	short["horizon"] = 6
	r := e.call(http.MethodPost, "/v1/forecasts", accountant, short)
	e.problemIs(r, "createForecast", 422, CodeInvalid)
	if !strings.Contains(r.Raw, `"body.horizon"`) {
		t.Errorf("the refusal names no horizon: %s", r.Raw)
	}
	bad := forecastPlan()
	bad["revenue"].(map[string]any)["churn"] = "2"
	bad["costs"].(map[string]any)["hosting"] = "0.1234567"
	bad["opex"].([]any)[0].(map[string]any)["report_group"] = "Rent and things"
	r = e.call(http.MethodPost, "/v1/forecasts", accountant, bad)
	e.problemIs(r, "createForecast", 422, CodeInvalid)
	for _, f := range []string{"body.revenue.churn", "body.costs.hosting", "body.opex[0].report_group"} {
		if !strings.Contains(r.Raw, `"`+f+`"`) || !strings.Contains(r.Raw, `"PLAN-10"`) {
			t.Errorf("the refusal names no %s: %s", f, r.Raw)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/forecasts:run", accountant, bad), "runForecast", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/forecasts", e.as(authz.Auditor), forecastPlan()), "createForecast", 403, "API-AUTHZ-01")

	// Change, then record: the company's own.
	changed := forecastPlan()
	changed["name"] = "FY2027 plan, revised"
	e.problemIs(e.call(http.MethodPut, "/v1/forecasts/"+id, accountant, changed), "replaceForecast", 428, CodeIfMatchRequired)
	put := e.call(http.MethodPut, "/v1/forecasts/"+id, accountant, changed, "If-Match", d.Header.Get("ETag"))
	if put.Status != http.StatusOK || put.str("plan", "name") != "FY2027 plan, revised" {
		t.Fatalf("replace: %d %s", put.Status, put.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/forecasts/"+id+"/record", accountant, nil, "If-Match", d.Header.Get("ETag")), "recordForecast", 412, CodeStale)
	rec := e.call(http.MethodPost, "/v1/forecasts/"+id+"/record", accountant, nil, "If-Match", put.Header.Get("ETag"))
	if rec.Status != http.StatusOK || rec.str("status") != "recorded" || rec.str("recorded_at") == "" {
		t.Fatalf("record: %d %s", rec.Status, rec.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/forecasts/"+id, accountant, changed, "If-Match", rec.Header.Get("ETag")), "replaceForecast", 409, "PLAN-11")
	// A second recorded forecast supersedes the first, and the list puts it first.
	d2 := e.call(http.MethodPost, "/v1/forecasts", accountant, forecastPlan())
	rec2 := e.call(http.MethodPost, "/v1/forecasts/"+d2.str("id")+"/record", accountant, nil, "If-Match", d2.Header.Get("ETag"))
	if rec2.str("status") != "recorded" {
		t.Fatalf("record the second: %d %s", rec2.Status, rec2.Raw)
	}
	l := e.call(http.MethodGet, "/v1/forecasts", accountant, nil).Body["items"].([]any)
	if len(l) != 2 || l[0].(map[string]any)["id"] != d2.str("id") || l[1].(map[string]any)["status"] != "superseded" {
		t.Errorf("the list: %v", l)
	}
	// Audited, and nothing posted (FR-PLN-08).
	if n := strings.Count(auditTrail(t, e), id); n < 3 {
		t.Errorf("%d audit events name the forecast; want its draft, change and record", n)
	}
	if after := entries(); after != before {
		t.Errorf("the ledger moved from revision %d to %d: a forecast never posts", before, after)
	}
}

type planFixture struct{ forecast string }

var matrixForecastOf = map[*env]planFixture{}

// matrixPlanForecast is the matrix's drafted forecast.
func matrixPlanForecast(e *env) planFixture {
	e.t.Helper()
	if f, ok := matrixForecastOf[e]; ok {
		return f
	}
	d := e.call(http.MethodPost, "/v1/forecasts", e.as(authz.Accountant), matrixForecastBody())
	if d.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's forecast: %d %s", d.Status, d.Raw)
	}
	out := planFixture{forecast: d.str("id")}
	matrixForecastOf[e] = out
	return out
}

func matrixForecastBody() map[string]any {
	p := forecastPlan()
	p["opex"] = []any{map[string]any{"label": "Rent", "base": "3000.00", "growth": "0", "percent": "0"}}
	return p
}

// matrixPlanForecastState is what a refused call must leave as it was: the draft and the list.
func matrixPlanForecastState(e *env, f planFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/forecasts/" + f.forecast, "/v1/forecasts"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// matrixForecasts are the authorisation matrix's rows for forecasts (FR-PLN-02).
var matrixForecasts = map[string]func(e *env, tok string) (response, func() bool){
	"listForecasts": func(e *env, tok string) (response, func() bool) {
		matrixPlanForecast(e)
		return e.call(http.MethodGet, "/v1/forecasts", tok, nil), nil
	},
	"createForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixPlanForecast(e)
		was := matrixPlanForecastState(e, f)
		r := e.call(http.MethodPost, "/v1/forecasts", tok, matrixForecastBody(), "Prefer", "return=preview")
		return r, func() bool { return matrixPlanForecastState(e, f) == was }
	},
	"getForecast": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/forecasts/"+matrixPlanForecast(e).forecast, tok, nil), nil
	},
	"replaceForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixPlanForecast(e)
		was := matrixPlanForecastState(e, f)
		r := e.call(http.MethodPut, "/v1/forecasts/"+f.forecast, tok, matrixForecastBody(), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPlanForecastState(e, f) == was }
	},
	"recordForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixPlanForecast(e)
		was := matrixPlanForecastState(e, f)
		r := e.call(http.MethodPost, "/v1/forecasts/"+f.forecast+"/record", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPlanForecastState(e, f) == was }
	},
	"getForecastResults": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/forecasts/"+matrixPlanForecast(e).forecast+"/results?scenario=Base", tok, nil), nil
	},
	"compareForecastScenarios": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/forecasts/"+matrixPlanForecast(e).forecast+"/comparison", tok, nil), nil
	},
	"runForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixPlanForecast(e)
		was := matrixPlanForecastState(e, f)
		r := e.call(http.MethodPost, "/v1/forecasts:run?scenario=Base", tok, matrixForecastBody())
		return r, func() bool { return matrixPlanForecastState(e, f) == was }
	},
}
