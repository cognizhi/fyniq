//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// oldBooks is an organisation whose first events occurred in January 2010, well past any records' retention.
func oldBooks(t *testing.T, name string) (porttest.Books, *clock.Frozen) {
	t.Helper()
	old := clock.NewFrozen(time.Date(2010, 1, 15, 9, 0, 0, 0, time.UTC))
	return porttest.NewBooks(t, store(t, "fyniq_api", postgres.WithClock(old)), name), old
}

// switchPurgeOn records the purge after retention switched on from 1 January 2010, as a second approver would.
func switchPurgeOn(t *testing.T, org uuid.UUID) {
	t.Helper()
	if err := store(t, "fyniq_api").Do(signedIn(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, audit.PurgeSetting, "false", "true", time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// The purge after retention (AUD-09, open question 38, the owner's decision of rev 0.230). Switched off, nothing goes.
// Switched on, the leading run of events past their records' retention goes, behind a boundary that is signed and
// written to the anchors' bucket first; the events kept still verify, from the boundary. A range reaching back into
// the purged days is explained by the boundary, never clean and never failed. A second run finds nothing more.
// req: AUD-09
// req: AUD-03
// req: AUD-10
func TestTheTrailIsPurgedBehindASignedBoundary(t *testing.T) {
	ctx := context.Background()
	b, old := oldBooks(t, "Purged Pte Ltd")
	recordAs(t, store(t, "fyniq_api", postgres.WithClock(old)), signedIn(), b.Org, "session.started") // with details beside the chain
	old2010 := len(events(t, b.Org))
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	now := clock.NewFrozen(time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC))
	purge := func() {
		t.Helper()
		if _, err := audit.PurgeAll(ctx, worker, blobs, signer, now); err != nil {
			t.Fatal(err)
		}
	}
	purge()
	if got := len(events(t, b.Org)); got != old2010 {
		t.Fatalf("switched off, %d events of %d are left", got, old2010)
	}

	switchPurgeOn(t, b.Org) // its own event, written now, is kept
	recordAs(t, store(t, "fyniq_api"), signedIn(), b.Org, "invoice.noted")
	purge()
	// Kept: the switch's change and the invoice's event, then the boundary's and the purge's own.
	left := events(t, b.Org)
	if len(left) != 4 || left[0].Seq != int64(old2010)+1 || !bytes.Contains([]byte(left[2].Body), []byte(`"action":"audit.boundary_written"`)) ||
		!bytes.Contains([]byte(left[3].Body), []byte(`"action":"`+audit.ActionPurged+`"`)) {
		t.Fatalf("after the purge: %d events, the first %d", len(left), left[0].Seq)
	}
	var bounds []storage.AuditBoundary
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		bounds, err = u.Audit().Boundaries(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(bounds) != 1 || bounds[0].LastSeq != int64(old2010) || bounds[0].Counts["events"] != int64(old2010) || bounds[0].Counts["security_events"] != 1 ||
		bounds[0].Approval != audit.PurgeSetting+" v1" || !bytes.Equal(bounds[0].LastHash, left[0].PrevHash) {
		t.Fatalf("the boundary: %+v", bounds)
	}
	var signed audit.SignedBoundary
	raw, err := blobs.Get(ctx, audit.BoundaryKey(b.Org, int64(old2010)))
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewDecoder(raw).Decode(&signed)
	_ = raw.Close()
	if err != nil || signed.Check(signer, b.Org, bounds[0]) != nil || signed.Boundary.LastHash != hex.EncodeToString(bounds[0].LastHash) {
		t.Errorf("the boundary's signed copy: %+v %v", signed, err)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer, nil)
	if r := reportFor(t, reports, b.Org); !r.OK() {
		t.Errorf("the trail kept, from its boundary: %+v", r)
	}
	verify := func(from, to time.Time) audit.RangeReport {
		t.Helper()
		var r audit.RangeReport
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = audit.VerifyRange(ctx, u, blobs, signer, nil, b.Org, from, to, time.UTC)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := verify(time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2010, 12, 31, 0, 0, 0, 0, time.UTC)); r.Outcome != audit.Explained || r.Events != 0 {
		t.Errorf("a range of purged days is explained: %+v", r)
	}
	var kept struct {
		OccurredAt time.Time `json:"occurred_at"`
	}
	if err := json.Unmarshal([]byte(left[1].Body), &kept); err != nil {
		t.Fatal(err)
	}
	if r := verify(kept.OccurredAt.AddDate(0, 0, -1), kept.OccurredAt.AddDate(0, 0, 1)); r.Outcome != audit.Passed || r.Events < 4 {
		t.Errorf("a range of kept days: %+v", r)
	}
	purge()
	if got := len(events(t, b.Org)); got < 2 {
		t.Errorf("a second run purged what is kept: %d left", got)
	}
}

// The purge keeps its rules whatever any role tries (000291): no event is deleted directly; audit_purge needs a
// boundary at exactly its sequence, the switch on and every event at least 7 years old, and runs for the worker only; a
// boundary names an event by its hash, moves forward only and never changes; even the table's owner, with the purge's
// flag set by hand, deletes nothing the purge could not.
// req: AUD-09
// req: AUD-08
// req: QA-ENG-05
// asvs: V16.4.2, V8.2.3
func TestAttackThePurgeKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	b, _ := oldBooks(t, "Purge Attacks Pte Ltd")
	recordAs(t, store(t, "fyniq_api"), signedIn(), b.Org, "invoice.noted") // a young event
	all := events(t, b.Org)
	young := all[len(all)-1]
	exec := func(role, stmt string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	for _, role := range allRoles {
		wantState(t, role+" deletes events", exec(role, "DELETE FROM fyniq.audit_event WHERE seq <= 2"), "42501", "FY060")
		if role != "fyniq_worker" && role != "fyniq_migrator" {
			wantState(t, role+" purges", exec(role, "SELECT fyniq.audit_purge(2)"), "42501")
			wantState(t, role+" writes a boundary", exec(role, "INSERT INTO fyniq.audit_boundary (last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at) VALUES (2, $1, now(), 'x', '{}', 'k', now())", all[1].Hash), "42501")
		}
	}
	insertBoundary := func(seq int64, hash []byte) error {
		return exec("fyniq_worker", "INSERT INTO fyniq.audit_boundary (last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at) VALUES ($1, $2, now(), 'x', '{}', 'k', now())", seq, hash)
	}
	wantState(t, "a purge with no boundary", exec("fyniq_worker", "SELECT fyniq.audit_purge(2)"), "FY304")
	wantState(t, "a boundary naming another hash", insertBoundary(2, all[0].Hash), "FY304")
	if err := insertBoundary(2, all[1].Hash); err != nil {
		t.Fatalf("a boundary at event 2: %v", err)
	}
	wantState(t, "a boundary moving back", insertBoundary(1, all[0].Hash), "FY304")
	wantState(t, "a boundary changed", exec("fyniq_migrator", "UPDATE fyniq.audit_boundary SET approval = 'y'"), "FY304")
	wantState(t, "a boundary deleted", exec("fyniq_migrator", "DELETE FROM fyniq.audit_boundary"), "FY304")
	wantState(t, "a purge switched off", exec("fyniq_worker", "SELECT fyniq.audit_purge(2)"), "FY304")
	flagged := "SELECT set_config('fyniq.audit_purging', 'on', true); DELETE FROM fyniq.audit_event WHERE seq <= 2"
	wantState(t, "the owner with the flag, switched off", exec("fyniq_migrator", flagged), "FY060")

	switchPurgeOn(t, b.Org)
	if err := insertBoundary(young.Seq, young.Hash); err != nil {
		t.Fatalf("a boundary at the young event: %v", err)
	}
	wantState(t, "a purge of a young event", exec("fyniq_worker", "SELECT fyniq.audit_purge($1)", young.Seq), "FY304")
	wantState(t, "the owner deleting a young event with the flag",
		exec("fyniq_migrator", "SELECT set_config('fyniq.audit_purging', 'on', true); DELETE FROM fyniq.audit_event WHERE seq = "+strconv.FormatInt(young.Seq, 10)), "FY060")
	if n := len(events(t, b.Org)); n != len(all)+1 { // the setting change's own event is the one more
		t.Errorf("events went: %d of %d", n, len(all)+1)
	}
}
