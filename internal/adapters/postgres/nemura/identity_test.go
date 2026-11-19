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
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
)

func refusalCode(err error) string {
	var r *identity.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// First-run setup works once, with the one-time code the migrator set at deployment and that is kept only as its hash: a wrong code
// creates nothing, the right one makes the first organisation, its Owner and their identity in one transaction, and a second try with
// the same code is refused (NM-ARC-01: Nemura signs people in with no Fyniq).
// req: NM-ARC-01
// asvs: V6.4.1, V8.2.1
func TestFirstRunSetupWorksOnceWithTheDeploymentsCode(t *testing.T) {
	ctx := context.Background()
	api := store(t, "nemura_api")
	if done, err := api.SetupDone(ctx); err != nil || done {
		t.Fatalf("before setup: done %v, %v", done, err)
	}
	display, hash := identity.NewSetupCode()
	if err := nemura.SetSetupCode(ctx, db.URL(nemura.Migrator), hash); err != nil {
		t.Fatal(err)
	}
	// Only the migrator sets it; the API cannot.
	if err := nemura.SetSetupCode(ctx, db.URL("nemura_api"), hash); err == nil {
		t.Error("the API set the setup code")
	}
	me := identity.Identity{Issuer: "https://accounts.example", Subject: "founder-1", Tenant: "tenant-a"}
	at := today.Add(9 * time.Hour)
	if _, _, err := api.FirstRun(ctx, identity.Hash("not-the-code"), at, "Wrong Code Pte Ltd", "Eve", me); refusalCode(err) != identity.CodeSetupRefused {
		t.Fatalf("a wrong code: %v", err)
	}
	var n int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.organisation WHERE name = 'Wrong Code Pte Ltd'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("a refused setup left %d organisations, %v", n, err)
	}
	org, owner, err := api.FirstRun(ctx, identity.Hash(display), at, "First Run Pte Ltd", "Fay Founder", me)
	if err != nil {
		t.Fatal(err)
	}
	if done, _ := api.SetupDone(ctx); !done {
		t.Error("setup is not done after the first run")
	}
	if _, _, err := api.FirstRun(ctx, identity.Hash(display), at, "Second Pte Ltd", "Sam", identity.Identity{Issuer: "i", Subject: "s", Tenant: "t"}); refusalCode(err) != identity.CodeSetupRefused {
		t.Errorf("the code a second time: %v", err)
	}
	if err := nemura.SetSetupCode(ctx, db.URL(nemura.Migrator), hash); err == nil {
		t.Error("a new setup code was set after setup was done")
	}
	// The Owner is an Owner, found by their identity; the code is nowhere in the database, only its hash.
	ms, err := api.MembershipsOf(ctx, me)
	if err != nil || len(ms) != 1 || ms[0].Organisation != org || ms[0].Member != owner {
		t.Fatalf("the founder's memberships: %+v, %v", ms, err)
	}
	if err := api.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		roles, err := nemura.ActiveRoles(ctx, tx, owner, at)
		if err != nil || len(roles) != 1 || roles[0] != authz.Owner {
			t.Errorf("the first member's roles: %v, %v", roles, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.installation WHERE setup_code_hash IS NOT NULL").Scan(&n); err != nil || n != 0 {
		t.Errorf("a used setup code is still in the installation row: %d, %v", n, err)
	}
}

// People are matched by issuer, subject and tenant, never by email: the same subject at another issuer or tenant is another person,
// one identity can belong to several organisations, and a deactivated member is not found (NM-INT-12's rule, built in now).
// req: NM-ARC-06
// asvs: V6.1.1, V8.2.1
func TestPeopleAreMatchedByIssuerSubjectAndTenant(t *testing.T) {
	ctx := context.Background()
	a, b := newCompany(t), newCompany(t)
	api := store(t, "nemura_api")
	at := today.Add(10 * time.Hour)
	sam := identity.Identity{Issuer: "https://idp.example", Subject: "sam", Tenant: "t1"}
	for _, c := range []*company{a, b} {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.LinkIdentity(ctx, tx, c.org, at, actor, c.pay, sam)
		})
	}
	ms, err := api.MembershipsOf(ctx, sam)
	if err != nil || len(ms) != 2 {
		t.Fatalf("one identity in two organisations: %+v, %v", ms, err)
	}
	for _, other := range []identity.Identity{{Issuer: "https://other.example", Subject: "sam", Tenant: "t1"}, {Issuer: sam.Issuer, Subject: "sam", Tenant: "t2"}, {Issuer: sam.Issuer, Subject: "SAM", Tenant: "t1"}} {
		if got, err := api.MembershipsOf(ctx, other); err != nil || len(got) != 0 {
			t.Errorf("%+v is another person but matched %d memberships, %v", other, len(got), err)
		}
	}
	// One member holds one identity per issuer and tenant, and an identity is one member of an organisation.
	err = a.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.LinkIdentity(ctx, tx, a.org, at, actor, a.pay, identity.Identity{Issuer: sam.Issuer, Subject: "sam-again", Tenant: sam.Tenant})
	})
	if err == nil {
		t.Error("a member was given two identities at one issuer and tenant")
	}
	err = a.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.LinkIdentity(ctx, tx, a.org, at, actor, a.hr, sam)
	})
	if err == nil {
		t.Error("an identity was linked to two members of one organisation")
	}
	// The directory is reached only through its functions: no role reads or writes the table.
	for _, sql := range []string{"SELECT * FROM nemura.identity_directory", "INSERT INTO nemura.identity_directory (issuer, subject, tenant, organisation_id, member_id) VALUES ('i', 's', 't', gen_random_uuid(), gen_random_uuid())"} {
		if _, err := connect(t, db.URL("nemura_api")).Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("the API reached the identity directory directly: %v", err)
		}
	}
	// A deactivated member is not found.
	a.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, a.org, at, a.owner, a.pay, today)
	})
	if ms, _ := api.MembershipsOf(ctx, sam); len(ms) != 1 || ms[0].Organisation != b.org {
		t.Errorf("after deactivation in one organisation: %+v", ms)
	}
}

