//go:build local

package api

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// publicJWK is key's public half as a request body takes it.
func publicJWK(t *testing.T, key *ecdsa.PrivateKey) map[string]any {
	t.Helper()
	b, err := (jose.JSONWebKey{Key: &key.PublicKey}).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// clientToken asks Hydra for an API token as clientID with the client-credentials grant, authenticated by a
// private_key_jwt assertion signed by key (RFC 7523): the status and the token response.
// thumbprint stands for an RFC 7638 key thumbprint: 43 base64url characters.
var thumbprint = strings.Repeat("t", 43)

func clientToken(t *testing.T, s *oauthStack, clientID string, key *ecdsa.PrivateKey, scope string) (int, map[string]any) {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	now := clock.System{}.Now()
	b, _ := json.Marshal(map[string]any{"iss": clientID, "sub": clientID, "aud": "http://hydra.test/oauth2/token", "jti": uuid.NewString(),
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix()})
	jws, err := signer.Sign(b)
	if err != nil {
		t.Fatal(err)
	}
	assertion, _ := jws.CompactSerialize()
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {scope}, "audience": {apiAudience},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}, "client_assertion": {assertion}}
	resp, err := s.browser().PostForm("http://hydra.test/oauth2/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// tokenAPI is the API as an integration reaches it: Hydra's tokens introspected at every call (no cache), and the API
// clients Fyniq registered read from the database.
func tokenAPI(t *testing.T, s *oauthStack) *env {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	in := &authn.Introspector{OAuth: s.admin, Audience: apiAudience, Clock: s.e.clock, Clients: s.e.store}
	New(mux, Deps{Store: s.e.store, Clock: s.e.clock, Auth: authn.Local{Dev: s.e.tokens, OAuth: in}, Log: slog.New(slog.DiscardHandler),
		Version: "0.0.0-test", OAuthClients: s.admin, APIAudience: apiAudience})
	te := *s.e
	te.srv = srv
	return &te
}

// An integration client (FR-INT-02, SEC-07, AI-34) end to end, against the pinned Hydra: an Administrator asks to
// register one with narrow scopes, its networks, its own write limit and the public half of its key; every invalid field
// is refused at once; the Administrator cannot approve their own request, and nothing is registered until the Owner
// does (dual control). Then the client gets its own tokens with the client-credentials grant and private_key_jwt, never a
// shared secret, and a key that is not its own gets none. Its tokens act for its member, narrowed to its scopes: it
// reads and writes its areas, its writes are audited as the integration with its client ID, and it reaches nothing
// outside them, approvals and Connected apps included, whatever its member may do. It calls only from its networks and
// within its own limit. A client Hydra knows but Fyniq never registered gets nothing. Revoked, its tokens are refused at
// once and the worker deletes it from Hydra.
// req: FR-INT-02
// req: SEC-07
// req: AI-34
// req: FR-INT-16
// req: AUD-01
// asvs: V10.3.1, V10.4.10, V10.4.11, V8.2.1, V8.3.1, V16.3.2
func TestIntegrationClients(t *testing.T) {
	s := startOAuth(t, 0)
	e := s.e
	e.api.deps.OAuthClients, e.api.deps.APIAudience = s.admin, apiAudience
	api := tokenAPI(t, s)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	// Ah Kow keeps the books too: the client acts for him, so it may prepare and post what an Accountant may.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Members().Membership(ctx, uuid.MustParse(f.ahkow))
		if err != nil {
			return err
		}
		_, err = u.Members().Assign(ctx, m.ID, "accountant", nil)
		return err
	})
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	expires := e.clock.Now().AddDate(0, 6, 0).UTC().Format(time.RFC3339)
	body := func(name string, networks ...string) map[string]any {
		return map[string]any{"name": name, "owner_id": f.ahkow, "scopes": []string{"fyniq.write.accounting", "fyniq.read.banking"},
			"networks": networks, "writes_per_minute": 3, "expires_at": expires, "public_key": publicJWK(t, key)}
	}

	// Every invalid field at once, each naming the field and the fix.
	bad := e.call(http.MethodPost, "/v1/integration-clients", admin, map[string]any{"name": " x", "scopes": []string{"fyniq.write.payroll"},
		"networks": []string{"10.0.0.1/99"}, "expires_at": e.clock.Now().AddDate(2, 0, 0).Format(time.RFC3339),
		"public_key": map[string]any{"kty": "oct", "k": "c2VjcmV0"}})
	e.problemIs(bad, "createIntegrationClient", 422, apiclient.CodeName)
	if errs, _ := bad.Body["errors"].([]any); len(errs) != 5 {
		t.Errorf("want 5 field problems at once (name, scope, network, expiry, key), got %s", bad.Raw)
	}
	stranger := body("Stranger's")
	stranger["owner_id"] = uuid.NewString()
	e.problemIs(e.call(http.MethodPost, "/v1/integration-clients", admin, stranger), "createIntegrationClient", 422, apiclient.CodeOwner)

	register := func(b map[string]any) storage.APIClient {
		t.Helper()
		asked := e.call(http.MethodPost, "/v1/integration-clients", admin, b)
		if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectAPIClient || asked.str("operation") != approval.OpAPIClientCreate {
			t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
		}
		if l := e.call(http.MethodGet, "/v1/integration-clients", admin, nil); strings.Contains(l.Raw, b["name"].(string)) {
			t.Fatalf("registered before its approval: %s", l.Raw)
		}
		e.problemIs(f.decide(admin, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
		if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
			t.Fatalf("approving: %d %s", d.Status, d.Raw)
		}
		var c storage.APIClient
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err := u.APIClients().List(ctx, storage.APIClientIntegration)
			for _, x := range all {
				if x.Name == b["name"] {
					c = x
				}
			}
			return err
		})
		if c.ClientID == "" || c.Class != authn.ClassIntegration || c.OwnerID.String() != f.ahkow || c.ApprovalID == nil {
			t.Fatalf("the registered client: %+v", c)
		}
		return c
	}
	c := register(body("Nightly canary", "127.0.0.0/8", "::1/128"))
	got := e.call(http.MethodGet, "/v1/integration-clients/"+c.ID.String(), admin, nil)
	if got.Status != http.StatusOK || got.str("client_id") != c.ClientID || got.str("status") != "live" || got.str("key_thumbprint") == "" ||
		strings.Contains(got.Raw, `"x":`) {
		t.Fatalf("getIntegrationClient: %d %s", got.Status, got.Raw)
	}

	// Its own tokens, by its own key: another key gets none.
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if status, out := clientToken(t, s, c.ClientID, other, "fyniq.write.accounting"); status == http.StatusOK {
		t.Fatalf("a token for an assertion signed by another key: %v", out)
	}
	status, out := clientToken(t, s, c.ClientID, key, "fyniq.write.accounting fyniq.read.banking")
	tok, _ := out["access_token"].(string)
	if status != http.StatusOK || tok == "" {
		t.Fatalf("the client's token: %d %v", status, out)
	}
	if status, out := clientToken(t, s, c.ClientID, key, "fyniq.write.sales"); status == http.StatusOK {
		t.Errorf("a scope it was not registered with: %v", out)
	}

	// Within its scopes it reads and writes, audited as the integration with its client ID.
	if r := api.call(http.MethodGet, "/v1/accounts", tok, nil); r.Status != http.StatusOK {
		t.Fatalf("reading the chart with fyniq.write.accounting: %d %s", r.Status, r.Raw)
	}
	draft := api.call(http.MethodPost, "/v1/journals", tok, e.draft("12.34"))
	if draft.Status != http.StatusCreated {
		t.Fatalf("a draft journal with fyniq.write.accounting: %d %s", draft.Status, draft.Raw)
	}
	if r := api.call(http.MethodGet, "/v1/money-accounts", tok, nil); r.Status != http.StatusOK {
		t.Errorf("reading banking with fyniq.read.banking: %d %s", r.Status, r.Raw)
	}
	var audited bool
	reader := workerStore(t)
	if err := reader.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 100000)
		for _, ev := range events {
			if strings.Contains(ev.Body, `"integration"`) && strings.Contains(ev.Body, c.ClientID) && strings.Contains(ev.Body, draft.str("id")) {
				audited = true
			}
		}
		return err
	}); err != nil || !audited {
		t.Errorf("the client's draft is not audited as the integration with its client ID (AUD-01): %v", err)
	}
	// Outside them it reaches nothing, whatever its member may do: an Owner's Connected apps and members, an approval.
	for _, x := range []struct{ method, path string }{{http.MethodGet, "/v1/integration-clients"}, {http.MethodGet, "/v1/members"},
		{http.MethodGet, "/v1/invoices"}, {http.MethodGet, "/v1/approvals"}, {http.MethodPost, "/v1/contacts"}} {
		r := api.call(x.method, x.path, tok, map[string]any{"legal_name": "Not in scope", "customer": true})
		if r.Status != http.StatusForbidden || r.problemCode() != CodeForbidden {
			t.Errorf("%s %s outside its scopes: %d %s", x.method, x.path, r.Status, r.Raw)
		}
	}
	// Its own write limit (FR-INT-16): three a minute, then 429 with Retry-After.
	var limited response
	for range 4 {
		if limited = api.call(http.MethodPost, "/v1/journals", tok, e.draft("1.00")); limited.Status == http.StatusTooManyRequests {
			break
		}
	}
	if limited.Status != http.StatusTooManyRequests || limited.Header.Get("Retry-After") == "" || !strings.Contains(limited.Header.Get("RateLimit-Policy"), "q=3") {
		t.Errorf("the client's own write limit: %d %v", limited.Status, limited.Header)
	}

	// Only from its networks (SEC-07).
	far := register(body("Far away", "203.0.113.0/24"))
	_, out = clientToken(t, s, far.ClientID, key, "fyniq.read.banking")
	farTok, _ := out["access_token"].(string)
	if r := api.call(http.MethodGet, "/v1/money-accounts", farTok, nil); r.Status != http.StatusForbidden || r.problemCode() != CodeForbidden {
		t.Errorf("a call from outside its networks: %d %s", r.Status, r.Raw)
	}

	// A client Hydra knows but Fyniq never registered, whatever its metadata says, gets nothing.
	signing := publicJWK(t, key) // as the API registers a key: for signing, with its algorithm
	signing["use"], signing["alg"] = "sig", "ES256"
	orphan, err := s.admin.RegisterIntegration(context.Background(), ports.IntegrationRegistration{Name: "Orphan", Class: authn.ClassIntegration,
		Audience: apiAudience, Scopes: []string{"fyniq.write.accounting"}, Key: mustJSON(t, signing), Alg: "ES256", TokenLifespan: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	status, out = clientToken(t, s, orphan, key, "fyniq.write.accounting")
	orphanTok, _ := out["access_token"].(string)
	if orphanTok == "" {
		t.Fatalf("the orphan client's token from Hydra: %d %v", status, out)
	}
	if r := api.call(http.MethodGet, "/v1/accounts", orphanTok, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("an orphan client's token: %d %s", r.Status, r.Raw)
	}

	// Revoked: refused at once, and removed from Hydra by the worker.
	e.problemIs(e.call(http.MethodPost, "/v1/integration-clients/"+c.ID.String()+"/revoke", owner, map[string]any{"reason": "Retired"}, "If-Match", got.Header.Get("ETag")),
		"revokeIntegrationClient", 403, CodeForbidden) // the Owner holds A, not C: an Administrator revokes
	revoked := e.call(http.MethodPost, "/v1/integration-clients/"+c.ID.String()+"/revoke", admin, map[string]any{"reason": "Retired"},
		"If-Match", got.Header.Get("ETag"))
	if revoked.Status != http.StatusOK || revoked.str("status") != "revoked" || revoked.str("revoke_reason") != "Retired" {
		t.Fatalf("revoking: %d %s", revoked.Status, revoked.Raw)
	}
	if r := api.call(http.MethodGet, "/v1/accounts", tok, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("a revoked client's token: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/integration-clients/"+c.ID.String()+"/revoke", admin, map[string]any{"reason": "Again"},
		"If-Match", revoked.Header.Get("ETag")), "revokeIntegrationClient", 409, apiclient.CodeState)
	var removal storage.OutboxRow
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.Outbox().ByKey(ctx, revoke.RemoveClientKind, "oauth.client.remove:"+c.ClientID)
		if row != nil {
			removal = *row
		}
		return err
	})
	if removal.Kind != revoke.RemoveClientKind {
		t.Fatal("no removal queued for the worker")
	}
	if err := revoke.RemoveClientWork(s.admin)(context.Background(), e.books.Org, removal); err != nil {
		t.Fatal(err)
	}
	if status, _ := clientToken(t, s, c.ClientID, key, "fyniq.read.banking"); status == http.StatusOK {
		t.Error("Hydra still issues tokens to a removed client")
	}
	if err := revoke.RemoveClientWork(s.admin)(context.Background(), e.books.Org, removal); err != nil {
		t.Errorf("removing a client twice: %v", err)
	}
}

