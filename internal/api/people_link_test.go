//go:build local

package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// signedEvent is an event as Nemura would send it: the envelope signed with the link key.
func signedEvent(t *testing.T, key ed25519.PrivateKey, e *env, link, nemura string, epoch int, typ, objectType, objectID string, version int, payload map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(payload)
	now := e.clock.Now().UTC().Format(time.RFC3339)
	env, err := peoplelink.Sign(key, peoplelink.Header{EventID: uuid.NewString(), Type: typ, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: nemura,
		TargetOrganisation: e.books.Org.String(), Epoch: epoch, Sequence: int64(version), ObjectType: objectType, ObjectID: objectID, ObjectVersion: version,
		EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(env)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// The link to Nemura end to end, against the pinned Hydra (FR-PPL-01, 15, 16; PRD-002 NM-ARC-04, NM-INT-10): an Admin asks to link and the Owner
// approves with another approver (dual control), which registers the link's client with Appendix E's scopes and no others and records the link
// at epoch 1; Nemura then sends person events signed with the link key through that client: applied once, a repeat does nothing, a late older
// version never sets the manager back, an altered payload, an old epoch, a stale signing time, another organisation's event and a field Appendix
// E does not list are each refused with their own code; no role, no ordinary client and no other scope reaches an event operation, and the
// link's client reaches nothing else. Unlinking revokes the client, its tokens end, and a later link is epoch 2.
func TestThePeopleLinkCarriesSignedEventsThroughItsOwnClient(t *testing.T) {
	s := startOAuth(t, 0)
	e := s.e
	e.api.deps.OAuthClients, e.api.deps.APIAudience = s.admin, apiAudience
	api := tokenAPI(t, s)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	nemura := uuid.NewString()
	linkPub, linkPriv, _ := ed25519.GenerateKey(rand.Reader)
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	expires := e.clock.Now().AddDate(0, 6, 0).UTC().Format(time.RFC3339)
	body := func() map[string]any {
		return map[string]any{"nemura_organisation_id": nemura, "nemura_origin": nemuraOrigin, "nemura_client_id": "fyniq-link-" + nemura, "nemura_ask_client_id": "fyniq-ask-" + nemura, "link_key": base64.RawURLEncoding.EncodeToString(linkPub), "client_name": "Nemura link",
			"client_public_key": publicJWK(t, clientKey), "expires_at": expires, "features": []string{"records", "leave"}}
	}
	if l := e.call(http.MethodGet, "/v1/people-link", admin, nil); l.Status != http.StatusOK || l.str("state") != "none" {
		t.Fatalf("before any link: %d %s", l.Status, l.Raw)
	}
	bad := body()
	bad["link_key"] = "tooshort"
	e.problemIs(e.call(http.MethodPost, "/v1/people-link", admin, bad), "requestPeopleLink", 422, CodeInvalid)
	bad = body()
	bad["client_public_key"] = map[string]any{"kty": "oct", "k": "c2VjcmV0"}
	e.problemIs(e.call(http.MethodPost, "/v1/people-link", admin, bad), "requestPeopleLink", 422, apiclient.CodeKey)

	asked := e.call(http.MethodPost, "/v1/people-link", admin, body())
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectPeopleLink || asked.str("operation") != approval.OpPeopleLink {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/people-link", admin, nil); l.str("state") != "none" {
		t.Fatalf("linked before its approval: %s", l.Raw)
	}
	e.problemIs(f.decide(admin, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	l := e.call(http.MethodGet, "/v1/people-link", admin, nil)
	if l.str("state") != "active" || l.Body["epoch"].(float64) != 1 || l.str("nemura_organisation_id") != nemura || l.str("client_id") == "" {
		t.Fatalf("the link: %s", l.Raw)
	}
	for _, src := range l.Body["sources"].([]any) {
		m := src.(map[string]any)
		want := "none"
		if m["feature"] == "records" || m["feature"] == "leave" {
			want = "nemura"
		}
		if m["source"] != want {
			t.Errorf("%v lives in %v, want %s", m["feature"], m["source"], want)
		}
	}
	// Fyniq's half of the link is shown for Nemura to complete its own: the public keys, never a private one, and the clients' IDs (design D9, D10).
	if k, _ := l.Body["fyniq_client_public_key"].(map[string]any); len(l.str("fyniq_link_key")) != 43 || k["kty"] != "EC" || k["d"] != nil || l.str("nemura_client_id") != "fyniq-link-"+nemura || l.str("nemura_ask_client_id") != "fyniq-ask-"+nemura || func() bool {
		k, _ := l.Body["fyniq_ask_client_public_key"].(map[string]any)
		return k["kty"] != "EC" || k["d"] != nil
	}() || l.str("oauth_client_id") == "" ||
		strings.Contains(l.Raw, "link_private") || strings.Contains(l.Raw, "client_private") || strings.Contains(l.Raw, "ask_private") {
		t.Errorf("Fyniq's half of the link: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/people-link", admin, body()), "requestPeopleLink", 409, CodeLinkExists)
	var client storage.APIClient
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		client, err = u.APIClients().Get(ctx, row.ClientID)
		return err
	})
	if client.Kind != storage.APIClientLink || strings.Join(client.Scopes, " ") != strings.Join(apiclient.LinkScopes, " ") {
		t.Fatalf("the link's client: %+v", client)
	}
	// The link's client is not an integration client: it is not listed there, and it holds no fyniq scope.
	if list := e.call(http.MethodGet, "/v1/integration-clients", admin, nil); strings.Contains(list.Raw, client.ClientID) {
		t.Errorf("the link's client is listed with the integration clients: %s", list.Raw)
	}

	status, out := clientToken(t, s, client.ClientID, clientKey, "people.projection.write")
	tok, _ := out["access_token"].(string)
	if status != 200 || tok == "" {
		t.Fatalf("the link's token: %d %v", status, out)
	}
	if status, out := clientToken(t, s, client.ClientID, clientKey, "fyniq.read.accounting"); status == 200 {
		t.Errorf("an ordinary scope for the link's client: %v", out)
	}
	_, holdOut := clientToken(t, s, client.ClientID, clientKey, "people.holds.write")
	holdTok, _ := holdOut["access_token"].(string)

	person, mgr1, mgr2 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	upsert := func(manager string) map[string]any {
		return map[string]any{"person_id": person, "display_name": "Wei Jie Ong", "work_email": "weijie@example.com", "status": "active", "started_on": "2026-01-05",
			"line_manager_person_id": manager, "deleted": false}
	}
	send := func(token string, ev map[string]any) response {
		return api.call(http.MethodPost, "/v1/people-link/projection-events", token, ev)
	}
	ev1 := signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 1, upsert(mgr1))
	if r := send(tok, ev1); r.Status != 200 || r.str("outcome") != "applied" {
		t.Fatalf("version 1: %d %s", r.Status, r.Raw)
	}
	if r := send(tok, ev1); r.Status != 200 || r.str("outcome") != "duplicate" {
		t.Errorf("the same event again: %d %s", r.Status, r.Raw)
	}
	if r := send(tok, signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 2, upsert(mgr2))); r.str("outcome") != "applied" {
		t.Fatalf("version 2: %s", r.Raw)
	}
	if r := send(tok, signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 1, upsert(mgr1))); r.Status != 200 || r.str("outcome") != "ignored_older" {
		t.Errorf("a late version 1: %d %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, uuid.MustParse(person))
		if err != nil || p.LineManager.String() != mgr2 || p.Version != 2 {
			t.Errorf("a late older version set the manager back: %+v %v", p, err)
		}
		counts, _ := u.PeopleLinks().InboxCounts(ctx, mustLink(ctx, t, u).ID)
		got := map[string]int{}
		for _, c := range counts {
			got[c.Outcome] = c.N
		}
		if got["applied"] != 2 || got["ignored_older"] != 1 {
			t.Errorf("the inbox keeps each event once: %v", got)
		}
		return nil
	})

	// Each forgery is refused with its own code, and changes nothing.
	altered := signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 3, upsert(mgr1))
	altered["payload"].(map[string]any)["display_name"] = "Someone Else"
	e.problemIs(send(tok, altered), "receivePeopleProjectionEvent", 403, peoplelink.CodeDigest)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	e.problemIs(send(tok, signedEvent(t, otherPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 3, upsert(mgr1))), "receivePeopleProjectionEvent", 403, peoplelink.CodeSignature)
	e.problemIs(send(tok, signedEvent(t, linkPriv, e, "", nemura, 2, peoplelink.TypePersonUpserted, "person", person, 3, upsert(mgr1))), "receivePeopleProjectionEvent", 403, peoplelink.CodeEpoch)
	e.problemIs(send(tok, signedEvent(t, linkPriv, e, "", uuid.NewString(), 1, peoplelink.TypePersonUpserted, "person", person, 3, upsert(mgr1))), "receivePeopleProjectionEvent", 403, peoplelink.CodeOrg)
	stale := signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 3, upsert(mgr1))
	raw, _ := json.Marshal(stale)
	_ = raw
	staleHeader := stale["header"].(map[string]any)
	old := e.clock.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	staleEnv := resign(t, linkPriv, stale, func(h *peoplelink.Header) { h.SignedAt = old })
	_ = staleHeader
	e.problemIs(send(tok, staleEnv), "receivePeopleProjectionEvent", 403, peoplelink.CodeReplay)
	extra := upsert(mgr1)
	extra["nric"] = "S1234567D" // never sent: Appendix E lists no NRIC (NM-INT-05)
	e.problemIs(send(tok, signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 3, extra)), "receivePeopleProjectionEvent", 422, CodeLinkMinimum)
	if r := send(tok, signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypeTaxHoldChanged, "tax_hold", person, 1, map[string]any{})); r.Status != 422 {
		t.Errorf("a hold event sent to the projection operation: %d %s", r.Status, r.Raw)
	}

	// Who may call: the link's client with the right scope, and no one else.
	hold := signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypeTaxHoldChanged, "tax_hold", person, 1,
		map[string]any{"person_id": person, "hold_id": uuid.NewString(), "effective_from": "2026-10-01", "status": "held"})
	hp := func(token string) response {
		return api.call(http.MethodPost, "/v1/people-link/hold-events", token, hold)
	}
	if r := hp(tok); r.Status != http.StatusForbidden {
		t.Errorf("a hold event with only people.projection.write: %d %s", r.Status, r.Raw)
	}
	if r := hp(holdTok); r.Status != 200 || r.str("outcome") != "applied" {
		t.Errorf("a hold event with people.holds.write: %d %s", r.Status, r.Raw)
	}
	for _, who := range []string{owner, admin, f.as(f.ahkow, authz.Accountant)} {
		if r := e.call(http.MethodPost, "/v1/people-link/projection-events", who, ev1); r.Status != http.StatusForbidden {
			t.Errorf("a person's session reached an event operation: %d %s", r.Status, r.Raw)
		}
	}
	if r := api.call(http.MethodGet, "/v1/accounts", tok, nil); r.Status != http.StatusForbidden {
		t.Errorf("the link's client read the chart: %d %s", r.Status, r.Raw)
	}
	if r := api.call(http.MethodGet, "/v1/people-link", tok, nil); r.Status != http.StatusForbidden {
		t.Errorf("the link's client read the settings page: %d %s", r.Status, r.Raw)
	}
	if r := api.call(http.MethodPost, "/v1/people-link/unlink", tok, map[string]any{"reason": "x"}); r.Status != http.StatusForbidden {
		t.Errorf("the link's client unlinked itself: %d %s", r.Status, r.Raw)
	}

	// Unlinking: the Owner and a second approver; the client is revoked and its tokens end; events of the ended link are refused.
	un := e.call(http.MethodPost, "/v1/people-link/unlink", admin, map[string]any{"reason": "The company moved to another provider."})
	if un.Status != http.StatusAccepted || un.str("operation") != approval.OpPeopleUnlink {
		t.Fatalf("asking to unlink: %d %s", un.Status, un.Raw)
	}
	if d := f.decide(owner, un.str("id"), "approve", hashOf(un)); d.Status != http.StatusOK {
		t.Fatalf("approving the unlink: %d %s", d.Status, d.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/people-link", admin, nil); l.str("state") != "none" {
		t.Errorf("after unlinking no link is live: %s", l.Raw)
	}
	if r := send(tok, signedEvent(t, linkPriv, e, "", nemura, 1, peoplelink.TypePersonUpserted, "person", person, 9, upsert(mgr1))); r.Status != http.StatusUnauthorized {
		t.Errorf("a token of the ended link: %d %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if c, err := u.APIClients().Get(ctx, client.ID); err != nil || c.RevokedAt == nil {
			t.Errorf("the link's client is revoked: %+v %v", c, err)
		}
		if _, err := u.PeopleLinks().Live(ctx); err == nil {
			t.Error("nothing is live")
		}
		return nil
	})
	again := e.call(http.MethodPost, "/v1/people-link", admin, body())
	if d := f.decide(owner, again.str("id"), "approve", hashOf(again)); d.Status != http.StatusOK {
		t.Fatalf("linking again: %d %s", d.Status, d.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/people-link", admin, nil); l.Body["epoch"].(float64) != 2 {
		t.Errorf("a later link is epoch 2: %s", l.Raw)
	}
	_ = ports.ErrNotFound
}