// An invitation works once, for a week, for whoever holds the code; every failure is the same plain refusal; only the code's hash is
// kept; and the invited member's identity is linked, so they can sign in (NM-ARC-01).
// req: NM-ARC-01
// asvs: V6.4.1, V7.2.1
func TestAnInvitationWorksOnceAndOnlyItsHashIsKept(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	api := store(t, "nemura_api")
	at := today.Add(11 * time.Hour)
	var code string
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		code, err = nemura.Invite(ctx, tx, c.org, at, c.owner, c.pay)
		return err
	})
	newcomer := identity.Identity{Issuer: "https://idp.example", Subject: "pia", Tenant: "t1"}
	refused := func(code string, when time.Time) error {
		_, _, err := api.AcceptInvitation(ctx, code, when, identity.Identity{Issuer: "https://idp.example", Subject: "x" + uuid.NewString(), Tenant: "t1"})
		return err
	}
	msg := ""
	for name, tc := range map[string]struct {
		code string
		when time.Time
	}{
		"a wrong secret": {c.org.String() + ".wrongwrongwrongwrongwrong", at},
		"another org":    {uuid.NewString() + "." + strings.SplitN(code, ".", 2)[1], at},
		"not a code":     {"hello", at},
		"after a week":   {code, at.Add(identity.InvitationTTL + time.Second)},
	} {
		err := refused(tc.code, tc.when)
		if refusalCode(err) != identity.CodeSignInRefused {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if msg == "" {
			msg = err.Error()
		} else if err.Error() != msg {
			t.Errorf("%s gave another answer: an attacker can tell the failures apart", name)
		}
	}
	org, member, err := api.AcceptInvitation(ctx, code, at.Add(time.Hour), newcomer)
	if err != nil || org != c.org || member != c.pay {
		t.Fatalf("accepting: %v %v %v", org, member, err)
	}
	if err := refused(code, at.Add(2*time.Hour)); refusalCode(err) != identity.CodeSignInRefused {
		t.Errorf("the code a second time: %v", err)
	}
	if ms, err := api.MembershipsOf(ctx, newcomer); err != nil || len(ms) != 1 || ms[0].Member != c.pay {
		t.Errorf("the invited person's memberships: %+v, %v", ms, err)
	}
	var hashOnly bool
	secret := strings.SplitN(code, ".", 2)[1]
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT bool_and(encode(code_hash, 'hex') <> $2 AND length(code_hash) = 32) FROM nemura.invitation WHERE organisation_id = $1", c.org, secret).Scan(&hashOnly); err != nil || !hashOnly {
		t.Errorf("the invitation keeps more than a hash: %v, %v", hashOnly, err)
	}
}

