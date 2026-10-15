//go:build local

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

const (
	apiAudience = "http://localhost:8080/api"
	mcpAudience = "http://localhost:8080/mcp"
	callback    = "http://client.test/cb"
)

// oauthStack is the pinned Hydra (in-memory store) with the API and its login and consent flow.
type oauthStack struct {
	t      *testing.T
	e      *env
	clock  *clock.Frozen // the API's introspection cache runs on it; Hydra runs on real time
	admin  *hydra.Admin
	public *url.URL // Hydra's public port as the host sees it; its issuer is http://hydra.test/
	srv    *httptest.Server
	user   string // the signed-in user's dev token
}

// startOAuth starts the stack; ttl is how long the API caches an introspection.
func startOAuth(t *testing.T, ttl time.Duration) *oauthStack {
	t.Helper()
	ctx := context.Background()
	e := newEnv(t)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image: hydra.Image,
		Cmd:   []string{"serve", "all", "--dev", "--sqa-opt-out"},
		Env: map[string]string{"DSN": "memory", "URLS_SELF_ISSUER": "http://hydra.test/", "URLS_LOGIN": srv.URL + "/auth/login",
			"URLS_CONSENT": srv.URL + "/auth/consent", "SECRETS_SYSTEM": "local-only-test-secret-0123456789",
			"OAUTH2_PKCE_ENFORCED": "true", "STRATEGIES_ACCESS_TOKEN": "opaque", "TTL_ACCESS_TOKEN": "1h"},
		ExposedPorts: []string{"4444/tcp", "4445/tcp"},
		WaitingFor:   wait.ForHTTP("/health/ready").WithPort("4445/tcp").WithStartupTimeout(time.Minute),
	}})
	if err != nil {
		t.Fatalf("start Hydra (Docker must run; the image is %s): %v", hydra.Image, err) // never skip: exit item 6
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	pub, _ := c.PortEndpoint(ctx, "4444/tcp", "http")
	adm, _ := c.PortEndpoint(ctx, "4445/tcp", "http")
	s := &oauthStack{t: t, e: e, admin: hydra.New(adm), srv: srv, clock: clock.NewFrozen(clock.System{}.Now())}
	s.public, _ = url.Parse(pub)
	dev := e.tokens // the first-party session, until the SignIn port: dev tokens
	auth := authn.Local{Dev: dev, OAuth: &authn.Introspector{OAuth: s.admin, Audience: apiAudience, Clock: s.clock, TTL: ttl}}
	New(mux, Deps{Store: e.store, Clock: s.clock, Auth: auth, Log: slog.New(slog.DiscardHandler), Version: "0.0.0-test"})
	oauthflow.Flow{OAuth: s.admin, SignedIn: dev, Clock: clock.System{}, APIAudience: apiAudience, MCPAudience: mcpAudience}.Register(mux)
	s.user = authn.DevPrefix + uuid.NewString()
	dev.mu.Lock()
	dev.m[s.user] = authn.Principal{Subject: "user-oauth-1", Client: "web", Class: authn.ClassUser, Organisation: e.books.Org,
		Roles: []authz.Role{authz.Accountant}}
	dev.mu.Unlock()
	return s
}

// browser follows redirects like a user agent: Hydra's issuer host goes to the container, the user's
// session goes to the API's login and consent pages, and the client's callback ends the walk.
func (s *oauthStack) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	srvHost := strings.TrimPrefix(s.srv.URL, "http://")
	return &http.Client{Jar: jar, Timeout: 30 * time.Second,
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r = r.Clone(r.Context())
			switch r.URL.Host {
			case "hydra.test":
				r.URL.Host, r.Host = s.public.Host, s.public.Host
			case srvHost:
				r.Header.Set("Authorization", "Bearer "+s.user)
			}
			return http.DefaultTransport.RoundTrip(r)
		}),
		CheckRedirect: func(r *http.Request, _ []*http.Request) error {
			if r.URL.Host == "client.test" {
				return http.ErrUseLastResponse
			}
			return nil
		}}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// authorize runs the authorisation code flow and returns the token response, or the error the client's
