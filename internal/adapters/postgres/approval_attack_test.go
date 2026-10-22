//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// founder joins org as an Owner through an invitation and returns their user ID; n makes the invitation's code hash.
func founder(t *testing.T, st *postgres.Store, org uuid.UUID, n byte, now time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	hash := make([]byte, 32)
	hash[0], hash[1], hash[2] = 9, n, org[15]
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
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
	requester, approver := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now)
	envelope := `{"object":{"id":"bank.tolerance_base","type":"organisation_setting"}}`
	sum := sha256.Sum256([]byte(envelope))
	var req storage.ApprovalRequest
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		req, err = u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: "organisation_setting", ObjectKey: "bank.tolerance_base",
			Operation: "organisation_setting.change", Summary: "Change the tolerance", Change: []byte(`{"execute":{}}`), Envelope: envelope,
			EnvelopeHash: sum[:], PolicyVersion: "founder/organisation_setting@2026-10-20.1", PolicySteps: []storage.ApprovalStep{{Quorum: 1}},
			RequestedBy: requester, RequestedAt: now,
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

// request records a pending request in org under policy steps, with an envelope of its own; change is its change.
func request(t *testing.T, st *postgres.Store, org, requester uuid.UUID, objectType, key, operation, change string, steps []storage.ApprovalStep,
	now time.Time) storage.ApprovalRequest {
	t.Helper()
	envelope := fmt.Sprintf(`{"object":{"id":%q,"type":%q},"nonce":%q}`, key, objectType, uuid.NewString())
	sum := sha256.Sum256([]byte(envelope))
	var req storage.ApprovalRequest
	if err := st.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		req, err = u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: objectType, ObjectKey: key, Operation: operation,
			Summary: "A change", Change: []byte(change), Envelope: envelope, EnvelopeHash: sum[:], PolicyVersion: "p", PolicySteps: steps,
			RequestedBy: requester, RequestedAt: now, RequesterMFAMethod: "session_step_up", RequesterMFAAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return req
}

// signs signs req's step as signer, through the unit of work, and returns the error.
func signs(st *postgres.Store, org uuid.UUID, req storage.ApprovalRequest, step int, signer uuid.UUID, now time.Time) error {
	return st.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: req.ID, Step: step, Signer: signer, Decision: "approve",
			EnvelopeHash: req.EnvelopeHash, MFAMethod: "session_step_up", MFAAt: now, SignedAt: now})
		return err
	})
}

// decides moves req to status through the unit of work, and returns the error.
func decides(st *postgres.Store, org uuid.UUID, req storage.ApprovalRequest, status string, now time.Time) error {
	return st.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Approvals().Decide(ctx, req.ID, req.Version, status, "", now)
		return err
	})
}

