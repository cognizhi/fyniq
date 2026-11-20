//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/passkey/passkeytest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// nemuraSays posts an event from Nemura to the projection's door, signed with the link key and sent as the link's client.
func (w *runWorld) nemuraSays(typ, objectType, objectID string, version int, payload map[string]any, edit func(*peoplelink.Header)) response {
	w.e.t.Helper()
	raw, _ := json.Marshal(payload)
	now := w.e.clock.Now().UTC().Format(time.RFC3339)
	h := peoplelink.Header{EventID: uuid.NewString(), Type: typ, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura, TargetOrganisation: w.org.String(), Epoch: 1,
		Sequence: int64(version), ObjectType: objectType, ObjectID: objectID, ObjectVersion: version, EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}
	if edit != nil {
		edit(&h)
	}
	env, err := peoplelink.Sign(w.linkKey, h, raw)
	if err != nil {
		w.e.t.Fatal(err)
	}
	b, _ := json.Marshal(env)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	return w.e.call(http.MethodPost, "/v1/people-link/projection-events", w.tok, body)
}

// person is a signed-in person of the world: a session of the given roles with the identity and provider it was signed in with.
func (w *runWorld) person(who peoplelink.Party, user uuid.UUID, provider string, roles ...authz.Role) string {
	return w.e.principal(authn.Principal{Client: "web", Via: authn.ViaSession, Class: authn.ClassUser, Organisation: w.org, Roles: roles, UserID: user, Issuer: who.Issuer, Subject: who.Subject, Provider: provider})
}

