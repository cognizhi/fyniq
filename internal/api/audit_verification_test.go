//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// tamperTrail changes the body of org's event seq from old to new as the database's superuser, with triggers off: what
// an attacker with full control of the database could do.
func tamperTrail(t *testing.T, org string, seq int64, old, new string) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if tag, err := tx.Exec(ctx, "UPDATE fyniq.audit_event SET body = replace(body, $3, $4) WHERE organisation_id = $1 AND seq = $2", org, seq, old, new); err != nil ||
		tag.RowsAffected() != 1 {
		t.Fatalf("tampering with event %d: %v %v", seq, tag, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func verifyRange(from, to string) string { return "/v1/audit/verification?from=" + from + "&to=" + to }

// Verify integrity (AUD-03, PRD-001 rev 0.235): a person with V on "Audit trail" verifies the events that occurred on a
// range of days. The range's links are recomputed from the event before it, and its anchors and security details checked;
// a changed event fails at its own sequence number, and undoing the change passes again. Each verification is itself in
// the trail. A range is at most 366 days and runs forwards; a Bookkeeper may not verify.
// req: AUD-03
// req: INV-12
// asvs: V16.4.3
func TestVerifyIntegrityForARange(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.postedJournal("100.00")
	e.postedJournal("250.00")
	worker := workerStore(t)
	if _, err := audit.AnchorAll(ctx, worker, e.anchors, e.signer, e.clock); err != nil {
		t.Fatal(err)
	}
	// The events are stamped by the store's clock, so the range is taken from the day of the last of them.
	var last time.Time
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		h, err := u.Audit().Head(ctx)
		if err != nil {
			return err
		}
		events, err := u.Audit().Events(ctx, h.Seq-1, 1)
		if err == nil && len(events) == 1 {
			var b struct {
				OccurredAt time.Time `json:"occurred_at"`
			}
			err = json.Unmarshal([]byte(events[0].Body), &b)
			last = b.OccurredAt
		}
		return err
	}); err != nil || last.IsZero() {
		t.Fatalf("the trail's last event: %v", err)
	}
	day := func(d int) string { return last.UTC().AddDate(0, 0, d).Format(time.DateOnly) }
	auditor := e.as(authz.Auditor)

	r := e.call(http.MethodGet, verifyRange(day(-2), day(1)), auditor, nil)
	if r.Status != 200 || r.str("outcome") != audit.Passed || r.Body["events"].(float64) < 3 || r.Body["anchors"].(float64) < 1 ||
		r.Body["details_checked"] != true || r.Body["anchored_through"].(float64) < 1 {
		t.Fatalf("a clean range: %d %s", r.Status, r.Raw)
	}
	if empty := e.call(http.MethodGet, verifyRange("2020-01-01", "2020-01-31"), auditor, nil); empty.str("outcome") != audit.Passed ||
		empty.Body["events"].(float64) != 0 || empty.Body["first_seq"].(float64) != 0 {
		t.Errorf("a range with no events: %s", empty.Raw)
	}

	// A journal's posting changed in place fails at its own event; the change undone, the range passes again.
	var posted int64
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			if strings.Contains(ev.Body, `"250.00"`) && posted == 0 {
				posted = ev.Seq
			}
		}
		return err
	}); err != nil || posted == 0 {
		t.Fatalf("no event of the 250.00 journal: %v", err)
	}
	tamperTrail(t, e.books.Org.String(), posted, `"250.00"`, `"25.00"`)
	if r := e.call(http.MethodGet, verifyRange(day(-2), day(1)), auditor, nil); r.Status != 200 || r.str("outcome") != audit.Failed ||
		int64(r.Body["failed_seq"].(float64)) != posted || !strings.Contains(r.str("problem"), "altered") {
		t.Errorf("a changed event: %s", r.Raw)
	}
	tamperTrail(t, e.books.Org.String(), posted, `"25.00"`, `"250.00"`)
	if r := e.call(http.MethodGet, verifyRange(day(-2), day(1)), auditor, nil); r.str("outcome") != audit.Passed {
		t.Errorf("the change undone: %s", r.Raw)
	}

	// Refused: backwards, too long, not a date, and without V on the row.
	e.problemIs(e.call(http.MethodGet, verifyRange(day(0), day(-1)), auditor, nil), "getAuditVerification", 422, audit.CodeRange)
	e.problemIs(e.call(http.MethodGet, verifyRange("2025-01-01", "2026-01-02"), auditor, nil), "getAuditVerification", 422, audit.CodeRange)
	e.problemIs(e.call(http.MethodGet, verifyRange("2026-13-01", day(0)), auditor, nil), "getAuditVerification", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodGet, verifyRange(day(-1), day(0)), e.as(authz.Bookkeeper), nil), "getAuditVerification", 403, CodeForbidden)

	// Each verification is itself in the trail.
	verified := 0
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			var b struct{ Action string }
			_ = json.Unmarshal([]byte(ev.Body), &b)
			if b.Action == audit.ActionVerifiedByMember {
				verified++
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if verified != 4 {
		t.Errorf("%d verifications in the trail, want 4", verified)
	}
}

// matrixAuditTrail are the authorisation matrix's rows for the audit trail (AUD-03).
var matrixAuditTrail = map[string]func(e *env, tok string) (response, func() bool){
	"getAuditVerification": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, verifyRange("2026-01-01", "2026-12-31"), tok, nil), nil
	},
}