// A request keeps its policy's steps, and the database holds the engine to them: a signature signs the current
// step only, in order, to its quorum, and a request is approved only when every step has its quorum (rev 0.40).
// req: FR-APR-01
// req: FR-APR-03
// asvs: V8.2.2
func TestAttackApprovalStepsAreSignedInOrder(t *testing.T) {
	ctx := context.Background()
	a := books(t, "Attack steps Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester, ann, ben := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now), founder(t, st, a.Org, 3, now)
	req := request(t, st, a.Org, requester, "organisation_setting", "bank.tolerance_base", "organisation_setting.change", `{"execute":{}}`,
		[]storage.ApprovalStep{{Quorum: 1}, {Quorum: 1}}, now)
	wantState(t, "signing the second step first", signs(st, a.Org, req, 2, ann, now), "FY106")
	wantState(t, "signing a step the policy does not have", signs(st, a.Org, req, 3, ann, now), "FY106")
	if err := signs(st, a.Org, req, 1, ann, now); err != nil {
		t.Fatal(err)
	}
	wantState(t, "signing a step past its quorum", signs(st, a.Org, req, 1, ben, now), "FY106")
	wantState(t, "approving with a step unsigned", decides(st, a.Org, req, "approved", now), "FY100")
	if err := signs(st, a.Org, req, 2, ben, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, a.Org, req, "approved", now); err != nil {
		t.Fatalf("approving with every step signed: %v", err)
	}

	envelope := `{"steps":"bad"}`
	sum := sha256.Sum256([]byte(envelope))
	for _, steps := range []string{`[]`, `[{"quorum":0,"roles":[],"users":[]}]`, `[{"quorum":1}]`, `[{"quorum":1,"roles":["owner"],"users":["` + ann.String() + `"]}]`,
		`[{"quorum":1,"roles":[],"users":["not-a-uuid"]},{"quorum":1,"roles":[],"users":[]}]`, `[{"quorum":6,"roles":[],"users":[]}]`,
		`[{"quorum":1,"roles":[],"users":[],"limit":"5000.00"}]`} {
		err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope, envelope_hash,
				policy_version, policy_steps, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at)
				VALUES ('organisation_setting', 'retention.records_years', 'organisation_setting.change', 'x', '{}', $1, $2, 'p', $3, $4, $5,
				'session_step_up', $5, $6)`, envelope, sum[:], steps, requester, now, now.Add(time.Hour))
			return err
		})
		wantState(t, "a request whose steps are "+steps, err, "23514")
	}
}

// A version of an approval policy exists only as the next one, with the approved request that holds exactly its
// steps; it never changes; adding one voids every pending request of its object type, audited (rev 0.40).
// req: FR-APR-09
// req: FR-APR-10
// asvs: V8.2.2, V16.3.3
func TestAttackApprovalPoliciesAreApprovedVersions(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack policies A Pte Ltd"), books(t, "Attack policies B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester, approver := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now)
	one := []storage.ApprovalStep{{Quorum: 1}}
	steps := []storage.ApprovalStep{{Quorum: 2, Roles: []string{"owner"}}}
	waiting := request(t, st, a.Org, requester, "organisation_setting", "bank.tolerance_base", "organisation_setting.change", `{"execute":{}}`, one, now)
	pol := request(t, st, a.Org, requester, "approval_policy", "organisation_setting", "approval_policy.change",
		`{"execute":{"object_type":"organisation_setting","version":1,"steps":[{"quorum":2,"roles":["owner"],"users":[]}]}}`, one, now)
	add := func(n storage.NewApprovalPolicy) error {
		return st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.ApprovalPolicies().Add(ctx, n)
			return err
		})
	}
	ok := storage.NewApprovalPolicy{ObjectType: "organisation_setting", Version: 1, Steps: steps, RequestID: pol.ID, CreatedBy: requester, CreatedAt: now}
	wantState(t, "a version whose request is pending", add(ok), "FY105")
	if err := signs(st, a.Org, pol, 1, approver, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, a.Org, pol, "approved", now); err != nil {
		t.Fatal(err)
	}
	other, gap, by, typ := ok, ok, ok, ok
	other.Steps = one
	gap.Version = 2
	by.CreatedBy = approver
	typ.ObjectType = "membership"
	wantState(t, "a version with steps other than the approved", add(other), "FY105")
	wantState(t, "a version past the next", add(gap), "FY105")
	wantState(t, "a version made by someone other than its requester", add(by), "FY105")
	wantState(t, "a version of another object type", add(typ), "FY105")
	if err := add(ok); err != nil {
		t.Fatalf("the approved version: %v", err)
	}
	wantState(t, "the same version again", add(ok), "FY105", "23505")
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Approvals().Get(ctx, waiting.ID)
		if err == nil && (r.Status != "void" || r.StatusReason != "the approval policy changed after the request") {
			t.Errorf("a pending request of the object type after its policy changed: %+v", r)
		}
		ps, err2 := u.ApprovalPolicies().List(ctx)
		if err2 != nil || len(ps) != 1 || ps[0].Version != 1 || len(ps[0].Steps) != 1 || ps[0].Steps[0].Quorum != 2 || ps[0].RequestID != pol.ID {
			t.Errorf("the policies: %+v (%v)", ps, err2)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "the API changing a version", "UPDATE fyniq.approval_policy SET steps = '[]'", "42501"},
		{"fyniq_api", "the API deleting a version", "DELETE FROM fyniq.approval_policy", "42501"},
		{"fyniq_migrator", "the owner changing a version", `UPDATE fyniq.approval_policy SET created_at = now()`, "FY104"},
		{"fyniq_migrator", "the owner deleting a version", "DELETE FROM fyniq.approval_policy", "FY104"},
		{"fyniq_worker", "the worker adding a version", `INSERT INTO fyniq.approval_policy (object_type, version, steps, request_id, created_by, created_at)
			SELECT 'membership', 1, '[{"quorum":1,"roles":[],"users":[]}]', gen_random_uuid(), gen_random_uuid(), now()`, "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.what, err, c.state)
	}
	if n := count(t, b.Org, "fyniq.approval_policy"); n != 0 {
		t.Errorf("B sees %d of A's policy versions", n)
	}
	if got := actions(t, a.Org, waiting.ID); !slices.Contains(got, "approval.void") {
		t.Errorf("the voided request's trail: %v", got)
	}
}

// An organisation activates a statutory pack version only as the record of the approved request to activate exactly
// that version: made by the proposer, signed by the approver, who is someone else. An activation never changes, the
// worker never writes one, and another organisation sees none (rev 0.41).
// req: ENG-32
// req: SOD-10
// req: FR-ORG-05
// asvs: V8.2.2, V8.3.1
func TestAttackPackActivationIsTheApprovedRequest(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack activation A Pte Ltd"), books(t, "Attack activation B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	proposer, approver, other := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now), founder(t, st, a.Org, 3, now)
	const version = "sg-gst@2026.1"
	req := request(t, st, a.Org, proposer, "statutory_pack", version, "statutory_pack.activate", `{"execute":{}}`,
		[]storage.ApprovalStep{{Quorum: 1}}, now)
	activate := func(n storage.NewPackActivation) error {
		return st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Packs().Activate(ctx, n)
			return err
		})
	}
	ok := storage.NewPackActivation{VersionID: version, ProposedBy: proposer, ApprovedBy: approver, Approval: req.ID, ActivatedAt: now}
	wantState(t, "an activation whose request is pending", activate(ok), "FY107")
	if err := signs(st, a.Org, req, 1, approver, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, a.Org, req, "approved", now); err != nil {
		t.Fatal(err)
	}
	notSigner, byApprover, self := ok, ok, ok
	notSigner.ApprovedBy = other
	byApprover.ProposedBy = approver
	self.ApprovedBy = proposer
	wantState(t, "an activation approved by someone who did not sign", activate(notSigner), "FY107")
	wantState(t, "an activation proposed by someone other than the requester", activate(byApprover), "FY107")
	wantState(t, "an activation approved by its proposer", activate(self), "FY107", "23514")
	err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.statutory_pack_activation (version_id, proposed_by, approved_by, approval_id) VALUES ($1, $2, $3, $4)`,
			version, proposer, approver, req.ID)
		return err
	})
	wantState(t, "an activation without its audit event", err, "FY060", "FY061")
	if err := activate(ok); err != nil {
		t.Fatalf("the approved activation: %v", err)
	}
	wantState(t, "the same version again", activate(ok), "23505")
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "the API changing an activation", "UPDATE fyniq.statutory_pack_activation SET activated_at = now()", "42501"},
		{"fyniq_api", "the API deleting an activation", "DELETE FROM fyniq.statutory_pack_activation", "42501"},
		{"fyniq_migrator", "the owner changing an activation", "UPDATE fyniq.statutory_pack_activation SET activated_at = now()", "FY107"},
		{"fyniq_migrator", "the owner deleting an activation", "DELETE FROM fyniq.statutory_pack_activation", "FY107"},
		{"fyniq_worker", "the worker activating a version", `INSERT INTO fyniq.statutory_pack_activation (version_id, proposed_by, approved_by, approval_id)
			SELECT 'sg-gst@2026.1', gen_random_uuid(), gen_random_uuid(), gen_random_uuid()`, "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.what, err, c.state)
	}
	if n := count(t, b.Org, "fyniq.statutory_pack_activation"); n != 0 {
		t.Errorf("B sees %d of A's activations", n)
	}
	if n := count(t, a.Org, "fyniq.statutory_pack_activation"); n != 1 {
		t.Errorf("A sees %d activations, not 1", n)
	}
}

