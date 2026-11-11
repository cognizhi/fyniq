//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// restoreBehind simulates a restore behind the anchors of org's trail: its head is anchored, then a published anchor
// signs an event five past it that the restored database never had, and new events take the sequence numbers after the
// head. It returns the anchored head.
func restoreBehind(t *testing.T, org uuid.UUID, blobs *local.Dir, signer *local.Ed25519, now *clock.Frozen) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := audit.AnchorAll(ctx, store(t, "fyniq_worker"), blobs, signer, now); err != nil {
		t.Fatal(err)
	}
	all := events(t, org)
	head := all[len(all)-2].Seq // the anchor's own event follows the head it anchored
	lost, err := audit.Sign(ctx, signer, org, head+5, bytes.Repeat([]byte{9}, 32), now.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(lost)
	if err := blobs.Create(ctx, audit.AnchorKey(org, head+5), bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		recordAs(t, store(t, "fyniq_api"), signedIn(), org, "invoice.noted")
	}
	return head
}

// A restore behind the anchors (AUD-09, design D8). The recovery step finds the restored head, the last event whose hash
// matches every anchor, and the anchored range no longer proven; it writes the signed recovery, records it with
// audit.restored in the chain, starts the next anchor epoch and tells the Owner and the DPO. Verification then passes,
// the lost anchor explained, and a range of days over it is explained, never failed; the next anchors go to the new
// epoch, and a second run finds nothing. The recovery is acknowledged once.
// req: AUD-09
// req: AUD-03
// req: INV-12
func TestARestoreBehindTheAnchorsIsRecovered(t *testing.T) {
	ctx := context.Background()
	b := porttest.NewBooks(t, store(t, "fyniq_api"), "Restored Pte Ltd")
	for range 3 {
		recordAs(t, store(t, "fyniq_api"), signedIn(), b.Org, "invoice.noted")
	}
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	now := clock.NewFrozen(time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC))
	head := restoreBehind(t, b.Org, blobs, signer, now)

	var told []storage.AuditRecovery
	tell := func(_ context.Context, _ storage.UnitOfWork, org uuid.UUID, r storage.AuditRecovery) error {
		if org == b.Org {
			told = append(told, r)
		}
		return nil
	}
	rec, err := audit.Recover(ctx, worker, blobs, signer, tell, now.Now(), b.Org)
	if err != nil || rec == nil {
		t.Fatalf("the recovery: %+v %v", rec, err)
	}
	if rec.Epoch != 1 || rec.RestoredSeq != head || rec.MissingFrom != head+1 || rec.MissingTo != head+5 || len(told) != 1 ||
		rec.AnchorKey != audit.RecoveryKey(b.Org, 1) {
		t.Fatalf("the recovery: %+v, told %d", rec, len(told))
	}
	var signed audit.SignedRecovery
	raw, err := blobs.Get(ctx, rec.AnchorKey)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewDecoder(raw).Decode(&signed)
	_ = raw.Close()
	msg, _ := json.Marshal(signed.Recovery)
	if err != nil || signer.Verify(signed.Recovery.KeyID, msg, signed.Signature) != nil || signed.Recovery.RestoredSeq != head ||
		signed.Recovery.Runbook != audit.Runbook {
		t.Errorf("the signed recovery: %+v %v", signed, err)
	}
	if all := events(t, b.Org); !strings.Contains(all[len(all)-1].Body, `"action":"audit.restored"`) {
		t.Errorf("the chain does not record the recovery: %s", all[len(all)-1].Body)
	}
	if again, err := audit.Recover(ctx, worker, blobs, signer, tell, now.Now(), b.Org); again != nil || err != nil {
		t.Errorf("a second run recovered again: %+v %v", again, err)
	}

	verified := func(what string, explained int) {
		t.Helper()
		reports, _ := audit.VerifyAll(ctx, worker, blobs, signer, nil)
		if r := reportFor(t, reports, b.Org); !r.OK() || r.Explained != explained || r.Recoveries != 1 {
			t.Errorf("%s: %+v", what, r)
		}
	}
	verified("after the recovery", 1)
	recordAs(t, store(t, "fyniq_api"), signedIn(), b.Org, "invoice.noted")
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, now); err != nil {
		t.Fatal(err)
	}
	if keys, err := blobs.List(ctx, audit.AnchorPrefix(b.Org)+"e1/"); err != nil || len(keys) != 1 {
		t.Errorf("the next anchors' epoch: %v %v", keys, err)
	}
	verified("anchored in the new epoch", 1)
	var last struct {
		OccurredAt time.Time `json:"occurred_at"`
	}
	all := events(t, b.Org)
	if err := json.Unmarshal([]byte(all[len(all)-1].Body), &last); err != nil {
		t.Fatal(err)
	}
	var r audit.RangeReport
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = audit.VerifyRange(ctx, u, blobs, signer, nil, b.Org, last.OccurredAt.AddDate(0, 0, -2), last.OccurredAt.AddDate(0, 0, 1), time.UTC)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r.Outcome != audit.Explained || !strings.Contains(r.Problem, "restore") {
		t.Errorf("a range over the lost events: %+v", r)
	}

	ack := func() (storage.AuditRecovery, error) {
		var out storage.AuditRecovery
		err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Audit().AcknowledgeRecovery(ctx, rec.ID, uuid.New(), now.Now())
			return err
		})
		return out, err
	}
	if got, err := ack(); err != nil || got.AcknowledgedBy == nil {
		t.Fatalf("the acknowledgement: %+v %v", got, err)
	}
	if _, err := ack(); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("acknowledged twice: %v", err)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer, nil)
	if r := reportFor(t, reports, b.Org); r.Unacknowledged != 0 {
		t.Errorf("after the acknowledgement: %+v", r)
	}
}

