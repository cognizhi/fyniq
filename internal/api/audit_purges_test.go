//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The purges after retention (AUD-09, AUD-10): listed for those with V on "Audit trail", the trail's with its last event
// and the ledger's with its boundary, each with its approval, counts and signed copy; with record_id, that record's
// purged postings too (none here). A Bookkeeper may not list them.
// req: AUD-09
// req: AUD-10
func TestThePurgesAfterRetentionAreListed(t *testing.T) {
	e := newEnv(t)
	e.postedJournal("100.00")
	if r := e.call(http.MethodGet, "/v1/audit/purges", e.as(authz.Auditor), nil); r.Status != 200 || len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("no purge yet: %d %s", r.Status, r.Raw)
	}
	if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 2)
		if err != nil || len(events) < 2 {
			return err
		}
		_, err = u.Audit().RecordBoundary(ctx, storage.NewAuditBoundary{LastSeq: events[1].Seq, LastHash: events[1].Hash,
			LastOccurredAt: time.Date(2019, 3, 31, 0, 0, 0, 0, time.UTC), Approval: "retention.purge_after_retention v1",
			Counts: map[string]int64{"events": 2}, AnchorKey: "audit-boundaries/x/2.json", WrittenAt: time.Date(2027, 4, 4, 2, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := e.call(http.MethodGet, "/v1/audit/purges?record_id="+uuid.NewString(), e.as(authz.Auditor), nil)
	items, _ := r.Body["items"].([]any)
	if r.Status != 200 || len(items) != 1 {
		t.Fatalf("the trail's purge: %d %s", r.Status, r.Raw)
	}
	if got := items[0].(map[string]any); got["kind"] != "trail" || got["last_seq"].(float64) != 2 || got["approval"] != "retention.purge_after_retention v1" ||
		got["anchor_key"] != "audit-boundaries/x/2.json" || got["boundary"] != nil {
		t.Errorf("the trail's purge: %v", got)
	}
	if r.Body["postings"] != nil {
		t.Errorf("a record with no purged posting: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/audit/purges", e.as(authz.Bookkeeper), nil), "listAuditPurges", 403, CodeForbidden)
}

// matrixAuditPurges is the authorisation matrix's row for the purges after retention (AUD-09, AUD-10).
var matrixAuditPurges = map[string]func(e *env, tok string) (response, func() bool){
	"listAuditPurges": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit/purges", tok, nil), nil
	},
}
