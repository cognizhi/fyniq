//go:build local

package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/unusual"
)

// FM-6 at launch (rev 0.287): in Founder mode, the nightly review puts the last week's unusual journals and payments into
// the weekly review, each once a rule, with its reason: a manual journal well above the company's usual and a round sum,
// a back-dated one, payments soon after their payee's bank details changed, a payee's first payment and a possible
// duplicate. Ordinary postings are not flagged; nothing is approved; a run again logs nothing new; the founder who
// posted an item cannot review it, the other founder can; and the worker can log one only for a posted journal or
// payment, naming nobody but who posted it.
// req: FM-6
// asvs: V13.2.2
func TestUnusualItemsEnterTheWeeklyReview(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ctx := context.Background()
	// The database stamps when each document posted, and the application's clock when each approval was decided: here
	// both are now, as they are in production.
	e.clock.Set(dbNow(t))
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	journal := func(amount, date string) string {
		t.Helper()
		d := e.draft(amount)
		d["date"] = date
		r := e.call(http.MethodPost, "/v1/journals", ahkow, d)
		if r.Status != http.StatusCreated {
			t.Fatalf("a journal of %s: %d %s", amount, r.Status, r.Raw)
		}
		id, tag := r.str("id"), r.Header.Get("ETag")
		if p := e.call(http.MethodPost, "/v1/journals/"+id+"/post", ahkow, nil, "If-Match", tag); p.Status == http.StatusOK {
			return id
		}
		// Above the self-approval threshold, Wei Ling approves its posting (FM-2).
		asked := e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil, "If-Match", tag)
		if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); asked.Status != http.StatusAccepted || a.Status != http.StatusOK {
			t.Fatalf("posting %s: %d %s / %d %s", amount, asked.Status, asked.Raw, a.Status, a.Raw)
		}
		return id
	}
	var usual []string
	for range 5 {
		usual = append(usual, journal("500.00", "2026-11-16"))
	}
	large := journal("12000.00", "2026-11-16")
	late := journal("300.00", "2026-10-05")

	// The supplier's bank details change, approved by Wei Ling; the next hours, two payments of the same sum.
	supplier := e.supplier()
	id, tag := e.bankChange(supplier, ahkow, sgAccount("012-345678-9"))
	e.bankLetter(id, ahkow)
	asked := e.call(http.MethodPost, "/v1/bank-changes/"+id+"/approval", ahkow, nil, "If-Match", tag)
	if r := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); asked.Status != http.StatusAccepted || r.Status != http.StatusOK {
		t.Fatalf("the bank change: %d %s / %d %s", asked.Status, asked.Raw, r.Status, r.Raw)
	}
	first, _ := e.recordedPayment()
	second, _ := e.recordedPayment()
	e.clock.Set(dbNow(t).Add(time.Minute)) // the night's run

	ws := workerStore(t)
	n, err := unusual.Review(ctx, ws, e.books.Org, e.clock.Now())
	if err != nil || n != 7 {
		t.Fatalf("Review = %d, %v; want 7 unusual items", n, err)
	}
	if n, err := unusual.Review(ctx, ws, e.books.Org, e.clock.Now()); err != nil || n != 0 {
		t.Fatalf("a second run = %d, %v; want nothing new", n, err)
	}

	l := e.call(http.MethodGet, "/v1/review-log?status=all&page_size=200", weiling, nil)
	type entry struct{ id, key, summary, approval string }
	var flagged []entry
	may := map[string]bool{}
	for _, x := range l.Body["items"].([]any) {
		m := x.(map[string]any)
		if m["kind"] != "unusual" {
			continue
		}
		approval, _ := m["approval_id"].(string)
		flagged = append(flagged, entry{m["id"].(string), m["object_key"].(string), m["summary"].(string), approval})
		may[m["id"].(string)] = m["you_may_review"] == true
	}
	has := func(key, prefix, contains string) entry {
		t.Helper()
		i := slices.IndexFunc(flagged, func(x entry) bool {
			return x.key == key && strings.HasPrefix(x.summary, prefix) && strings.Contains(x.summary, contains)
		})
		if i < 0 {
			t.Fatalf("no %q item for %s with %q: %+v", prefix, key, contains, flagged)
		}
		return flagged[i]
	}
	has(large, "Well above the usual: ", "debits 12,000.00 SGD, more than three times the company's usual manual journal (500.00 SGD")
	has(large, "A round sum: ", "debits exactly 12,000.00 SGD")
	backdated := has(late, "Back-dated: ", "is dated 5 Oct 2026 and was posted ")
	has(first, "A first payment: ", "is the first payment to Atlassian Pty Ltd: 10.90 SGD")
	has(first, "Soon after a bank change: ", "pays 10.90 SGD to Atlassian Pty Ltd 0 day(s) after its bank details changed")
	has(second, "Soon after a bank change: ", "10.90 SGD")
	has(second, "A possible duplicate: ", "pays 10.90 SGD to Atlassian Pty Ltd, as ")
	if len(flagged) != 7 {
		t.Fatalf("%d unusual items: %+v", len(flagged), flagged)
	}
	for _, x := range flagged {
		if slices.Contains(usual, x.key) || x.approval != "" {
			t.Errorf("an ordinary journal flagged, or an item tied to an approval: %+v", x)
		}
	}

	// Ah Kow posted the back-dated journal: Wei Ling reviews it, he cannot.
	mine := e.call(http.MethodGet, "/v1/review-log?status=all&page_size=200", ahkow, nil)
	if !may[backdated.id] || !strings.Contains(mine.Raw, `"id":"`+backdated.id+`"`) {
		t.Fatalf("Wei Ling may not review the back-dated journal's item: %s", l.Raw)
	}
	for _, x := range mine.Body["items"].([]any) {
		if m := x.(map[string]any); m["id"] == backdated.id && m["you_may_review"] == true {
			t.Errorf("Ah Kow may review the item of the journal he posted: %v", m)
		}
	}
	// The worker's fence (FY201): no item for a draft, for a document of another kind, or naming someone else.
	draft, _ := e.createDraft("1.00")
	for what, row := range map[string][]any{
		"a draft":                {"journal", draft, nil},
		"a payment as a journal": {"journal", first, nil},
		"Wei Ling for Ah Kow's":  {"journal", late, f.weiling},
	} {
		if err := workerLogs(t, e, row[0].(string), row[1].(string), row[2]); err == nil || !strings.Contains(err.Error(), "FY201") {
			t.Errorf("the worker logged an unusual item for %s: %v", what, err)
		}
	}
	if r := e.call(http.MethodPost, "/v1/review-log/reviews", weiling, map[string]any{"entry_ids": []string{backdated.id}, "note": "Accrual agreed with the auditor"},
		"Idempotency-Key", "unusual-review-1"); r.Status != http.StatusOK {
		t.Fatalf("Wei Ling's review: %d %s", r.Status, r.Raw)
	}
}

// workerLogs inserts an unusual item as the worker's database role, as a forged run would.
func workerLogs(t *testing.T, e *env, objectType, key string, actor any) error {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", e.books.Org.String()); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO fyniq.review_log_entry (kind, object_type, object_key, summary, actor, logged_at)
		VALUES ('unusual', $1, $2, 'forged', $3, now())`, objectType, key, actor)
	return err
}

// dbNow is the database's time.
func dbNow(t *testing.T) time.Time {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	var now time.Time
	if err := c.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now.UTC()
}
