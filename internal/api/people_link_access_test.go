//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
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