func mustLink(ctx context.Context, t *testing.T, u storage.UnitOfWork) storage.PeopleLinkRow {
	t.Helper()
	l, err := u.PeopleLinks().Live(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// resign signs an event's header again after a change to it, keeping its payload.
func resign(t *testing.T, key ed25519.PrivateKey, ev map[string]any, edit func(*peoplelink.Header)) map[string]any {
	t.Helper()
	b, _ := json.Marshal(ev)
	var env peoplelink.Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	edit(&env.Header)
	signed, err := peoplelink.Sign(key, env.Header, env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(signed)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

// No client class reaches an event operation by claiming a link scope: the operation needs the link's own client (p1-payroll-link design D1).
// asvs: V8.2.1, V4.2.1, V10.3.1
func TestNoOneButTheLinksClientReachesAnEventOperation(t *testing.T) {
	e := newEnv(t)
	linkPub, linkPriv, _ := ed25519.GenerateKey(rand.Reader)
	_ = linkPub
	ev := signedEvent(t, linkPriv, e, "", uuid.NewString(), 1, peoplelink.TypePersonUpserted, "person", uuid.NewString(), 1,
		map[string]any{"person_id": uuid.NewString(), "display_name": "x", "deleted": false})
	for _, class := range []string{authn.ClassUser, authn.ClassIntegration, authn.ClassAgent} {
		tok := e.principal(authn.Principal{Client: "x", Class: class, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, Scoped: class != authn.ClassUser,
			Scopes: []string{"people.projection.write", "people.holds.write", "payroll.journal.submit", "identity.sessions.write"}})
		for _, path := range []string{"/v1/people-link/projection-events", "/v1/people-link/hold-events", "/v1/people-link/session-events"} {
			if r := e.call(http.MethodPost, path, tok, ev); r.Status != http.StatusForbidden {
				t.Errorf("a %s principal claiming the link's scopes reached %s: %d %s", class, path, r.Status, r.Raw)
			}
		}
	}
	// The link's own principal reaches the link's operations only.
	link := e.principal(authn.Principal{Client: "link", Class: authn.ClassIntegration, Organisation: e.books.Org, Scoped: true, Link: true, Scopes: apiclient.LinkScopes})
	for _, path := range []string{"/v1/accounts", "/v1/people-link", "/v1/members", "/v1/journals", "/v1/approvals"} {
		if r := e.call(http.MethodGet, path, link, nil); r.Status != http.StatusForbidden {
			t.Errorf("the link's client reached %s: %d %s", path, r.Status, r.Raw)
		}
	}
}

// matrixPeopleLink are the authorisation matrix's rows of the link to the people platform: reading and asking under People and HR's rights, and
// the event operations refused to every role, whatever it holds (only the link's client calls them).
var matrixPeopleLink = map[string]func(e *env, tok string) (response, func() bool){
	"getPeopleLink": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/people-link", tok, nil), nil
	},
	"requestPeopleLink": func(e *env, tok string) (response, func() bool) {
		was, aud := e.api.deps.OAuthClients, e.api.deps.APIAudience
		defer func() { e.api.deps.OAuthClients, e.api.deps.APIAudience = was, aud }()
		e.api.deps.OAuthClients, e.api.deps.APIAudience = fakeOAuthClients{}, apiAudience
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		linkPub, _, _ := ed25519.GenerateKey(rand.Reader)
		return e.call(http.MethodPost, "/v1/people-link", steppedUp(e, tok), map[string]any{"nemura_organisation_id": uuid.NewString(), "nemura_origin": nemuraOrigin, "nemura_client_id": "fyniq-link-matrix", "nemura_ask_client_id": "fyniq-ask-matrix",
			"link_key": base64.RawURLEncoding.EncodeToString(linkPub), "client_name": "Matrix link", "client_public_key": publicJWK(e.t, key),
			"expires_at": e.clock.Now().AddDate(0, 6, 0).Format(time.RFC3339), "features": []string{"records"}}, "Prefer", "return=preview"), nil
	},
	"requestPeopleUnlink": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/unlink", steppedUp(e, tok), map[string]any{"reason": "Matrix"}, "Prefer", "return=preview"), nil
	},
	"receivePeopleProjectionEvent": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/projection-events", tok, map[string]any{}), nil
	},
	"receivePeopleHoldEvent": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/hold-events", tok, map[string]any{}), nil
	},
	"receivePeopleSessionEvent": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/session-events", tok, map[string]any{}), nil
	},
}

