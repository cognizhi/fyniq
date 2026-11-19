//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
)

func refusedWith(err error, code string) bool {
	var r *identity.Refusal
	return errors.As(err, &r) && r.Code == code
}

// The deployment's two-person recovery of an Owner (NM-ARC-01; FR-ADM-09, SEC-35 as Fyniq's): two different named operators with the migrator
// credential issue a one-time code for an active Owner, recorded with both names in the trail; the code works once, within 24 hours, and only if
// it is the member's newest; it replaces every other sign-in, ends their sessions and revokes their passkeys, each audited; every refusal is the
// same answer and the application roles can neither issue a code nor replace a sign-in without one. Attacks: another organisation's or another
// member's account, a replayed, older, expired or forged code.
// req: NM-ARC-01
// asvs: V6.3.1, V7.4.1, V6.4.3
func TestAnOwnerIsRecoveredByTwoOperatorsWithAOneTimeCode(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	other := newCompany(t)
	now := today.Add(9 * time.Hour)
	api := store(t, "nemura_api")
	migrator := db.URL(nemura.Migrator)
	oldID := identity.Identity{Issuer: "https://idp.example", Subject: "owen-old", Tenant: "t"}
	hrID := identity.Identity{Issuer: "https://idp.example", Subject: "hana", Tenant: "t"}
	newID := identity.Identity{Issuer: "https://idp.example", Subject: "owen-new", Tenant: "t"}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.LinkIdentity(ctx, tx, c.org, now, actor, c.owner, oldID); err != nil {
			return err
		}
		return nemura.LinkIdentity(ctx, tx, c.org, now, actor, c.hr, hrID)
	})
	// The Owner holds a session and a passkey under the old account.
	old := c.sessionOf(t, c.owner, now)
	passkeyID := uuid.New()
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.passkey (id, member_id, credential_id, public_key, aaguid, name) VALUES ($1, $2, $3, $4, $5, 'Old laptop')",
			passkeyID, c.owner, []byte("cred-1"), []byte("key-1"), make([]byte, 16)); err != nil {
			return err
		}
		return nemura.Audit(ctx, tx, c.org, now, actor, "nemura.passkey.registered", audit.Object{Type: "passkey", ID: passkeyID}, nil, "")
	})

	// Only two different, named operators; only an active Owner of the organisation.
	for name, tc := range map[string]struct {
		org, member uuid.UUID
		ops         []string
		want        string
	}{
		"one operator":    {c.org, c.owner, []string{"Ann"}, identity.CodeRecoveryOperators},
		"three operators": {c.org, c.owner, []string{"Ann", "Bob", "Cy"}, identity.CodeRecoveryOperators},
		"the same twice":  {c.org, c.owner, []string{"Ann", "ann "}, identity.CodeRecoveryOperators},
		"a blank name":    {c.org, c.owner, []string{"Ann", "  "}, identity.CodeRecoveryOperators},
		"not an Owner":    {c.org, c.hr, []string{"Ann", "Bob"}, identity.CodeRecoveryMember},
		"no such member":  {c.org, uuid.New(), []string{"Ann", "Bob"}, identity.CodeRecoveryMember},
		"another company": {c.org, other.owner, []string{"Ann", "Bob"}, identity.CodeRecoveryMember},
	} {
		if _, _, err := nemura.IssueOwnerRecovery(ctx, migrator, tc.org, tc.member, tc.ops, now); !refusedWith(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The application roles can neither issue a code nor write one, and cannot replace a sign-in with no recovery used.
	for _, role := range []string{"nemura_api", "nemura_worker"} {
		conn := connect(t, db.URL(role))
		if _, err := conn.Exec(ctx, "SELECT nemura.issue_owner_recovery($1, $2, $3, $4, $5, $6, $7, $8)", c.org, uuid.New(), c.owner, make([]byte, 32), []string{"A", "B"}, uuid.New(), "{}", "2099-03-02"); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s issuing a code: %v", role, err)
		}
	}
	err := api.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO nemura.owner_recovery (id, member_id, code_hash, operators, expires_at) VALUES ($1, $2, $3, '{A,B}', now() + interval '1 hour')", uuid.New(), c.owner, make([]byte, 32))
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the api writing a recovery code: %v", err)
	}
	err = api.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT nemura.replace_member_identities($1, 'i', 's', 't')", c.owner)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "NM112") {
		t.Errorf("replacing sign-ins with no recovery used: %v", err)
	}

	// Issue: the trail records both names; a newer code replaces an unused older one.
	first, expires, err := nemura.IssueOwnerRecovery(ctx, migrator, c.org, c.owner, []string{"Ann Lee", "Bob Tan"}, now)
	if err != nil || first == "" {
		t.Fatalf("issuing: %v", err)
	}
	if d := expires.Sub(clock.System{}.Now()); d < 23*time.Hour || d > 24*time.Hour+time.Minute {
		t.Errorf("valid for %v, want 24 hours", d)
	}
	var trail string
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT body FROM nemura.audit_event WHERE organisation_id = $1 AND body LIKE '%recovery_issued%'", c.org).Scan(&trail); err != nil ||
		!strings.Contains(trail, "Ann Lee") || !strings.Contains(trail, "Bob Tan") || strings.Contains(trail, strings.SplitN(first, ".", 2)[1]) {
		t.Fatalf("the trail of the issue: %s, %v", trail, err)
	}
	second, _, err := nemura.IssueOwnerRecovery(ctx, migrator, c.org, c.owner, []string{"Ann Lee", "Bob Tan"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.RecoverOwner(ctx, first, now, newID); !refusedWith(err, identity.CodeSignInRefused) {
		t.Errorf("an older code: %v", err)
	}
	// Forged, truncated, another organisation's and another member's attempts.
	secret := strings.SplitN(second, ".", 2)[1]
	for name, code := range map[string]string{"empty": "", "no secret": c.org.String() + ".", "wrong secret": c.org.String() + "." + strings.Repeat("a", 43), "another organisation": other.org.String() + "." + secret} {
		if _, _, err := api.RecoverOwner(ctx, code, now, newID); !refusedWith(err, identity.CodeSignInRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// An account that belongs to another member of the organisation is refused, and the code stays good.
	if _, _, err := api.RecoverOwner(ctx, second, now, hrID); !refusedWith(err, identity.CodeSignInRefused) {
		t.Errorf("another member's account: %v", err)
	}
	// Expired: past its 24 hours, the database's own clock decides.
	super := connect(t, db.SuperURL())
	if _, err := super.Exec(ctx, "SET session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if _, err := super.Exec(ctx, "UPDATE nemura.owner_recovery SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE organisation_id = $1", c.org); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.RecoverOwner(ctx, second, now, newID); !refusedWith(err, identity.CodeSignInRefused) {
		t.Errorf("an expired code: %v", err)
	}
	if _, err := super.Exec(ctx, "UPDATE nemura.owner_recovery SET created_at = now(), expires_at = now() + interval '24 hours' WHERE organisation_id = $1", c.org); err != nil {
		t.Fatal(err)
	}
	if _, err := super.Exec(ctx, "SET session_replication_role = origin"); err != nil {
		t.Fatal(err)
	}

	// The recovery: the Owner's new account is their only sign-in, their sessions and passkeys end, each with its event.
	org, member, err := api.RecoverOwner(ctx, second, now.Add(time.Minute), newID)
	if err != nil || org != c.org || member != c.owner {
		t.Fatalf("recovering: %v %v %v", org, member, err)
	}
	if m, _ := api.MembershipsOf(ctx, oldID); len(m) != 0 {
		t.Errorf("the old account still signs in: %v", m)
	}
	if m, _ := api.MembershipsOf(ctx, newID); len(m) != 1 || m[0].Member != c.owner {
		t.Errorf("the new account: %v", m)
	}
	if m, _ := api.MembershipsOf(ctx, hrID); len(m) != 1 || m[0].Member != c.hr {
		t.Errorf("another member's account was touched: %v", m)
	}
	if _, err := api.Authenticate(ctx, old.token, now.Add(time.Minute)); err == nil {
		t.Error("the Owner's old session still works")
	}
	var revoked *time.Time
	if err := super.QueryRow(ctx, "SELECT revoked_at FROM nemura.passkey WHERE id = $1", passkeyID).Scan(&revoked); err != nil || revoked == nil {
		t.Errorf("the passkey was not revoked: %v, %v", revoked, err)
	}
	var events int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action IN
('nemura.owner.recovery_issued', 'nemura.owner.recovered', 'nemura.session.ended', 'nemura.passkey.revoked')`, c.org).Scan(&events); err != nil || events != 5 {
		t.Errorf("audit events of the recovery: %d, %v", events, err)
	}
	// Once only, and the guard says so below the code.
	if _, _, err := api.RecoverOwner(ctx, second, now.Add(2*time.Minute), identity.Identity{Issuer: "i", Subject: "again", Tenant: "t"}); !refusedWith(err, identity.CodeSignInRefused) {
		t.Errorf("a used code: %v", err)
	}
	for _, sql := range []string{"UPDATE nemura.owner_recovery SET used_at = NULL", "DELETE FROM nemura.owner_recovery", "UPDATE nemura.owner_recovery SET expires_at = expires_at + interval '1 hour'"} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM113") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the audit chain after the recovery: %v", err)
	}
}
