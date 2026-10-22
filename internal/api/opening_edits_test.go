//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Posted opening balances change only on another person's approval, in every team mode (FM-1 lists editing opening
// balances; rev 0.66): the request keeps the new content, which must pass the posting rules, and shows what changes;
// on the approval their next version posts as a reversal of the entry they had and the new entry, at the books start,
// as the approver, with the requester as the one who prepared it.
// req: FR-GL-06
// req: FR-GL-12
// req: FM-1
// req: FR-APR-02
func TestChangingPostedOpeningBalances(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	var capital storage.Account
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		capital, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3010", Name: "Share capital, ordinary", Type: storage.Equity, Role: storage.RoleShareCapital})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ahkow, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ahkow,
		map[string]any{"fy_start_month": 1, "books_start": "2026-10-01", "first_year_end": "2026-12-31"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the books start: %d %s", p.Status, p.Raw)
	}
	line := func(account, amount string) map[string]any {
		return map[string]any{"account_id": account, "amount": map[string]any{"amount": amount, "currency": "SGD"}}
	}
	bank, sales, shares := e.books.Bank.ID.String(), e.books.Sales.ID.String(), capital.ID.String()
	saved := e.call(http.MethodPut, "/v1/opening-balances", ahkow, map[string]any{"lines": []any{line(bank, "10000.00"), line(shares, "-10000.00")}},
		"If-Match", `"0-none"`)
	posted := e.call(http.MethodPost, "/v1/opening-balances/post", ahkow, nil, "If-Match", saved.Header.Get("ETag"))
	if posted.Status != 200 {
		t.Fatalf("posting: %d %s", posted.Status, posted.Raw)
	}
	etag := posted.Header.Get("ETag")
	ask := func(who string, body map[string]any, headers ...string) response {
		return e.call(http.MethodPost, "/v1/opening-balances/edit-approval", who, body, headers...)
	}
	change := map[string]any{"memo": "From the bank statement", "lines": []any{line(bank, "12000.00"), line(shares, "-10000.00"), line(sales, "-2000.00")},
		"reason": "The bank balance at the books start was 12,000.00"}

	// Refused: no reason, no If-Match, content the posting rules refuse, a draft.
	e.problemIs(ask(ahkow, map[string]any{"lines": change["lines"], "reason": " "}, "If-Match", etag), "requestOpeningBalancesEdit", 422, CodeInvalid)
	e.problemIs(ask(ahkow, change), "requestOpeningBalancesEdit", 428, CodeIfMatchRequired)
	e.problemIs(ask(ahkow, map[string]any{"lines": []any{line(bank, "12000.00"), line(shares, "-10000.00")}, "reason": "x"}, "If-Match", etag),
		"requestOpeningBalancesEdit", 422, ledger.CodeUnbalancedBase)

	// Asked: what changes, for the approver; the envelope holds the content's hash, not the lines.
	asked := ask(ahkow, change, "If-Match", etag)
	if asked.Status != 202 || asked.str("operation") != approval.OpOpeningEdit || asked.str("object_type") != approval.ObjectOpening ||
		!strings.HasSuffix(asked.str("summary"), ": The bank balance at the books start was 12,000.00") ||
		!strings.Contains(asked.Raw, `{"field":"debits","before":"10000.00","after":"12000.00"}`) ||
		!strings.Contains(asked.Raw, `{"field":"memo","before":"","after":"From the bank statement"}`) ||
		!strings.Contains(asked.Raw, `{"field":"line 3","before":"","after":"4000 Sales: -2000.00 SGD"}`) ||
		!strings.Contains(asked.str("envelope"), `"content_sha256":"`) || strings.Contains(asked.str("envelope"), `"lines"`) {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/opening-balances", weiling, nil); g.str("edit_asked") != asked.str("id") || g.Body["version"] != float64(1) {
		t.Errorf("the opening balances do not name the request, or changed: %s", g.Raw)
	}
	e.problemIs(ask(weiling, change, "If-Match", etag), "requestOpeningBalancesEdit", 409, approval.CodePending)

	// The requester never approves it; another founder does, and it posts as them.
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	g := e.call(http.MethodGet, "/v1/opening-balances", ahkow, nil)
	if g.Header.Get("ETag") != `"2-posted"` || g.str("memo") != "From the bank statement" || len(g.Body["lines"].([]any)) != 3 ||
		g.str("prepared_by", "id") != f.ahkow || g.str("posted_by", "id") != f.weiling || g.str("edit_asked") != "" || g.str("date") != "2026-10-01" {
		t.Fatalf("after the approval: %s", g.Raw)
	}
	// The ledger keeps both: the first entry, its reversal and the new one, all at the books start.
	gl := e.call(http.MethodGet, "/v1/reports/general-ledger?account_id="+bank+"&from=2026-10-01&to=2026-10-31", weiling, nil)
	acct := gl.Body["accounts"].([]any)[0].(map[string]any)
	if gl.Status != 200 || len(acct["lines"].([]any)) != 3 || acct["closing"].(map[string]any)["amount"] != "12000.00" {
		t.Fatalf("the bank's ledger: %d %s", gl.Status, gl.Raw)
	}
	if !strings.Contains(auditTrail(t, e), `"approval_ids":["`+asked.str("id")+`"],"action":"document.edited"`) {
		t.Errorf("the change's audit event does not name its approval")
	}
	// Once the month the books start in is closed, a change is refused when asked, not when approved.
	ctx := context.Background()
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	var oct storage.Period
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ps, err := u.Periods().List(ctx)
		for _, p := range ps {
			if p.StartsOn.Format("2006-01-02") == "2026-10-01" {
				oct = p
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	checklistDone(t, e.store, e.books.Org, oct.ID)
	c, err := e.api.closing.RunChecks(as("preparer"), e.books.Org, oct.ID)
	if err == nil {
		_, _, err = e.api.closing.Close(as("attester"), e.books.Org, oct.ID, c.ID, "Reviewed.")
	}
	if err != nil {
		t.Fatal(err)
	}
	e.problemIs(ask(ahkow, change, "If-Match", `"2-posted"`), "requestOpeningBalancesEdit", 409, storage.CodePeriodClosed)

	// Posted, they never change by a save or a second post.
	e.problemIs(e.call(http.MethodPut, "/v1/opening-balances", ahkow, map[string]any{"lines": change["lines"]}, "If-Match", `"2-posted"`), "replaceOpeningBalances", 409, journal.CodeNotDraft)
}