// A payment to a person held for tax clearance is not released (FR-PPL-13, FR-PPL-10): the release refuses while the projection shows the person held, by an approval or by break-glass
// (both call the one check), names who is held and not a figure, ignores vendors and people who are not mapped, and pays them again once Nemura lifts the hold.
// part of FR-PPL-13 (asking Nemura at the release and releasing the rest of the batch are open)
func TestAReleaseNeverPaysAPersonHeldForTaxClearance(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user, other := uuid.New(), uuid.New()
	now := e.clock.Now()
	person := uuid.New()
	batch := paymentbatch.Batch{Payments: []paymentbatch.Payment{{Payee: payee.Person(user)}, {Payee: payee.Vendor(uuid.New())}, {Payee: payee.Person(other)}}}
	check := func() error {
		return e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return heldPayees(ctx, u, batch, nil) })
	}
	save := func(p peoplelink.Person) {
		t.Helper()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.PeopleLinks().SavePerson(ctx, p) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := check(); err != nil {
		t.Fatalf("nobody is mapped: %v", err)
	}
	p := peoplelink.Person{ID: person, Version: 1, DisplayName: "Chen Yu", UserID: user, UpdatedAt: now}
	save(p)
	if err := check(); err != nil {
		t.Fatalf("a mapped person who is not held: %v", err)
	}
	p.HoldVersion, p.HoldID, p.Held = 1, uuid.New(), true
	save(p)
	err := check()
	var pr *Problem
	if !errors.As(err, &pr) || pr.Code != peoplelink.CodeHold || !strings.Contains(pr.Detail, "Chen Yu") {
		t.Fatalf("a held person: %v", err)
	}
	p.HoldVersion, p.Held = 2, false
	save(p)
	if err := check(); err != nil {
		t.Errorf("after Nemura lifts the hold: %v", err)
	}
}

