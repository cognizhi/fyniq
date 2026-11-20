//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A delegation of approval authority lends no right and is kept (migration 001200): it is only ended, once; a signature on behalf of someone
// needs a live delegation on the day it was signed (by signed_at, not the clock), is never for the requester or for a person who signed or has
// been signed for, and a person's own signature after a delegate signed for them is refused; a reminder is logged by the worker alone and kept.
// req: FR-APR-06
// req: FR-APR-08
// req: FM-1
// asvs: V8.2.2, V8.3.1, V16.3.3
func TestAttackDelegationsLendNoMoreThanTheirDaysAndAreKept(t *testing.T) {
	ctx := context.Background()
	a := books(t, "Attack delegations Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester, delegator, delegate, other, later := founder(t, st, a.Org, 11, now), founder(t, st, a.Org, 12, now), founder(t, st, a.Org, 13, now), founder(t, st, a.Org, 14, now), founder(t, st, a.Org, 15, now)
	today := time.Date(now.In(clock.Singapore).Year(), now.In(clock.Singapore).Month(), now.In(clock.Singapore).Day(), 0, 0, 0, 0, time.UTC)

	envelope := `{"object":{"id":"bank.tolerance_base","type":"organisation_setting"},"delegation":"attack"}`
	sum := sha256.Sum256([]byte(envelope))
	var req storage.ApprovalRequest
	var live, ended storage.ApprovalDelegation
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		req, err = u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: "organisation_setting", ObjectKey: "bank.tolerance_base",
			Operation: "organisation_setting.change", Summary: "Change the tolerance", Change: []byte(`{"execute":{}}`), Envelope: envelope,
			EnvelopeHash: sum[:], PolicyVersion: "founder/organisation_setting@2026-10-20.1", PolicySteps: []storage.ApprovalStep{{Quorum: 3}},
			RequestedBy: requester, RequestedAt: now.Add(-time.Minute),
			RequesterMFAMethod: "session_step_up", RequesterMFAAt: now.Add(-time.Minute), ExpiresAt: now.Add(7 * 24 * time.Hour)})
		if err != nil {
			return err
		}
		// the delegator delegates to the delegate for today and the next days; another delegation of theirs to the other ended; one for later days
		if live, err = u.ApprovalDelegations().Create(ctx, storage.NewApprovalDelegation{Delegator: delegator, Delegate: delegate, StartsOn: today,
			EndsOn: today.AddDate(0, 0, 5), Reason: "Leave", At: now.Add(-time.Minute)}); err != nil {
			return err
		}
		if ended, err = u.ApprovalDelegations().Create(ctx, storage.NewApprovalDelegation{Delegator: other, Delegate: delegate, StartsOn: today,
			EndsOn: today.AddDate(0, 0, 5), Reason: "Leave", At: now.Add(-time.Minute)}); err != nil {
			return err
		}
		if ended, err = u.ApprovalDelegations().End(ctx, ended.ID, ended.Version, other, now.Add(-30*time.Second)); err != nil {
			return err
		}
		_, err = u.ApprovalDelegations().Create(ctx, storage.NewApprovalDelegation{Delegator: later, Delegate: delegate, StartsOn: today.AddDate(0, 0, 10),
			EndsOn: today.AddDate(0, 0, 12), Reason: "Later", At: now.Add(-time.Minute)})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	apiRole := func(stmt string, args ...any) error {
		return asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	insert := `INSERT INTO fyniq.approval_delegation (delegator, delegate, starts_on, ends_on, reason, created_by, created_at) VALUES ($1, $2, $3, $4, 'x', $5, $6)`
	wantState(t, "a delegation to oneself", apiRole(insert, delegator, delegator, today, today, delegator, now), "23514")
	wantState(t, "a delegation of over 90 days", apiRole(insert, delegator, delegate, today, today.AddDate(0, 0, 91), delegator, now), "23514")
	wantState(t, "a delegation ending before it starts", apiRole(insert, delegator, delegate, today, today.AddDate(0, 0, -1), delegator, now), "23514")
	wantState(t, "a delegation made by someone other than the delegator", apiRole(insert, delegator, delegate, today, today, delegate, now), "23514")
	wantState(t, "a delegation's days changed", apiRole(`UPDATE fyniq.approval_delegation SET ends_on = ends_on + 30, version = version + 1 WHERE id = $1`, live.ID), "FYD01", "42501")
	wantState(t, "a delegation's delegate changed", apiRole(`UPDATE fyniq.approval_delegation SET delegate = $2, version = version + 1 WHERE id = $1`, live.ID, other), "FYD01", "42501")
	wantState(t, "a delegation deleted", apiRole(`DELETE FROM fyniq.approval_delegation WHERE id = $1`, live.ID), "FYD01", "42501")
	wantState(t, "an ended delegation ended again", apiRole(`UPDATE fyniq.approval_delegation SET status = 'ended', ended_at = now(), ended_by = $2, version = version + 1 WHERE id = $1`, ended.ID, other), "FYD01")

	sign := `INSERT INTO fyniq.approval_signature (request_id, step, signer, decision, envelope_hash, mfa_method, mfa_at, signed_at, on_behalf_of)
	         VALUES ($1, 1, $2, 'approve', $3, 'session_step_up', $4, $4, $5)`
	wantState(t, "a signature on an ended delegation", apiRole(sign, req.ID, delegate, sum[:], now, other), "FYD02")
	wantState(t, "a signature on a delegation of later days", apiRole(sign, req.ID, delegate, sum[:], now, later), "FYD02")
	wantState(t, "a signature on behalf of the requester", apiRole(sign, req.ID, delegate, sum[:], now, requester), "FYD02")
	wantState(t, "a signature on behalf of oneself", apiRole(sign, req.ID, delegate, sum[:], now, delegate), "23514", "FYD02")
	wantState(t, "a signature on a delegation before it was made", apiRole(sign, req.ID, delegate, sum[:], now.Add(-2*time.Minute), delegator), "FYD02", "23514", "FY103")
	wantState(t, "a signature by the wrong delegate", apiRole(sign, req.ID, other, sum[:], now, delegator), "FYD02")

	// a live delegation: the delegate's signature on behalf of the delegator is kept
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: req.ID, Step: 1, Signer: delegate, Decision: "approve",
			EnvelopeHash: sum[:], MFAMethod: "session_step_up", MFAAt: now, SignedAt: now, OnBehalfOf: &delegator})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// the delegator was signed for: their own signature is refused, and so is a second one for them
	wantState(t, "the delegator signing after their delegate signed for them", apiRole(sign, req.ID, delegator, sum[:], now, nil), "FYD02")
	wantState(t, "a second signature for the same delegator", apiRole(sign, req.ID, other, sum[:], now, delegator), "FYD02")
	wantState(t, "the delegate signing again", apiRole(sign, req.ID, delegate, sum[:], now, nil), "23505", "FYD02")
	// a signature never changes, the on-behalf column too
	wantState(t, "a signature's delegator changed", apiRole(`UPDATE fyniq.approval_signature SET on_behalf_of = $1`, other), "42501", "FY102")

	// reminders: the worker alone logs one, once, and nobody changes or deletes it
	wantState(t, "the API logging a reminder", apiRole(`INSERT INTO fyniq.approval_reminder (request_id, kind, number, sent_at) VALUES ($1, 'reminder', 1, $2)`, req.ID, now), "42501")
	worker := store(t, "fyniq_worker")
	logReminder := func(kind string, n int) error {
		return worker.Do(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "system", ID: "approvals.remind"}}), a.Org,
			func(ctx context.Context, u storage.UnitOfWork) error {
				return u.ApprovalReminders().Record(ctx, req.ID, kind, n, now)
			})
	}
	if err := logReminder("reminder", 1); err != nil {
		t.Fatal(err)
	}
	if err := logReminder("escalation", 1); err != nil {
		t.Fatal(err)
	}
	if err := logReminder("reminder", 1); err == nil {
		t.Error("a reminder logged twice")
	}
	err := asOrg(ctx, conn(t, "fyniq_worker"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.approval_reminder SET number = 2`)
		return err
	})
	wantState(t, "a reminder changed", err, "42501", "FYD03")
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		sent, err := u.ApprovalReminders().Sent(ctx, req.ID)
		if err == nil && len(sent) != 2 {
			t.Errorf("the log holds %d, want 2", len(sent))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
