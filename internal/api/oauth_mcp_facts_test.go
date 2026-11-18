//go:build local

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/authn"
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
