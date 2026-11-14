//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The approver sees exactly what approving would post (FR-APR-02; PRD-001 rev 0.177): a journal above the threshold
// waits, and its request shows the entry approving it would post now, debits positive, with the balances of Rent and
// the bank before and after; working it out keeps nothing. A change that posts nothing shows no impact. Once October
// is closed, the request shows what would refuse it.
// req: FR-APR-02
// req: access/approvals Approval view
func TestAnApproverSeesWhatApprovingWouldPost(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Accountant, authz.Controller), f.as(f.weiling, authz.Accountant, authz.Controller)
	e.postedJournal("100.00")
	r := e.call(http.MethodPost, "/v1/journals", ahkow, e.draft("2500.00"))
	id, tag := r.str("id"), r.Header.Get("ETag")
	asked := e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil, "If-Match", tag)
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to post: %d %s", asked.Status, asked.Raw)
	}
	revision := func() (rev int64) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			rev, err = u.Ledger().Revision(ctx)
			return err
		})
		return rev
	}
	before := revision()

	g := e.call(http.MethodGet, "/v1/approvals/"+asked.str("id"), weiling, nil)
	impact, _ := g.Body["impact"].(map[string]any)
	if impact == nil || impact["refused"] != nil {
		t.Fatalf("no impact: %s", g.Raw)
	}
	entries := impact["entries"].([]any)
	lines := entries[0].(map[string]any)["lines"].([]any)
	if len(entries) != 1 || len(lines) != 2 || lines[0].(map[string]any)["amount"].(map[string]any)["amount"] != "2500.00" {
		t.Errorf("the entry: %v", entries)
	}
	got := map[string][2]string{}
	for _, b := range impact["balances"].([]any) {
		m := b.(map[string]any)
		got[m["account_id"].(string)] = [2]string{m["before"].(map[string]any)["amount"].(string), m["after"].(map[string]any)["amount"].(string)}
	}
	if got[e.books.Rent.ID.String()] != [2]string{"100.00", "2600.00"} || got[e.books.Bank.ID.String()] != [2]string{"-100.00", "-2600.00"} {
		t.Errorf("the balances: %v", got)
	}
	if revision() != before || e.call(http.MethodGet, "/v1/journals/"+id, weiling, nil).str("status") != "draft" {
		t.Errorf("working out the impact kept something")
	}

	// A change that posts nothing shows no impact.
	s := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", f.as(f.ahkow), map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if s.Status != http.StatusAccepted {
		t.Fatalf("asking to change a setting: %d %s", s.Status, s.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/approvals/"+s.str("id"), weiling, nil); g.Status != http.StatusOK || g.Body["impact"] != nil {
		t.Errorf("a setting's request shows an impact: %s", g.Raw)
	}

	// Once October is closed, the request shows what would refuse it.
	f.closeMonth(e.books.Oct.ID.String())
	g = e.call(http.MethodGet, "/v1/approvals/"+asked.str("id"), weiling, nil)
	if refused, _ := g.Body["impact"].(map[string]any)["refused"].(map[string]any); refused == nil || refused["code"] != storage.CodePeriodClosed {
		t.Errorf("the refusal: %s", g.Raw)
	}
}
