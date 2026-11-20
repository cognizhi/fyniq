//go:build local

package nemura_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// fyniqSays signs an event from Fyniq's side, for the inbox.
func fyniqSays(t *testing.T, key ed25519.PrivateKey, lt *leaveTeam, fyniqOrg uuid.UUID, now time.Time, typ, objectType, objectID string, version int, seq int64, body any) link.Envelope {
	t.Helper()
	raw, _ := json.Marshal(body)
	at := now.UTC().Format(time.RFC3339)
	e, err := link.Sign(key, link.Header{EventID: uuid.NewString(), Type: typ, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(), TargetOrganisation: lt.c.org.String(), Epoch: 1,
		Sequence: seq, ObjectType: objectType, ObjectID: objectID, ObjectVersion: version, EffectiveAt: at, RecordedAt: at, SignedAt: at, CorrelationID: uuid.NewString()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (lt *leaveTeam) receive(t *testing.T, e link.Envelope, now time.Time) (string, error) {
	t.Helper()
	var out string
	err := lt.c.do(func(ctx context.Context, tx pgx.Tx) (err error) {
		out, err = nemura.ReceiveLinkEvent(ctx, tx, lt.c.org, now, e)
		if out == "refused" {
			return nil
		}
		return err
	})
	return out, err
}

// approvedRun is a linked organisation with a run approved with proof, for the tests of what comes after.
type approvedRun struct {
	lt       *leaveTeam
	now      time.Time
	run      uuid.UUID
	rl       nemura.RunLinkService
	ops      nemura.LinkOpsService
	offer    nemura.LinkOffer
	fyniqKey ed25519.PrivateKey
	fyniqOrg uuid.UUID
	approver api.Session
}

func newApprovedRun(t *testing.T) *approvedRun {
	t.Helper()
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, _ := nemura.NewPacksService(st, clock.NewFrozen(now))
	blobs, _ := local.NewDir(t.TempDir())
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Blobs: blobs}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err := svc.SetPay(ctx, prep, lt.ben, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC"}); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth})
	})
	run, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	offer, fyniqPriv, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	engineering := uuid.New()
	if out, err := lt.receive(t, fyniqSays(t, fyniqPriv, lt, fyniqOrg, now, nemura.TypeDepartmentChanged, "payroll_mapping", fyniqOrg.String(), 3, 3,
		map[string]any{"mapping_version": 3, "departments": []map[string]any{{"department_id": engineering.String(), "name": "Engineering", "cost_centre_id": nil}}}), now); err != nil || out != "applied" {
		t.Fatalf("the mapping: %q %v", out, err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.LinkIdentity(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.c.pay, identity.Identity{Issuer: "https://idp.example.test", Subject: "preparer-sub", Tenant: "t"}); err != nil {
			return err
		}
		return nemura.LinkIdentity(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.c.owner, identity.Identity{Issuer: "https://idp.example.test", Subject: "approver-sub", Tenant: "t"})
	})
	rl := nemura.RunLinkService{Store: st, Clock: clock.NewFrozen(now), Origin: testOrigin, RPID: testRPID}
	env, err := rl.Envelope(ctx, approver, mustUUID(run.ID), 1)
	if err != nil || !env.Ready {
		t.Fatalf("the envelope: %+v %v", env, err)
	}
	digest, _ := link.Challenge(env.EnvelopeSHA256)
	if err := rl.Approve(ctx, approver, mustUUID(run.ID), api.LinkedApprovalInput{Version: 1, Digest: run.Latest.Digest, EnvelopeSHA256: env.EnvelopeSHA256, Approval: api.RunProof(answer(digest, testOrigin, testRPID, 0x05))}); err != nil {
		t.Fatal(err)
	}
	return &approvedRun{lt: lt, now: now, run: mustUUID(run.ID), rl: rl, ops: nemura.LinkOpsService{Store: st, Clock: clock.NewFrozen(now)}, offer: offer, fyniqKey: fyniqPriv, fyniqOrg: fyniqOrg, approver: approver}
}

// A gap is repaired by sending the current state again, never an old payload (NM-INT-10): repair queues each person's present record, bank-detail version and hold at the version they have now (so Fyniq
// ignores what it holds and applies what it missed) and queues again what failed or was refused; one person can be repaired alone; Fyniq's request for a range of sequence numbers does the same for the objects
// the range named, and a range that is backwards or too wide is refused; repair needs an active link; and the link's health shows events by state, the oldest waiting and the checkpoint, warns after 24 hours
// of failing, and lists what failed by identity and never a payload.
// part of NM-INT-10 (Fyniq's detection of a gap and its request are Fyniq's)
// part of NM-INT-04
// asvs: V8.2.1, V13.2.1, V16.2.5
func TestAGapIsRepairedWithTheCurrentStateAndTheLinksHealthIsShownByIdentity(t *testing.T) {
	ctx := context.Background()
	r := newApprovedRun(t)
	lt, now := r.lt, r.now
	var owner = api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}

	// Standalone repairs nothing.
	standalone := newLeaveTeam(t)
	if _, err := (nemura.LinkOpsService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now)}).Repair(ctx, api.Session{Organisation: standalone.c.org, Member: standalone.c.owner}, uuid.Nil); err == nil {
		t.Error("a repair of a link that is not there")
	}
	// Send the snapshot; one event fails for good (Fyniq refuses it), one stays waiting.
	fy := newFyniqInMiniature(t, r.offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events", "/v1/people-link/payroll-runs")
	work := fy.sender(t, now).Work()
	rows := queuedRows(t, lt)
	for i, row := range rows {
		if i == 0 {
			fy.status, fy.code = 422, "PPL-09"
		} else {
			fy.status = 0
		}
		if err := work(ctx, lt.c.org, row); err != nil {
			t.Fatal(err)
		}
	}
	h, err := r.ops.Health(ctx, owner)
	if err != nil || h.State != "active" || h.Refused != 1 || h.Sent != len(rows)-1 || len(h.Undelivered) != 1 || h.Undelivered[0].Code != "PPL-09" || h.LastSequenceSent == 0 || h.FailingOver24Hours {
		t.Fatalf("the health after the snapshot: %+v %v", h, err)
	}
	if hj, _ := json.Marshal(h); strings.Contains(string(hj), "Ben Lim") || strings.Contains(string(hj), "payload") {
		t.Errorf("the health shows more than identities: %s", hj)
	}
	before := len(queuedRows(t, lt))
	n, err := r.ops.Repair(ctx, owner, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 { // every person's present record (the refused one among them): the three people, no bank details and no hold yet
		t.Errorf("a repair of everyone queued %d events", n)
	}
	if got := len(queuedRows(t, lt)) - before; got != n {
		t.Errorf("queued %d rows for %d events", got, n)
	}
	// One person alone: their person event, at the version they have.
	one, err := r.ops.Repair(ctx, owner, lt.ben)
	if err != nil || one != 1 {
		t.Errorf("one person's repair: %d %v", one, err)
	}
	log := eventLog(t, lt)
	var ben []loggedEvent
	for _, e := range log {
		if e.Object == lt.ben.String() && e.Type == nemura.TypePersonUpserted {
			ben = append(ben, e)
		}
	}
	if len(ben) < 3 {
		t.Fatalf("Ben's events: %+v", ben)
	}
	for _, e := range ben {
		if e.Version != 1 {
			t.Errorf("a repair does not change a version: %+v", e)
		}
	}
	// Fyniq asks for a range it is missing: the objects of those sequence numbers are queued again at their current state.
	before = len(queuedRows(t, lt))
	if out, err := lt.receive(t, fyniqSays(t, r.fyniqKey, lt, r.fyniqOrg, now, nemura.TypeReplayRequested, "link", r.fyniqOrg.String(), 1, 5, map[string]any{"from_sequence": 1, "to_sequence": 3}), now); err != nil || out != "applied" {
		t.Fatalf("a replay request: %q %v", out, err)
	}
	if got := len(queuedRows(t, lt)) - before; got < 1 {
		t.Errorf("a replay of sequences 1 to 3 queued %d events", got)
	}
	for _, bad := range []map[string]any{{"from_sequence": 5, "to_sequence": 2}, {"from_sequence": 1, "to_sequence": 20000}, {"from_sequence": 0, "to_sequence": 1}} {
		if out, _ := lt.receive(t, fyniqSays(t, r.fyniqKey, lt, r.fyniqOrg, now, nemura.TypeReplayRequested, "link", r.fyniqOrg.String(), 2, 6, bad), now); out != "refused" {
			t.Errorf("a replay of %v: %q", bad, out)
		}
	}
	// The events since a sequence, by identity.
	es, err := r.ops.Events(ctx, owner, int64(h.LastSequenceQueued), 50)
	if err != nil || len(es) == 0 {
		t.Errorf("the events since the checkpoint: %d %v", len(es), err)
	}
	// 24 hours of failing: a ⚠ on Checks (N.1 #7).
	later := now.Add(25 * time.Hour)
	late := nemura.LinkOpsService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(later)}
	if h2, _ := late.Health(ctx, owner); !h2.FailingOver24Hours || h2.FailingSince == nil {
		t.Errorf("failing for 25 hours: %+v", h2)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		fs, err := nemura.LinkFindings(ctx, tx, later)
		if err != nil || len(fs) != 1 || fs[0].Check != 7 || fs[0].Status != "warn" || !strings.Contains(fs[0].Message, "not synced") {
			t.Errorf("the check: %+v %v", fs, err)
		}
		fs, _ = nemura.LinkFindings(ctx, tx, now)
		if len(fs) != 0 {
			t.Errorf("a link failing for an hour is not yet a warning: %+v", fs)
		}
		return nil
	})
	// The role that cannot read the keys cannot change the log.
	if err := store(t, "nemura_api").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.link_event SET state = 'sent' WHERE state = 'refused'`)
		return err
	}); err == nil {
		t.Error("the API settled an event")
	}
	_ = sha256.Size
}

// After a restore the link is held and the work after the restore point is reconciled before anything is sent or paid (NM-INT-13): the deployment's two operators record the restore, which suspends the
// link, keeps the checkpoint and lists each run approved with proof whose answer the restored database lacks; nothing is sent while it is open (the sender waits) and a run is neither approved nor sent
// again; an Owner or Administrator with a fresh passkey decides each run against Fyniq's own record (a run Fyniq posted gets its reference; one it did not is sent again with its proof); when the last is
// decided the restore is reconciled, the link resumes and the current state goes again; a restore is recorded once at a time, by two different operators, and what was recorded is never rewritten.
// part of NM-INT-13 (the restore drill across both databases is the deployment's: Fyniq's half is its own)
// asvs: V8.2.1, V8.3.1, V13.2.1
func TestARestoreHoldsTheLinkAndTheWorkAfterItUntilAnOwnerReconcilesIt(t *testing.T) {
	ctx := context.Background()
	r := newApprovedRun(t)
	lt, now := r.lt, r.now
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	migrator := db.URL(nemura.Migrator)
	if _, _, err := nemura.RecordLinkRestore(ctx, migrator, lt.c.org, []string{"Ann Lee"}, now); err == nil {
		t.Error("one operator recorded a restore")
	}
	if _, _, err := nemura.RecordLinkRestore(ctx, migrator, lt.c.org, []string{"Ann Lee", "ann lee"}, now); err == nil {
		t.Error("the same operator twice recorded a restore")
	}
	held, linked, err := nemura.RecordLinkRestore(ctx, migrator, lt.c.org, []string{"Ann Lee", "Bob Tan"}, now)
	if err != nil || !linked || held != 1 {
		t.Fatalf("recording the restore: %d %v %v", held, linked, err)
	}
	if _, _, err := nemura.RecordLinkRestore(ctx, migrator, lt.c.org, []string{"Ann Lee", "Bob Tan"}, now); err == nil {
		t.Error("a second restore was recorded while one is open")
	}
	h, err := r.ops.Health(ctx, owner)
	if err != nil || h.State != "suspended" || h.Restore == nil || h.Restore.State != "open" || len(h.Restore.Runs) != 1 || h.Restore.Runs[0].RunID != r.run.String() || h.Restore.RecordedBy != "Ann Lee, Bob Tan" {
		t.Fatalf("the health after a restore: %+v %v", h, err)
	}
	// Nothing is sent while it is open: the sender waits, and the run is neither approved again nor sent again.
	fy := newFyniqInMiniature(t, r.offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events", "/v1/people-link/payroll-runs")
	work := fy.sender(t, now).Work()
	rows := queuedRows(t, lt)
	if err := work(ctx, lt.c.org, rows[0]); err == nil || len(fy.calls) != 0 {
		t.Errorf("a suspended link sent: %v (%d calls)", err, len(fy.calls))
	}
	if err := r.rl.Retry(ctx, r.approver, r.run, 1); err == nil {
		t.Error("a run was sent again while the link is held")
	}
	if _, err := r.ops.Repair(ctx, owner, uuid.Nil); err == nil {
		t.Error("a repair ran while the link is held")
	}
	checks := func() int {
		n := 0
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			fs, err := nemura.LinkFindings(ctx, tx, now)
			for _, f := range fs {
				if f.Status == "fail" {
					n++
				}
			}
			return err
		})
		return n
	}
	if checks() != 1 {
		t.Error("an open restore is a ✖ on Checks")
	}
	// The decision needs a fresh passkey, a run the restore holds and, for posted, Fyniq's reference.
	stale := now.Add(-time.Hour)
	if _, err := r.ops.DecideRestore(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &stale}, r.run, 1, true, "ref"); err == nil {
		t.Error("a restore decided without a fresh passkey")
	}
	if _, err := r.ops.DecideRestore(ctx, owner, r.run, 1, true, ""); err == nil {
		t.Error("posted with no reference")
	}
	if _, err := r.ops.DecideRestore(ctx, owner, uuid.New(), 1, false, ""); err == nil {
		t.Error("a run the restore does not hold")
	}
	before := len(queuedRows(t, lt))
	resumed, err := r.ops.DecideRestore(ctx, owner, r.run, 1, false, "")
	if err != nil || !resumed {
		t.Fatalf("the last decision resumes the link: %v %v", resumed, err)
	}
	if _, err := r.ops.DecideRestore(ctx, owner, r.run, 1, false, ""); err == nil {
		t.Error("a run decided twice")
	}
	h, _ = r.ops.Health(ctx, owner)
	if h.State != "active" || h.Restore != nil || checks() != 0 {
		t.Errorf("after reconciling: %+v", h)
	}
	if after := len(queuedRows(t, lt)); after-before < 4 { // the three people again and the run's approval again
		t.Errorf("the link resumed and queued %d events", after-before)
	}
	sv, _ := r.rl.Submission(ctx, r.approver, r.run)
	if sv.State != "queued" && sv.State != "sent" {
		t.Errorf("a run Fyniq did not post is sent again: %+v", sv)
	}
	// What was recorded is never rewritten, and the migrator cannot delete it either.
	for _, x := range []struct{ role, stmt string }{
		{"nemura_migrator", "UPDATE nemura.link_restore SET recorded_by = 'someone else'"},
		{"nemura_migrator", "DELETE FROM nemura.link_restore_run"},
		{"nemura_api", "UPDATE nemura.link_restore SET state = 'open'"},
	} {
		if err := store(t, x.role).InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err }); err == nil {
			t.Errorf("%s ran %q", x.role, x.stmt)
		}
	}
}

// A run's receipt is what two databases are reconciled by (NM-INT-13): the proof kept for each approved version (its digest, the mapping version), each sending and where it stands, Fyniq's answers and the
// payment instructions and outcomes, with no figure, no name and no account.
// part of NM-INT-13 (per-run receipts; Fyniq's own receipt is `payroll_run_receipt`)
// asvs: V8.2.1, V14.2.4
func TestARunsReceiptShowsItsProofItsSendingsAndFyniqsAnswersWithNoFigure(t *testing.T) {
	ctx := context.Background()
	r := newApprovedRun(t)
	lt, now := r.lt, r.now
	fy := newFyniqInMiniature(t, r.offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events", "/v1/people-link/payroll-runs")
	work := fy.sender(t, now).Work()
	rows := queuedRows(t, lt)
	if err := work(ctx, lt.c.org, rows[len(rows)-1]); err != nil { // the run's submission is the last queued
		t.Fatal(err)
	}
	if out, err := lt.receive(t, fyniqSays(t, r.fyniqKey, lt, r.fyniqOrg, now, nemura.TypePayrollRunPosted, "payroll_run", r.run.String(), 1, 9, map[string]any{"run_id": r.run.String(), "version": 1, "status": "posted", "posting_ref": "ref-1", "code": "", "message": ""}), now); err != nil || out != "applied" {
		t.Fatal(out, err)
	}
	rc, err := r.ops.Receipt(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner}, r.run)
	if err != nil || len(rc.Approvals) != 1 || rc.Approvals[0].Version != 1 || len(rc.Approvals[0].EnvelopeSHA256) != 64 || rc.Approvals[0].MappingVersion != 3 ||
		len(rc.Submissions) != 1 || rc.Submissions[0].State != "sent" || len(rc.Postings) != 1 || rc.Postings[0].Reference != "ref-1" {
		t.Fatalf("the receipt: %+v %v", rc, err)
	}
	b, _ := json.Marshal(rc)
	for _, leak := range []string{"Ben", "4500", "gross", "account", "nric"} {
		if strings.Contains(strings.ToLower(string(b)), strings.ToLower(leak)) {
			t.Errorf("the receipt carries %q: %s", leak, b)
		}
	}
	if _, err := r.ops.Receipt(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner}, uuid.New()); err == nil {
		t.Error("a receipt of a run that is not there")
	}
}
