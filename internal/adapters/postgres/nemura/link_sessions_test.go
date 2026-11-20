//go:build local

package nemura_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/link"
)

// One sign-in across the apps (PRD-002 NM-INT-12; PRD-001 FR-PPL-18): a person Fyniq signed out, or removed, is signed out of Nemura within one message: the sessions of the members of this organisation with that
// identity end (issuer and subject, never an email), no one else's, and none of another organisation's; the event is Fyniq's signed one at the link's epoch for this organisation (a forged, old-epoch or
// other-organisation one ends nothing); a reason other than signed_out or removed is refused; a person with no sessions or no identity here is no error; and the audit trail records the reason and how many, never the identity.
// asvs: V7.4.1, V8.2.1, V13.2.1
func TestAPersonSignedOutOfFyniqIsSignedOutOfNemuraAndNoOneElse(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	_, fyniqPriv, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	idp := "https://idp.example.test"
	actor := audit.Actor{Type: "user", ID: lt.c.owner.String()}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.LinkIdentity(ctx, tx, lt.c.org, now, actor, lt.c.owner, identity.Identity{Issuer: idp, Subject: "owner-sub", Tenant: "t"}); err != nil {
			return err
		}
		return nemura.LinkIdentity(ctx, tx, lt.c.org, now, actor, lt.c.hr, identity.Identity{Issuer: idp, Subject: "hr-sub", Tenant: "t"})
	})
	// The same person, with the same identity, in another organisation of Nemura.
	other := newLeaveTeam(t)
	other.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.LinkIdentity(ctx, tx, other.c.org, now, audit.Actor{Type: "user", ID: other.c.owner.String()}, other.c.owner, identity.Identity{Issuer: idp, Subject: "owner-sub", Tenant: "t"})
	})
	open := func(c *company, member uuid.UUID) string {
		var tok string
		c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
			tok, err = nemura.StartSession(ctx, tx, c.org, now, member, identity.Via{})
			return err
		})
		return tok
	}
	alive := func(tok string) bool {
		_, err := store(t, "nemura_api").Authenticate(ctx, tok, now.Add(time.Minute))
		return err == nil
	}
	o1, o2, h1, x1 := open(lt.c, lt.c.owner), open(lt.c, lt.c.owner), open(lt.c, lt.c.hr), open(other.c, other.c.owner)
	for _, tok := range []string{o1, o2, h1, x1} {
		if !alive(tok) {
			t.Fatal("a session that was just opened")
		}
	}
	revoke := func(key ed25519.PrivateKey, org uuid.UUID, epoch int, body map[string]any) (string, error) {
		e := fyniqSays(t, key, lt, fyniqOrg, now, nemura.TypeSessionRevoked, "identity", "u", 1, 20, body)
		if epoch != 1 || org != lt.c.org {
			raw := e.Payload
			at := now.UTC().Format(time.RFC3339)
			var err error
			e, err = link.Sign(key, link.Header{EventID: uuid.NewString(), Type: nemura.TypeSessionRevoked, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(), TargetOrganisation: org.String(), Epoch: epoch,
				Sequence: 21, ObjectType: "identity", ObjectID: "u", ObjectVersion: 1, EffectiveAt: at, RecordedAt: at, SignedAt: at, CorrelationID: uuid.NewString()}, raw)
			if err != nil {
				t.Fatal(err)
			}
		}
		return lt.receive(t, e, now)
	}
	person := map[string]any{"issuer": idp, "subject": "owner-sub", "reason": "signed_out"}
	// Forgeries and refusals end nothing.
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	if out, err := revoke(otherKey, lt.c.org, 1, person); err == nil || out == "applied" {
		t.Errorf("another key: %q %v", out, err)
	}
	if out, err := revoke(fyniqPriv, lt.c.org, 2, person); err == nil || out == "applied" {
		t.Errorf("an old epoch: %q %v", out, err)
	}
	if out, err := revoke(fyniqPriv, other.c.org, 1, person); err == nil || out == "applied" {
		t.Errorf("another organisation's target: %q %v", out, err)
	}
	for _, bad := range []map[string]any{{"issuer": idp, "subject": "owner-sub", "reason": "because"}, {"issuer": "", "subject": "owner-sub", "reason": "removed"}, {"issuer": idp, "subject": "owner-sub", "reason": "removed", "email": "x@y.test"}} {
		if out, _ := revoke(fyniqPriv, lt.c.org, 1, bad); out != "refused" {
			t.Errorf("a revocation of %v: %q", bad, out)
		}
	}
	if !alive(o1) || !alive(o2) || !alive(h1) || !alive(x1) {
		t.Fatal("a refused revocation ended a session")
	}
	// The signed one ends the owner's sessions here, and no one else's.
	if out, err := revoke(fyniqPriv, lt.c.org, 1, person); err != nil || out != "applied" {
		t.Fatalf("the revocation: %q %v", out, err)
	}
	if alive(o1) || alive(o2) {
		t.Error("the person is still signed in to Nemura")
	}
	if !alive(h1) {
		t.Error("another member was signed out")
	}
	if !alive(x1) {
		t.Error("the same identity in another organisation was signed out")
	}
	// A person with no identity or no session here is no error; the owner can sign in again afterwards (a new session is not ended by the old message).
	if out, err := revoke(fyniqPriv, lt.c.org, 1, map[string]any{"issuer": idp, "subject": "nobody", "reason": "removed"}); err != nil || out != "applied" {
		t.Errorf("an identity Nemura does not know: %q %v", out, err)
	}
	if !alive(open(lt.c, lt.c.owner)) {
		t.Error("a new session after the revocation")
	}
	var leaks int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%session.ended%' OR body LIKE '%revoked_by_fyniq%') AND (body LIKE '%owner-sub%' OR body LIKE '%idp.example.test%')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("an identity in the audit trail: %d %v", leaks, err)
	}
	var recorded int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body LIKE '%revoked_by_fyniq%'`, lt.c.org).Scan(&recorded); err != nil || recorded < 1 {
		t.Errorf("the revocation is audited: %d %v", recorded, err)
	}
	// The API cannot end anyone's sessions but through the one function, and cannot read the directory.
	if err := store(t, "nemura_api").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT * FROM nemura.identity_directory`)
		return err
	}); err == nil {
		t.Error("the API read the identity directory")
	}
}