// fakeHolds is Nemura as the release asks it: an answer per person, or an error (no answer within five seconds).
type fakeHolds struct {
	answer map[uuid.UUID]linksend.HoldAnswer
	err    error
	asked  []uuid.UUID
}

func (f *fakeHolds) Ask(_ context.Context, _ storage.PeopleLinkAskKey, person uuid.UUID) (linksend.HoldAnswer, error) {
	f.asked = append(f.asked, person)
	return f.answer[person], f.err
}

// At the release Nemura, the hold's authority, is asked too, and nothing is paid on a guess (FR-PPL-13, design D10): a person Nemura says is held is held though the projection is behind; no answer within five
// seconds, or an answer with an older version than the projection already saw, leaves the person's lines waiting with PPL-22; vendors and people who are not mapped are not asked; a lifted hold pays; and with
// no authority, or no link with an ask key, the projection alone decides as before.
// part of FR-PPL-13 (the ask and failing closed; releasing the rest of the batch waits for the salary batch)
// asvs: V8.2.1, V11.2.1, V16.5.2
func TestAReleaseAsksNemuraWhetherAPersonIsHeldAndFailsClosed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user, vendor := uuid.New(), uuid.New()
	person := uuid.New()
	now := e.clock.Now()
	batch := paymentbatch.Batch{Payments: []paymentbatch.Payment{{Payee: payee.Person(user)}, {Payee: payee.Vendor(vendor)}, {Payee: payee.Person(uuid.New())}}}
	save := func(fn func(u storage.UnitOfWork) error) {
		t.Helper()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) }); err != nil {
			t.Fatal(err)
		}
	}
	check := func(h HoldAuthority) error {
		return e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return heldPayees(ctx, u, batch, h) })
	}
	code := func(err error) string {
		var pr *Problem
		if errors.As(err, &pr) {
			return pr.Code
		}
		return ""
	}
	p := peoplelink.Person{ID: person, Version: 1, DisplayName: "Chen Yu", UserID: user, UpdatedAt: now, HoldVersion: 2, HoldID: uuid.New()}
	save(func(u storage.UnitOfWork) error { return u.PeopleLinks().SavePerson(ctx, p) })
	// No link yet: the authority is never asked, and the projection (not held) lets the person be paid.
	f := &fakeHolds{answer: map[uuid.UUID]linksend.HoldAnswer{person: {Status: "held", Version: 3}}}
	if err := check(f); err != nil || len(f.asked) != 0 {
		t.Fatalf("with no link: %v, asked %v", err, f.asked)
	}
	save(func(u storage.UnitOfWork) error {
		approval := uuid.New()
		l, err := u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: uuid.New(), LinkKey: make([]byte, 32), ApprovalID: approval, CreatedBy: "user:" + user.String(), CreatedAt: now})
		if err != nil {
			return err
		}
		k, err := newAskKey(l.ID, "fyniq-ask-test")
		if err != nil {
			return err
		}
		return u.PeopleLinks().SaveAskKey(ctx, k)
	})
	if err := check(nil); err != nil {
		t.Errorf("with no authority the projection decides: %v", err)
	}
	// Nemura says held, newer than the projection: refused as held, naming the person and no figure.
	if err := check(f); code(err) != peoplelink.CodeHold || !strings.Contains(err.Error(), "Chen Yu") || len(f.asked) != 1 || f.asked[0] != person {
		t.Errorf("Nemura says held: %v, asked %v", err, f.asked)
	}
	// Nemura says released at a version the projection already saw or newer: paid. The vendor and the unmapped person are never asked.
	f = &fakeHolds{answer: map[uuid.UUID]linksend.HoldAnswer{person: {Status: "released", Version: 2}}}
	if err := check(f); err != nil || len(f.asked) != 1 {
		t.Errorf("Nemura says released: %v, asked %v", err, f.asked)
	}
	// An older answer than the projection's newest hold event is not an answer; no answer at all fails closed; both leave the lines waiting.
	f = &fakeHolds{answer: map[uuid.UUID]linksend.HoldAnswer{person: {Status: "released", Version: 1}}}
	if err := check(f); code(err) != peoplelink.CodeHoldUnknown {
		t.Errorf("an older answer: %v", err)
	}
	if err := check(&fakeHolds{err: errors.New("Nemura did not answer")}); code(err) != peoplelink.CodeHoldUnknown || !strings.Contains(err.Error(), "Chen Yu") {
		t.Errorf("no answer: %v", err)
	}
	// A person the projection already holds is refused without asking.
	p.Held, p.HoldVersion = true, 3
	save(func(u storage.UnitOfWork) error { return u.PeopleLinks().SavePerson(ctx, p) })
	f = &fakeHolds{answer: map[uuid.UUID]linksend.HoldAnswer{person: {Status: "released", Version: 4}}}
	if err := check(f); code(err) != peoplelink.CodeHold || len(f.asked) != 0 {
		t.Errorf("the projection holds: %v, asked %v", err, f.asked)
	}
}

// The ask has its five seconds and no more: a Nemura that never answers fails the ask in about that time, never hangs the release (FR-PPL-13).
// part of FR-PPL-13 (the five-second bound)
func TestTheAskAtReleaseGivesUpAfterFiveSeconds(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		select { // never answers, until the caller leaves or the test is over
		case <-r.Context().Done():
		case <-time.After(6 * time.Second):
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	k, err := newAskKey(uuid.New(), "fyniq-ask-slow")
	if err != nil {
		t.Fatal(err)
	}
	asker := &linksend.HoldAsker{HTTP: srv.Client(), APIURL: srv.URL, TokenURL: srv.URL + "/oauth2/token", Audience: "nemura-api", Clock: clock.System{}}
	start := clock.System{}.Now()
	if _, err := asker.Ask(context.Background(), k, uuid.New()); err == nil {
		t.Fatal("a Nemura that never answers gave an answer")
	}
	took := clock.System{}.Now().Sub(start)
	if took < 4*time.Second || took > 8*time.Second {
		t.Errorf("the ask gave up after %s, want about 5s", took)
	}
}
