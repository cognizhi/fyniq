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
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/mcpserver"
	"github.com/cognizhi/fyniq/internal/mcpsurface"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	oauthtokens "github.com/cognizhi/fyniq/internal/tokens"
)

// loginRequestOf starts an authorisation request with q and returns the pending login the server hands the login app,
// without signing anyone in: what the server made of the request's parameters.
func (s *oauthStack) loginRequestOf(q url.Values, redirectURI string) (ports.OAuthLogin, string) {
	s.t.Helper()
	srvHost := strings.TrimPrefix(s.srv.URL, "http://")
	b := s.browser()
	b.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
		if r.URL.Host == srvHost || r.URL.Host == "client.test" || strings.HasPrefix(r.URL.Host, "127.0.0.1:") || strings.HasPrefix(r.URL.Host, "localhost:") {
			return http.ErrUseLastResponse
		}
		return nil
	}
	q.Set("redirect_uri", redirectURI)
	resp, err := b.Get("http://hydra.test/oauth2/auth?" + q.Encode())
	if err != nil {
		s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	u, _ := url.Parse(loc)
	if c := u.Query().Get("login_challenge"); c != "" {
		l, err := s.admin.LoginRequest(context.Background(), c)
		if err != nil {
			s.t.Fatal(err)
		}
		return l, ""
	}
	return ports.OAuthLogin{}, loc
}

func pkceQuery(clientID, scope string) (url.Values, string) {
	verifier := rand.Text() + rand.Text()
	sum := sha256.Sum256([]byte(verifier))
	return url.Values{"client_id": {clientID}, "response_type": {"code"}, "scope": {scope}, "state": {"state-" + uuid.NewString()},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}, verifier
}