// A version of the team mode exists only as the next one, with the approved switch that holds exactly it; adding one
// voids every pending request of the organisation, audited. An override of a conflict exists only for the version in
// force, with the approved switch or role change that holds exactly it. Neither ever changes, and another organisation
// sees neither (rev 0.42).
// req: access/team-modes Mode switching
// req: SOD-6
// req: FR-ADM-04
// asvs: V8.2.2, V16.3.3
func TestAttackTeamModesAreApprovedSwitches(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack team mode A Pte Ltd"), books(t, "Attack team mode B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester, approver := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now)
	one := []storage.ApprovalStep{{Quorum: 1}}
	waiting := request(t, st, a.Org, requester, "organisation_setting", "bank.tolerance_base", "organisation_setting.change", `{"execute":{}}`, one, now)
	sw := request(t, st, a.Org, requester, "team_mode", "organisation", "team_mode.change",
		`{"execute":{"mode":"standard","version":1,"overrides":[{"user_id":"`+approver.String()+`","conflict":"SOD-6","reason":"Founder"}]}}`, one, now)
	add := func(n storage.NewTeamMode) error {
		return st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.TeamModes().Add(ctx, n)
			return err
		})
	}
	override := func(n storage.NewSoDOverride) error {
		return st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.TeamModes().Override(ctx, n)
			return err
		})
	}
	ok := storage.NewTeamMode{Version: 1, Mode: "standard", RequestID: sw.ID, CreatedBy: requester, CreatedAt: now}
	okOverride := storage.NewSoDOverride{TeamModeVersion: 1, UserID: approver, Conflict: "SOD-6", Reason: "Founder", RequestID: sw.ID,
		CreatedBy: requester, CreatedAt: now}
	wantState(t, "a version whose switch is pending", add(ok), "FY108")
	if err := signs(st, a.Org, sw, 1, approver, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, a.Org, sw, "approved", now); err != nil {
		t.Fatal(err)
	}
	mode, gap, by := ok, ok, ok
	mode.Mode = "enterprise"
	gap.Version = 2
	by.CreatedBy = approver
	wantState(t, "a version of another mode than the approved", add(mode), "FY108")
	wantState(t, "a version past the next", add(gap), "FY108")
	wantState(t, "a version made by someone other than its requester", add(by), "FY108")
	wantState(t, "an override before its version", override(okOverride), "FY109", "23503")
	if err := add(ok); err != nil {
		t.Fatalf("the approved version: %v", err)
	}
	wantState(t, "the same version again", add(ok), "FY108", "23505")
	reason, who, conflict := okOverride, okOverride, okOverride
	reason.Reason = "Another reason"
	who.UserID = requester
	conflict.Conflict = "SOD-2"
	wantState(t, "an override with another reason than the approved", override(reason), "FY109")
	wantState(t, "an override of another member", override(who), "FY109")
	wantState(t, "an override of another conflict", override(conflict), "FY109")
	if err := override(okOverride); err != nil {
		t.Fatalf("the approved override: %v", err)
	}
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Approvals().Get(ctx, waiting.ID)
		if err == nil && (r.Status != "void" || r.StatusReason != "the team mode changed after the request") {
			t.Errorf("a pending request after the team mode changed: %+v", r)
		}
		ms, err2 := u.TeamModes().List(ctx)
		os, err3 := u.TeamModes().Overrides(ctx)
		if err2 != nil || err3 != nil || len(ms) != 1 || ms[0].Mode != "standard" || len(os) != 1 || os[0].UserID != approver {
			t.Errorf("the team modes %+v (%v) and overrides %+v (%v)", ms, err2, os, err3)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "the API changing a version", "UPDATE fyniq.team_mode SET mode = 'founder'", "42501"},
		{"fyniq_api", "the API deleting an override", "DELETE FROM fyniq.sod_override", "42501"},
		{"fyniq_migrator", "the owner changing a version", "UPDATE fyniq.team_mode SET mode = 'founder'", "FY108"},
		{"fyniq_migrator", "the owner deleting a version", "DELETE FROM fyniq.team_mode", "FY108"},
		{"fyniq_migrator", "the owner changing an override", "UPDATE fyniq.sod_override SET reason = 'x'", "FY109"},
		{"fyniq_worker", "the worker adding a version", `INSERT INTO fyniq.team_mode (version, mode, request_id, created_by, created_at)
			SELECT 2, 'founder', gen_random_uuid(), gen_random_uuid(), now()`, "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.what, err, c.state)
	}
	for _, table := range []string{"team_mode", "sod_override"} {
		if n := count(t, b.Org, table); n != 0 {
			t.Errorf("B sees %d of A's rows in %s", n, table)
		}
	}
	if got := actions(t, a.Org, waiting.ID); !slices.Contains(got, "approval.void") {
		t.Errorf("the voided request's trail: %v", got)
	}
}

