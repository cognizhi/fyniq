//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-CTL-06, SEC-06 (rev 0.162): the API attests a quarter once, after it ends, with the report as the Owner saw it,
// whose SHA-256 the database computes; audited; never changed or deleted (FY247); the worker reads it and never writes
// one, and another organisation never sees it.
// req: FR-CTL-06
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.1
func TestAnAccessReviewIsAttestedOncePerQuarter(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "founder"}})
	api := store(t, "fyniq_api")
	b := books(t, "Access review Pte Ltd")
	other := books(t, "Other access review Pte Ltd")
	n := storage.NewAccessReviewAttestation{Quarter: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), AttestedBy: "user:founder",
		AttestedAt: time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC), Note: "Ended the auditor's access", Report: `{"members":[{"display_name":"Ann"}]}`}
	attest := func(org uuid.UUID, n storage.NewAccessReviewAttestation) (a storage.AccessReviewAttestation, err error) {
		err = api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			a, err = u.AccessReviews().Attest(ctx, n)
			return err
		})
		return a, err
	}
	got, err := attest(b.Org, n)
	if sum := sha256.Sum256([]byte(n.Report)); err != nil || !bytes.Equal(got.ReportHash, sum[:]) || got.Note != n.Note || got.Quarter != n.Quarter {
		t.Fatalf("the attestation: %+v, %v", got, err)
	}
	if _, err := attest(b.Org, n); storage.CodeOf(err) != storage.CodeDuplicate {
		t.Errorf("the quarter attested again: %v", err)
	}
	early := n
	early.Quarter = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := attest(b.Org, early); storage.CodeOf(err) != storage.CodeAccessReview {
		t.Errorf("a quarter attested before it ends: %v", err)
	}
	if err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		as, err := u.AccessReviews().List(ctx)
		if len(as) != 1 || as[0].Report != n.Report {
			t.Errorf("the worker reads %+v", as)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		as, err := u.AccessReviews().List(ctx)
		if len(as) != 0 {
			t.Errorf("another organisation reads %+v", as)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if tr := trail(t, b.Org); strings.Count(tr, `"action":"access_review.attested"`) != 1 {
		t.Errorf("the attestation's audit event: %s", tr)
	}
	insert := `INSERT INTO fyniq.access_review_attestation (quarter, attested_by, attested_at, report, report_hash) VALUES ('2026-04-01', 'user:x', now(), '{}', `
	for _, c := range []struct {
		role, sql, code string
	}{
		{"fyniq_api", `UPDATE fyniq.access_review_attestation SET note = 'x'`, "42501"},
		{"fyniq_migrator", `UPDATE fyniq.access_review_attestation SET note = 'x'`, "FY247"},
		{"fyniq_migrator", `DELETE FROM fyniq.access_review_attestation`, "FY247"},
		{"fyniq_worker", insert + `sha256('{}'))`, "42501"},
		{"fyniq_api", insert + `sha256('{ }'))`, "23514"}, // a hash that is not the report's
		{"fyniq_api", insert + `sha256('{}'))`, "FY061"},  // unaudited
	} {
		err := asOrg(context.Background(), conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			if err == nil {
				_, err = tx.Exec(context.Background(), "SET CONSTRAINTS ALL IMMEDIATE")
			}
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %s: want %s, got %v", c.role, c.sql, c.code, err)
		}
	}
}

// joinAs makes the person signed in as subject a member of org with roles, until expires (nil: no end), through an
// invitation; n makes the invitation's code hash.
func joinAs(t *testing.T, st *postgres.Store, org uuid.UUID, subject string, roles []string, expires *time.Time, n byte, now time.Time) storage.Joined {
	t.Helper()
	hash := make([]byte, 32)
	hash[0], hash[1], hash[2], hash[3] = 7, n, org[15], subject[0]
	if err := st.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: subject, Roles: roles, RolesExpireAt: expires,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	j, err := st.JoinOrganisation(context.Background(), storage.NewJoin{CodeHash: hash, DisplayName: subject, Issuer: "urn:fyniq:access", Subject: subject, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// FR-ADM-01, SEC-06 (rev 0.165): a member is removed at their membership's version, each of their roles ended first,
// expired ones included, audited; the last Owner never is. A removed member is given no role, and a member with a role
// not ended is never marked removed (FY248). Accepting a new invitation makes a removed person a member again, on the
// same membership. member_sign_ins() gives each active member's last sign-in and second factor, in their organisation
// only.
// req: FR-ADM-01
// req: SEC-06
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.1
func TestAMemberIsRemovedAndMayJoinAgain(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "founder"}})
	api := store(t, "fyniq_api")
	b := books(t, "Removals Pte Ltd")
	other := books(t, "Other removals Pte Ltd")
	// A role is created at the database's own time, so its end date is after any day this test runs on.
	now := time.Date(2099, 10, 1, 9, 0, 0, 0, time.UTC)
	ends := now.Add(2 * time.Hour)
	olive, xavier := "olive-"+uuid.NewString()[:8], "xavier-"+uuid.NewString()[:8]
	owner := joinAs(t, api, b.Org, olive, []string{"owner"}, nil, 1, now)
	ext := joinAs(t, api, b.Org, xavier, []string{"auditor", "tax_agent"}, &ends, 2, now)
	later := now.Add(3 * time.Hour)

	members := func(at time.Time) (ms []storage.Member) {
		t.Helper()
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			ms, err = u.Members().List(ctx, at)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ms
	}
	find := func(ms []storage.Member, id uuid.UUID) *storage.Member {
		for i := range ms {
			if ms[i].ID == id {
				return &ms[i]
			}
		}
		return nil
	}
	x := find(members(later), ext.MembershipID)
	if x == nil || len(x.Roles) != 0 || len(x.Expired) != 2 || x.Since.IsZero() {
		t.Fatalf("Xavier past his end date: %+v", x)
	}
	remove := func(id uuid.UUID, version int) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.Members().Remove(ctx, id, version) })
	}
	o := find(members(later), owner.MembershipID)
	if err := remove(o.ID, o.Version); storage.CodeOf(err) != storage.CodeMembership {
		t.Errorf("removing the last Owner: %v", err)
	}
	if err := remove(x.ID, x.Version+1); storage.CodeOf(err) != storage.CodeVersion {
		t.Errorf("removing at another version: %v", err)
	}
	if err := remove(x.ID, x.Version); err != nil {
		t.Fatalf("removing Xavier: %v", err)
	}
	if find(members(later), ext.MembershipID) != nil {
		t.Fatal("Xavier is still a member")
	}
	if tr := trail(t, b.Org); !strings.Contains(tr, `"action":"user_membership.removed"`) || strings.Count(tr, `"action":"role_assignment.revoked"`) != 2 {
		t.Errorf("the removal's audit events: %s", tr)
	}

	for _, c := range []struct {
		name, sql, code string
	}{
		{"a role for a removed member", `INSERT INTO fyniq.role_assignment (membership_id, role) VALUES ('` + ext.MembershipID.String() + `', 'auditor')`, "FY248"},
		{"removing a member who holds a role", `UPDATE fyniq.user_membership SET status = 'removed', version = version + 1 WHERE id = '` + owner.MembershipID.String() + `'`, "FY248"},
	} {
		err := asOrg(context.Background(), conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: want %s, got %v", c.name, c.code, err)
		}
	}

	// Xavier is invited again: the same membership, active once more, with the new invitation's roles.
	again := joinAs(t, api, b.Org, xavier, []string{"auditor"}, nil, 3, later)
	if again.MembershipID != ext.MembershipID || again.UserID != ext.UserID {
		t.Fatalf("Xavier joining again: %+v, was %+v", again, ext)
	}
	if x := find(members(later), ext.MembershipID); x == nil || len(x.Roles) != 1 || x.Version != 3 {
		t.Fatalf("Xavier a member again: %+v", x)
	}
	if tr := trail(t, b.Org); !strings.Contains(tr, `"action":"user_membership.rejoined"`) {
		t.Errorf("the rejoining's audit event: %s", tr)
	}

	// Olive signs in, passing a second factor at the sign-in provider; Xavier never has.
	if err := api.StartSession(ctx, storage.NewSession{TokenHash: bytes.Repeat([]byte{b.Org[0]}, 32), Issuer: "urn:fyniq:access", Subject: olive,
		Provider: "test", StepUpAt: later, Now: later, Expires: later.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for org, want := range map[uuid.UUID]int{b.Org: 1, other.Org: 0} {
		if err := store(t, "fyniq_worker").Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			ss, err := u.AccessReviews().SignIns(ctx)
			if len(ss) != want || (want == 1 && (ss[0].UserID != owner.UserID || !ss[0].LastSignIn.Equal(later) || ss[0].StepUp != "sign_in")) {
				t.Errorf("the sign-ins of %s: %+v", org, ss)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
}
