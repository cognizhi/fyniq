//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The opening balances: saved as a draft dated at the books start, a version at a time at the version read, checked,
// and posted once as entered, share capital included, in the base currency, netting to zero.
// req: FR-GL-06
// req: ledger/posting-rules Opening balances
// req: INV-13
func TestOpeningBalances(t *testing.T) {
	e := newEnv(t)
	var capital storage.Account
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		capital, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3010", Name: "Share capital, ordinary", Type: storage.Equity, Role: storage.RoleShareCapital})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	accountant := e.as(authz.Accountant)
	line := func(account, amount, currency string) map[string]any {
		return map[string]any{"account_id": account, "amount": map[string]any{"amount": amount, "currency": currency}}
	}
	bank, sales, shares := e.books.Bank.ID.String(), e.books.Sales.ID.String(), capital.ID.String()
	draft := func(lines ...map[string]any) map[string]any { return map[string]any{"memo": "From the 30 September trial balance", "lines": lines} }
	save := func(tag string, body map[string]any) response {
		return e.call(http.MethodPut, "/v1/opening-balances", accountant, body, "If-Match", tag)
	}

	// None yet; they need the books start to have a date.
	r := e.call(http.MethodGet, "/v1/opening-balances", accountant, nil)
	if r.Status != 200 || r.Body["status"] != "none" || r.Header.Get("ETag") != `"0-none"` || len(r.Body["lines"].([]any)) != 0 {
		t.Fatalf("no opening balances: %d %s %v", r.Status, r.Raw, r.Header)
	}
	balanced := draft(line(bank, "10000.00", "SGD"), line(shares, "-10000.00", "SGD"))
	e.problemIs(save(`"0-none"`, balanced), "replaceOpeningBalances", 409, journal.CodeNoBooksStart)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Controller), nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller),
		map[string]any{"fy_start_month": 1, "books_start": "2026-10-01", "first_year_end": "2026-12-31"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the books start: %d %s", p.Status, p.Raw)
	}

	// The first save creates them, at the books start, with who saved them.
	e.problemIs(e.call(http.MethodPut, "/v1/opening-balances", accountant, balanced), "replaceOpeningBalances", 428, CodeIfMatchRequired)
	pv := e.call(http.MethodPut, "/v1/opening-balances", accountant, balanced, "If-Match", `"0-none"`, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["status"] != "draft" {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/opening-balances", accountant, nil); g.Body["status"] != "none" {
		t.Fatalf("the preview saved them: %s", g.Raw)
	}
	r = save(`"0-none"`, balanced)
	if r.Status != 200 || r.Body["status"] != "draft" || r.Body["version"] != float64(1) || r.Body["date"] != "2026-10-01" ||
		r.str("prepared_by", "kind") != "user" || len(r.Body["lines"].([]any)) != 2 {
		t.Fatalf("the first save: %d %s", r.Status, r.Raw)
	}
	first := r.Header.Get("ETag")
	e.problemIs(save(`"0-none"`, balanced), "replaceOpeningBalances", 412, CodeStale)

	// They must net to zero and be in the base currency; every problem is said when they are posted, or checked.
	post := func(tag string, headers ...string) response {
		return e.call(http.MethodPost, "/v1/opening-balances/post", accountant, nil, append([]string{"If-Match", tag}, headers...)...)
	}
	r = save(first, draft(line(bank, "10000.00", "SGD"), line(shares, "-9000.00", "SGD")))
	e.problemIs(post(r.Header.Get("ETag")), "postOpeningBalances", 422, ledger.CodeUnbalancedBase)
	r = save(r.Header.Get("ETag"), draft(line(bank, "100.00", "USD"), line(shares, "-135.00", "SGD")))
	e.problemIs(post(r.Header.Get("ETag")), "postOpeningBalances", 422, ledger.CodeForeignOpening)

	// Balanced, with share capital (INV-13): the check shows the entry and saves nothing; then they post once.
	r = save(r.Header.Get("ETag"), draft(line(bank, "12000.00", "SGD"), line(sales, "-2000.00", "SGD"), line(shares, "-10000.00", "SGD")))
	ready := r.Header.Get("ETag")
	if r.Body["version"] != float64(4) {
		t.Fatalf("the fourth version: %s", r.Raw)
	}
	check := post(ready, "Prefer", "return=preview")
	if check.Status != 200 || check.str("entry", "posting_date") != "2026-10-01" || len(check.Body["entry"].(map[string]any)["lines"].([]any)) != 3 {
		t.Fatalf("the check: %d %s", check.Status, check.Raw)
	}
	r = post(ready, "Idempotency-Key", "post-opening")
	if r.Status != 200 || r.str("opening_balances", "status") != "posted" || r.str("opening_balances", "posted_by", "kind") != "user" ||
		r.str("entry", "posting_identity")[:len("opening_balance:")] != "opening_balance:" || r.str("entry", "rule_version") != ledger.RuleOpeningBalance {
		t.Fatalf("posting: %d %s", r.Status, r.Raw)
	}
	if again := post(ready, "Idempotency-Key", "post-opening"); again.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("the replay: %d %v", again.Status, again.Header)
	}
	posted := r.Header.Get("ETag")
	e.problemIs(post(posted), "postOpeningBalances", 409, journal.CodeNotDraft)
	e.problemIs(save(posted, balanced), "replaceOpeningBalances", 409, journal.CodeNotDraft)

	// They are not a journal: the journal operations refuse their document, and the journal list leaves them out.
	e.problemIs(e.call(http.MethodGet, "/v1/journals/"+r.str("opening_balances", "id"), accountant, nil), "getJournal", 404, journal.CodeNotJournal)
	if l := e.call(http.MethodGet, "/v1/journals", accountant, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("the journal list shows the opening balances: %s", l.Raw)
	}

	// The matrix: the Owner reads them; a Tax agent prepares but does not post; a Bookkeeper sees nothing.
	if g := e.call(http.MethodGet, "/v1/opening-balances", e.as(authz.Owner), nil); g.Status != 200 || g.Body["status"] != "posted" {
		t.Errorf("the Owner reads them: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/opening-balances/post", e.as(authz.TaxAgent), nil, "If-Match", posted), "postOpeningBalances", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/opening-balances", e.as(authz.Bookkeeper), nil), "getOpeningBalances", 403, CodeForbidden)
}