// The review log never changes: an entry and its review are added once, and the one who acted never reviews it; another
// organisation sees neither (FM-3, rev 0.46).
// req: FM-3
// asvs: V8.2.2, V16.3.3
func TestAttackTheReviewLogNeverChanges(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack review log A Pte Ltd"), books(t, "Attack review log B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	actor, reviewer := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now)
	var entry uuid.UUID
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		entry, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewFounderBootstrap, ObjectType: "membership",
			ObjectKey: "x", Summary: "Give Ann the role owner", Actor: &actor, LoggedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	review := func(by uuid.UUID, at time.Time) error {
		return st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.ReviewLog().Review(ctx, entry, by, at, "") })
	}
	wantState(t, "the one who acted reviewing", review(actor, now), "FY151")
	wantState(t, "a review before the action", review(reviewer, now.Add(-time.Hour)), "FY151")
	if err := review(reviewer, now); err != nil {
		t.Fatalf("another founder's review: %v", err)
	}
	wantState(t, "a second review", review(reviewer, now), "23505")
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "the API changing an entry", "UPDATE fyniq.review_log_entry SET summary = 'x'", "42501"},
		{"fyniq_api", "the API deleting a review", "DELETE FROM fyniq.review_log_review", "42501"},
		{"fyniq_migrator", "the owner changing an entry", "UPDATE fyniq.review_log_entry SET summary = 'x'", "FY150"},
		{"fyniq_migrator", "the owner deleting a review", "DELETE FROM fyniq.review_log_review", "FY150"},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.what, err, c.state)
	}
	for _, table := range []string{"review_log_entry", "review_log_review"} {
		if n := count(t, b.Org, table); n != 0 {
			t.Errorf("B sees %d of A's rows in %s", n, table)
		}
	}
}