// callback received.
func (s *oauthStack) authorize(clientID, audience, scope string, pkce bool, decision string) (map[string]any, string) {
	s.t.Helper()
	b := s.browser()
	q := url.Values{"client_id": {clientID}, "response_type": {"code"}, "scope": {scope}, "redirect_uri": {callback},
		"state": {"state-" + uuid.NewString()}, "audience": {audience}}
	verifier := rand.Text() + rand.Text()
	if pkce {
		sum := sha256.Sum256([]byte(verifier))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("code_challenge_method", "S256")
	}
	resp, err := b.Get("http://hydra.test/oauth2/auth?" + q.Encode())
	if err != nil {
		s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, callback) {
		return nil, mustQuery(s.t, loc).Get("error") // refused before consent
	}
	challenge := resp.Request.URL.Query().Get("consent_challenge")
	if resp.StatusCode != http.StatusOK || challenge == "" {
		s.t.Fatalf("the flow did not reach the consent page: %d %s", resp.StatusCode, resp.Request.URL)
	}
	resp, err = b.PostForm(s.srv.URL+"/auth/consent", url.Values{"consent_challenge": {challenge}, "decision": {decision}})
	if err != nil {
		s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	back := mustQuery(s.t, resp.Header.Get("Location"))
	if back.Get("error") != "" {
		return nil, back.Get("error")
	}
	tok, err := b.PostForm("http://hydra.test/oauth2/token", url.Values{"grant_type": {"authorization_code"}, "code": {back.Get("code")},
		"redirect_uri": {callback}, "client_id": {clientID}, "code_verifier": {verifier}})
	if err != nil {
		s.t.Fatal(err)
	}
	defer tok.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(tok.Body)
	if err := json.Unmarshal(raw, &out); err != nil || tok.StatusCode != http.StatusOK {
		s.t.Fatalf("token exchange: %d %s", tok.StatusCode, raw)
	}
	return out, ""
}

func mustQuery(t *testing.T, loc string) url.Values {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("bad redirect %q", loc)
	}
	return u.Query()
}