// One sign-in across the apps (PRD-001 FR-PPL-02, FR-PPL-03; PRD-002 NM-INT-02, design D7): what Nemura says of a sign-in identity (an active member there, an approver of pay runs) arrives signed through the
// projection's door as an issuer, a subject and two yes or no answers, and is kept by version (an older word arriving late changes nothing); a role name, an email or any other field is refused; it is a door and a hint
// and never a grant: the Finance | People switch shows only to a person who holds a finance role here and whom Nemura says is a member there, and it names the provider the session used; a person Nemura says approves
// pay runs and who has no passkey is reminded to enrol one, and one who has a passkey is not.
// part of FR-PPL-02 (Nemura's side is its own test)
// part of FR-PPL-03
// asvs: V4.2.1, V8.2.1, V13.2.1
func TestNemurasWordOnAnIdentityIsAHintByVersionAndDecidesTheSwitchAndThePasskeyReminder(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	ctx := context.Background()
	mei, err := e.store.UserForIdentity(ctx, w.preparer.Issuer, w.preparer.Subject)
	if err != nil {
		t.Fatal(err)
	}
	wei := w.approverUser
	session := func(who peoplelink.Party, user uuid.UUID, roles ...authz.Role) map[string]any {
		r := e.call(http.MethodGet, "/v1/session", w.person(who, user, "zoho", roles...), nil)
		if r.Status != http.StatusOK {
			t.Fatalf("the session: %d %s", r.Status, r.Raw)
		}
		return r.Body
	}
	says := func(who peoplelink.Party, version int, access, approver bool) response {
		return w.nemuraSays(peoplelink.TypeAccessChanged, "identity", uuid.NewString(), version, map[string]any{"issuer": who.Issuer, "subject": who.Subject, "people_access": access, "payroll_approver": approver}, nil)
	}
	// Before Nemura says anything there is no switch and no reminder, whatever the roles.
	if b := session(w.approver, wei, authz.Owner); b["workspace"] != nil || b["nemura_passkey_needed"] != nil {
		t.Fatalf("the session before Nemura said anything: %v", b)
	}
	if r := says(w.approver, 5, true, true); r.Status != http.StatusOK || r.str("outcome") != "applied" {
		t.Fatalf("Nemura's word: %d %s", r.Status, r.Raw)
	}
	b := session(w.approver, wei, authz.Owner)
	ws, _ := b["workspace"].(map[string]any)
	if ws["people_url"] != nemuraOrigin+"/?switch=1&provider=zoho" {
		t.Errorf("the switch names the provider the session used: %v", b["workspace"])
	}
	if b["nemura_passkey_needed"] != nil {
		t.Errorf("Wei Jie has a passkey enrolled here, so no reminder: %v", b)
	}
	// A door, never a grant: an employee who only claims has no switch, and an identity Nemura said nothing of has none either.
	if b := session(w.approver, wei, authz.Employee); b["workspace"] != nil {
		t.Errorf("an employee who only claims sees a switch: %v", b["workspace"])
	}
	if b := session(w.preparer, mei, authz.Owner); b["workspace"] != nil {
		t.Errorf("an identity Nemura said nothing of sees a switch: %v", b["workspace"])
	}
	// An older word arriving late changes nothing; the same version again is ignored; a newer one replaces.
	if r := says(w.approver, 4, false, false); r.Status != http.StatusOK || r.str("outcome") != "ignored_older" {
		t.Errorf("an older word: %d %s", r.Status, r.Raw)
	}
	if r := says(w.approver, 5, false, false); r.Status != http.StatusOK || r.str("outcome") != "ignored_older" {
		t.Errorf("the same version again: %d %s", r.Status, r.Raw)
	}
	if b := session(w.approver, wei, authz.Owner); b["workspace"] == nil {
		t.Error("an older word took the switch away")
	}
	// Mei Ling approves pay runs in Nemura and has no passkey here: she is reminded; Wei Jie, with one, is not.
	if r := says(w.preparer, 6, true, true); r.Status != http.StatusOK || r.str("outcome") != "applied" {
		t.Fatalf("Nemura's word on the preparer: %d %s", r.Status, r.Raw)
	}
	if b := session(w.preparer, mei, authz.Owner); b["nemura_passkey_needed"] != true {
		t.Errorf("an approver with no passkey is reminded: %v", b)
	}
	if r := says(w.approver, 7, false, true); r.Status != http.StatusOK || r.str("outcome") != "applied" {
		t.Fatalf("a newer word: %d %s", r.Status, r.Raw)
	}
	if b := session(w.approver, wei, authz.Owner); b["workspace"] != nil || b["nemura_passkey_needed"] != nil {
		t.Errorf("a member Nemura says is no longer there: %v", b)
	}
	// What is refused: a role name, an email, any other field, no identity, no version, another epoch.
	for what, body := range map[string]map[string]any{
		"a role name": {"issuer": w.approver.Issuer, "subject": w.approver.Subject, "people_access": true, "payroll_approver": true, "role": "owner"},
		"an email":    {"issuer": w.approver.Issuer, "subject": w.approver.Subject, "people_access": true, "payroll_approver": true, "email": "wei@example.test"},
		"no issuer":   {"issuer": "", "subject": w.approver.Subject, "people_access": true, "payroll_approver": true},
		"no subject":  {"subject": "", "issuer": w.approver.Issuer, "people_access": true, "payroll_approver": true},
	} {
		r := w.nemuraSays(peoplelink.TypeAccessChanged, "identity", uuid.NewString(), 8, body, nil)
		if r.Status != http.StatusUnprocessableEntity && r.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	if r := w.nemuraSays(peoplelink.TypeAccessChanged, "identity", "x", 9, map[string]any{"issuer": w.approver.Issuer, "subject": w.approver.Subject, "people_access": true, "payroll_approver": true},
		func(h *peoplelink.Header) { h.Epoch = 2 }); r.Status == http.StatusOK {
		t.Errorf("an event of another epoch: %d %s", r.Status, r.Raw)
	}
	// A repeat of one event ID does nothing.
	ev := w.nemuraSays(peoplelink.TypeAccessChanged, "identity", "y", 10, map[string]any{"issuer": w.approver.Issuer, "subject": w.approver.Subject, "people_access": true, "payroll_approver": false},
		func(h *peoplelink.Header) { h.EventID = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e99" })
	again := w.nemuraSays(peoplelink.TypeAccessChanged, "identity", "y", 10, map[string]any{"issuer": w.approver.Issuer, "subject": w.approver.Subject, "people_access": true, "payroll_approver": false},
		func(h *peoplelink.Header) { h.EventID = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e99" })
	if ev.Status != http.StatusOK || again.str("outcome") != "duplicate" {
		t.Errorf("a repeat: %d %s then %d %s", ev.Status, ev.Raw, again.Status, again.Raw)
	}
}

// A linked Nemura is told what each identity may enter here when a role or a passkey changes, a member is removed or joins, and when the link is approved (design D7): an issuer, a subject and two yes or no answers
// (a finance role, a passkey), in the change's own unit of work, through the link's one event scope, versioned by the event's sequence, and never an email or a role name.
// part of FR-PPL-02
// part of FR-PPL-18 (Nemura's half, and sign-out, are its own)
// asvs: V4.2.1, V8.2.1, V13.2.1
func TestAnIdentitysAccessIsToldToNemuraWhenARoleOrAPasskeyOrMembershipChanges(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	ctx := context.Background()
	mei, _ := e.store.UserForIdentity(ctx, w.preparer.Issuer, w.preparer.Subject)
	access := func() map[string]linksend.IdentityAccess { // the last word queued about each identity
		out := map[string]linksend.IdentityAccess{}
		last := map[string]int{}
		for _, q := range w.queued(t) {
			if q.Type != linksend.TypeIdentityAccess {
				continue
			}
			var p linksend.IdentityAccess
			d := json.NewDecoder(strings.NewReader(string(q.Payload)))
			d.DisallowUnknownFields()
			if err := d.Decode(&p); err != nil || q.ObjectType != "identity" || q.ObjectVersion != int(q.Sequence) || strings.Contains(string(q.Payload), "@") {
				t.Fatalf("an access event: %+v %v", q, err)
			}
			if q.ObjectVersion <= last[p.Subject] {
				t.Errorf("versions rise: %d after %d", q.ObjectVersion, last[p.Subject])
			}
			last[p.Subject], out[p.Subject] = q.ObjectVersion, p
		}
		return out
	}
	if len(access()) != 0 {
		t.Fatalf("nothing is queued before something changes: %v", access())
	}
	tell := func(user uuid.UUID) {
		if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error { return e.api.tellNemuraAccess(ctx, u, user) }); err != nil {
			t.Fatal(err)
		}
	}
	tell(w.approverUser)
	if a := access()[w.approver.Subject]; a.Issuer != w.approver.Issuer || !a.Finance || !a.Passkey {
		t.Errorf("Wei Jie is an Owner with a passkey: %+v", a)
	}
	tell(mei)
	if a := access()[w.preparer.Subject]; !a.Finance || a.Passkey {
		t.Errorf("Mei Ling is an Owner with no passkey: %+v", a)
	}
	// A role that does not reach the finance workspace, and then one that does, through the same function the role operations call.
	var member uuid.UUID
	var held []string
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Members().List(ctx, e.clock.Now())
		for _, m := range all {
			if m.UserID == mei {
				member = m.ID
				for _, r := range m.Roles {
					held = append(held, r.Role)
				}
			}
		}
		return err
	}); err != nil || member == uuid.Nil || len(held) == 0 {
		t.Fatalf("Mei Ling's membership: %v", err)
	}
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, r := range held { // Wei Jie is an Owner too, so the last Owner's role is not at stake
			if _, err := e.api.revokeRoleNow(ctx, u, member, r); err != nil {
				return err
			}
		}
		_, err := e.api.assignRoleNow(ctx, u, member, "employee", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if a := access()[w.preparer.Subject]; a.Finance {
		t.Errorf("an employee who only claims has no finance workspace: %+v", a)
	}
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := e.api.assignRoleNow(ctx, u, member, "accountant", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if a := access()[w.preparer.Subject]; !a.Finance {
		t.Errorf("an accountant reaches the finance workspace: %+v", a)
	}
	// Removed: the identity no longer reaches it, and Nemura is also told to end their sessions.
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := e.api.removeMemberNow(ctx, u, member)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if a := access()[w.preparer.Subject]; a.Finance {
		t.Errorf("a removed member's identity reaches the finance workspace: %+v", a)
	}
	// Approving the link tells Nemura of everyone.
	before := len(access())
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error { return e.api.tellNemuraEveryone(ctx, u) }); err != nil {
		t.Fatal(err)
	}
	if after := len(access()); after < before || access()[w.approver.Subject].Issuer != w.approver.Issuer {
		t.Errorf("the link's approval tells Nemura of every member: %d then %d", before, after)
	}
}

// Nemura ends sessions here, narrowly (PRD-001 FR-PPL-18; PRD-002 NM-INT-12; owner's decision of 21 Nov 2026: the one scope identity.sessions.write): a signed identity.session.revoked ends the sessions of the user
// behind the identity when, and only when, that user is mapped to a person of the link; a user nobody mapped and an identity with no user here are left as they are and the answer is the same, so the call tells Nemura
// nothing of who has an account; a role name, an email, any other field or a reason of a third kind is refused; no membership, role or passkey changes; the link's client without the scope, a person and a client of
// another kind reach nothing; and the audit trail holds the reason and how many, never the identity.
// part of FR-PPL-18 (Nemura's half is its own test)
// asvs: V7.4.1, V8.2.1, V13.2.1, V4.2.1
func TestNemuraEndsTheSessionsOfAMappedUserAndNoOneElse(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	ctx := context.Background()
	mei, err := e.store.UserForIdentity(ctx, w.preparer.Issuer, w.preparer.Subject)
	if err != nil {
		t.Fatal(err)
	}
	// Wei Jie is mapped to a person of the link; Mei Ling is not.
	person := uuid.NewString()
	now := e.clock.Now().UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(map[string]any{"person_id": person, "display_name": "Wei Jie Lim", "work_email": "weijie@example.com", "status": "active", "deleted": false})
	pe, err := peoplelink.Sign(w.linkKey, peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypePersonUpserted, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura,
		TargetOrganisation: w.org.String(), Epoch: 1, Sequence: 1, ObjectType: "person", ObjectID: person, ObjectVersion: 1, EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	pb, _ := json.Marshal(pe)
	var body map[string]any
	_ = json.Unmarshal(pb, &body)
	if r := e.call(http.MethodPost, "/v1/people-link/projection-events", w.tok, body); r.Status != http.StatusOK {
		t.Fatalf("the person: %d %s", r.Status, r.Raw)
	}
	admin := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Administrator}})
	if r := e.call(http.MethodPost, "/v1/people-link/people/"+person+"/user", admin, map[string]any{"user_id": wei(w).String()}); r.Status != http.StatusOK {
		t.Fatalf("mapping: %d %s", r.Status, r.Raw)
	}
	revoke := func(who peoplelink.Party, reason string) response {
		return w.sessionEvent(map[string]any{"issuer": who.Issuer, "subject": who.Subject, "reason": reason}, w.tok)
	}
	signedIn := func(b *browser) bool {
		return b.call(http.MethodGet, "/v1/session", "same-origin", nil).Status == http.StatusOK
	}
	if !signedIn(w.phone) || !signedIn(w.desk) {
		t.Fatal("both people are signed in to begin with")
	}
	// An unmapped user, an identity with no user here and a mapped user all get the same answer; only the mapped user's sessions end.
	for what, who := range map[string]peoplelink.Party{"an unmapped user": w.preparer, "an identity with no user": {Issuer: "https://idp.example.test", Subject: "nobody"}} {
		if r := revoke(who, "signed_out"); r.Status != http.StatusOK || r.str("outcome") != "applied" {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	if !signedIn(w.desk) || !signedIn(w.phone) {
		t.Fatal("a session ended for a user nobody mapped, or for an identity with no user")
	}
	// Refused: a reason of a third kind, an email, any other field, no identity; none ends anything.
	for what, b := range map[string]map[string]any{
		"a third reason": {"issuer": w.approver.Issuer, "subject": w.approver.Subject, "reason": "because"},
		"an email":       {"issuer": w.approver.Issuer, "subject": w.approver.Subject, "reason": "removed", "email": "weijie@example.com"},
		"no identity":    {"issuer": "", "subject": "", "reason": "removed"},
	} {
		if r := w.sessionEvent(b, w.tok); r.Status != http.StatusUnprocessableEntity && r.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	if !signedIn(w.phone) {
		t.Fatal("a refused event ended a session")
	}
	if r := revoke(w.approver, "removed"); r.Status != http.StatusOK || r.str("outcome") != "applied" {
		t.Fatalf("the mapped user: %d %s", r.Status, r.Raw)
	}
	if signedIn(w.phone) {
		t.Error("the mapped user is still signed in")
	}
	if !signedIn(w.desk) {
		t.Error("another user was signed out")
	}
	// Nothing but sessions ends: the member's roles and passkey stay (a pay run's approval still verifies), and the same event again does nothing.
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		ks, err := u.Passkeys().List(ctx, wei(w), false)
		if err != nil || len(ks) == 0 {
			t.Errorf("the mapped user's passkey after the revocation: %d %v", len(ks), err)
		}
		all, err := u.Members().List(ctx, e.clock.Now())
		if err != nil {
			return err
		}
		for _, m := range all {
			if m.UserID == wei(w) && len(m.Roles) == 0 {
				t.Error("the mapped user lost their roles")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Who may call: the link's client without the scope, a person and an ordinary integration client reach nothing.
	noScope := e.principal(authn.Principal{Client: "link-without-scope", Class: authn.ClassIntegration, Organisation: w.org, Scoped: true, Link: true, Scopes: slices.DeleteFunc(slices.Clone(apiclient.LinkScopes), func(s string) bool { return s == "identity.sessions.write" })})
	if r := w.sessionEvent(map[string]any{"issuer": w.approver.Issuer, "subject": w.approver.Subject, "reason": "removed"}, noScope); r.Status != http.StatusForbidden {
		t.Errorf("the link's client without the scope: %d %s", r.Status, r.Raw)
	}
	for who, tok := range map[string]string{"an Owner": admin, "an integration client": e.principal(authn.Principal{Client: "integration", Class: authn.ClassIntegration, Organisation: w.org, Scoped: true, Scopes: []string{"fyniq.write.accounting"}})} {
		if r := w.sessionEvent(map[string]any{"issuer": w.approver.Issuer, "subject": w.approver.Subject, "reason": "removed"}, tok); r.Status != http.StatusForbidden {
			t.Errorf("%s: %d %s", who, r.Status, r.Raw)
		}
	}
	// The trail says how many and why, and never whose.
	var recorded int
	rows := w.auditBodies(t, "people_link.sessions_ended")
	for _, b := range rows {
		recorded++
		if strings.Contains(b, w.approver.Subject) || strings.Contains(b, w.approver.Issuer) || !strings.Contains(b, "sessions_ended") {
			t.Errorf("the audit event: %s", b)
		}
	}
	if recorded != 1 {
		t.Errorf("audit events of the sessions ended: %d", recorded)
	}
	_ = mei
}

func wei(w *runWorld) uuid.UUID { return w.approverUser }

// sessionEvent sends an identity.session.revoked from Nemura, signed with the link key, as the token's client.
func (w *runWorld) sessionEvent(payload map[string]any, tok string) response {
	w.e.t.Helper()
	raw, _ := json.Marshal(payload)
	now := w.e.clock.Now().UTC().Format(time.RFC3339)
	env, err := peoplelink.Sign(w.linkKey, peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypeSessionRevoked, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura,
		TargetOrganisation: w.org.String(), Epoch: 1, Sequence: 77, ObjectType: "identity", ObjectID: uuid.NewString(), ObjectVersion: 1, EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, raw)
	if err != nil {
		w.e.t.Fatal(err)
	}
	b, _ := json.Marshal(env)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	return w.e.call(http.MethodPost, "/v1/people-link/session-events", tok, body)
}

// auditBodies are the audit events of an action in the world's organisation, as stored.
func (w *runWorld) auditBodies(t *testing.T, action string) []string {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, w.readonly)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", w.org.String()); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `SELECT body FROM fyniq.audit_event WHERE body LIKE '%' || $1 || '%'`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

// An answer of an ended link decides nothing for the link that follows it (design D16, D17): a sequence starts again at 1 with each link, so Nemura's word is kept with its epoch and the newest (epoch, version)
// wins; an answer of an earlier epoch is ignored when it arrives late and decides no switch and no reminder for the live link.
// part of FR-PPL-02
// asvs: V8.2.1, V4.2.1
func TestAnAnswerOfAnEndedLinkDecidesNothingForTheLinkThatFollowsInFyniq(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	ctx := context.Background()
	save := func(epoch int, version int64, access bool) bool {
		t.Helper()
		var saved bool
		if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			saved, err = u.PeopleLinks().SaveAccess(ctx, storage.PeopleAccess{Issuer: w.approver.Issuer, Subject: w.approver.Subject, PeopleAccess: access, PayrollApprover: true, Version: version, Epoch: epoch, ReceivedAt: e.clock.Now()})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return saved
	}
	session := func() map[string]any {
		return e.call(http.MethodGet, "/v1/session", w.person(w.approver, w.approverUser, "zoho", authz.Owner), nil).Body
	}
	if !save(1, 700, true) || session()["workspace"] == nil {
		t.Fatal("the live link's answer shows the switch")
	}
	// The link is made again later at epoch 2 (the world's link is epoch 1: what epoch 2 says is what a later link will have said).
	if !save(2, 1, true) {
		t.Error("the next link's first answer (version 1) did not replace an old one with version 700")
	}
	if session()["workspace"] != nil || session()["nemura_passkey_needed"] != nil {
		t.Errorf("an answer of another epoch decides the live link's switch: %v", session())
	}
	if save(1, 900, true) {
		t.Error("an answer of the earlier epoch, arriving late, replaced the newer epoch's")
	}
	if save(2, 1, false) {
		t.Error("the same epoch and version again replaced it")
	}
}

// A passkey enrolled or revoked through the API reaches a linked Nemura (design D16): the change belongs to the installation's trail, so the word to Nemura is queued in the person's organisation's own unit of work
// after it, and says whether the person's identity now has a passkey (what a pay run's approval is signed with), by issuer and subject.
// part of FR-PPL-02
// asvs: V8.2.1, V13.2.1
func TestEnrollingOrRevokingAPasskeyIsToldToALinkedNemura(t *testing.T) {
	w := payrollWorld(t)
	passkeyOf := func() (seen bool, passkey bool) {
		for _, q := range w.queued(t) {
			var p linksend.IdentityAccess
			if q.Type == linksend.TypeIdentityAccess && json.Unmarshal(q.Payload, &p) == nil && p.Subject == w.preparer.Subject {
				seen, passkey = true, p.Passkey // the last word about Mei Ling
			}
		}
		return
	}
	if seen, _ := passkeyOf(); seen {
		t.Fatal("nothing is queued about Mei Ling before she enrols a passkey")
	}
	authenticator := passkeytest.New(t, w.rpHost, w.origin)
	r := w.desk.call(http.MethodPost, "/v1/session/passkeys/challenge", "same-origin", nil)
	reg := w.desk.call(http.MethodPost, "/v1/session/passkeys", "same-origin", map[string]any{"challenge_id": r.str("challenge_id"), "name": "Desk key",
		"credential": credential(t, authenticator.Create(passkeytest.Challenge(t, options(t, r))))})
	if reg.Status != http.StatusCreated {
		t.Fatalf("enrolling: %d %s", reg.Status, reg.Raw)
	}
	if seen, passkey := passkeyOf(); !seen || !passkey {
		t.Errorf("enrolling a passkey told Nemura: seen %v, passkey %v", seen, passkey)
	}
	if del := w.desk.call(http.MethodDelete, "/v1/session/passkeys/"+reg.str("id"), "same-origin", nil); del.Status != http.StatusOK {
		t.Fatalf("revoking: %d %s", del.Status, del.Raw)
	}
	if seen, passkey := passkeyOf(); !seen || passkey {
		t.Errorf("revoking the passkey told Nemura: seen %v, passkey %v", seen, passkey)
	}
}