// A purge followed by a restore behind the last anchor (G4-071): the recovery's restored head is past the boundary,
// verification runs from the boundary and explains the anchored range no longer proven, and a range of the purged days
// is explained by the boundary.
// req: G4-071
// req: AUD-09
func TestAPurgeThenARestoreBehindTheLastAnchor(t *testing.T) {
	ctx := context.Background()
	b, old := oldBooks(t, "Purged And Restored Pte Ltd")
	recordAs(t, store(t, "fyniq_api", postgres.WithClock(old)), signedIn(), b.Org, "invoice.noted")
	switchPurgeOn(t, b.Org)
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	now := clock.NewFrozen(time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC))
	if _, err := audit.PurgeAll(ctx, worker, blobs, signer, now); err != nil {
		t.Fatal(err)
	}
	var bounds []storage.AuditBoundary
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		bounds, err = u.Audit().Boundaries(ctx)
		return err
	}); err != nil || len(bounds) != 1 {
		t.Fatalf("the purge: %v %v", bounds, err)
	}
	head := restoreBehind(t, b.Org, blobs, signer, now)
	rec, err := audit.Recover(ctx, worker, blobs, signer, nil, now.Now(), b.Org)
	if err != nil || rec == nil || rec.RestoredSeq != head || rec.RestoredSeq <= bounds[0].LastSeq {
		t.Fatalf("the recovery after the purge: %+v %v (boundary at %d)", rec, err, bounds[0].LastSeq)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer, nil)
	if r := reportFor(t, reports, b.Org); !r.OK() || r.Explained != 1 {
		t.Errorf("verified from the boundary: %+v", r)
	}
	var r audit.RangeReport
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = audit.VerifyRange(ctx, u, blobs, signer, nil, b.Org, time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2010, 12, 31, 0, 0, 0, 0, time.UTC), time.UTC)
		return err
	}); err != nil || r.Outcome != audit.Explained {
		t.Errorf("the purged days: %+v %v", r, err)
	}
}