// call sends a request to the API with an OAuth access token.
func (s *oauthStack) call(token string) (int, string) {
	req, _ := http.NewRequest(http.MethodGet, s.srv.URL+"/v1/organisation", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// P0 exit item 6, first part (ADR-T3, open question 40), against the pinned Hydra: a test client gets
// an API-audience token through login and consent with PKCE, and the API accepts it; an allowlisted
// client gets an MCP-audience token, which the API refuses, as it refuses expired and revoked tokens.
// req: AI-36
// req: AI-37
// req: SEC-02
// asvs: V10.3.1, V10.4.6, V10.7.1
func TestOAuthTokensAreAudienceBound(t *testing.T) {
	s := startOAuth(t, 0) // no cache: every call introspects
	ctx := context.Background()
	newClient := func(name, class string, audiences []string, lifespan time.Duration) string {
		c, err := s.admin.CreateClient(ctx, hydra.NewClient{Name: name, Class: class, Audiences: audiences, RedirectURIs: []string{callback},
			Scopes: []string{"openid", "offline_access", "fyniq.read.accounting", "fyniq.read.payroll"}, AccessTokenLifespan: lifespan})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	integration := newClient("Test integration", authn.ClassIntegration, []string{apiAudience}, 0)
	agent := newClient("Test MCP client", authn.ClassAgent, []string{mcpAudience}, 0)
	shortLived := newClient("Short-lived", authn.ClassIntegration, []string{apiAudience}, 2*time.Second)

	tok, refused := s.authorize(integration, apiAudience, "openid offline_access fyniq.read.accounting fyniq.read.payroll", true, "accept")
	if refused != "" {
		t.Fatalf("the integration's flow was refused: %s", refused)
	}
	if scope, _ := tok["scope"].(string); strings.Contains(scope, "payroll") || !strings.Contains(scope, "fyniq.read.accounting") {
		t.Errorf("granted scope %q: payroll is never granted in P0 (AI-37)", scope)
	}
	apiToken := tok["access_token"].(string)
	if status, body := s.call(apiToken); status != http.StatusOK || !strings.Contains(body, `"base_currency":"SGD"`) {
		t.Fatalf("an API-audience token: %d %s", status, body)
	}

	tok, refused = s.authorize(agent, mcpAudience, "openid fyniq.read.accounting", true, "accept")
	if refused != "" {
		t.Fatalf("the allowlisted MCP client's flow was refused: %s", refused)
	}
	if status, body := s.call(tok["access_token"].(string)); status != http.StatusUnauthorized || !strings.Contains(body, CodeUnauthenticated) {
		t.Fatalf("an MCP-audience token at the API: %d %s", status, body)
	}

	if _, refused := s.authorize(integration, mcpAudience, "openid", true, "accept"); refused == "" {
		t.Error("a client got an audience its record does not allow")
	}
	if _, refused := s.authorize(integration, apiAudience, "openid", false, "accept"); refused != "invalid_request" {
		t.Errorf("a flow without PKCE: %q, want invalid_request", refused)
	}
	if _, refused := s.authorize(integration, apiAudience, "openid", true, "deny"); refused != "access_denied" {
		t.Errorf("a refused consent: %q, want access_denied", refused)
	}

	tok, _ = s.authorize(shortLived, apiAudience, "openid fyniq.read.accounting", true, "accept")
	time.Sleep(3 * time.Second) // Hydra's own clock decides the expiry
	s.clock.Set(clock.System{}.Now())
	if status, _ := s.call(tok["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("an expired token: %d", status)
	}

	resp, err := s.browser().PostForm("http://hydra.test/oauth2/revoke", url.Values{"token": {apiToken}, "client_id": {integration}})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	if status, _ := s.call(apiToken); status != http.StatusUnauthorized {
		t.Errorf("a revoked token: %d", status)
	}
}

// refresh exchanges a refresh token; it returns the new tokens, or the OAuth error.
func (s *oauthStack) refresh(clientID, refreshToken string) (map[string]any, string) {
	s.t.Helper()
	resp, err := s.browser().PostForm("http://hydra.test/oauth2/token", url.Values{"grant_type": {"refresh_token"},
		"refresh_token": {refreshToken}, "client_id": {clientID}})
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		e, _ := out["error"].(string)
		return nil, e
	}
	return out, ""
}

// P0 exit item 6, second part: refresh tokens rotate and a reused one revokes its family; a change to
// what a user was granted revokes their tokens, and the API denies the next call within 60 seconds.
// req: AI-38
// asvs: V10.4.5, V10.4.9, V8.3.2
func TestTokenLifecycle(t *testing.T) {
	s := startOAuth(t, authn.MaxCache) // the production cache: 30 seconds
	ctx := context.Background()
	c, err := s.admin.CreateClient(ctx, hydra.NewClient{Name: "Lifecycle client", Class: authn.ClassIntegration, Audiences: []string{apiAudience},
		RedirectURIs: []string{callback}, Scopes: []string{"openid", "offline_access", "fyniq.read.accounting"}})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.authorize(c.ID, apiAudience, "openid offline_access fyniq.read.accounting", true, "accept")
	second, refused := s.refresh(c.ID, first["refresh_token"].(string))
	if refused != "" {
		t.Fatalf("refresh: %s", refused)
	}
	if second["refresh_token"] == first["refresh_token"] {
		t.Fatal("the refresh token did not rotate")
	}
	if status, _ := s.call(first["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("the access token replaced by a refresh still works: %d", status)
	}
	if status, _ := s.call(second["access_token"].(string)); status != http.StatusOK {
		t.Fatalf("the refreshed access token: %d", status)
	}
	if _, refused := s.refresh(c.ID, first["refresh_token"].(string)); refused != "invalid_grant" {
		t.Fatalf("a reused refresh token: %q, want invalid_grant", refused)
	}
	s.clock.Advance(authn.MaxCache + time.Second) // past the cache of the call above
	if status, _ := s.call(second["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("after a refresh token was reused, its family's access token still works: %d", status)
	}
	if _, refused := s.refresh(c.ID, second["refresh_token"].(string)); refused == "" {
		t.Error("after a refresh token was reused, its family's refresh token still works")
	}

	// A change to the user's roles asks, in its own unit of work, for their tokens to be revoked; the
	// worker revokes them; the API's next call after its cache lapses is denied: within 60 seconds.
	third, _ := s.authorize(c.ID, apiAudience, "openid fyniq.read.accounting", true, "accept")
	if status, _ := s.call(third["access_token"].(string)); status != http.StatusOK {
		t.Fatalf("a fresh token: %d", status)
	}
	queue := local.NewMemory()
	var row storage.OutboxRow
	if err := s.e.store.Do(ctx, s.e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := revoke.RevokeAll(ctx, outbox.Dispatcher{Store: s.e.store, Queue: queue, Clock: s.clock}, u, s.e.books.Org,
			"user-oauth-1", "roles changed"); err != nil {
			return err
		}
		rows, err := u.Sweeping().Due(ctx, s.clock.Now().Add(time.Hour), s.clock.Now().Add(time.Hour))
		for _, r := range rows {
			if r.Kind == revoke.Kind {
				row = r
			}
		}
		return err
	}); err != nil || row.Kind != revoke.Kind {
		t.Fatalf("the revocation was not queued with the change: %v", err)
	}
	if err := revoke.Work(s.admin)(ctx, s.e.books.Org, row); err != nil { // what the worker runs
		t.Fatal(err)
	}
	if status, _ := s.call(third["access_token"].(string)); status != http.StatusOK {
		t.Errorf("within the 30-second cache the answer stands: %d", status)
	}
	s.clock.Advance(authn.MaxCache + time.Second)
	if status, _ := s.call(third["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("31 seconds after the revocation the token still works: %d", status)
	}
}