// A period reopens only on its latest close and an approved request to reopen it, signed by the one who reopens it,
// with the close's snapshot as the one before and an after snapshot whose hash is its own; a reopening never changes
// (FR-GL-08, FM-1, rev 0.51).
// req: FR-GL-08
// req: FM-1
// asvs: V8.2.2, V16.3.3
func TestAttackAReopeningIsTheApprovedRequest(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack reopen Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester, approver := founder(t, st, b.Org, 1, now), founder(t, st, b.Org, 2, now)
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	svc := closing.Service{Store: st}
	checklistDone(t, st, b.Org, b.Oct.ID)
	c, err := svc.RunChecks(as("preparer"), b.Org, b.Oct.ID)
	if err != nil {
		t.Fatal(err)
	}
	closed, _, err := svc.Close(as("attester"), b.Org, b.Oct.ID, c.ID, "Reviewed.")
	if err != nil {
		t.Fatal(err)
	}
	req := request(t, st, b.Org, requester, "period", b.Oct.ID.String(), "period.reopen", `{"execute":{}}`, []storage.ApprovalStep{{Quorum: 1}}, now)
	after := `{"after":true}`
	insert := func(closeID uuid.UUID, by string, before []byte, afterHash string) string {
		return fmt.Sprintf(`INSERT INTO fyniq.period_reopen (period_id, close_id, approval_id, reason, reopened_by, reopened_at, reopened_revision, before_hash,
  after_snapshot, after_hash) VALUES ('%s', '%s', '%s', 'Late invoice', '%s', now(), 1, '\x%x', '%s', %s)`, b.Oct.ID, closeID, req.ID, by, before, after, afterHash)
	}
	good := "sha256(convert_to('" + after + "', 'UTF8'))"
	byApprover := "user:" + approver.String()
	try := func(what, stmt, state string) {
		t.Helper()
		err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		wantState(t, what, err, state)
	}
	try("a reopening on a pending request", insert(closed.ID, byApprover, closed.SnapshotHash, good), "FY155")
	if err := signs(st, b.Org, req, 1, approver, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, b.Org, req, "approved", now); err != nil {
		t.Fatal(err)
	}
	try("a reopening by someone who did not sign", insert(closed.ID, "user:"+requester.String(), closed.SnapshotHash, good), "FY155")
	try("a reopening of a close that is not the latest", insert(uuid.Must(uuid.NewV7()), byApprover, closed.SnapshotHash, good), "FY155")
	try("a reopening whose before is not the close's snapshot", insert(closed.ID, byApprover, make([]byte, 32), good), "FY155")
	try("an after snapshot whose hash is not its own", insert(closed.ID, byApprover, closed.SnapshotHash, "sha256('other')"), "23514")
	// Through the unit of work, by the signer: it is recorded, and never changes after.
	if err := st.Do(as(approver.String()), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := closing.Reopen(ctx, u, b.Oct.ID, req.ID, "Late invoice")
		return err
	}); err != nil {
		t.Fatalf("the approved reopening: %v", err)
	}
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "the owner rewriting a reopening", "UPDATE fyniq.period_reopen SET reason = 'other'", "FY154"},
		{"fyniq_migrator", "the owner removing a reopening", "DELETE FROM fyniq.period_reopen", "FY154"},
		{"fyniq_api", "the API rewriting a reopening", "UPDATE fyniq.period_reopen SET reason = 'other'", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.what, err, c.state)
	}
	if n := count(t, b.Org, "fyniq.period_reopen"); n != 1 {
		t.Errorf("%d reopenings, not 1", n)
	}
}