// fakeOAuthClients registers nothing anywhere: the matrix's previews never reach it.
type fakeOAuthClients struct{}

func (fakeOAuthClients) RegisterIntegration(context.Context, ports.IntegrationRegistration) (string, error) {
	return "fake-" + uuid.NewString(), nil
}
func (fakeOAuthClients) RegisterPublicClient(_ context.Context, c ports.PublicRegistration) (string, error) {
	return cmp.Or(c.ID, "fake-"+uuid.NewString()), nil
}
func (fakeOAuthClients) RemoveClient(context.Context, string) error { return nil }

// matrixIntegrationClient is a live integration client of the matrix's organisation, made once.
func matrixIntegrationClient(e *env) string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.APIClients().List(ctx, storage.APIClientIntegration)
		if err != nil || len(all) > 0 {
			if len(all) > 0 {
				id = all[0].ID.String()
			}
			return err
		}
		approval, now := uuid.Must(uuid.NewV7()), e.clock.Now()
		c, err := u.APIClients().Create(ctx, storage.APIClient{Kind: storage.APIClientIntegration, ClientID: "matrix-" + uuid.NewString(), Name: "Matrix",
			Class: authn.ClassIntegration, OwnerID: uuid.MustParse(matrixApprover(e)), Scopes: []string{"fyniq.read.reports"},
			KeyThumbprint: thumbprint, ExpiresAt: now.AddDate(0, 6, 0), ApprovalID: &approval,
			CreatedBy: "user:matrix", CreatedAt: now})
		id = c.ID.String()
		return err
	})
	return id
}

