//go:build local

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The financial statements through the API (FR-RPT-01, FR-RPT-08), worked by hand. October: opening balances put
// 1,000.00 in the bank from share capital; a sale of 500.00 is received; 100.00 of advertising is paid. The P&L:
// revenue 500.00, operating expenses 100.00 (20.0%), net profit 400.00 (80.0%). The balance sheet at 31 October: the
// bank 1,400.00; equity 1,000.00 and the profit 400.00. Read again as at its revision after a back-dated 50.00, the
// P&L is unchanged; the cash flow ties the profit to the bank. Once the advertising account is renamed, the re-run is flagged restated presentation. A month
// without a period and a revision later than the ledger are refused.
// req: FR-RPT-01
// req: FR-RPT-08
// req: G4-051
// req: INV-11
func TestFinancialStatementsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	acc := map[string]storage.Account{}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []storage.NewAccount{
			{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{ReportGroup: "Cash & bank", CashFlowLine: "Cash & cash equivalents"}},
			{Code: "3010", Name: "Share capital", Type: storage.Equity, Role: storage.RoleShareCapital, AccountAttributes: storage.AccountAttributes{ReportGroup: "Share capital", CashFlowLine: "Share capital"}},
			{Code: "4010", Name: "Subscription revenue", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Subscription revenue"}},
			{Code: "6110", Name: "Advertising", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Sales & marketing", Function: "S&M"}},
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
	post := func(source string, day int, dr, cr, amount string) {
		t.Helper()
		id := uuid.NewString()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, day), PostingIdentity: source + ":" + id + ":v1:test@1",
				RuleVersion: "test@1", Source: storage.Source{Type: source, ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: acc[dr].ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
					{AccountID: acc[cr].ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
				}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	post(ledger.SourceOpeningBalance, 1, "1010", "3010", "1000.00")
	post(ledger.SourceManualJournal, 5, "1010", "4010", "500.00")
	post(ledger.SourceManualJournal, 9, "6110", "1010", "100.00")

	block := func(r response, key string) map[string]any {
		t.Helper()
		for _, b := range r.Body["blocks"].([]any) {
			if m := b.(map[string]any); m["key"] == key {
				return m
			}
		}
		t.Fatalf("no block %s in %s", key, r.Raw)
		return nil
	}
	first := func(m map[string]any, field string) string {
		v := m[field].([]any)[0]
		if x, ok := v.(map[string]any); ok {
			return x["amount"].(string)
		}
		return v.(string)
	}
	pl := e.call(http.MethodGet, "/v1/reports/statements/pl?from=2026-10&to=2026-10", e.as(authz.Accountant), nil)
	if pl.Status != 200 || pl.Body["statement_title"] != "Management statements" || pl.Body["from"] != "2026-10-01" || pl.Body["to"] != "2026-10-31" ||
		pl.Body["restated_presentation"] != false {
		t.Fatalf("October's P&L: %d %s", pl.Status, pl.Raw)
	}
	if b := block(pl, "revenue"); first(b, "totals") != "500.00" || first(b, "percents") != "100.0" {
		t.Errorf("revenue: %v", b)
	}
	if b := block(pl, "operating_expenses"); first(b, "totals") != "100.00" || first(b, "percents") != "20.0" {
		t.Errorf("operating expenses: %v", b)
	}
	if b := block(pl, "net_profit"); first(b, "totals") != "400.00" || first(b, "percents") != "80.0" || b["measure"] != true {
		t.Errorf("net profit: %v", b)
	}
	revision := int64(pl.Body["ledger_revision"].(float64))

	bs := e.call(http.MethodGet, "/v1/reports/statements/bs?to=2026-10", e.as(authz.Auditor), nil)
	if first(block(bs, "current_assets"), "totals") != "1400.00" || first(block(bs, "equity"), "totals") != "1400.00" ||
		!strings.Contains(bs.Raw, `"name":"Profit for the year to date","totals":[{"amount":"400.00","currency":"SGD"}]`) {
		t.Errorf("31 October's balance sheet: %s", bs.Raw)
	}

	if first(block(bs, "equity_movement"), "totals") != "1400.00" {
		t.Errorf("the movement in equity: %s", bs.Raw)
	}
	// The cash flow: the profit of 400.00 is what the sale and the advertising moved; the opening 1,000.00 is where the
	// books start, not a flow.
	cf := e.call(http.MethodGet, "/v1/reports/statements/cf?from=2026-10&to=2026-10", e.as(authz.Accountant), nil)
	if cf.Status != 200 || first(block(cf, "net_operating"), "totals") != "400.00" || first(block(cf, "cash_at_start"), "totals") != "1000.00" ||
		first(block(cf, "cash_at_end"), "totals") != "1400.00" || strings.Contains(cf.Raw, `"ok":false`) || !strings.Contains(cf.Raw, `"non_cash":[]`) {
		t.Errorf("October's cash flow: %d %s", cf.Status, cf.Raw)
	}

	// G4-051: a back-dated 50.00 of advertising after the revision leaves October as at it unchanged.
	post(ledger.SourceManualJournal, 20, "6110", "1010", "50.00")
	at := "/v1/reports/statements/pl?from=2026-10&to=2026-10&as_at_revision=" + strconv.FormatInt(revision, 10)
	if again := e.call(http.MethodGet, at, e.as(authz.Accountant), nil); first(block(again, "net_profit"), "totals") != "400.00" ||
		again.Body["restated_presentation"] != false || int64(again.Body["ledger_revision"].(float64)) != revision {
		t.Errorf("October as at revision %d: %s", revision, again.Raw)
	}
	if now := e.call(http.MethodGet, "/v1/reports/statements/pl?from=2026-10&to=2026-10", e.as(authz.Accountant), nil); first(block(now, "net_profit"), "totals") != "350.00" {
		t.Errorf("October now: %s", now.Raw)
	}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a := acc["6110"]
		_, err := u.Accounts().Update(ctx, a.ID, a.Version, storage.AccountChange{Name: "Advertising & events", Active: true, AccountAttributes: a.AccountAttributes})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if again := e.call(http.MethodGet, at, e.as(authz.Accountant), nil); again.Body["restated_presentation"] != true ||
		!strings.Contains(again.Raw, `"name":"Advertising & events"`) {
		t.Errorf("as at the revision after a rename: %s", again.Raw)
	}

	for path, code := range map[string]string{
		"/v1/reports/statements/pl?from=2027-01&to=2027-01": CodeNoPeriod,
		at + "000": CodeLaterRevision,
		"/v1/reports/statements/pl?from=2026-11&to=2026-10": CodeInvalid,
	} {
		if r := e.call(http.MethodGet, path, e.as(authz.Accountant), nil); r.Status != 422 || !strings.Contains(r.Raw, `"code":"`+code+`"`) {
			t.Errorf("%s: %d %s", path, r.Status, r.Raw)
		}
	}
}
