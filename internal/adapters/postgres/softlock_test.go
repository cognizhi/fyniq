//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func actingAs(ctx context.Context, typ, id string) context.Context {
	return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: typ, ID: id}})
}

// A soft-locked period (FR-GL-09) refuses every posting but a person's own under a live pass of the period, made with a reason: a job, the proposal executor (which acts as the
// approving person on behalf of another and carries the AI's proposal), an integration, a person with no pass and a person whose pass ended are all refused with one code. A posting
// under a pass is kept on the exceptions report with the reason, in the same unit of work. The hard close is not changed.
// req: FR-GL-09
// asvs: V8.2.2, V4.2.1, V16.3.3
func TestAttackASoftLockedPeriodTakesOnlyAPersonsPassAndKeepsThePostingOnTheReport(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack soft locks Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	ann, ben := founder(t, st, b.Org, 31, now), founder(t, st, b.Org, 32, now)
	svc := journal.Service{Store: st}
	asAnn, asBen := actingAs(ctx, "user", ann.String()), actingAs(ctx, "user", ben.String())

	draft := func(day int) uuid.UUID {
		t.Helper()
		doc, err := svc.Create(asBen, b.Org, saleDraft(t, b, porttest.Day(10, day), "10.00"))
		if err != nil {
			t.Fatal(err)
		}
		return doc.ID
	}
	post := func(c context.Context, id uuid.UUID) error { _, err := svc.Post(c, b.Org, id); return err }
	want := func(what string, err error) {
		t.Helper()
		if got := storage.CodeOf(err); got != storage.CodeSoftLocked {
			t.Errorf("%s: want %s, got %q (%v)", what, storage.CodeSoftLocked, got, err)
		}
	}

	var oct storage.Period
	if err := st.Do(asAnn, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, found, err := u.SoftLocks().OfDate(ctx, porttest.Day(10, 14))
		if err != nil || !found {
			t.Fatalf("the period of 14 Oct: %v %v", found, err)
		}
		oct, err = u.SoftLocks().SoftLock(ctx, p.ID)
		return err
	}); err != nil || oct.Status != storage.PeriodSoftLocked {
		t.Fatalf("soft-locking: %v %+v", err, oct)
	}
	// Soft-locking twice, or releasing an open period, is a state error, not a silent success.
	if err := st.Do(asAnn, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.SoftLocks().SoftLock(ctx, oct.ID)
		return err
	}); storage.CodeOf(err) != storage.CodePeriodState {
		t.Errorf("soft-locking a soft-locked period: %v", err)
	}

	id := draft(14)
	want("a person with no pass", post(asBen, id))
	want("a scheduler job", post(actingAs(ctx, "scheduler", "depreciation"), id))
	want("an integration", post(actingAs(ctx, "integration", "connector/x"), id))
	want("no actor at all", post(ctx, id))

	var pass storage.SoftLockPass
	if err := st.Do(asBen, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		pass, err = u.SoftLocks().MakePass(ctx, storage.NewSoftLockPass{PeriodID: oct.ID, UserID: ben, Reason: "A late bill the auditor asked for", CreatedAt: now, ExpiresAt: now.Add(4 * time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The pass is Ben's alone, and the AI's proposal never uses it: the executor acts as the approver on behalf of the requester, carrying the proposal.
	want("another person, with Ben's pass in the period", post(asAnn, id))
	exec := storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: ben.String()}, OnBehalfOf: &storage.Actor{Type: "user", ID: ann.String()}, Originator: "ai_proposal:" + uuid.NewString()})
	want("the proposal executor, acting as the holder of the pass", post(exec, id))
	want("a delegate acting on behalf of another, as the holder of the pass", post(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: ben.String()}, OnBehalfOf: &storage.Actor{Type: "user", ID: ann.String()}}), id))

	if err := post(asBen, id); err != nil {
		t.Fatalf("posting under the pass: %v", err)
	}
	var ex []storage.SoftLockException
	if err := st.Do(asBen, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		ex, err = u.SoftLocks().Exceptions(ctx, 10)
		return
	}); err != nil {
		t.Fatal(err)
	}
	if len(ex) != 1 || ex[0].UserID != ben || ex[0].PassID != pass.ID || ex[0].Scope != storage.ScopePeriod || ex[0].Reason != "A late bill the auditor asked for" || ex[0].SourceType != "manual_journal" ||
		!ex[0].PostingDate.Equal(porttest.Day(10, 14)) {
		t.Fatalf("the exceptions report: %+v", ex)
	}

	// An ended pass is no pass: made in the past, it ends in the past.
	if err := st.Do(asBen, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.SoftLocks().MakePass(ctx, storage.NewSoftLockPass{PeriodID: oct.ID, UserID: ann, Reason: "An old pass for the test", CreatedAt: now.Add(-10 * time.Hour), ExpiresAt: now.Add(-9 * time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want("a person whose pass ended", post(asAnn, draft(15)))

	// Released, the period takes postings again, and an hour's late posting is no exception.
	if err := st.Do(asAnn, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.SoftLocks().Unlock(ctx, oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := post(asAnn, draft(16)); err != nil {
		t.Fatalf("posting after the release: %v", err)
	}
	if err := st.Do(asBen, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		ex, err = u.SoftLocks().Exceptions(ctx, 10)
		return
	}); err != nil || len(ex) != 1 {
		t.Fatalf("a posting in an open period is no exception: %v %d", err, len(ex))
	}

	// The hard close is the database's and is not changed: a closed period still takes nothing, pass or no pass.
	if err := st.Do(asAnn, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := post(asBen, draft(17)); storage.CodeOf(err) != storage.CodePeriodClosed {
		t.Errorf("a closed period: %v", err)
	}
}

// A module lock refuses the module's postings and no other's, and a pass opens it for the person who holds it; the module of every source is listed.
// req: FR-GL-09
// asvs: V8.2.2
func TestAModuleLockStopsOnlyItsOwnModulesSources(t *testing.T) {
	for source, module := range map[string]string{"invoice": "ar", "customer_receipt": "ar", "bill": "ap", "bill_payment": "ap", "expense_claim": "ap", "bank_transaction": "banking",
		"payroll_run": "payroll", "manual_journal": "", "asset_depreciation": "", "fx_revaluation": ""} {
		if got := journal.ModuleOf(source); got != module {
			t.Errorf("the module of %s: want %q, got %q", source, module, got)
		}
	}
	ctx := context.Background()
	b := books(t, "Attack module locks Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	ann := founder(t, st, b.Org, 33, now)
	asAnn := actingAs(ctx, "user", ann.String())
	if err := st.Do(asAnn, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, _, err := u.SoftLocks().OfDate(ctx, porttest.Day(10, 14))
		if err != nil {
			return err
		}
		if _, err := u.SoftLocks().SetModule(ctx, p.ID, "ap", true, ann, now); err != nil {
			return err
		}
		if _, err := u.SoftLocks().SetModule(ctx, p.ID, "payroll", true, ann, now); err != nil {
			return err
		}
		if _, err := u.SoftLocks().SetModule(ctx, p.ID, "payroll", false, ann, now); err != nil {
			return err
		}
		if _, err := u.SoftLocks().SetModule(ctx, p.ID, "sales", true, ann, now); storage.CodeOf(err) != storage.CodeInvalid {
			t.Errorf("a module that is not one: %v", err)
		}
		locked, err := u.SoftLocks().LockedModules(ctx, p.ID)
		if err != nil || len(locked) != 1 || locked[0] != "ap" {
			t.Errorf("the locked modules: %v %v", locked, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A manual journal belongs to no module: only the whole-period lock stops it, so it posts.
	if _, err := (journal.Service{Store: st}).Post(asAnn, b.Org, func() uuid.UUID {
		doc, err := (journal.Service{Store: st}).Create(asAnn, b.Org, saleDraft(t, b, porttest.Day(10, 14), "5.00"))
		if err != nil {
			t.Fatal(err)
		}
		return doc.ID
	}()); err != nil {
		t.Fatalf("a manual journal while ap is locked: %v", err)
	}
}

// What the database holds of soft locks (migration 001450): a pass is for at most eight hours with a reason, a pass and an exception are made once and never change or go, a module lock
// is released and never deleted, and only the API writes any of them: the worker and the executor only read.
// req: FR-GL-09
// asvs: V8.2.2, V8.3.1, V16.3.3
func TestAttackSoftLockRecordsAreKeptAndOnlyTheAPIWritesThem(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack soft lock records Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	ann := founder(t, st, b.Org, 34, now)
	asAnn := actingAs(ctx, "user", ann.String())
	var period storage.Period
	var pass storage.SoftLockPass
	if err := st.Do(asAnn, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, _, err := u.SoftLocks().OfDate(ctx, porttest.Day(10, 14))
		if err != nil {
			return err
		}
		period = p
		if _, err = u.SoftLocks().SetModule(ctx, p.ID, "ar", true, ann, now); err != nil {
			return err
		}
		pass, err = u.SoftLocks().MakePass(ctx, storage.NewSoftLockPass{PeriodID: p.ID, UserID: ann, Reason: "Reason of ten or more", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	run := func(role, stmt string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	insPass := `INSERT INTO fyniq.soft_lock_pass (period_id, user_id, reason, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)`
	wantState(t, "a pass of nine hours", run("fyniq_api", insPass, period.ID, ann, "Reason of ten or more", now, now.Add(9*time.Hour)), "23514")
	wantState(t, "a pass that ends before it starts", run("fyniq_api", insPass, period.ID, ann, "Reason of ten or more", now, now.Add(-time.Hour)), "23514")
	wantState(t, "a pass with a short reason", run("fyniq_api", insPass, period.ID, ann, "short", now, now.Add(time.Hour)), "23514")
	wantState(t, "a pass for a period of nowhere", run("fyniq_api", insPass, uuid.New(), ann, "Reason of ten or more", now, now.Add(time.Hour)), "23503")
	wantState(t, "changing a pass", run("fyniq_api", `UPDATE fyniq.soft_lock_pass SET expires_at = expires_at + interval '1 hour'`), "42501")
	wantState(t, "deleting a pass", run("fyniq_api", `DELETE FROM fyniq.soft_lock_pass`), "42501")
	wantState(t, "deleting a module lock", run("fyniq_api", `DELETE FROM fyniq.period_module_lock`), "42501")
	wantState(t, "a module off the list", run("fyniq_api", `INSERT INTO fyniq.period_module_lock (period_id, module, locked, changed_by, changed_at) VALUES ($1, 'sales', true, $2, $3)`, period.ID, ann, now), "23514")
	// A superuser-free role cannot undo the guards: the triggers refuse even where a grant would allow (the owner role in a repair).
	for _, role := range []string{"fyniq_worker", "fyniq_executor", "fyniq_readonly"} {
		wantState(t, role+" making a pass", run(role, insPass, period.ID, ann, "Reason of ten or more", now, now.Add(time.Hour)), "42501")
		wantState(t, role+" locking a module", run(role, `INSERT INTO fyniq.period_module_lock (period_id, module, locked, changed_by, changed_at) VALUES ($1, 'ap', true, $2, $3)`, period.ID, ann, now), "42501")
		wantState(t, role+" recording an exception", run(role, `INSERT INTO fyniq.soft_lock_exception (pass_id, period_id, entry_id, user_id, scope, source_type, posting_date, reason, created_at)
			VALUES ($1, $2, $3, $4, 'period', 'bill', '2026-10-14', 'x', $5)`, pass.ID, period.ID, uuid.New(), ann, now), "42501")
		if err := run(role, `SELECT 1 FROM fyniq.soft_lock_pass`); role != "fyniq_readonly" && err != nil {
			t.Errorf("%s reads passes: %v", role, err)
		}
	}
	// The API records an exception for an entry once, and an exception never changes or goes.
	ins := `INSERT INTO fyniq.soft_lock_exception (pass_id, period_id, entry_id, user_id, scope, source_type, posting_date, reason, created_at) VALUES ($1, $2, $3, $4, 'ar', 'invoice', '2026-10-14', 'Reason of ten or more', $5)`
	entry := uuid.New()
	// The audit trigger wants an audit event in the same transaction; the API's unit of work writes it, a bare insert is refused.
	wantState(t, "an exception with no audit event", run("fyniq_api", ins, pass.ID, period.ID, entry, ann, now), "FY061")
}
