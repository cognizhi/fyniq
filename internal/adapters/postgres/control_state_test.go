//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A control's state is the worker's, one per organisation and control, read under row-level security: written at version
// 1, changed only as its next version and with its status change audited, never deleted (migration 000295, FY308).
// req: CMP-01
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.3
func TestAttackControlStatesAreTheWorkersRecord(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack control states A Pte Ltd"), books(t, "Attack control states B Pte Ltd")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	worker := store(t, "fyniq_worker")
	var saved storage.ControlState
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		saved, err = u.Compliance().SaveState(ctx, storage.ControlState{Control: "CTL-FIN-08", Status: "pass", Detail: "Each passed",
			RunAt: now, ChangedAt: now}, 0)
		return err
	}); err != nil || saved.Version != 1 {
		t.Fatalf("the first state: %+v %v", saved, err)
	}
	insert := `INSERT INTO fyniq.control_state (control, status, detail, failing_since, run_at, changed_at) VALUES ($1, $2, 'x', $3, $4, $4)`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the API writing a state", "fyniq_api", insert, []any{"CTL-AUD-05", "pass", nil, now}, []string{"42501"}},
		{"a second state of a control", "fyniq_worker", insert, []any{"CTL-FIN-08", "pass", nil, now}, []string{"23505"}},
		{"an unknown status", "fyniq_worker", insert, []any{"CTL-AUD-05", "green", nil, now}, []string{"23514"}},
		{"a failure that never began", "fyniq_worker", insert, []any{"CTL-AUD-05", "fail", nil, now}, []string{"23514"}},
		{"a pass still failing", "fyniq_worker", insert, []any{"CTL-AUD-05", "pass", now, now}, []string{"23514"}},
		{"a status changed without its audit event", "fyniq_worker",
			`UPDATE fyniq.control_state SET status = 'fail', failing_since = run_at, version = version + 1 WHERE id = $1`, []any{saved.ID}, []string{"FY061"}},
		{"a state changed out of order", "fyniq_worker", `UPDATE fyniq.control_state SET detail = 'y', version = version + 2 WHERE id = $1`,
			[]any{saved.ID}, []string{"FY308"}},
		{"the worker deleting a state", "fyniq_worker", `DELETE FROM fyniq.control_state WHERE id = $1`, []any{saved.ID}, []string{"42501"}},
		{"the owner deleting a state", "fyniq_migrator", `DELETE FROM fyniq.control_state WHERE id = $1`, []any{saved.ID}, []string{"FY308"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	// The store's own change of status is audited: control.failed, then control.recovered.
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		failed, err := u.Compliance().SaveState(ctx, storage.ControlState{Control: "CTL-FIN-08", Status: "fail", Detail: "1 found",
			FailingSince: &now, RunAt: now, ChangedAt: now}, 1)
		if err != nil {
			return err
		}
		_, err = u.Compliance().SaveState(ctx, storage.ControlState{Control: "CTL-FIN-08", Status: "pass", Detail: "Each passed",
			RunAt: now, ChangedAt: now}, failed.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type = 'control_state' AND object_id = $1 ORDER BY seq`, saved.ID)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"control.status_changed", "control.failed", "control.recovered"}; len(actions) != 3 ||
		actions[0] != want[0] || actions[1] != want[1] || actions[2] != want[2] {
		t.Errorf("the state's audit events: %v, want %v", actions, want)
	}
	for org, want := range map[uuid.UUID]int{a.Org: 1, b.Org: 0} {
		var n int
		if err := asOrg(ctx, conn(t, "fyniq_api"), org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.control_state`).Scan(&n)
		}); err != nil || n != want {
			t.Errorf("organisation %s sees %d states (%v), want %d", org, n, err, want)
		}
	}
}

// compliance.run saves each control's status and tells its owners once when a detective check newly fails it; a control
// still failing tells nobody again and keeps when its failure began; its recovery is recorded and tells nobody
// (design D1, D5).
// req: CMP-01
// req: compliance/compliance-center Failing controls block releases or page the owner
// req: compliance/compliance-center The Compliance Center shows controls and raises tasks
func TestAFailingControlIsToldOnceAndItsRecoveryRecorded(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Control statuses Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester := founder(t, api, b.Org, 1, now)
	assets, err := registry.LoadReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	controls := []registry.Control{
		{ID: "CTL-FIN-08", Owner: "Controller", Checks: []registry.Check{{Frequency: "CI", Status: "running", How: "make test"},
			{Frequency: "N", Status: "running", How: "verify payments.dual-control"}}},
		{ID: "CTL-CHG-08", Owner: "Owner", Checks: []registry.Check{{Frequency: "CI", Status: "running", How: "make images"}}},
	}
	frozen := clock.NewFrozen(now)
	var told []string
	runner := compliance.Runner{Store: store(t, "fyniq_worker"), Clock: frozen, Controls: controls, Assets: assets,
		Release: compliance.Release{Tag: "v1.2.0", Repository: "https://github.com/cognizhi/fyniq"},
		Tell: func(_ context.Context, _ storage.UnitOfWork, _ uuid.UUID, c registry.Control, s storage.ControlState) error {
			told = append(told, c.ID+" "+s.Status)
			return nil
		}}
	run := func() map[string]storage.ControlState {
		t.Helper()
		rep, err := runner.Verify(ctx, b.Org) // Nightly would run every organisation of the shared database
		if err == nil {
			err = runner.Statuses(ctx, b.Org, &rep)
		}
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]storage.ControlState{}
		for _, s := range rep.States {
			out[s.Control] = s
		}
		return out
	}
	if got := run(); got["CTL-FIN-08"].Status != "pass" || got["CTL-CHG-08"].Status != "pass" || len(told) != 0 {
		t.Fatalf("clean books: %+v, told %v", got, told)
	}

	// A bank change approved by whoever asked for it, seeded past the database's guards.
	envelope := `{"object":{"type":"payee_bank_change"}}`
	sum := sha256.Sum256([]byte(envelope))
	if err := asOrg(ctx, conn(t, "fyniq"), b.Org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		var approval uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope,
			envelope_hash, policy_version, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at)
			VALUES ('payee_bank_change', $1, 'payee_bank_change.approve', 'Change the bank details', '{}', $2, $3, 'p', $4, $5,
			'session_step_up', $5, $6, 'approved', $5) RETURNING id`, uuid.NewString(), envelope, sum[:], requester, now,
			now.Add(time.Hour)).Scan(&approval); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.approval_signature (request_id, step, signer, decision, envelope_hash, mfa_method,
			mfa_at, signed_at) VALUES ($1, 1, $2, 'approve', $3, 'session_step_up', $4, $4)`, approval, requester, sum[:], now)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	frozen.Advance(time.Minute)
	failed := run()["CTL-FIN-08"]
	if failed.Status != "fail" || failed.FailingSince == nil || len(told) != 1 || told[0] != "CTL-FIN-08 fail" {
		t.Fatalf("a control newly failing: %+v, told %v", failed, told)
	}
	frozen.Advance(time.Hour)
	still := run()["CTL-FIN-08"]
	if still.Status != "fail" || !still.FailingSince.Equal(*failed.FailingSince) || !still.ChangedAt.Equal(failed.ChangedAt) || len(told) != 1 {
		t.Errorf("a control still failing: %+v, told %v", still, told)
	}
	frozen.Advance(2 * 24 * time.Hour) // the breach leaves the window
	recovered := run()["CTL-FIN-08"]
	if recovered.Status != "pass" || recovered.FailingSince != nil || len(told) != 1 {
		t.Errorf("a recovery: %+v, told %v", recovered, told)
	}
}
