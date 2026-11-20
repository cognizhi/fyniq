//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// noSessions is a session store that knows nobody: the link's token is not a session.
type noSessions struct{}

func (noSessions) Authenticate(context.Context, string, time.Time) (api.Session, error) {
	return api.Session{}, errors.New("no such session")
}
func (noSessions) RolesAt(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]authz.Role, error) {
	return nil, errors.New("no such member")
}

// tokenServer stands in for Nemura's OAuth server's introspection: it knows the tokens the test minted.
type tokenServer struct {
	ports.OAuthServer
	tokens map[string]ports.Introspection
}

func (s *tokenServer) Introspect(_ context.Context, token string) (ports.Introspection, error) {
	return s.tokens[token], nil // an unknown token is not active
}

// linked makes a link the way the ceremony does: an Owner proposes, another Owner or Administrator approves with Fyniq's half, and Fyniq's client is registered with the OAuth server.
func linked(t *testing.T, lt *leaveTeam, now time.Time, oauth *fakeOAuth) (offer nemura.LinkOffer, fyniq ed25519.PrivateKey, fyniqOrg uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	svc := nemura.LinkService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(now)}
	fyniqPub, fyniqPriv, _ := ed25519.GenerateKey(rand.Reader)
	fyniqOrg = uuid.New()
	var err error
	if offer, err = svc.Propose(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}, fyniqOrg); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, lt.c.org, now, lt.c.owner, lt.c.hr, authz.Administrator, today, nil)
		return err
	})
	if err := svc.Activate(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &now}, fyniqPub, "fyniq-client-1", json.RawMessage(jwk), json.RawMessage(jwk)); err != nil {
		t.Fatal(err)
	}
	return offer, fyniqPriv, fyniqOrg
}

