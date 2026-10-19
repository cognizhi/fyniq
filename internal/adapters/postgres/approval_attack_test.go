//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The database refuses an approval that is not a signature of exactly the envelope asked for (migration 000030):
// a request whose hash is not its envelope's; a signed approval without a hash or a fresh second factor, of
// another hash, by the requester, or after the request expired; a request approved without a signed approval, or
// changed after it was made; and a signature changed after it was made. A new version of the record voids the
// pending request and its signatures. Another organisation sees none of it.
// req: FR-APR-03
// req: FR-APR-05
// req: FM-1
// req: compliance/singapore-statutory Electronic signatures on approvals
// asvs: V8.2.2, V8.3.1, V16.3.3
func TestAttackApprovalsAreSignedEnvelopes(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack approvals A Pte Ltd"), books(t, "Attack approvals B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	user := func(n byte) uuid.UUID {
		hash := make([]byte, 32)
		hash[0], hash[1] = 9, n
		if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: "Founder", Roles: []string{"owner"},
				ExpiresAt: now.Add(time.Hour)})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		j, err := st.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: "Founder", Issuer: "urn:fyniq:attack", Subject: uuid.NewString(), Now: now})
		if err != nil {
			t.Fatal(err)
		}
		return j.UserID
	}
	requester, approver := user(1), user(2)
	envelope := `{"object":{"id":"bank.tolerance_base","type":"organisation_setting"}}`
	sum := sha256.Sum256([]byte(envelope))
	var req storage.ApprovalRequest
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		req, err = u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: "organisation_setting", ObjectKey: "bank.tolerance_base",
			Operation: "organisation_setting.change", Summary: "Change the tolerance", Change: []byte(`{"execute":{}}`), Envelope: envelope,
			EnvelopeHash: sum[:], PolicyVersion: "founder/organisation_setting@2026-10-20.1", RequestedBy: requester, RequestedAt: now,
			RequesterMFAMethod: "session_step_up", RequesterMFAAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	sign := `INSERT INTO fyniq.approval_signature (request_id, step, signer, decision, envelope_hash, mfa_method, mfa_at, signed_at)
	         VALUES ($1, 1, $2, 'approve', $3, $4, $5, $6)`
	other := make([]byte, 32)
	for _, c := range []struct {
		what  string
		stmt  string
		args  []any
		state []string
	}{
		{"a request whose hash is not its envelope's", `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change,
			envelope, envelope_hash, policy_version, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at)
			VALUES ('organisation_setting', 'retention.records_years', 'organisation_setting.change', 'x', '{}', '{}', $1, 'p', $2, $3,
			'session_step_up', $3, $4)`, []any{sum[:], requester, now, now.Add(time.Hour)}, []string{"23514"}},
		{"a signature without a hash", sign, []any{req.ID, approver, nil, "session_step_up", now, now}, []string{"23502", "FY100"}},
		{"a signature with a short hash", sign, []any{req.ID, approver, sum[:16], "session_step_up", now, now}, []string{"23514", "FY100"}},
		{"a signature without a second factor", sign, []any{req.ID, approver, sum[:], nil, now, now}, []string{"23502"}},
		{"a signature with an unknown second factor", sign, []any{req.ID, approver, sum[:], "password", now, now}, []string{"23514"}},
		{"a signature with a stale second factor", sign, []any{req.ID, approver, sum[:], "session_step_up", now.Add(-6 * time.Minute), now}, []string{"23514"}},
		{"a signature of another envelope", sign, []any{req.ID, approver, other, "session_step_up", now, now}, []string{"FY100"}},
		{"the requester signing their own request", sign, []any{req.ID, requester, sum[:], "session_step_up", now, now}, []string{"FY101"}},
		{"a signature after the request expired", sign, []any{req.ID, approver, sum[:], "session_step_up", now.Add(7 * 24 * time.Hour),
			now.Add(7 * 24 * time.Hour)}, []string{"FY103"}},
		{"a request approved without a signature", `UPDATE fyniq.approval_request SET status = 'approved', decided_at = now(), version = version + 1
			WHERE id = $1`, []any{req.ID}, []string{"FY100"}},
		{"the API changing a request's envelope", `UPDATE fyniq.approval_request SET envelope = '{}' WHERE id = $1`, []any{req.ID}, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.state...)
	}
	err := asOrg(ctx, conn(t, "fyniq_migrator"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.approval_request SET summary = 'Something else', version = version + 1, status = 'rejected',
			decided_at = now() WHERE id = $1`, req.ID)
		return err
	})
	wantState(t, "changing what a request asks for", err, "FY102")

	// A valid signature is kept; it never changes; a new version of the record voids the request and the signature.
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: req.ID, Step: 1, Signer: approver, Decision: "approve",
			EnvelopeHash: sum[:], MFAMethod: "session_step_up", MFAAt: now, SignedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err = asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.approval_signature SET decision = 'reject'`)
		return err
	})
	wantState(t, "the API changing a signature", err, "42501")
	err = asOrg(ctx, conn(t, "fyniq_migrator"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.approval_signature SET decision = 'reject', reason = 'no'`)
		return err
	})
	wantState(t, "changing a signature", err, "FY102")
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "bank.tolerance_base", "1.00", "2.00", now, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Approvals().Get(ctx, req.ID)
		if err != nil || r.Status != "void" || r.StatusReason == "" {
			t.Errorf("after its record changed, the request is %+v (%v)", r, err)
		}
		sigs, err := u.Approvals().Signatures(ctx, req.ID)
		if err != nil || len(sigs) != 1 || sigs[0].VoidedAt == nil {
			t.Errorf("after the request was voided, its signatures are %+v (%v)", sigs, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Tenant isolation: B sees none of A's requests or signatures.
	for _, table := range []string{"fyniq.approval_request", "fyniq.approval_signature"} {
		if n := count(t, b.Org, table); n != 0 {
			t.Errorf("B sees %d rows of A's %s", n, table)
		}
		if n := count(t, a.Org, table); n != 1 {
			t.Errorf("A sees %d rows of its %s, not 1", n, table)
		}
	}
}