// A session is an opaque token kept only as its hash; it opens until it expires or is revoked; a deactivated member's sessions end, their
// roles end, and they cannot start another; a change of roles is seen on the very next request; and a session confirmed with a passkey
// is fresh for five minutes (NM-ARC-06).
// req: NM-ARC-06
// asvs: V7.2.1, V7.4.1, V7.4.2
func TestSessionsAreOpaqueExpireAndEndWithTheMember(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	api := store(t, "nemura_api")
	at := today.Add(12 * time.Hour)
	var token string
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		token, err = nemura.StartSession(ctx, tx, c.org, at, c.hr)
		return err
	})
	s, err := api.Authenticate(ctx, token, at.Add(time.Hour))
	if err != nil || s.Member != c.hr || s.Organisation != c.org {
		t.Fatalf("a fresh session: %+v, %v", s, err)
	}
	secret := strings.SplitN(token, ".", 2)[1]
	var leaked int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.session WHERE encode(token_hash, 'hex') = $1 OR encode(token_hash, 'escape') LIKE '%' || $1 || '%'", secret).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("the session keeps its token: %d, %v", leaked, err)
	}
	if _, err := api.Authenticate(ctx, token, at.Add(identity.SessionTTL+time.Second)); refusalCode(err) != identity.CodeSignInRefused {
		t.Errorf("an expired session: %v", err)
	}
	for _, bad := range []string{"", "nonsense", c.org.String() + ".wrongwrongwrongwrongwrong", uuid.NewString() + "." + secret} {
		if _, err := api.Authenticate(ctx, bad, at); refusalCode(err) != identity.CodeSignInRefused {
			t.Errorf("a token %q: %v", bad, err)
		}
	}
	// Step-up: none, fresh, then stale.
	if err := s.FreshStepUp(at); refusalCode(err) != identity.CodeStepUp {
		t.Errorf("no step-up: %v", err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error { return nemura.RecordStepUp(ctx, tx, c.org, at, s.ID, c.hr) })
	s, _ = api.Authenticate(ctx, token, at.Add(time.Minute))
	if err := s.FreshStepUp(at.Add(4 * time.Minute)); err != nil {
		t.Errorf("a step-up four minutes ago: %v", err)
	}
	if err := s.FreshStepUp(at.Add(6 * time.Minute)); refusalCode(err) != identity.CodeStepUp {
		t.Errorf("a step-up six minutes ago: %v", err)
	}
	// Roles are read on every request: ending the HR administrator's role is seen at once.
	rolesNow := func() []authz.Role {
		var r []authz.Role
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			r, err = nemura.ActiveRoles(ctx, tx, c.hr, at.Add(time.Hour))
			return err
		})
		return r
	}
	if len(rolesNow()) != 1 {
		t.Fatal("the HR administrator has no role")
	}
	// Signing out revokes it once; deactivating a member ends their other sessions and roles and stops a new one.
	var second string
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		second, err = nemura.StartSession(ctx, tx, c.org, at, c.hr)
		if err != nil {
			return err
		}
		return nemura.Revoke(ctx, tx, c.org, at, c.hr, s.ID)
	})
	if _, err := api.Authenticate(ctx, token, at.Add(time.Hour)); refusalCode(err) != identity.CodeSignInRefused {
		t.Errorf("a signed-out session: %v", err)
	}
	if _, err := api.Authenticate(ctx, second, at.Add(time.Hour)); err != nil {
		t.Errorf("the other session: %v", err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, c.org, at.Add(30*time.Minute), c.owner, c.hr, today)
	})
	if _, err := api.Authenticate(ctx, second, at.Add(time.Hour)); refusalCode(err) != identity.CodeSignInRefused {
		t.Errorf("a deactivated member's session: %v", err)
	}
	if got := rolesNow(); len(got) != 0 {
		t.Errorf("a deactivated member still holds %v", got)
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.StartSession(ctx, tx, c.org, at, c.hr)
		return err
	}); refusalCode(err) != identity.CodeSignInRefused {
		t.Errorf("a deactivated member signed in: %v", err)
	}
	// Sessions only end: nothing else about them changes, and nothing is deleted.
	for _, sql := range []string{"DELETE FROM nemura.session", "UPDATE nemura.session SET token_hash = token_hash || '\\x00'", "UPDATE nemura.session SET expires_at = expires_at + interval '1 day'"} {
		if _, err := connect(t, db.SuperURL()).Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM101") && !strings.Contains(err.Error(), "violates") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}