// A role in one app grants nothing in the other, and linking makes no member (PRD-002 NM-INT-07, NM-INT-12; PRD-001 FR-PPL-18): linking and every event the link carries leave Nemura's members and roles exactly as
// they were; a person who is an Owner in Fyniq, known to Nemura only by their sign-in identity (issuer and subject), has no membership here and so no session; the same identity is matched by issuer, subject and
// tenant and never by email, so another tenant's person with the same subject is not them; and an identity is a member of the organisations it was linked to and of no other.
// asvs: V4.2.1, V8.2.1, V8.3.1
func TestARoleInOneAppGrantsNothingInTheOtherAndLinkingMakesNoMember(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	idp := "https://idp.example.test"
	actor := audit.Actor{Type: "user", ID: lt.c.owner.String()}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.LinkIdentity(ctx, tx, lt.c.org, now, actor, lt.c.owner, identity.Identity{Issuer: idp, Subject: "nemura-owner", Tenant: "t"})
	})
	count := func() (members, grants int) {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.member`).Scan(&members); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.role_assignment`).Scan(&grants)
		})
		return
	}
	beforeMembers, beforeGrants := count()
	offer, fyniqPriv, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	_ = offer
	// Everything Fyniq can send in changes no member and no role.
	for i, typ := range []string{nemura.TypeDepartmentChanged, nemura.TypeSessionRevoked, nemura.TypeReplayRequested} {
		var body any
		switch typ {
		case nemura.TypeDepartmentChanged:
			body = map[string]any{"mapping_version": 1, "departments": []map[string]any{{"department_id": uuid.NewString(), "name": "Sales", "cost_centre_id": nil}}}
		case nemura.TypeSessionRevoked:
			body = map[string]any{"issuer": idp, "subject": "fyniq-owner", "reason": "removed"}
		default:
			body = map[string]any{"from_sequence": 1, "to_sequence": 2}
		}
		if _, err := lt.receive(t, fyniqSays(t, fyniqPriv, lt, fyniqOrg, now, typ, "x", "x", 1, int64(30+i), body), now); err != nil {
			t.Fatal(err)
		}
	}
	// An event that would make a member, or a role, is no event Nemura receives.
	for _, typ := range []string{"identity.member.created", "roles.granted", "people.person.upserted"} {
		if out, _ := lt.receive(t, fyniqSays(t, fyniqPriv, lt, fyniqOrg, now, typ, "x", "x", 1, 40, map[string]any{"role": "owner", "issuer": idp, "subject": "fyniq-owner"}), now); out != "refused" {
			t.Errorf("%s: %q", typ, out)
		}
	}
	// (The helper that makes the link appoints the second approver an Administrator itself: that is the one grant it adds; the link and its events add none.)
	if m, g := count(); m != beforeMembers || g != beforeGrants+1 {
		t.Errorf("linking changed members %d to %d or roles %d to %d", beforeMembers, m, beforeGrants, g)
	}
	// The Fyniq Owner has no membership here, whatever they are there; the Nemura owner's is theirs alone, by issuer, subject and tenant.
	st := store(t, "nemura_api")
	who := func(i, s, tenant string) int {
		m, err := st.MembershipsOf(ctx, identity.Identity{Issuer: i, Subject: s, Tenant: tenant})
		if err != nil {
			t.Fatal(err)
		}
		return len(m)
	}
	if n := who(idp, "fyniq-owner", "t"); n != 0 {
		t.Errorf("a Fyniq Owner has %d memberships in Nemura", n)
	}
	if n := who(idp, "nemura-owner", "t"); n != 1 {
		t.Errorf("the Nemura owner has %d memberships", n)
	}
	for _, other := range [][3]string{{idp, "nemura-owner", "another-tenant"}, {"https://other-idp.example.test", "nemura-owner", "t"}, {idp, "Nemura-Owner", "t"}} {
		if n := who(other[0], other[1], other[2]); n != 0 {
			t.Errorf("the identity %v is matched as the owner (%d memberships): an identity is issuer, subject and tenant, exactly", other, n)
		}
	}
	roles, err := st.RolesAt(ctx, lt.c.org, lt.c.owner, now)
	if err != nil || len(roles) == 0 {
		t.Fatalf("the owner's roles: %v %v", roles, err)
	}
	if rs, _ := st.RolesAt(ctx, lt.c.org, lt.ana, now); len(rs) != 0 { // a person record with no login holds no role
		t.Errorf("a person record holds roles: %v", rs)
	}
}
