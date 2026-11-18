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

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/mcpserver"
	"github.com/cognizhi/fyniq/internal/mcpsurface"
	"github.com/cognizhi/fyniq/internal/ports"
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

	// A loopback redirect matches the registered one on any port (RFC 8252 section 7.3); another host does not.
	q, _ = pkceQuery(c.ID, "openid fyniq.read.reports")
	if _, refused := s.loginRequestOf(q, "http://127.0.0.1:53124/callback"); refused != "" {
		t.Errorf("a loopback redirect on another port: %s", refused)
	}

	// Claude Code asks for http://localhost:<port>/callback (the capture in design.md, task 1.2), so the host name localhost
	// matches too; a different path does not.
	q, _ = pkceQuery(c.ID, "openid fyniq.read.reports")
	if _, refused := s.loginRequestOf(q, "http://localhost:3118/callback"); refused != "" {
		t.Errorf("a localhost redirect on another port: %s", refused)
	}
	q, _ = pkceQuery(c.ID, "openid fyniq.read.reports")
	if l, _ := s.loginRequestOf(q, "http://localhost:3118/elsewhere"); l.Challenge != "" {
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
	// A token for both audiences could be passed through to the API: the verifier refuses it, whatever consent granted.
	if both, refused := s.authorize(agent, mcpAudience+" "+apiAudience, "openid fyniq.read.reports", true, "accept"); refused != "" {
		t.Logf("a request for both audiences is refused at consent: %s", refused)
	} else if status, _ := list(both["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("a token for the MCP server and the API: %d", status)
	}
	user, _ := s.authorize(integration, mcpAudience, "openid fyniq.read.reports", true, "accept")
	if status, _ := list(user["access_token"].(string)); status != http.StatusUnauthorized {
		t.Errorf("an integration-class client's MCP token: %d", status)
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