// matrixIntegrationClients are the authorisation matrix's rows of integration clients (FR-INT-02): reading under any
// right on "Connected apps"; asking to register one under its C, stepped up, as a preview; revoking one under its C, as a
// preview. The AI holds no right on the row, so none of it reaches the AI or MCP (§9.6).
var matrixIntegrationClients = map[string]func(e *env, tok string) (response, func() bool){
	"listIntegrationClients": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/integration-clients", tok, nil), nil
	},
	"getIntegrationClient": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/integration-clients/"+matrixIntegrationClient(e), tok, nil), nil
	},
	"createIntegrationClient": func(e *env, tok string) (response, func() bool) {
		was, aud := e.api.deps.OAuthClients, e.api.deps.APIAudience
		defer func() { e.api.deps.OAuthClients, e.api.deps.APIAudience = was, aud }()
		e.api.deps.OAuthClients, e.api.deps.APIAudience = fakeOAuthClients{}, apiAudience
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		return e.call(http.MethodPost, "/v1/integration-clients", steppedUp(e, tok), map[string]any{"name": "Matrix new", "owner_id": matrixApprover(e),
			"scopes": []string{"fyniq.read.reports"}, "expires_at": e.clock.Now().AddDate(0, 6, 0).Format(time.RFC3339),
			"public_key": publicJWK(e.t, key)}, "Prefer", "return=preview"), nil
	},
	"revokeIntegrationClient": func(e *env, tok string) (response, func() bool) {
		id := matrixIntegrationClient(e)
		return e.call(http.MethodPost, "/v1/integration-clients/"+id+"/revoke", tok, map[string]any{"reason": "Matrix"}, "Prefer", "return=preview",
			"If-Match", "*"), nil
	},
}