// The link is made in two halves by two people (NM-ARC-04): an Owner with a fresh passkey proposes, and Nemura makes its two keys (one for what it signs, one for its token) and the ID Fyniq's
// client will have here; another Owner or Administrator with a fresh passkey approves with Fyniq's half, and Fyniq's client is registered at Nemura's own OAuth server with the link scope alone, under that
// ID. The asker cannot approve, nor anyone who is neither; a link is completed once; the private halves are the worker's to read and the API cannot; and no ordinary registration can be given the link scope.
// part of NM-ARC-04 (the proposal and its approval by a second person; the page around them and unlinking with work in flight are not built)
// asvs: V8.2.1, V6.4.1, V13.2.1, V11.2.1
func TestTheLinkIsProposedByAnOwnerAndApprovedByAnotherWithFyniqsHalf(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	oauth := &fakeOAuth{}
	svc := nemura.LinkService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(now)}
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	stale := now.Add(-time.Hour)
	fyniqPub, _, _ := ed25519.GenerateKey(rand.Reader)
	fyniqOrg := uuid.New()
	var r *payroll.Refusal

	if _, err := svc.Propose(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &stale}, fyniqOrg); !errors.As(err, &r) || r.Code != nemura.CodeLinkStepUp {
		t.Errorf("proposing without a fresh passkey: %v", err)
	}
	if _, err := svc.Propose(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &now}, fyniqOrg); !errors.As(err, &r) || r.Code != nemura.CodeLinkApprover {
		t.Errorf("a person who is not the Owner proposing: %v", err)
	}
	offer, err := svc.Propose(ctx, owner, fyniqOrg)
	if err != nil {
		t.Fatal(err)
	}
	if offer.Epoch != 1 || len(offer.LinkKey) != ed25519.PublicKeySize || !strings.HasPrefix(offer.PeerClientID, "fyniq-link-") || !strings.Contains(string(offer.ClientKey), `"kty":"EC"`) || strings.Contains(string(offer.ClientKey), `"d"`) {
		t.Fatalf("the offer: %+v", offer)
	}
	if _, err := svc.Propose(ctx, owner, fyniqOrg); err == nil {
		t.Error("a second link was proposed while one is live")
	}
	// A proposed link receives nothing and is no client's link.
	if _, err := store(t, "nemura_api").LinkClientRoute(ctx, offer.PeerClientID); err != nil {
		t.Errorf("the route of a proposed link: %v", err)
	}

	hr := api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &now}
	if err := svc.Activate(ctx, owner, fyniqPub, "fyniq-client-1", json.RawMessage(jwk), json.RawMessage(jwk)); !errors.As(err, &r) || r.Code != nemura.CodeLinkApprover {
		t.Errorf("the asker approved their own link: %v", err)
	}
	if err := svc.Activate(ctx, hr, fyniqPub, "fyniq-client-1", json.RawMessage(jwk), json.RawMessage(jwk)); !errors.As(err, &r) || r.Code != nemura.CodeLinkApprover {
		t.Errorf("someone who is neither Owner nor Administrator approved: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, lt.c.org, now, lt.c.owner, lt.c.hr, authz.Administrator, today, nil)
		return err
	})
	if err := svc.Activate(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &stale}, fyniqPub, "fyniq-client-1", json.RawMessage(jwk), json.RawMessage(jwk)); !errors.As(err, &r) || r.Code != nemura.CodeLinkStepUp {
		t.Errorf("an approval without a fresh passkey: %v", err)
	}
	if err := svc.Activate(ctx, hr, fyniqPub, "fyniq-client-1", json.RawMessage(`{"kty":"EC","crv":"P-256","x":"a","y":"b","d":"secret"}`), json.RawMessage(jwk)); err == nil || len(oauth.registered) != 0 {
		t.Errorf("a client key with a private part: %v", err)
	}
	if err := svc.Activate(ctx, hr, fyniqPub, "fyniq-client-1", json.RawMessage(jwk), json.RawMessage(jwk)); err != nil {
		t.Fatalf("the second approver completes the link: %v", err)
	}
	if len(oauth.registered) != 2 {
		t.Fatalf("clients registered: %d", len(oauth.registered))
	}
	if a := oauth.registered[1]; a.ClientID != offer.PeerAskClientID || strings.Join(a.Scopes, " ") != apiclient.AskScope+" "+apiclient.SyncScope || a.ClientID == offer.PeerClientID || a.Audience != apiclient.Audience {
		t.Errorf("the client registered for the ask at release: %+v", a)
	}
	g := oauth.registered[0]
	if g.ClientID != offer.PeerClientID || strings.Join(g.Scopes, " ") != apiclient.LinkScope || g.Audience != apiclient.Audience || g.Alg != "ES256" || g.TokenLifespan > time.Hour {
		t.Errorf("the client registered for Fyniq: %+v", g)
	}
	if err := svc.Activate(ctx, hr, fyniqPub, "fyniq-client-1", json.RawMessage(jwk), json.RawMessage(jwk)); !errors.As(err, &r) || r.Code != nemura.CodeLinkState {
		t.Errorf("a link completed twice: %v", err)
	}
	var l nemura.LinkRow
	lt.c.must(func(ctx context.Context, tx pgx.Tx) (err error) { l, err = nemura.LiveLink(ctx, tx); return err })
	if l.State != "active" || l.Epoch != 1 || !bytes.Equal(l.FyniqKey, fyniqPub) || l.FyniqClientID != "fyniq-client-1" || l.PeerClientID != offer.PeerClientID || l.ProposedBy != lt.c.owner {
		t.Errorf("the link: %+v", l)
	}
	route, err := store(t, "nemura_api").LinkClientRoute(ctx, offer.PeerClientID)
	if err != nil || route.Organisation != lt.c.org || route.Link != l.ID || route.State != "active" || route.Epoch != 1 || route.FyniqOrganisation != fyniqOrg {
		t.Errorf("the route: %+v %v", route, err)
	}
	if _, err := store(t, "nemura_api").LinkClientRoute(ctx, "someone-else"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("the route of a client that is no link's: %v", err)
	}

	// The private halves: the worker reads them, the API cannot, and the key never reaches the audit trail.
	if err := store(t, "nemura_worker").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		s, err := nemura.SecretsOf(ctx, tx, l.ID)
		if err != nil || len(s.Envelope) != ed25519.PrivateKeySize || s.Client == nil {
			t.Errorf("the worker's secrets: %v", err)
		}
		if err == nil && !bytes.Equal(s.Envelope.Public().(ed25519.PublicKey), offer.LinkKey) {
			t.Error("the envelope key is not the one offered to Fyniq")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "nemura_api").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.SecretsOf(ctx, tx, l.ID)
		return err
	}); err == nil {
		t.Error("the API read the link's private keys")
	}
	var leaks int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action LIKE 'nemura.fyniq_link.%' AND (body LIKE '%client_private%' OR body LIKE '%link_private%' OR body LIKE '%"x"%')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("a key in the audit trail: %d %v", leaks, err)
	}
	// The link's keys and clients cannot be edited once the link is active.
	for _, stmt := range []string{"UPDATE nemura.fyniq_link SET fyniq_key = link_public", "UPDATE nemura.fyniq_link SET peer_client_id = 'x'", "UPDATE nemura.fyniq_link SET proposed_by = NULL", "DELETE FROM nemura.fyniq_link"} {
		if err := store(t, "nemura_migrator").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err }); err == nil {
			t.Errorf("%s ran", stmt)
		}
	}
	// No ordinary registration can be given the link scope, with or without a second approver.
	clients := nemura.APIClientService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(now)}
	if _, err := clients.RequestRegistration(ctx, owner, "Sneaky", []string{apiclient.LinkScope}, "ES256", json.RawMessage(jwk)); err == nil {
		t.Error("an ordinary client request asked for the link scope")
	}
}

