//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The nightly replay posts every stored entry again under the rule version that posted it, from the document version
// it names: posted journals, an edit's reversal and replacement, a void, and the opening balances all reproduce. A rule
// whose output changed under the same version, and a version the engine no longer has, are each reported, and the
// failure is recorded in the organisation's audit trail; nothing stored changes.
// req: ENG-06
// req: QA-ENG-06
func TestReplayReproducesEveryStoredEntry(t *testing.T) {
	e := newEnv(t)
	standardMode(e) // in Founder mode an edit or a void needs a second person (FM-1)
	accountant := e.as(authz.Accountant)
	id, tag := e.postedJournal("100.00")
	if r := e.call(http.MethodPost, "/v1/journals/"+id+"/edit", accountant, e.edit("120.00", "2026-10-22", "Rent was 120"), "If-Match", tag); r.Status != 200 {
		t.Fatalf("the edit: %d %s", r.Status, r.Raw)
	}
	other, otherTag := e.postedJournal("50.00")
	if r := e.call(http.MethodPost, "/v1/journals/"+other+"/reverse", accountant, map[string]any{"date": "2026-10-25", "reason": "Posted twice"},
		"If-Match", otherTag); r.Status != 200 {
		t.Fatalf("the void: %d %s", r.Status, r.Raw)
	}
	tag = e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Controller), nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller),
		map[string]any{"fy_start_month": 1, "books_start": "2026-10-01", "first_year_end": "2026-12-31"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the books start: %d %s", p.Status, p.Raw)
	}
	line := func(account, amount string) map[string]any {
		return map[string]any{"account_id": account, "amount": map[string]any{"amount": amount, "currency": "SGD"}}
	}
	saved := e.call(http.MethodPut, "/v1/opening-balances", accountant, map[string]any{"lines": []any{line(e.books.Bank.ID.String(), "900.00"),
		line(e.books.Sales.ID.String(), "-900.00")}}, "If-Match", `"0-none"`)
	if r := e.call(http.MethodPost, "/v1/opening-balances/post", accountant, nil, "If-Match", saved.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("the opening balances: %d %s", r.Status, r.Raw)
	}
	ctx := context.Background()
	replay := func() journal.Replayed {
		t.Helper()
		var r journal.Replayed
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = journal.Replay(ctx, u)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Every entry reproduces: the first version and its reversal, the edit, the voided journal and its reversal, the
	// opening balances.
	if r := replay(); r.Entries != 6 || len(r.Mismatches) != 0 {
		t.Fatalf("the replay: %d entries, %+v", r.Entries, r.Mismatches)
	}

	// A rule whose output changed under the same version: each entry it posted is reported, not its reversals.
	manual := ledger.Rules[ledger.RuleManualJournal]
	ledger.Rules[ledger.RuleManualJournal] = func(j ledger.Journal, s ledger.Settings) (ledger.Entry, error) {
		out, err := manual(j, s)
		out.Memo += " (changed)"
		return out, err
	}
	r := replay()
	ledger.Rules[ledger.RuleManualJournal] = manual
	if len(r.Mismatches) != 3 || !strings.Contains(r.Mismatches[0].Why, "memo:") || !strings.HasPrefix(r.Mismatches[0].PostingIdentity, "manual_journal:") {
		t.Errorf("a changed rule: %+v", r.Mismatches)
	}

	// A rule version the engine no longer has: reported, and recorded in the audit trail by the nightly job's step.
	opening := ledger.Rules[ledger.RuleOpeningBalance]
	delete(ledger.Rules, ledger.RuleOpeningBalance)
	org, err := journal.ReplayOrganisation(ctx, e.store, e.books.Org)
	ledger.Rules[ledger.RuleOpeningBalance] = opening
	if err != nil || len(org.Mismatches) != 1 || !strings.Contains(org.Mismatches[0].Why, ledger.CodeUnknownRule) {
		t.Fatalf("a missing rule version: %v %+v", err, org.Mismatches)
	}
	trail := auditTrail(t, e)
	if !strings.Contains(trail, `"action":"control.check.failed"`) || !strings.Contains(trail, `"ledger.replay"`) || !strings.Contains(trail, `"QA-ENG-06"`) {
		t.Errorf("the failed replay is not in the audit trail")
	}
	if r := replay(); len(r.Mismatches) != 0 {
		t.Errorf("the replay after the rules are restored: %+v", r.Mismatches)
	}
}
