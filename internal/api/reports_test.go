//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The general ledger of an account over a range: its balance before (from the cached balances of earlier periods and
// the earlier lines of the period that holds the first day), its lines in posting order with the running balance and
// their source, and its balance at the end; only for a view of every report.
// req: FR-GL-11
// req: ENG-04
func TestTheGeneralLedger(t *testing.T) {
	e := newEnv(t)
	standardMode(e) // a void in Founder mode needs a second person (FM-1)
	accountant := e.as(authz.Accountant)
	post := func(date, amount, memo string) string {
		t.Helper()
		d := e.draft(amount)
		d["date"], d["memo"] = date, memo
		r := e.call(http.MethodPost, "/v1/journals", accountant, d)
		p := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
		if p.Status != 200 {
			t.Fatalf("posting %s: %d %s", memo, p.Status, p.Raw)
		}
		return r.str("id")
	}
	post("2026-10-05", "30.00", "Early October")
	rent := post("2026-10-20", "100.00", "October rent")
	post("2026-11-03", "50.00", "November rent")
	g := e.call(http.MethodGet, "/v1/journals/"+rent, accountant, nil)
	if r := e.call(http.MethodPost, "/v1/journals/"+rent+"/reverse", accountant, map[string]any{"date": "2026-11-05", "reason": "Wrong month"},
		"If-Match", g.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("the reversal: %d %s", r.Status, r.Raw)
	}
	bank, rentAcc := e.books.Bank.ID.String(), e.books.Rent.ID.String()
	report := func(query string, who string) response {
		return e.call(http.MethodGet, "/v1/reports/general-ledger?"+query, who, nil)
	}
	money := func(m any) string { return m.(map[string]any)["amount"].(string) }

	// From 10 October: the 5 October line is before the range, in the same period.
	r := report("account_id="+bank+"&from=2026-10-10&to=2026-11-30", accountant)
	if r.Status != 200 || r.str("currency") != "SGD" || r.Body["ledger_revision"] != float64(4) {
		t.Fatalf("the report: %d %s", r.Status, r.Raw)
	}
	acc := r.Body["accounts"].([]any)[0].(map[string]any)
	lines := acc["lines"].([]any)
	if acc["code"] != e.books.Bank.Code || money(acc["opening"]) != "-30.00" || len(lines) != 3 || money(acc["closing"]) != "-80.00" ||
		money(acc["debits"]) != "100.00" || money(acc["credits"]) != "150.00" {
		t.Fatalf("the bank's ledger: %v", acc)
	}
	want := []struct {
		date, base, balance string
		reversal            bool
	}{{"2026-10-20", "-100.00", "-130.00", false}, {"2026-11-03", "-50.00", "-180.00", false},
		{"2026-11-05", "100.00", "-80.00", true}}
	for i, w := range want {
		l := lines[i].(map[string]any)
		if l["posting_date"] != w.date || money(l["base"]) != w.base || money(l["balance"]) != w.balance || l["reversal"] != w.reversal ||
			l["source_type"] != "manual_journal" {
			t.Errorf("line %d: %v", i, l)
		}
	}
	if l := lines[0].(map[string]any); l["source_id"] != rent || l["memo"] != "October rent" || l["number"] == nil {
		t.Errorf("the line's source: %v", l)
	}

	// From 1 November: October's cached balance is the opening.
	r = report("account_id="+bank+"&account_id="+rentAcc+"&from=2026-11-01&to=2026-11-30", accountant)
	accs := r.Body["accounts"].([]any)
	if len(accs) != 2 || money(accs[0].(map[string]any)["opening"]) != "-130.00" || money(accs[1].(map[string]any)["opening"]) != "130.00" ||
		money(accs[1].(map[string]any)["closing"]) != "80.00" {
		t.Fatalf("two accounts from November: %s", r.Raw)
	}

	// Refusals: dates, an unknown account, a view of part of the reports.
	e.problemIs(report("account_id="+bank+"&from=2026-11-30&to=2026-11-01", accountant), "getGeneralLedger", 422, CodeInvalid)
	e.problemIs(report("account_id=nope&from=2026-11-01&to=2026-11-30", accountant), "getGeneralLedger", 422, CodeInvalid)
	e.problemIs(report("account_id="+uuid.NewString()+"&from=2026-11-01&to=2026-11-30", accountant), "getGeneralLedger", 404, CodeNotFound)
	e.problemIs(report("account_id="+bank+"&from=2026-11-01&to=2026-11-30", e.as(authz.ARSpecialist)), "getGeneralLedger", 403, CodeForbidden)
	if r := report("account_id="+bank+"&from=2026-11-01&to=2026-11-30", e.as(authz.Owner)); r.Status != 200 {
		t.Errorf("the Owner reads the ledger: %d", r.Status)
	}
}

// Year-end (FR-GL-10; rev 0.56): once the financial years and the retained-earnings account are set, a general ledger
// range across the start of a financial year is refused, and one within a year is read.
// req: FR-GL-10
// req: FR-GL-11
func TestTheGeneralLedgerReadsOneFinancialYearAtATime(t *testing.T) {
	e := newEnv(t)
	controller := e.as(authz.Controller)
	g := e.call(http.MethodGet, "/v1/organisation/profile", controller, nil)
	if r := e.call(http.MethodPatch, "/v1/organisation/profile", controller, map[string]any{"fy_start_month": 11, "books_start": "2026-01-01",
		"first_year_end": "2026-10-31"}, "If-Match", g.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("setting the years: %d %s", r.Status, r.Raw)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3100", Name: "Retained earnings", Type: storage.Equity, Role: storage.RoleRetainedEarnings})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/reports/general-ledger?account_id=" + e.books.Sales.ID.String()
	e.problemIs(e.call(http.MethodGet, path+"&from=2026-10-01&to=2026-11-30", controller, nil), "getGeneralLedger", 422, balance.CodeCrossesYear)
	if r := e.call(http.MethodGet, path+"&from=2026-11-01&to=2026-11-30", controller, nil); r.Status != 200 {
		t.Fatalf("a range within the second year: %d %s", r.Status, r.Raw)
	}
}