// Only Fyniq's link client reaches the inbox, and only for the live link at its epoch (NM-INT-10, AI-36, golden rule 6): a session cookie, a token for another audience (Fyniq's own API: no passthrough),
// a token of an ordinary client, a token that is not the client's own, a token whose link has ended and one with no scope are each refused before anything is read; the envelope is then checked against
// the link, and a forgery, a replay, an old epoch, another organisation's event and a field the contract does not list are refused with their own code and change nothing.
// part of NM-INT-10 (the inbox, the token and the checks against the link; gap repair needs Nemura's read API)
// asvs: V8.2.1, V10.3.1, V13.2.1, V4.2.1
func TestOnlyFyniqsLinkClientReachesTheInboxAndItsEventsAreCheckedAgainstTheLink(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	oauth := &fakeOAuth{}
	offer, fyniqPriv, fyniqOrg := linked(t, lt, now, oauth)
	st := store(t, "nemura_api")
	clk := clock.NewFrozen(now)
	srv := &tokenServer{tokens: map[string]ports.Introspection{}}
	grant := func(token, client, audience string, scopes ...string) {
		srv.tokens[token] = ports.Introspection{Active: true, Subject: client, ClientID: client, Audiences: []string{audience}, Scopes: scopes, ExpiresAt: now.Add(time.Hour), IssuedAt: now}
	}
	grant("link-token", offer.PeerClientID, apiclient.Audience, apiclient.LinkScope)
	grant("fyniq-audience", offer.PeerClientID, "fyniq-api", apiclient.LinkScope)
	grant("ordinary", "ordinary-client", apiclient.Audience, "directory.read")
	grant("no-scope", offer.PeerClientID, apiclient.Audience, "directory.read")
	grant("ask-token", offer.PeerAskClientID, apiclient.Audience, apiclient.AskScope)
	grant("ask-client-sends", offer.PeerAskClientID, apiclient.Audience, apiclient.LinkScope)
	grant("events-client-asks", offer.PeerClientID, apiclient.Audience, apiclient.AskScope)
	grant("not-the-links-client", "some-other-client", apiclient.Audience, apiclient.LinkScope)
	srv.tokens["delegated"] = ports.Introspection{Active: true, Subject: "a-person", ClientID: offer.PeerClientID, Audiences: []string{apiclient.Audience}, Scopes: []string{apiclient.LinkScope}, ExpiresAt: now.Add(time.Hour)}
	srv.tokens["expired"] = ports.Introspection{Active: true, Subject: offer.PeerClientID, ClientID: offer.PeerClientID, Audiences: []string{apiclient.Audience}, Scopes: []string{apiclient.LinkScope}, ExpiresAt: now.Add(-time.Minute)}

	mux := http.NewServeMux()
	api.New(mux, api.Deps{LinkAuth: &linkauth.Introspector{OAuth: srv, Routes: st, Audience: apiclient.Audience, Clock: clk, TTL: 0}, Link: nemura.LinkService{Store: st, OAuth: oauth, Clock: clk}, LinkHolds: nemura.LinkService{Store: st, OAuth: oauth, Clock: clk}, Clock: clk, Sessions: noSessions{},
		CookieNames: []string{"nemura_session"}})
	post := func(token, cookie string, body []byte) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/v1/link/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != "" {
			req.Header.Set("Cookie", "nemura_session="+cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	signed := func(key ed25519.PrivateKey, epoch int, typ string, version int, payload any, edit func(*link.Header)) []byte {
		raw, _ := json.Marshal(payload)
		at := now.UTC().Format(time.RFC3339)
		h := link.Header{EventID: uuid.NewString(), Type: typ, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(), TargetOrganisation: lt.c.org.String(), Epoch: epoch,
			Sequence: int64(version), ObjectType: "payroll_run", ObjectID: "run-1", ObjectVersion: version, EffectiveAt: at, RecordedAt: at, SignedAt: at, CorrelationID: uuid.NewString()}
		if edit != nil {
			edit(&h)
		}
		e, err := link.Sign(key, h, raw)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(e)
		return b
	}
	run := uuid.New()
	posted := func(version int, ref string) map[string]any {
		return map[string]any{"run_id": run.String(), "version": version, "status": "posted", "posting_ref": ref, "code": "", "message": ""}
	}
	code := func(out map[string]any) string { c, _ := out["code"].(string); return c }
	fieldCode := func(out map[string]any) string {
		errs, _ := out["errors"].([]any)
		if len(errs) == 0 {
			return ""
		}
		c, _ := errs[0].(map[string]any)["code"].(string)
		return c
	}
	good := signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 1, posted(1, "JE-0001"), nil)

	// Who may call.
	for name, c := range map[string]struct {
		token, cookie string
		status        int
		code          string
	}{
		"no credential":                       {"", "", 401, api.CodeUnauthenticated},
		"a session cookie and no token":       {"", "any-session-token", 401, api.CodeUnauthenticated},
		"a token nobody issued":               {"nonsense", "", 401, api.CodeUnauthenticated},
		"an expired token":                    {"expired", "", 401, api.CodeUnauthenticated},
		"a token for Fyniq's own audience":    {"fyniq-audience", "", 401, api.CodeUnauthenticated},
		"a token not issued to the client":    {"delegated", "", 401, api.CodeUnauthenticated},
		"an ordinary client's token":          {"ordinary", "", 403, api.CodeLinkClient},
		"the link's client without the scope": {"no-scope", "", 403, api.CodeLinkClient},
		"a client that is no link's":          {"not-the-links-client", "", 403, api.CodeLinkClient},
	} {
		if status, out := post(c.token, c.cookie, good); status != c.status || code(out) != c.code {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	var n int
	count := func() int {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM nemura.link_inbox) + (SELECT count(*) FROM nemura.link_run_posting)`).Scan(&n)
		})
		return n
	}
	if count() != 0 {
		t.Fatalf("something was received without the link's client: %d", count())
	}
	// The link's token reaches no other operation: those read a session cookie only.
	for _, path := range []string{"/v1/session", "/v1/api-clients", "/v1/payroll/runs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer link-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 401 && rec.Code != 404 {
			t.Errorf("the link's token on %s: %d", path, rec.Code)
		}
	}

	// What the link's client sends.
	if status, out := post("link-token", "", good); status != 200 || out["outcome"] != "applied" {
		t.Fatalf("a posting answer: %d %v", status, out)
	}
	if status, out := post("link-token", "", good); status != 200 || out["outcome"] != "duplicate" {
		t.Errorf("the same event again: %d %v", status, out)
	}
	if status, out := post("link-token", "", signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 2, posted(1, "JE-0001"), nil)); status != 200 || out["outcome"] != "ignored_older" {
		t.Errorf("the same answer in another event: %d %v", status, out)
	}
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	altered := signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 3, posted(2, "JE-0002"), nil)
	altered = bytes.Replace(altered, []byte("JE-0002"), []byte("JE-9999"), 1)
	extra := posted(2, "JE-0002")
	extra["nric"] = "S1234567D"
	for name, c := range map[string]struct {
		body []byte
		want string
	}{
		"another key":        {signed(otherPriv, 1, nemura.TypePayrollRunPosted, 3, posted(2, "JE-0002"), nil), "PPL-02"},
		"an altered payload": {altered, "PPL-03"},
		"an old epoch":       {signed(fyniqPriv, 2, nemura.TypePayrollRunPosted, 3, posted(2, "JE-0002"), nil), "PPL-05"},
		"a stale time":       {signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 3, posted(2, "JE-0002"), func(h *link.Header) { h.SignedAt = now.Add(-time.Hour).UTC().Format(time.RFC3339) }), "PPL-04"},
		"another target":     {signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 3, posted(2, "JE-0002"), func(h *link.Header) { h.TargetOrganisation = uuid.NewString() }), "PPL-06"},
		"another sender":     {signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 3, posted(2, "JE-0002"), func(h *link.Header) { h.SourceOrganisation = uuid.NewString() }), "PPL-06"},
		"an unlisted field":  {signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, 3, extra, nil), nemura.CodeLinkMinimum},
		"another type":       {signed(fyniqPriv, 1, "people.person.upserted", 3, posted(2, "JE-0002"), nil), nemura.CodeLinkType},
	} {
		if status, out := post("link-token", "", c.body); status != 422 || code(out) != api.CodeLinkRefused || fieldCode(out) != c.want {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	// kept: the applied answer's inbox row and its posting, the ignored one's inbox row, and the two refusals that came after the signature held; every forgery left nothing
	if got := count(); got != 5 {
		t.Errorf("what was kept: %d", got)
	}

	// The ask at release (FR-PPL-13): the read-only client reads a hold's status and nothing else; the client that sends events cannot ask, the asking client cannot send, and no one else can.
	ben := uuid.New()
	askPath := "/v1/link/people/" + ben.String() + "/hold"
	get := func(token, path string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if status, out := get("ask-token", askPath); status != 200 || out["status"] != "none" || out["version"] != float64(0) {
		t.Errorf("a person with no hold: %d %v", status, out)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PutTaxHold(ctx, tx, lt.c.org, now, lt.c.hr, lt.ben, d("2099-06-30"), "resigned")
		return err
	})
	if status, out := get("ask-token", "/v1/link/people/"+lt.ben.String()+"/hold"); status != 200 || out["status"] != "held" || out["version"] != float64(1) || out["hold_id"] == nil || strings.Contains(fmt.Sprint(out), "resigned") {
		t.Errorf("a held person: %d %v", status, out)
	}
	for name, c := range map[string]struct {
		token string
		want  int
	}{
		"nothing":                        {"", 401},
		"a token nobody issued":          {"nonsense", 401},
		"the events client asking":       {"events-client-asks", 403},
		"an ordinary client":             {"ordinary", 403},
		"the events token without scope": {"link-token", 403},
		"Fyniq's own audience":           {"fyniq-audience", 401},
	} {
		if status, out := get(c.token, askPath); status != c.want {
			t.Errorf("asking with %s: %d %v", name, status, out)
		}
	}
	if status, _ := post("ask-client-sends", "", good); status != 403 {
		t.Errorf("the asking client sending an event: %d", status)
	}
	if status, _ := post("ask-token", "", good); status != 403 {
		t.Errorf("the asking client's token on the events inbox: %d", status)
	}
	if count() != 5 {
		t.Errorf("the ask or a refused call changed what Nemura kept: %d", count())
	}

	// A token of the link whose epoch has ended: unlinking, then a new link, is a new client; the old one's token is refused.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.EndLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()})
	})
	if status, out := post("link-token", "", good); status != 403 || code(out) != api.CodeLinkClient {
		t.Errorf("the token of a link that ended: %d %v", status, out)
	}
	if _, err := st.LinkClientRoute(ctx, offer.PeerClientID); err != nil {
		t.Errorf("the route of an ended link: %v", err)
	}
}

// A hold travels to Fyniq through the outbox, signed (NM-INT-01, NM-INT-08, NM-INT-10): the change that puts it on queues the event in its own transaction, with identities only; the worker builds the
// payload from the hold's row, gets the link's token with the client key (private_key_jwt, the scope the event's type needs) and posts the envelope signed with the link's key; a retry after a failure is
// the same event (same ID, same sequence); a standalone organisation queues nothing; and nothing of the reason, a person's name or a token reaches the outbox row, the audit trail or an error.
// part of NM-INT-10 (the sender and the outbox row; the person, bank-detail and run events and gap repair are not built)
// asvs: V8.2.1, V9.1.2, V11.2.1, V14.1.1
func TestAHoldIsQueuedWithItsChangeAndSentSignedWithTheLinksToken(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)

	// Standalone: a hold queues nothing.
	standalone := newLeaveTeam(t)
	standalone.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PutTaxHold(ctx, tx, standalone.c.org, now, standalone.c.hr, standalone.ben, d("2099-06-30"), "resigned")
		return err
	})
	var queued int
	standalone.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.outbox WHERE kind = $1`, nemura.KindLinkSend).Scan(&queued)
	})
	if queued != 0 {
		t.Errorf("a standalone organisation queued %d events for Fyniq", queued)
	}

	offer, _, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	snapshot := len(queuedRows(t, lt)) // the people who already existed went ahead with the link: the hold is the next event after them
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PutTaxHold(ctx, tx, lt.c.org, now, lt.c.hr, lt.ben, d("2099-06-30"), "resigned, owes tax clearance")
		return err
	})
	var rows []outbox.Row
	readRows := func() {
		rows = nil
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			rs, err := tx.Query(ctx, `SELECT id, payload FROM nemura.outbox WHERE kind = $1 ORDER BY created_at`, nemura.KindLinkSend)
			if err != nil {
				return err
			}
			defer rs.Close()
			for rs.Next() {
				var r outbox.Row
				if err := rs.Scan(&r.ID, &r.Payload); err != nil {
					return err
				}
				rows = append(rows, r)
			}
			return rs.Err()
		})
	}
	readRows()
	rows = rows[snapshot:]
	if len(rows) != 1 {
		t.Fatalf("queued events: %d", len(rows))
	}
	for _, leak := range []string{"resigned", "tax clearance", "2099-06-30", "Ben"} {
		if strings.Contains(string(rows[0].Payload), leak) {
			t.Errorf("the outbox row carries %q: %s", leak, rows[0].Payload)
		}
	}

	// Fyniq, in miniature: its token endpoint checks the client assertion against the public key Nemura offered; its API takes the envelope.
	type call struct {
		path, bearer string
		env          link.Envelope
	}
	var calls []call
	var tokens []string
	failNext := true
	var clientKey jose.JSONWebKey
	if err := json.Unmarshal(offer.ClientKey, &clientKey); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		jws, err := jose.ParseSigned(r.PostForm.Get("client_assertion"), []jose.SignatureAlgorithm{jose.ES256})
		if err != nil {
			http.Error(w, "bad assertion", 401)
			return
		}
		raw, err := jws.Verify(clientKey.Key)
		var claims struct {
			Iss, Sub string
			Aud      any
		}
		if err != nil || json.Unmarshal(raw, &claims) != nil || claims.Iss != "fyniq-client-1" || claims.Sub != "fyniq-client-1" || r.PostForm.Get("scope") != "people.holds.write" ||
			r.PostForm.Get("audience") != "fyniq-api" || r.PostForm.Get("grant_type") != "client_credentials" || fmt.Sprint(claims.Aud) != srv.URL+"/oauth2/token" {
			http.Error(w, "not this client", 401)
			return
		}
		tok := "token-" + uuid.NewString()
		tokens = append(tokens, tok)
		_, _ = w.Write([]byte(`{"access_token":"` + tok + `","expires_in":3600,"token_type":"bearer"}`))
	})
	mux.HandleFunc("POST /v1/people-link/hold-events", func(w http.ResponseWriter, r *http.Request) {
		var env link.Envelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		calls = append(calls, call{r.URL.Path, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), env})
		if failNext {
			failNext = false
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"code":"UNAVAILABLE","detail":"resigned, owes tax clearance"}`))
			return
		}
		_, _ = w.Write([]byte(`{"event_id":"x","outcome":"applied"}`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	sender := &nemura.LinkSender{Store: store(t, "nemura_worker"), Clock: clock.NewFrozen(now), HTTP: srv.Client(), APIURL: srv.URL, TokenURL: srv.URL + "/oauth2/token", Audience: "fyniq-api"}
	work := sender.Work()

	err := work(ctx, lt.c.org, rows[0])
	if err == nil || strings.Contains(err.Error(), "resigned") || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "UNAVAILABLE") {
		t.Fatalf("a failing Fyniq: %v", err)
	}
	if err := work(ctx, lt.c.org, rows[0]); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if len(calls) != 2 || len(tokens) != 1 {
		t.Fatalf("calls %d, tokens %d: the token is got once and kept", len(calls), len(tokens))
	}
	first, second := calls[0], calls[1]
	if first.env.Header.EventID != second.env.Header.EventID || first.env.Header.Sequence != second.env.Header.Sequence || first.env.Header.Sequence != int64(snapshot)+1 {
		t.Errorf("a retry is the same event: %+v %+v", first.env.Header, second.env.Header)
	}
	if second.bearer != tokens[0] || second.path != "/v1/people-link/hold-events" {
		t.Errorf("the call: %+v", second)
	}
	if err := second.env.Verify(link.Link{Key: offer.LinkKey, Epoch: 1, SourceOrganisation: lt.c.org.String(), TargetOrganisation: fyniqOrg.String(), SourceSystem: "nemura"}, now); err != nil {
		t.Errorf("the envelope verifies with the key Nemura offered: %v", err)
	}
	if second.env.Header.Type != "people.tax_hold.changed" || second.env.Header.ObjectType != "tax_hold" || second.env.Header.ObjectID != lt.ben.String() || second.env.Header.ObjectVersion != 1 {
		t.Errorf("the header: %+v", second.env.Header)
	}
	var p map[string]any
	_ = json.Unmarshal(second.env.Payload, &p)
	if p["status"] != "held" || p["person_id"] != lt.ben.String() || len(p) != 5 || strings.Contains(string(second.env.Payload), "resigned") {
		t.Errorf("the payload is the hold's row and nothing else: %s", second.env.Payload)
	}

	// The audit trail names the event's type and number and holds no payload; a second hold is the next sequence.
	var leaks int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%resigned%' OR body LIKE '%token-%')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("a reason or a token in the audit trail: %d %v", leaks, err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReleaseTaxHold(ctx, tx, lt.c.org, now, lt.c.hr, lt.ben, mustDocument(t, lt, now), d("2099-07-01"), "cleared")
	})
	readRows()
	rows = rows[snapshot:]
	if len(rows) != 2 {
		t.Fatalf("queued events after the release: %d", len(rows))
	}
	if err := work(ctx, lt.c.org, rows[1]); err != nil || len(calls) != 3 || calls[2].env.Header.Sequence != int64(snapshot)+2 || calls[2].env.Header.ObjectVersion != 2 {
		t.Errorf("the release: %v %d", err, len(calls))
	}

	// The API role cannot read the keys the sender signs with.
	if err := (&nemura.LinkSender{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), HTTP: srv.Client(), APIURL: srv.URL, TokenURL: srv.URL + "/oauth2/token", Audience: "fyniq-api"}).Work()(ctx, lt.c.org, rows[0]); err == nil {
		t.Error("the API role sent an event")
	}
	// With the link ended, an unsent event has no one to tell, and the sender holds no endpoint for a type Nemura does not send.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.EndLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()})
	})
	before := len(calls)
	if err := work(ctx, lt.c.org, rows[1]); err != nil || len(calls) != before {
		t.Errorf("an ended link: %v, %d calls", err, len(calls)-before)
	}
	if err := work(ctx, lt.c.org, outbox.Row{ID: uuid.New(), Payload: []byte(`{"event_id":"e","type":"leave.taken.changed"}`)}); err == nil {
		t.Error("a type Nemura does not send was accepted")
	}
}

// mustDocument files a clearance letter of Ben's, the evidence a hold's release needs.
func mustDocument(t *testing.T, lt *leaveTeam, now time.Time) uuid.UUID {
	t.Helper()
	blobs, _ := local.NewDir(t.TempDir())
	rs := nemura.RestrictedService{Store: store(t, "nemura_api"), Blobs: blobs, Clock: clock.NewFrozen(now)}
	doc, err := rs.AddDocument(context.Background(), api.Session{Organisation: lt.c.org, Member: lt.c.hr}, lt.ben, "other", "IRAS clearance letter", "employment_record", "application/pdf", []byte("%PDF clearance"), []string{"payroll_admin"})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// The link's page reads the same steps the ceremony takes: nothing before a proposal, the proposal with Nemura's envelope key and the client's ID (and the client's public key once, in the answer to the
// proposal), then the active link with Fyniq's client; and none of them shows a private key.
// part of NM-ARC-04 (the page's door over the ceremony)
// asvs: V8.2.1, V6.4.1
func TestTheLinkPageShowsTheProposalAndTheActiveLinkWithoutAnyPrivateKey(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	oauth := &fakeOAuth{}
	svc := nemura.LinkAdminService{LinkService: nemura.LinkService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(now)}}
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	if v, err := svc.Current(ctx, owner); err != nil || v.State != "" {
		t.Fatalf("before any proposal: %+v %v", v, err)
	}
	// A client registered before the link is listed with it, for the two approvers to review (NM-ARC-04).
	before := nemura.APIClientService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(now)}
	if _, err := before.Register(ctx, owner, "Payroll bureau", []string{"directory.read"}, "ES256", json.RawMessage(jwk)); err != nil {
		t.Fatal(err)
	}
	fyniqOrg := uuid.New()
	v, err := svc.Propose(ctx, owner, fyniqOrg)
	if len(v.Clients) != 1 || v.Clients[0].Name != "Payroll bureau" || strings.Contains(fmt.Sprint(v.Clients), "kty") {
		t.Errorf("the clients listed with the proposal: %+v", v.Clients)
	}
	if err != nil || v.State != "proposed" || v.FyniqOrganisation != fyniqOrg || len(v.LinkKey) != 32 || !strings.Contains(string(v.ClientKey), "EC") || strings.Contains(string(v.ClientKey), `"d"`) || v.PeerClientID == "" {
		t.Fatalf("the proposal: %+v %v", v, err)
	}
	if cur, err := svc.Current(ctx, owner); err != nil || cur.State != "proposed" || len(cur.ClientKey) != 0 || cur.PeerClientID != v.PeerClientID {
		t.Errorf("the proposal read again: %+v %v", cur, err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, lt.c.org, now, lt.c.owner, lt.c.hr, authz.Administrator, today, nil)
		return err
	})
	fyniqPub, _, _ := ed25519.GenerateKey(rand.Reader)
	got, err := svc.Approve(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &now}, fyniqPub, "fyniq-client-9", json.RawMessage(jwk), json.RawMessage(jwk))
	if err != nil || got.State != "active" || got.FyniqClientID != "fyniq-client-9" || got.Epoch != 1 {
		t.Errorf("the approval: %+v %v", got, err)
	}
}