// The erasure register re-applied (G4-070's audit part, design D2 and D8): a backup taken before an erasure brings the
// details back; re-applying the register's signed entries erases them again before the copy opens, and only once. An
// entry whose signature does not verify is refused.
// req: G4-070
// req: AUD-06
func TestTheErasureRegisterIsReapplied(t *testing.T) {
	ctx := context.Background()
	b, old := oldBooks(t, "Erased Again Pte Ltd")
	recordAs(t, store(t, "fyniq_api", postgres.WithClock(old)), signedIn(), b.Org, "session.started") // with details beside the chain
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	all := events(t, b.Org)
	ids := make([]uuid.UUID, len(all))
	for i, e := range all {
		ids[i] = e.ID
	}
	kept := func() int {
		t.Helper()
		var n int
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := u.Audit().Details(ctx, ids)
			n = len(d)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if kept() == 0 {
		t.Fatal("no details to erase")
	}
	// The original database erased them after the backup was taken: its signed register entry is in the bucket.
	cutoff := time.Date(2011, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := audit.Erasure{Schema: audit.ErasureSchema, OrganisationID: b.Org, Cutoff: cutoff.Format(time.RFC3339Nano), Count: 1,
		WrittenAt: cutoff.Format(time.RFC3339Nano), KeyID: signer.KeyID()}
	msg, _ := json.Marshal(entry)
	sig, err := signer.Sign(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(audit.SignedErasure{Erasure: entry, Signature: sig})
	if err := blobs.Create(ctx, audit.ErasureKey(b.Org, cutoff), bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if n, err := audit.Reapply(ctx, worker, blobs, signer, b.Org); err != nil || n == 0 || kept() != 0 {
		t.Fatalf("re-applied: %d %v, %d details kept", n, err, kept())
	}
	if n, err := audit.Reapply(ctx, worker, blobs, signer, b.Org); err != nil || n != 0 {
		t.Errorf("re-applied twice: %d %v", n, err)
	}
	forged, _ := json.Marshal(audit.SignedErasure{Erasure: entry, Signature: []byte("forged")})
	if err := blobs.Create(ctx, audit.ErasureKey(b.Org, cutoff.AddDate(0, 1, 0)), bytes.NewReader(forged)); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.Reapply(ctx, worker, blobs, signer, b.Org); err == nil {
		t.Error("a register entry whose signature does not verify was re-applied")
	}
}

// A recovery keeps its rules whatever any role tries (000293): only the worker records one, starting the next epoch
// only; it never changes or goes, nor does its acknowledgement, which the API writes once and audits.
// req: AUD-09
// req: QA-ENG-05
// asvs: V16.4.2
func TestAttackARecoveryKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	b := porttest.NewBooks(t, store(t, "fyniq_api"), "Recovery Attacks Pte Ltd")
	exec := func(role, stmt string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	insert := "INSERT INTO fyniq.audit_recovery (epoch, restored_seq, restored_hash, missing_from, missing_to, detected_at, anchor_key) VALUES ($1, 3, $2, 4, 9, now(), 'k')"
	hash := bytes.Repeat([]byte{1}, 32)
	for _, role := range allRoles {
		if role != "fyniq_worker" && role != "fyniq_migrator" {
			wantState(t, role+" records a recovery", exec(role, insert, 1, hash), "42501")
		}
	}
	wantState(t, "a recovery skipping an epoch", exec("fyniq_worker", insert, 2, hash), "FY306")
	var id uuid.UUID
	if err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Audit().RecordRecovery(ctx, storage.NewAuditRecovery{Epoch: 1, RestoredSeq: 3, RestoredHash: hash, MissingFrom: 4, MissingTo: 9,
			DetectedAt: time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC), AnchorKey: "k"})
		id = r.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantState(t, "a recovery changed", exec("fyniq_migrator", "UPDATE fyniq.audit_recovery SET missing_to = 99 WHERE id = $1", id), "FY306")
	wantState(t, "a recovery deleted", exec("fyniq_migrator", "DELETE FROM fyniq.audit_recovery WHERE id = $1", id), "FY306")
	wantState(t, "the API changing a recovery", exec("fyniq_api", "UPDATE fyniq.audit_recovery SET restored_seq = 1 WHERE id = $1", id), "42501")
	acknowledge := "INSERT INTO fyniq.audit_recovery_acknowledgement (recovery_id, acknowledged_by, acknowledged_at) VALUES ($1, $2, now())"
	wantState(t, "the worker acknowledging", exec("fyniq_worker", acknowledge, id, uuid.New()), "42501")
	wantState(t, "a recovery acknowledged without its audit event", exec("fyniq_api", acknowledge, id, uuid.New()), "FY061")
	if err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Audit().AcknowledgeRecovery(ctx, id, uuid.New(), time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantState(t, "an acknowledgement changed", exec("fyniq_migrator", "UPDATE fyniq.audit_recovery_acknowledgement SET acknowledged_by = $1", uuid.New()), "FY060")
	wantState(t, "an acknowledgement deleted", exec("fyniq_migrator", "DELETE FROM fyniq.audit_recovery_acknowledgement"), "FY060")
}
