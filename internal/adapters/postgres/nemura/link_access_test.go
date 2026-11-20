//go:build local

package nemura_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// One sign-in, finished (PRD-002 NM-INT-02, NM-INT-12; PRD-001 FR-PPL-03; design D7, owner decision of 21 Nov 2026: the sign-in is one identity-provider tenant): a session records the provider and the
// identity it was signed in with, and they never change; a session made before they were recorded has none; a half-recorded way in is refused by the database.
// part of NM-INT-12
// asvs: V7.2.1, V8.2.1, V3.3.1
func TestASessionKeepsHowItWasSignedInAndItNeverChanges(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	idp := "https://idp.example.test"
	via := identity.Via{Provider: "zoho", Issuer: idp, Subject: "owner-b"}
	var withVia, without string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		if withVia, err = nemura.StartSession(ctx, tx, lt.c.org, now, lt.c.owner, via); err != nil {
			return err
		}
		without, err = nemura.StartSession(ctx, tx, lt.c.org, now, lt.c.owner, identity.Via{})
		return err
	})
	st := store(t, "nemura_api")
	got, err := st.Authenticate(ctx, withVia, now.Add(time.Minute))
	if err != nil || got.Via != via {
		t.Fatalf("a session signed in with zoho: %+v %v", got.Via, err)
	}
	if got, err = st.Authenticate(ctx, without, now.Add(time.Minute)); err != nil || got.Via != (identity.Via{}) {
		t.Fatalf("a session made before the way in was recorded: %+v %v", got.Via, err)
	}
	super := connect(t, db.SuperURL())
	for what, q := range map[string]string{
		"the identity changed":    `UPDATE nemura.session SET subject = 'someone-else' WHERE organisation_id = $1 AND subject IS NOT NULL`,
		"the provider changed":    `UPDATE nemura.session SET provider = 'google' WHERE organisation_id = $1 AND provider IS NOT NULL`,
		"an identity added later": `UPDATE nemura.session SET provider = 'google', issuer = 'x', subject = 'y' WHERE organisation_id = $1 AND provider IS NULL`,
	} {
		if _, err := super.Exec(ctx, q, lt.c.org); err == nil || !strings.Contains(err.Error(), "NM101") {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := super.Exec(ctx, `INSERT INTO nemura.session (organisation_id, member_id, token_hash, expires_at, provider) VALUES ($1, $2, decode(repeat('ab', 32), 'hex'), now() + interval '1 hour', 'zoho')`, lt.c.org, lt.c.owner); err == nil {
		t.Error("a provider with no identity was kept")
	}
}

// What each app tells the other of an identity (PRD-002 NM-INT-02; PRD-001 FR-PPL-02, FR-PPL-03, design D7): while the link is active every identity of a member is told to Fyniq as an issuer, a subject and two
// yes or no answers (an active member, an approver of pay runs), when the link is activated, when an identity is linked, when a role is granted or ended and when a member is deactivated, and never with
// an email or a role name; a standalone organisation queues none; Fyniq's own answer (a finance role, a passkey) is kept by version, an older one ignored, a field the contract does not list or an identity-less
// one refused, and it decides whether the switch shows (with the provider the session used) and what the approver is warned of; Nemura cannot read the identity directory.
// part of NM-INT-02 (the other app's page is Fyniq's)
// part of FR-PPL-02
// asvs: V4.2.1, V8.2.1, V13.2.1
func TestAnIdentitysAccessIsToldToFyniqAndFyniqsAnswerDecidesTheSwitchAndTheWarning(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	idpA, idpB := "https://idp-a.example.test", "https://idp-b.example.test"
	by := audit.Actor{Type: "user", ID: lt.c.owner.String()}
	link := func(member uuid.UUID, issuer, subject string) {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.LinkIdentity(ctx, tx, lt.c.org, now, by, member, identity.Identity{Issuer: issuer, Subject: subject, Tenant: "t"})
		})
	}
	// Before the link nothing is queued, however the identities change.
	link(lt.c.owner, idpA, "owner-a")
	link(lt.c.owner, idpB, "owner-b")
	link(lt.c.hr, idpA, "hr-a")
	if got := eventLog(t, lt); len(got) != 0 {
		t.Fatalf("a standalone organisation logged %d events for Fyniq", len(got))
	}
	offer, fyniqPriv, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	count := func(typ string) (n int) {
		for _, e := range eventLog(t, lt) {
			if e.Type == typ {
				n++
			}
		}
		return n
	}
	if n := count(nemura.TypeIdentityAccess); n != 3 {
		t.Fatalf("activating the link tells Fyniq of each identity (the owner's two and the HR officer's one): %d", n)
	}
	// A member deactivated: Fyniq is told their identity no longer enters.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, lt.c.org, now, lt.c.owner, lt.c.hr, now)
	})
	if n := count(nemura.TypeIdentityAccess); n != 4 {
		t.Fatalf("deactivating a member tells Fyniq of their identity: %d", n)
	}
	// A new identity linked to the owner.
	link(lt.c.owner, "https://idp-c.example.test", "owner-c")
	if n := count(nemura.TypeIdentityAccess); n != 7 { // the owner's three identities are told again: what an identity may enter is the member's
		t.Fatalf("linking an identity tells Fyniq of the member's identities: %d", n)
	}

	// What goes out: the identity and two answers, by the contract's fields, to the projection's door, signed; never an email or a role name.
	fy := newFyniqInMiniature(t, offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events")
	work := fy.sender(t, now).Work()
	for _, r := range queuedRows(t, lt) {
		if err := work(ctx, lt.c.org, r); err != nil {
			t.Fatalf("sending: %v", err)
		}
	}
	told := map[string]nemura.AccessEvent{}
	var lastSeq int64
	for _, c := range fy.calls {
		if c.Header.Type != nemura.TypeIdentityAccess {
			continue
		}
		if c.Header.ObjectType != "identity" || c.Header.ObjectVersion != int(c.Header.Sequence) {
			t.Errorf("an access event's object and version are the identity and its sequence: %+v", c.Header)
		}
		d := json.NewDecoder(strings.NewReader(string(c.Payload)))
		d.DisallowUnknownFields()
		var ev nemura.AccessEvent
		if err := d.Decode(&ev); err != nil {
			t.Fatalf("an access payload with more than the contract's fields: %v (%s)", err, c.Payload)
		}
		told[ev.Subject] = ev // the last word about each identity
		lastSeq = c.Header.Sequence
	}
	if lastSeq == 0 {
		t.Fatal("no access event was sent")
	}
	if o := told["owner-a"]; !o.PeopleAccess || !o.PayrollApprover || o.Issuer != idpA {
		t.Errorf("an Owner is an active member and approves pay runs: %+v", o)
	}
	if h := told["hr-a"]; h.PeopleAccess || h.PayrollApprover {
		t.Errorf("a deactivated member has no access: %+v", h)
	}
	if c := told["owner-c"]; !c.PeopleAccess || !c.PayrollApprover {
		t.Errorf("an identity linked after the link: %+v", c)
	}

	// What comes in: Fyniq's answer for an identity, by version.
	say := func(version int, seq int64, body map[string]any) (string, error) {
		return lt.receive(t, fyniqSays(t, fyniqPriv, lt, fyniqOrg, now, nemura.TypeIdentityAccess, "identity", "u1", version, seq, body), now)
	}
	finance := map[string]any{"issuer": idpA, "subject": "owner-a", "finance": true, "passkey": false}
	if out, err := say(7, 50, finance); err != nil || out != "applied" {
		t.Fatalf("Fyniq's answer: %q %v", out, err)
	}
	if out, err := say(6, 51, map[string]any{"issuer": idpA, "subject": "owner-a", "finance": false, "passkey": true}); err != nil || out != "ignored_older" {
		t.Errorf("an older answer arriving late: %q %v", out, err)
	}
	for what, body := range map[string]map[string]any{
		"a role name":    {"issuer": idpA, "subject": "owner-a", "finance": true, "passkey": true, "role": "owner"},
		"an email":       {"issuer": idpA, "subject": "owner-a", "finance": true, "passkey": true, "email": "a@b.test"},
		"no issuer":      {"issuer": "", "subject": "owner-a", "finance": true, "passkey": true},
		"no subject":     {"issuer": idpA, "subject": "", "finance": true, "passkey": true},
		"an extra field": {"issuer": idpA, "subject": "owner-a", "finance": true, "passkey": true, "extra": 1},
	} {
		if out, _ := say(8, 52, body); out != "refused" {
			t.Errorf("%s: %q", what, out)
		}
	}
	ws := nemura.WorkspaceService{Store: store(t, "nemura_api"), FinanceOrigin: "https://app.example.test/"}
	session := func(issuer, subject, provider string) api.Session {
		return api.Session{Organisation: lt.c.org, Member: lt.c.owner, Issuer: issuer, Subject: subject, Provider: provider}
	}
	if u, err := ws.FinanceURL(ctx, session(idpA, "owner-a", "zoho"), nil); err != nil || u != "https://app.example.test/?switch=1&provider=zoho" {
		t.Errorf("the switch for an identity Fyniq says holds a finance role: %q %v", u, err)
	}
	if u, _ := ws.FinanceURL(ctx, session(idpA, "owner-a", ""), nil); u != "https://app.example.test/?switch=1" {
		t.Errorf("a session with no provider: %q", u)
	}
	if u, _ := ws.FinanceURL(ctx, session(idpB, "owner-b", "google"), nil); u != "" {
		t.Errorf("the owner's other identity, of which Fyniq said nothing: %q", u)
	}
	if u, _ := ws.FinanceURL(ctx, session("", "", ""), nil); u != "" {
		t.Errorf("a session made before its way in was recorded: %q", u)
	}
	if u, _ := (nemura.WorkspaceService{Store: store(t, "nemura_api")}).FinanceURL(ctx, session(idpA, "owner-a", "zoho"), nil); u != "" {
		t.Errorf("a deployment that does not name Fyniq: %q", u)
	}
	if out, err := say(9, 53, map[string]any{"issuer": idpA, "subject": "owner-a", "finance": false, "passkey": true}); err != nil || out != "applied" {
		t.Fatalf("a newer answer: %q %v", out, err)
	}
	if u, _ := ws.FinanceURL(ctx, session(idpA, "owner-a", "zoho"), nil); u != "" {
		t.Errorf("the switch after Fyniq said the role ended: %q", u)
	}
	// An organisation whose link has ended shows no switch whatever Fyniq once said.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error { return nemura.EndLink(ctx, tx, lt.c.org, now, by) })
	if out, err := say(10, 54, finance); err == nil && out == "applied" {
		t.Errorf("an answer after the link ended: %q", out)
	}
	if u, _ := ws.FinanceURL(ctx, session(idpA, "owner-a", "zoho"), nil); u != "" {
		t.Errorf("the switch of an unlinked organisation: %q", u)
	}
	// Nemura's API role can read what Fyniq said and cannot rewrite it.
	if err := store(t, "nemura_api").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.link_identity_access SET finance = true`)
		return err
	}); err == nil {
		t.Error("the API rewrote what Fyniq said of an identity without a newer version")
	}
}

// Nemura tells Fyniq when a person is signed out or removed, narrowly (PRD-002 NM-INT-12; owner's decision of 21 Nov 2026: the one scope identity.sessions.write): signing out queues the identity the session
// was signed in with, a deactivation queues each identity of the member, a session made before the identity was recorded queues nothing, a standalone organisation queues nothing, an event Fyniq sent (the one
// that ends sessions here) never comes back as one, and what is sent is an issuer, a subject and a reason, to the door for sessions, signed, with no email.
// part of NM-INT-12 (Fyniq's half is its own test)
// asvs: V7.4.1, V8.2.1, V13.2.1
func TestASignOutOrARemovalIsToldToFyniqByIdentityAndFyniqsOwnRevocationNeverComesBack(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	idpA, idpB := "https://idp-a.example.test", "https://idp-b.example.test"
	by := audit.Actor{Type: "user", ID: lt.c.owner.String()}
	for _, x := range []struct {
		m    uuid.UUID
		i, s string
	}{{lt.c.owner, idpA, "owner-a"}, {lt.c.owner, idpB, "owner-b"}, {lt.c.hr, idpA, "hr-a"}} {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.LinkIdentity(ctx, tx, lt.c.org, now, by, x.m, identity.Identity{Issuer: x.i, Subject: x.s, Tenant: "t"})
		})
	}
	open := func(member uuid.UUID, via identity.Via) string {
		var tok string
		lt.c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
			tok, err = nemura.StartSession(ctx, tx, lt.c.org, now, member, via)
			return err
		})
		return tok
	}
	st := store(t, "nemura_api")
	// Standalone: signing out queues nothing.
	if err := st.CloseSession(ctx, open(lt.c.owner, identity.Via{Provider: "zoho", Issuer: idpA, Subject: "owner-a"}), now); err != nil {
		t.Fatal(err)
	}
	if got := eventLog(t, lt); len(got) != 0 {
		t.Fatalf("a standalone organisation queued %d events", len(got))
	}
	offer, fyniqPriv, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	count := func() (n int) {
		for _, e := range eventLog(t, lt) {
			if e.Type == nemura.TypeSessionRevoked {
				n++
			}
		}
		return n
	}
	if err := st.CloseSession(ctx, open(lt.c.owner, identity.Via{Provider: "zoho", Issuer: idpB, Subject: "owner-b"}), now); err != nil || count() != 1 {
		t.Fatalf("signing out of a session signed in with the owner's second identity: %d %v", count(), err)
	}
	if err := st.CloseSession(ctx, open(lt.c.owner, identity.Via{}), now); err != nil || count() != 1 {
		t.Errorf("a session made before the identity was recorded: %d %v", count(), err)
	}
	if err := st.CloseSession(ctx, "nobody.not-a-token", now); err != nil || count() != 1 {
		t.Errorf("a token that opens nothing: %d %v", count(), err)
	}
	// Fyniq's own revocation ends the owner's sessions here and sends nothing back.
	open(lt.c.owner, identity.Via{Provider: "zoho", Issuer: idpA, Subject: "owner-a"})
	if out, err := lt.receive(t, fyniqSays(t, fyniqPriv, lt, fyniqOrg, now, nemura.TypeSessionRevoked, "identity", "u", 1, 70, map[string]any{"issuer": idpA, "subject": "owner-a", "reason": "removed"}), now); err != nil || out != "applied" {
		t.Fatalf("Fyniq's revocation: %q %v", out, err)
	}
	if count() != 1 {
		t.Errorf("Fyniq's revocation came back: %d", count())
	}
	// A deactivation tells Fyniq of each of the member's identities.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, lt.c.org, now, lt.c.owner, lt.c.hr, now)
	})
	if count() != 2 {
		t.Fatalf("a deactivation tells Fyniq of the HR officer's one identity: %d", count())
	}
	// What is sent: the identity and a reason, to the sessions' door, signed.
	fy := newFyniqInMiniature(t, offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events", "/v1/people-link/session-events")
	work := fy.sender(t, now).Work()
	for _, r := range queuedRows(t, lt) {
		if err := work(ctx, lt.c.org, r); err != nil {
			t.Fatalf("sending: %v", err)
		}
	}
	got := map[string]string{}
	for _, c := range fy.calls {
		if c.Header.Type != nemura.TypeSessionRevoked {
			continue
		}
		d := json.NewDecoder(strings.NewReader(string(c.Payload)))
		d.DisallowUnknownFields()
		var ev nemura.SessionRevokedEvent
		if err := d.Decode(&ev); err != nil || strings.Contains(string(c.Payload), "@") {
			t.Fatalf("a sign-out payload with more than the contract's fields: %v (%s)", err, c.Payload)
		}
		got[ev.Subject] = ev.Reason
	}
	if len(got) != 2 || got["owner-b"] != "signed_out" || got["hr-a"] != "removed" {
		t.Errorf("what Fyniq was told: %v", got)
	}
}

// Fyniq's read-only client reads the link's event log by identity, and nothing more (PRD-001 FR-PPL-08, PRD-002 NM-INT-10; owner's decision of 21 Nov 2026: link.sync.read, identities only): each event's sequence, ID,
// type, object, version and state in order, the highest sequence queued, and pages of a size asked for; never a payload (the answer has no field that could hold one); and only the asking client of the live link
// at its epoch with this scope: the client that sends events, another client of the link, another epoch and a call for another scope are each refused, and an ended link answers nobody.
// part of NM-INT-10 (Fyniq's detection of a gap is its own)
// asvs: V8.2.1, V4.2.1, V13.2.1
func TestFyniqsReadOnlyClientReadsTheLinksEventLogByIdentityAndNothingMore(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	offer, _, _ := linked(t, lt, now, &fakeOAuth{})
	for i := 0; i < 4; i++ { // identity-only events of any type will do
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.QueueLinkEvent(ctx, tx, lt.c.org, now, "people.tax_hold.changed", "tax_hold", uuid.NewString(), 1)
			return err
		})
	}
	svc := nemura.LinkService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now)}
	var l nemura.LinkRow
	lt.c.must(func(ctx context.Context, tx pgx.Tx) (err error) { l, err = nemura.LiveLink(ctx, tx); return err })
	caller := linkauth.Caller{Organisation: lt.c.org, Link: l.ID, Epoch: l.Epoch, ClientID: offer.PeerAskClientID, Scope: apiclient.SyncScope}
	page, err := svc.Sync(ctx, caller, 0, 3)
	if err != nil || len(page.Events) != 3 || page.HighestSequence < 7 || page.NextCursor == "" || page.Epoch != l.Epoch {
		t.Fatalf("the first page: %+v %v", page, err)
	}
	// Every page, in order: the people the link was activated with (a snapshot) and the four events queued after it, ending at the highest sequence.
	var all []api.LinkSyncEvent
	for p, cursor := page, ""; ; {
		all = append(all, p.Events...)
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
		n, _ := strconv.ParseInt(cursor, 10, 64)
		var err error
		if p, err = svc.Sync(ctx, caller, n, 3); err != nil {
			t.Fatal(err)
		}
	}
	if len(all) != 7 || all[len(all)-1].Sequence != page.HighestSequence {
		t.Fatalf("the whole log: %d events, highest %d", len(all), page.HighestSequence)
	}
	for i, e := range all {
		if e.EventID == "" || e.State != "queued" || e.ObjectID == "" || (i > 0 && e.Sequence != all[i-1].Sequence+1) || !slices.Contains([]string{"people.person.upserted", "people.tax_hold.changed"}, e.Type) {
			t.Errorf("an event of the log: %+v", e)
		}
	}
	// The answer has no field that could hold a payload, a name or a figure.
	raw, _ := json.Marshal(page)
	var shape map[string]any
	_ = json.Unmarshal(raw, &shape)
	for k := range shape {
		if !slices.Contains([]string{"epoch", "highest_sequence", "events", "next_cursor"}, k) {
			t.Errorf("the log's answer carries %q", k)
		}
	}
	for _, e := range shape["events"].([]any) {
		for k := range e.(map[string]any) {
			if !slices.Contains([]string{"sequence", "event_id", "type", "object_type", "object_id", "object_version", "state"}, k) {
				t.Errorf("an event of the log carries %q", k)
			}
		}
	}
	// Who may read it.
	var r *payroll.Refusal
	for what, c := range map[string]linkauth.Caller{
		"the client that sends events": {Organisation: lt.c.org, Link: l.ID, Epoch: l.Epoch, ClientID: offer.PeerClientID, Scope: apiclient.SyncScope},
		"a token for the ask scope":    {Organisation: lt.c.org, Link: l.ID, Epoch: l.Epoch, ClientID: offer.PeerAskClientID, Scope: apiclient.AskScope},
		"a token for the events scope": {Organisation: lt.c.org, Link: l.ID, Epoch: l.Epoch, ClientID: offer.PeerAskClientID, Scope: apiclient.LinkScope},
		"another client":               {Organisation: lt.c.org, Link: l.ID, Epoch: l.Epoch, ClientID: "someone-else", Scope: apiclient.SyncScope},
		"another epoch":                {Organisation: lt.c.org, Link: l.ID, Epoch: l.Epoch + 1, ClientID: offer.PeerAskClientID, Scope: apiclient.SyncScope},
		"another link":                 {Organisation: lt.c.org, Link: uuid.New(), Epoch: l.Epoch, ClientID: offer.PeerAskClientID, Scope: apiclient.SyncScope},
	} {
		if _, err := svc.Sync(ctx, c, 0, 10); !errors.As(err, &r) || r.Code != nemura.CodeLinkNone {
			t.Errorf("%s: %v", what, err)
		}
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.EndLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()})
	})
	if _, err := svc.Sync(ctx, caller, 0, 10); err == nil {
		t.Error("an ended link answered")
	}
}
