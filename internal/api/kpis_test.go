//go:build local

package api

import (
	"context"
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

// The KPIs through the API (FR-KPI-02, FR-KPI-03, RND-KPI; rev 0.161), worked by hand. October: 100,000.00 of share
// capital in the bank; 4,000.00 of subscriptions invoiced and 3,000.00 of it received; advertising 2,000.00 and rent
// 6,000.00 paid; hosting 500.00 on credit. November: 5,000.00 of subscriptions invoiced, 1,000.00 of services paid in
// cash, 4,000.00 received; advertising 1,000.00 and rent 6,000.00 paid; October's hosting paid, November's 600.00 on
// credit. So November's revenue is 6,000.00 (5,000.00 subscriptions), gross profit 5,400.00, EBITDA and net profit
// -1,600.00; net burn 2,500.00 (October's 5,000.00); at its end cash 92,500.00, receivables 2,000.00, payables 600.00,
// equity 93,900.00. With 5 paying customers, 1 new and none lost, and 4 people entered for November:
// ARPA 1,000.00; churn 0.0%, so the LTV is unbounded; CAC 1,000.00; payback 1,000 ÷ 900 = 1.1 months; runway
// 92,500 ÷ 3,750 = 24.7 months; burn multiple 2,500 ÷ 12,000 = 0.2; DSO 2,000 ÷ (5,000 ÷ 30) = 12.0 days; DPO 600 ÷
// (8,050 ÷ 30) = 2.2 days; the year to date's net profit, October's and November's, -6,100.00. October had no
// counts: its customer KPIs have none.
// req: FR-KPI-02
// req: FR-KPI-03
// req: RND-KPI
// asvs: V8.2.1
func TestKPIsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	acc := map[string]storage.Account{}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []storage.NewAccount{
			{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{ReportGroup: "Cash & bank", CashFlowLine: "Cash & cash equivalents"}},
			{Code: "1100", Name: "Trade receivables", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{ReportGroup: "Trade receivables", CashFlowLine: "Receivables"}},
			{Code: "2010", Name: "Trade payables", Type: storage.Liability, AccountAttributes: storage.AccountAttributes{ReportGroup: "Trade payables", CashFlowLine: "Payables & accruals"}},
			{Code: "3010", Name: "Share capital", Type: storage.Equity, Role: storage.RoleShareCapital, AccountAttributes: storage.AccountAttributes{ReportGroup: "Share capital", CashFlowLine: "Share capital"}},
			{Code: "4010", Name: "Subscriptions", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Subscription revenue"}},
			{Code: "4020", Name: "Services", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Professional services"}},
			{Code: "5010", Name: "Hosting", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Hosting", Function: "COGS"}},
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
	}); err != nil {
		t.Fatal(err)
	}
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
	post(ledger.SourceOpeningBalance, 10, 1, "1010", "3010", "100000.00")
	post(mj, 10, 5, "1100", "4010", "4000.00")
	post(mj, 10, 12, "1010", "1100", "3000.00")
	post(mj, 10, 15, "6110", "1010", "2000.00")
	post(mj, 10, 20, "6200", "1010", "6000.00")
	post(mj, 10, 25, "5010", "2010", "500.00")
	post(mj, 11, 5, "1100", "4010", "5000.00")
	post(mj, 11, 6, "1010", "4020", "1000.00")
	post(mj, 11, 12, "1010", "1100", "4000.00")
	post(mj, 11, 15, "6110", "1010", "1000.00")
	post(mj, 11, 20, "6200", "1010", "6000.00")
	post(mj, 11, 22, "2010", "1010", "500.00")
	post(mj, 11, 25, "5010", "2010", "600.00")

	accountant := e.as(authz.Accountant)
	// The counts: If-Match needed, "0" for a month without them; the three customer counts together.
	e.problemIs(e.call(http.MethodPut, "/v1/kpi-inputs/2026-11", accountant, map[string]any{"headcount": 4}), "setKpiInput", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, "/v1/kpi-inputs/2026-11", accountant, map[string]any{"paying_customers": 5}, "If-Match", `"0"`), "setKpiInput", 422, CodeInvalid)
	set := e.call(http.MethodPut, "/v1/kpi-inputs/2026-11", accountant, map[string]any{"paying_customers": 5, "new_customers": 1, "lost_customers": 0, "headcount": 4},
		"If-Match", `"0"`)
	if set.Status != http.StatusOK || set.Header.Get("ETag") != `"1"` || set.Body["version"].(float64) != 1 {
		t.Fatalf("entering November's counts = %d: %s", set.Status, set.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/kpi-inputs/2026-11", accountant, map[string]any{"headcount": 5}, "If-Match", `"0"`), "setKpiInput", 412, CodeStale)
	e.problemIs(e.call(http.MethodPut, "/v1/kpi-inputs/2026-11", e.as(authz.BoardViewer), map[string]any{"headcount": 5}, "If-Match", `"1"`), "setKpiInput", 403, "API-AUTHZ-01")

	r := e.call(http.MethodGet, "/v1/kpis?month=2026-11", accountant, nil)
	if r.Status != http.StatusOK || r.str("from") != "2026-11-01" || r.str("currency") != "SGD" || r.Body["counts"].(map[string]any)["headcount"].(float64) != 4 {
		t.Fatalf("November's KPIs = %d: %.400s", r.Status, r.Raw)
	}
	figures := map[string]string{}
	before := map[string]string{}
	for _, x := range r.Body["kpis"].([]any) {
		k := x.(map[string]any)
		show := func(f map[string]any) string {
			switch {
			case f["value"] != nil:
				return f["value"].(string)
			case f["words"] != nil:
				return f["words"].(string)
			}
			return "— " + f["undefined"].(string)
		}
		figures[k["key"].(string)] = show(k["month"].(map[string]any))
		if b, ok := k["before"].(map[string]any); ok {
			before[k["key"].(string)] = show(b)
		}
	}
	for key, want := range map[string]string{
		"cash": "92500.00", "revenue": "6000.00", "net_profit_ytd": "-6100.00", "owed_to_us": "2000.00", "owed_by_us": "600.00",
		"booked_mrr": "5000.00", "arr": "60000.00", "revenue_growth": "50.0", "gross_margin": "90.0", "ebitda_margin": "-26.7", "net_margin": "-26.7",
		"spend_sm": "1000.00", "spend_rd": "0.00", "spend_ga": "6000.00", "net_burn": "2500.00", "runway": "24.7", "arpa": "1000.00", "churn": "0.0",
		"cac": "1000.00", "ltv": "— no customers lost, so a lifetime is not bounded", "cac_payback": "1.1", "burn_multiple": "0.2",
		"current_ratio": "157.5", "quick_ratio": "157.5", "working_capital": "93900.00", "debt_equity": "0.0", "dso": "12.0", "dpo": "2.2",
		"headcount": "4", "revenue_per_employee": "18000.00", "contracted_mrr": "— no subscription data",
	} {
		if figures[key] != want {
			t.Errorf("November's %s = %q, want %q", key, figures[key], want)
		}
	}
	for key, want := range map[string]string{"booked_mrr": "4000.00", "runway": "19.0", "net_burn": "5000.00", "churn": "— no subscription data: enter the month's customer counts",
		"revenue_growth": "— the books have no month before"} {
		if before[key] != want {
			t.Errorf("October's %s = %q, want %q", key, before[key], want)
		}
	}
	if !strings.Contains(r.Raw, `"key":"arpa"`) || !strings.Contains(r.Raw, `"version":"runway@1"`) || !strings.Contains(r.Raw, `"entered":true`) {
		t.Errorf("the definitions and their sources: %.300s", r.Raw)
	}

	// Who reads them: the board pack's view does, a sales view does not; the counts list pages.
	if b := e.call(http.MethodGet, "/v1/kpis?month=2026-11", e.as(authz.BoardViewer), nil); b.Status != http.StatusOK {
		t.Errorf("the board viewer reading the KPIs = %d", b.Status)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/kpis?month=2026-11", e.as(authz.ARSpecialist), nil), "getKpis", 403, "API-AUTHZ-01")
	if l := e.call(http.MethodGet, "/v1/kpi-inputs?from=2026-10&to=2026-11", accountant, nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the months' counts = %d: %s", l.Status, l.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"action":"kpi_input.entered"`) {
		t.Errorf("the counts' audit event: %s", trail)
	}
	if r := e.call(http.MethodGet, "/v1/kpis?month=2030-01", accountant, nil); r.Status != 422 || !strings.Contains(r.Raw, `"code":"`+CodeNoPeriod+`"`) {
		t.Errorf("a month the books have no period for: %d %s", r.Status, r.Raw)
	}
}