// walk runs a whole authorisation request through the login and consent app and the token endpoint, as an MCP client
// would, and returns what the callback received, the token response and the token as the server introspects it.
func (s *oauthStack) walk(q url.Values, verifier, redirectURI string, tokenExtra url.Values) (url.Values, map[string]any, ports.Introspection) {
	s.t.Helper()
	b := s.browser()
	redirectHost := ""
	if u, err := url.Parse(redirectURI); err == nil {
		redirectHost = u.Host
	}
	b.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
		if r.URL.Host == redirectHost {
			return http.ErrUseLastResponse
		}
		return nil
	}
	q.Set("redirect_uri", redirectURI)
	resp, err := b.Get("http://hydra.test/oauth2/auth?" + q.Encode())
	if err != nil {
		s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	challenge := resp.Request.URL.Query().Get("consent_challenge")
	if challenge == "" {
		s.t.Fatalf("no consent challenge: %d %s %s", resp.StatusCode, resp.Request.URL, resp.Header.Get("Location"))
	}
	resp, err = b.PostForm(s.srv.URL+"/auth/consent", url.Values{"consent_challenge": {challenge}, "decision": {"accept"}})
	if err != nil {
		s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	back := mustQuery(s.t, resp.Header.Get("Location"))
	form := url.Values{"grant_type": {"authorization_code"}, "code": {back.Get("code")}, "redirect_uri": {redirectURI},
		"client_id": {q.Get("client_id")}, "code_verifier": {verifier}}
	for k, v := range tokenExtra {
		form[k] = v
	}
	tok, err := b.PostForm("http://hydra.test/oauth2/token", form)
	if err != nil {
		s.t.Fatal(err)
	}
	defer tok.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(tok.Body).Decode(&out)
	var in ports.Introspection
	if at, ok := out["access_token"].(string); ok {
		in, _ = s.admin.Introspect(context.Background(), at)
	}
	return back, out, in
}

// Task 1.1 of p1-mcp-first-party: what the pinned Hydra does with the parameters an MCP client sends. Each assertion is
// a fact the consent app and the resource server are built on.
// req: AI-36
// req: AI-35
// asvs: V10.4.1, V10.4.4
func TestHydraAndTheParametersAnMCPClientSends(t *testing.T) {
	s := startOAuth(t, 0)
	ctx := context.Background()
	c, err := s.admin.CreateClient(ctx, hydra.NewClient{Name: "Probe", Class: authn.ClassAgent, Audiences: []string{mcpAudience},
		RedirectURIs: []string{"http://127.0.0.1/callback", "http://localhost/callback", callback}, Scopes: []string{"openid", "fyniq.read.reports"}})
	if err != nil {
		t.Fatal(err)
	}

	// The RFC 8707 `resource` parameter is carried in the request URL and is not an audience: the server leaves the
	// requested audiences empty, so the consent app must read it and set the audience itself (G4-082).
	q, _ := pkceQuery(c.ID, "openid fyniq.read.reports")
	q.Set("resource", mcpAudience)
	login, refused := s.loginRequestOf(q, callback)
	if refused != "" {
		t.Fatalf("a request with a resource was refused: %s", refused)
	}
	if len(login.RequestedAudiences) != 0 {
		t.Errorf("the server turned resource into the requested audiences %v", login.RequestedAudiences)
	}
	if got := mustQuery(t, login.RequestURL).Get("resource"); got != mcpAudience {
		t.Errorf("the login request's URL carries resource %q, want %q", got, mcpAudience)
	}

	// The pinned Hydra emits no `iss` in the authorisation response (RFC 9207) and does not say so in its metadata
	// (`authorization_response_iss_parameter_supported`), which the MCP revision in force allows: it makes `iss` a SHOULD
	// for the authorisation server and has a client proceed when both are absent, and it signals a future MUST. A client
	// can still be told the issuer by metadata; the gap goes in the design and the security-review pack. The test pins the
	// fact, so an upgrade that starts emitting it is noticed and the consent app can rely on it.
	q, verifier := pkceQuery(c.ID, "openid fyniq.read.reports")
	q.Set("audience", mcpAudience)
	back, _, in := s.walk(q, verifier, callback, url.Values{"resource": {mcpAudience}}) // a token request that repeats resource is accepted
	if back.Get("iss") != "" {
		t.Errorf("the authorisation response carries iss %q now: record it, and let clients check it (RFC 9207)", back.Get("iss"))
	}
	if !in.Active || len(in.Audiences) != 1 || in.Audiences[0] != mcpAudience {
		t.Errorf("the token's audiences %v (active %v), want the MCP resource alone", in.Audiences, in.Active)
	}

	// A loopback IP redirect matches the registered one on any port (RFC 8252 section 7.3); another host does not.
	q, _ = pkceQuery(c.ID, "openid fyniq.read.reports")
	if l, _ := s.loginRequestOf(q, "http://127.0.0.1:53124/callback"); l.Challenge == "" {
		t.Error("a loopback IP redirect on another port was refused")
	}

	// Claude Code asks for http://localhost:<port>/callback (the capture in design.md, task 1.2). Hydra does NOT match the host
	// name localhost on another port, only the IP loopback addresses, so a localhost redirect is registered with its port; and a
	// different path never matches.
	q, _ = pkceQuery(c.ID, "openid fyniq.read.reports")
	if l, _ := s.loginRequestOf(q, "http://localhost:3118/callback"); l.Challenge != "" {
		t.Error("a localhost redirect on another port was accepted: record it, the registration may then be portless")
	}
	q, _ = pkceQuery(c.ID, "openid fyniq.read.reports")
	if l, _ := s.loginRequestOf(q, "http://127.0.0.1:53124/elsewhere"); l.Challenge != "" {
		t.Error("a loopback redirect on another path was accepted")
	}

	// Dynamic client registration is off.
	resp, err := http.Post("http://"+s.public.Host+"/oauth2/register", "application/json",
		strings.NewReader(`{"client_name":"rogue","redirect_uris":["http://rogue.test/cb"]}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("dynamic client registration answered %d: it must be off (AI-35)", resp.StatusCode)
	}
}

// The MCP server accepts what the pinned Hydra issues to an allowlisted agent-class client for the MCP audience, and nothing
// else: the consent app's claims (class, organisation, granted_at) are what its verifier reads; an API-audience token, an
// expired or revoked one, and a client of another class are refused; and the token lists the tools its scopes allow. The
// server answers in the revision's shape end to end (task 4.2 of p1-mcp-first-party).
// req: AI-36
// req: AI-37
// req: AI-39
// req: QA-MCP-01
// asvs: V10.3.1, V10.4.6, V13.1.1
func TestTheMCPServerChecksATokenHydraIssued(t *testing.T) {
	s := startOAuth(t, 0)
	ctx := context.Background()
	newClient := func(class string, audiences []string) string {
		c, err := s.admin.CreateClient(ctx, hydra.NewClient{Name: "Test " + class, Class: class, Audiences: audiences, RedirectURIs: []string{callback},
			Scopes: []string{"openid", "fyniq.read.reports", "fyniq.read.sales"}})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	agent := newClient(authn.ClassAgent, []string{mcpAudience, apiAudience})
	integration := newClient(authn.ClassIntegration, []string{mcpAudience})
	manifest, _ := mcpsurface.Default()
	srv, err := mcpserver.New(mcpserver.Config{Resource: mcpAudience, Issuer: "http://hydra.test/", Manifest: manifest, Calls: mcpserver.Unavailable{},
		Verify: mcpserver.OAuthVerifier{OAuth: s.admin, Resource: mcpAudience, Clock: clock.System{}}, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	mcp := httptest.NewServer(srv.Handler())
	t.Cleanup(mcp.Close)
	list := func(token string) (int, string) {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"` + mcpserver.ProtocolVersion + `"}}}`
		req, _ := http.NewRequest(http.MethodPost, mcp.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("MCP-Protocol-Version", mcpserver.ProtocolVersion)
		req.Header.Set("Mcp-Method", "tools/list")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	tok, refused := s.authorize(agent, mcpAudience, "openid fyniq.read.reports", true, "accept")
	if refused != "" {
		t.Fatalf("the agent's flow was refused: %s", refused)
	}
	mcpToken := tok["access_token"].(string)
	status, body := list(mcpToken)
	if status != http.StatusOK || !strings.Contains(body, `"metrics.query"`) || strings.Contains(body, `"records.get"`) {
		t.Fatalf("a reports token lists: %d %s", status, body)
	}
	if status, _ := list("not-a-token"); status != http.StatusUnauthorized {
		t.Errorf("a made-up token: %d", status)
	}
	api, _ := s.authorize(agent, apiAudience, "openid fyniq.read.reports", true, "accept")
	if status, _ := list(api["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("an API-audience token at the MCP server: %d", status)
	}
	// Consent refuses what the verifier would: a token for both audiences (it could be passed through to the API) and an MCP
	// token for a client that is not an agent.
	if _, refused := s.authorize(agent, mcpAudience+" "+apiAudience, "openid fyniq.read.reports", true, "accept"); refused == "" {
		t.Error("a token for the MCP server and the API was granted")
	}
	if _, refused := s.authorize(integration, mcpAudience, "openid fyniq.read.reports", true, "accept"); refused == "" {
		t.Error("an integration-class client was given an MCP token")
	}
	resp, err := s.browser().PostForm("http://hydra.test/oauth2/revoke", url.Values{"token": {mcpToken}, "client_id": {agent}})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	if status, _ := list(mcpToken); status != http.StatusUnauthorized {
		t.Errorf("a revoked token: %d", status)
	}
}

// The first-party MCP clients register with the OAuth server under fixed IDs, and an MCP client's own flow, as Claude Code
// runs it (the resource parameter and no audience, a localhost redirect on a port it picks, PKCE), ends in a token for the MCP
// server alone, but only through the gates: the organisation's switch, the allowlist and the person's opt-in (the gate's
// refusals), a fresh second factor and the consent itself (task 3.1 and 3.2 of p1-mcp-first-party).
// req: AI-35
// req: AI-36
// req: AI-37
// req: CTL-MCP-03
// asvs: V10.4.1, V10.4.4, V10.3.1
func TestAClaudeCodeFlowEndsInATokenOnlyThroughTheGates(t *testing.T) {
	s := startOAuth(t, 0)
	ctx := context.Background()
	if err := mcpgate.Register(ctx, s.admin, mcpAudience); err != nil {
		t.Fatal(err)
	}
	if err := mcpgate.Register(ctx, s.admin, mcpAudience); err != nil { // registering again replaces: a changed contract reaches a running server
		t.Fatalf("registering again: %v", err)
	}
	const redirect = "http://localhost:3118/callback"
	flow := func(clientID, scope string, resource string) (url.Values, map[string]any, ports.Introspection) {
		q, verifier := pkceQuery(clientID, scope)
		q.Set("resource", resource)
		return s.walk(q, verifier, redirect, url.Values{"resource": {resource}})
	}

	back, tok, in := flow("claude-code", "openid fyniq.read.reports", mcpAudience)
	if back.Get("error") != "" || tok["access_token"] == nil {
		t.Fatalf("the open gates: %v %v", back, tok)
	}
	if !in.Active || len(in.Audiences) != 1 || in.Audiences[0] != mcpAudience || in.Claims["class"] != "agent" || in.Claims["granted_at"] == nil {
		t.Errorf("the token: %+v", in)
	}
	if sc, _ := tok["scope"].(string); sc != "openid fyniq.read.reports" {
		t.Errorf("the scopes granted: %q", sc)
	}
	// The client's record is the first-party one: agent class, the MCP audience only, a redirect that is exact.

	refused := func(name, clientID, scope, resource string) {
		t.Helper()
		back, tok, _ := flow(clientID, scope, resource)
		if back.Get("error") == "" || tok["access_token"] != nil {
			t.Errorf("%s: a token was issued: %v %v", name, back, tok)
		}
	}
	for _, g := range [][2]string{{"MCP-GATE-01", "organisation off"}, {"MCP-GATE-02", "not allowed"}, {"MCP-GATE-03", "not opted in"}} {
		s.gate.code, s.gate.why = g[0], g[1]
		refused("the gate says "+g[1], "claude-code", "openid fyniq.read.reports", mcpAudience)
	}
	s.gate.code, s.gate.why = "", ""
	refused("a resource that is not this server's", "claude-code", "openid fyniq.read.reports", "http://evil.test/mcp")
	refused("the API as the resource for an agent client", "claude-code", "openid fyniq.read.reports", apiAudience)
	// A scope no client may hold (payroll, equity) is refused by the OAuth server before any consent: the client's record
	// does not hold it.
	for _, scope := range []string{"openid fyniq.read.payroll", "openid fyniq.read.equity", "openid fyniq.propose.sec.create_task"} {
		q, _ := pkceQuery("claude-code", scope)
		q.Set("resource", mcpAudience)
		if l, refused := s.loginRequestOf(q, redirect); l.Challenge != "" || !strings.Contains(refused, "error=invalid_scope") {
			t.Errorf("%s: %q %q", scope, l.Challenge, refused)
		}
	}
	// Without a fresh second factor nothing is granted.
	s.e.tokens.mu.Lock()
	p := s.e.tokens.m[s.user]
	p.StepUpAt = clock.System{}.Now().Add(-time.Hour)
	s.e.tokens.m[s.user] = p
	s.e.tokens.mu.Unlock()
	refused("a stale second factor", "claude-code", "openid fyniq.read.reports", mcpAudience)
	// Another redirect than the registered loopback is refused by the OAuth server before any consent.
	q, _ := pkceQuery("claude-code", "openid fyniq.read.reports")
	q.Set("resource", mcpAudience)
	if l, _ := s.loginRequestOf(q, "http://evil.test/callback"); l.Challenge != "" {
		t.Error("an unregistered redirect reached the login")
	}
}

// A person's opt-out of AI apps asks, in the unit of work that records it, for the tokens their AI apps hold to be revoked; the
// worker revokes them, and only them: the same person's token for an integration or the web stays valid. The per-call gate stops the
// next call anyway; this ends the tokens too (AI-35, AI-38).
// req: AI-35
// req: AI-38
// req: CTL-MCP-04
// asvs: V10.4.9, V7.4.1
func TestAnOptOutRevokesTheAIAppsTokensAndNothingElse(t *testing.T) {
	s := startOAuth(t, 0)
	ctx := context.Background()
	if err := mcpgate.Register(ctx, s.admin, mcpAudience); err != nil {
		t.Fatal(err)
	}
	q, verifier := pkceQuery("claude-code", "openid offline_access fyniq.read.reports")
	q.Set("resource", mcpAudience)
	_, agent, _ := s.walk(q, verifier, "http://localhost:3118/callback", url.Values{"resource": {mcpAudience}})
	integration, err := s.admin.CreateClient(ctx, hydra.NewClient{Name: "Test integration", Class: authn.ClassIntegration, Audiences: []string{apiAudience},
		RedirectURIs: []string{callback}, Scopes: []string{"openid", "fyniq.read.accounting"}})
	if err != nil {
		t.Fatal(err)
	}
	other, refused := s.authorize(integration.ID, apiAudience, "openid fyniq.read.accounting", true, "accept")
	if refused != "" || agent["access_token"] == nil {
		t.Fatalf("the tokens: %q %v", refused, agent)
	}
	active := func(token any) bool {
		in, err := s.admin.Introspect(ctx, token.(string))
		return err == nil && in.Active
	}
	if !active(agent["access_token"]) || !active(other["access_token"]) {
		t.Fatal("both tokens should be live")
	}
	var row storage.OutboxRow
	err = s.e.store.Do(ctx, s.e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := oauthtokens.RevokeAgents(ctx, outbox.Dispatcher{Store: s.e.store, Queue: local.NewMemory(), Clock: s.clock}, u, s.e.books.Org, s.userID.String(), "the person opted out"); err != nil {
			return err
		}
		rows, err := u.Sweeping().Due(ctx, s.clock.Now().Add(time.Hour), s.clock.Now().Add(time.Hour))
		for _, r := range rows {
			if r.Kind == oauthtokens.AgentKind {
				row = r
			}
		}
		return err
	})
	if err != nil || row.Kind != oauthtokens.AgentKind {
		t.Fatalf("the revocation was not queued with the change: %v", err)
	}
	if err := oauthtokens.AgentWork(s.admin)(ctx, s.e.books.Org, row); err != nil {
		t.Fatal(err)
	}
	if active(agent["access_token"]) {
		t.Error("the AI app's token survives the opt-out")
	}
	if rt, _ := agent["refresh_token"].(string); rt != "" {
		if _, refusedRefresh := s.refresh("claude-code", rt); refusedRefresh == "" {
			t.Error("the AI app's refresh token survives the opt-out")
		}
	}
	if !active(other["access_token"]) {
		t.Error("the opt-out revoked a token that is not an AI app's")
	}
	if err := oauthtokens.AgentWork(s.admin)(ctx, s.e.books.Org, row); err != nil { // again: nothing left to revoke, and no error
		t.Errorf("revoking twice: %v", err)
	}
}
