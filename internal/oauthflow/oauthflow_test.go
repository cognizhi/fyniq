package oauthflow_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/ports"
)

// req: AI-37
// asvs: V10.4.11, V10.7.2
func TestOnlyKnownScopesAreGranted(t *testing.T) {
	for scope, want := range map[string]bool{
		"openid": true, "offline_access": true, "fyniq.read.accounting": true, "fyniq.read.proposals": true,
		"fyniq.propose.bank.categorise": true, "fyniq.read.payroll": false, "fyniq.read.equity": false,
		"fyniq.write.accounting": false, "fyniq.read.": false, "admin": false, "fyniq.propose.": false,
	} {
		if got := oauthflow.Grantable(scope); got != want {
			t.Errorf("Grantable(%q) = %v, want %v", scope, got, want)
		}
	}
}

// req: AI-36
// req: AI-34
// asvs: V10.3.1, V10.7.1
func TestConsentGrantsOnlyWhatTheClientRecordAllows(t *testing.T) {
	f := oauthflow.Flow{APIAudience: "https://api", MCPAudience: "https://mcp"}
	user := authn.Principal{Subject: "u", Organisation: uuid.New(), Roles: []authz.Role{authz.Accountant}}
	client := ports.OAuthClient{ID: "c", Class: authn.ClassAgent, Audiences: []string{"https://mcp"}}
	req := ports.OAuthConsent{Subject: "u", Client: client, RequestedAudiences: []string{"https://mcp"},
		RequestedScopes: []string{"openid", "fyniq.read.accounting", "fyniq.read.payroll", "sudo"}}
	d := f.Decide(req, user)
	if d.Problem != "" || strings.Join(d.Scopes, " ") != "openid fyniq.read.accounting" || strings.Join(d.RefusedScopes, " ") != "fyniq.read.payroll sudo" ||
		strings.Join(d.Audiences, " ") != "https://mcp" {
		t.Fatalf("Decide = %+v", d)
	}
	for name, change := range map[string]func(c *ports.OAuthConsent, p *authn.Principal){
		"an audience the client record does not allow": func(c *ports.OAuthConsent, _ *authn.Principal) { c.RequestedAudiences = []string{"https://api"} },
		"an audience that is neither the API nor MCP": func(c *ports.OAuthConsent, _ *authn.Principal) {
			c.Client.Audiences = append(c.Client.Audiences, "https://evil")
			c.RequestedAudiences = []string{"https://evil"}
		},
		"no audience":                       func(c *ports.OAuthConsent, _ *authn.Principal) { c.RequestedAudiences = nil },
		"a client without a class":          func(c *ports.OAuthConsent, _ *authn.Principal) { c.Client.Class = "" },
		"a class the request made up":       func(c *ports.OAuthConsent, _ *authn.Principal) { c.Client.Class = "superuser" },
		"a user outside every organisation": func(_ *ports.OAuthConsent, p *authn.Principal) { p.Organisation = uuid.Nil },
	} {
		c, p := req, user
		c.Client.Audiences = append([]string(nil), client.Audiences...)
		change(&c, &p)
		if d := f.Decide(c, p); d.Problem == "" {
			t.Errorf("%s: consent was not refused: %+v", name, d)
		}
	}
}

// fakeOAuth is the OAuth server's admin side: one pending login and one pending consent.
type fakeOAuth struct {
	login    ports.OAuthLogin
	consent  ports.OAuthConsent
	accepted string // the subject AcceptLogin got
	granted  bool
}

func (f *fakeOAuth) LoginRequest(context.Context, string) (ports.OAuthLogin, error) {
	return f.login, nil
}
func (f *fakeOAuth) AcceptLogin(_ context.Context, _ string, a ports.LoginAcceptance) (string, error) {
	f.accepted = a.Subject
	return "https://hydra.test/after-login", nil
}
func (f *fakeOAuth) RejectLogin(context.Context, string, ports.OAuthRejection) (string, error) {
	return "https://client.test/cb?error=invalid_request", nil
}
func (f *fakeOAuth) ConsentRequest(context.Context, string) (ports.OAuthConsent, error) {
	return f.consent, nil
}
func (f *fakeOAuth) AcceptConsent(context.Context, string, ports.ConsentAcceptance) (string, error) {
	f.granted = true
	return "https://hydra.test/after-consent", nil
}
func (f *fakeOAuth) RejectConsent(context.Context, string, ports.OAuthRejection) (string, error) {
	return "https://client.test/cb?error=access_denied", nil
}
func (f *fakeOAuth) Introspect(context.Context, string) (ports.Introspection, error) {
	return ports.Introspection{}, nil
}
func (f *fakeOAuth) RevokeSubject(context.Context, string) error { return nil }

// cookieSessions is the session cookie's authenticator: a cookie value names a principal.
type cookieSessions map[string]authn.Principal

func (c cookieSessions) Authenticate(_ context.Context, cr authn.Credentials) (authn.Principal, error) {
	if p, ok := c[authn.CookieValue(cr.Cookie, "fyniq_session")]; ok {
		return p, nil
	}
	return authn.Principal{}, authn.ErrUnauthenticated
}

// A browser signs in to an OAuth client with its Fyniq session: the OAuth server knows the person by
// their user ID, the subject a role change or a recovery revokes (AI-38). A browser without a session
// is sent to sign in and comes back; a signed-in identity without a Fyniq user is refused; and a
// consent the session cookie carries must come from the web app's own origin.
// req: AI-38
// req: SEC-05
// asvs: V10.4.6, V3.5.3, V7.2.1
func TestTheOAuthLoginUsesTheSessionsUser(t *testing.T) {
	user := uuid.New()
	org := uuid.New()
	oauth := &fakeOAuth{login: ports.OAuthLogin{Challenge: "lc",
		RequestURL: "https://hydra.test/oauth2/auth?code_challenge=x&code_challenge_method=S256"}}
	sessions := cookieSessions{
		"with-user": {Via: authn.ViaSession, Issuer: "https://accounts.zoho.com", Subject: "zoho-123", UserID: user, Organisation: org,
			Roles: []authz.Role{authz.Accountant}},
		"no-user": {Via: authn.ViaSession, Issuer: "https://accounts.zoho.com", Subject: "zoho-456"},
	}
	mux := http.NewServeMux()
	oauthflow.Flow{OAuth: oauth, SignedIn: authn.Local{Sessions: sessions}, Clock: clock.System{}, APIAudience: "https://api",
		MCPAudience: "https://mcp", SignInPage: "/", Base: "/api"}.Register(mux)
	do := func(method, path, cookie, site string, form url.Values) *http.Response {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		r := httptest.NewRequest(method, path, body)
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "fyniq_session", Value: cookie})
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Result()
	}

	resp := do(http.MethodGet, "/auth/login?login_challenge=lc", "", "", nil)
	if want := "/?return_to=" + url.QueryEscape("/api/auth/login?login_challenge=lc"); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
		t.Errorf("no session: %d %q, want a redirect to %q", resp.StatusCode, resp.Header.Get("Location"), want)
	}
	if resp := do(http.MethodGet, "/auth/login?login_challenge=lc", "no-user", "", nil); resp.StatusCode != http.StatusForbidden || oauth.accepted != "" {
		t.Errorf("an identity without a user signed in to a client: %d", resp.StatusCode)
	}
	if resp := do(http.MethodGet, "/auth/login?login_challenge=lc", "with-user", "", nil); resp.StatusCode != http.StatusSeeOther || oauth.accepted != user.String() {
		t.Fatalf("a session's login: %d, subject %q, want the user ID", resp.StatusCode, oauth.accepted)
	}

	oauth.consent = ports.OAuthConsent{Challenge: "cc", Subject: user.String(), RequestedAudiences: []string{"https://api"},
		RequestedScopes: []string{"openid"}, Client: ports.OAuthClient{ID: "c", Class: authn.ClassIntegration, Audiences: []string{"https://api"}}}
	form := url.Values{"consent_challenge": {"cc"}, "decision": {"accept"}}
	for _, site := range []string{"", "cross-site", "same-site"} {
		if resp := do(http.MethodPost, "/auth/consent", "with-user", site, form); resp.StatusCode != http.StatusForbidden || oauth.granted {
			t.Errorf("a consent sent with Sec-Fetch-Site %q: %d, granted %v", site, resp.StatusCode, oauth.granted)
		}
	}
	if resp := do(http.MethodPost, "/auth/consent", "with-user", "same-origin", form); resp.StatusCode != http.StatusSeeOther || !oauth.granted {
		t.Errorf("the web app's consent: %d, granted %v", resp.StatusCode, oauth.granted)
	}
}

type fakeGate struct {
	code, why string
	err       error
	asked     []string
}

func (g *fakeGate) Admit(_ context.Context, _, _ uuid.UUID, app string) (string, string, error) {
	g.asked = append(g.asked, app)
	return g.code, g.why, g.err
}

// An MCP client sends the RFC 8707 resource, not an audience, and the OAuth server leaves the audience empty: consent reads
// the resource, requires it to be exactly the API's or the MCP server's, once, and in agreement with any audience named
// (G4-082, AI-36). A token for the MCP server is for it alone and for an agent-class client only.
// req: AI-36
// req: AI-34
// asvs: V10.3.1, V10.3.2
func TestConsentTurnsTheResourceIntoTheAudience(t *testing.T) {
	f := oauthflow.Flow{APIAudience: "https://api.test/api", MCPAudience: "https://mcp.test/mcp"}
	user := authn.Principal{Subject: "u", Organisation: uuid.New()}
	agent := ports.OAuthClient{ID: "claude-code", Class: authn.ClassAgent, Audiences: []string{"https://mcp.test/mcp", "https://api.test/api"}}
	ask := func(client ports.OAuthClient, requestURL string, audiences ...string) oauthflow.Decision {
		return f.Decide(ports.OAuthConsent{Client: client, RequestedScopes: []string{"openid", "fyniq.read.reports"}, RequestedAudiences: audiences, RequestURL: requestURL}, user)
	}
	if d := ask(agent, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp"); d.Problem != "" || strings.Join(d.Audiences, " ") != "https://mcp.test/mcp" {
		t.Errorf("the resource alone: %+v", d)
	}
	if d := ask(agent, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp", "https://mcp.test/mcp"); d.Problem != "" {
		t.Errorf("a resource and the same audience: %+v", d)
	}
	for name, d := range map[string]oauthflow.Decision{
		"another resource":                       ask(agent, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fevil.test%2Fmcp"),
		"a path suffix":                          ask(agent, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp%2F"),
		"two resources":                          ask(agent, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp&resource=https%3A%2F%2Fapi.test%2Fapi"),
		"a resource and an audience that differ": ask(agent, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp", "https://api.test/api"),
		"both audiences":                         ask(agent, "", "https://mcp.test/mcp", "https://api.test/api"),
		"an integration for MCP":                 ask(ports.OAuthClient{ID: "i", Class: authn.ClassIntegration, Audiences: []string{"https://mcp.test/mcp"}}, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp"),
		"a user-class client":                    ask(ports.OAuthClient{ID: "u", Class: authn.ClassUser, Audiences: []string{"https://mcp.test/mcp"}}, "https://hydra.test/oauth2/auth?resource=https%3A%2F%2Fmcp.test%2Fmcp"),
	} {
		if d.Problem == "" {
			t.Errorf("%s: consent was not refused: %+v", name, d)
		}
	}
}

// An MCP consent passes every gate or is refused, closed by default: no gate in the deployment, a second factor that is not
// fresh, an organisation that has not turned AI apps on, an app that is not allowed and a person who has not opted in each
// refuse, and a gate that cannot answer refuses too. Consent for the API is untouched by the gate (AI-35, AI-37).
// req: AI-35
// req: AI-37
// req: CTL-MCP-03
// asvs: V10.4.1, V8.2.2
func TestAnMCPConsentPassesEveryGateOrIsRefused(t *testing.T) {
	now := clock.System{}.Now()
	gate := &fakeGate{}
	f := oauthflow.Flow{APIAudience: "https://api.test/api", MCPAudience: "https://mcp.test/mcp", Clock: clock.NewFrozen(now), MCP: gate}
	user := authn.Principal{Via: authn.ViaSession, Subject: "u", UserID: uuid.New(), Organisation: uuid.New(), StepUpAt: now.Add(-time.Minute)}
	consent := ports.OAuthConsent{Subject: "u", RequestedScopes: []string{"openid", "fyniq.read.reports"}, RequestedAudiences: []string{"https://mcp.test/mcp"},
		Client: ports.OAuthClient{ID: "claude-code", Class: authn.ClassAgent, Audiences: []string{"https://mcp.test/mcp"}}}
	if d := f.Admit(context.Background(), consent, user); d.Problem != "" || len(gate.asked) != 1 || gate.asked[0] != "claude-code" {
		t.Fatalf("every gate open: %+v asked %v", d, gate.asked)
	}
	check := func(name string, f oauthflow.Flow, p authn.Principal, code string) {
		t.Helper()
		if d := f.Admit(context.Background(), consent, p); d.Problem == "" || d.Code != code {
			t.Errorf("%s: %+v, want a refusal with %s", name, d, code)
		}
	}
	noGate := f
	noGate.MCP = nil
	check("no gate in the deployment", noGate, user, oauthflow.CodeMCPOff)
	stale := user
	stale.StepUpAt = now.Add(-authn.StepUpWindow - time.Minute)
	check("a second factor that is not fresh", f, stale, oauthflow.CodeStepUp)
	none := user
	none.StepUpAt = time.Time{}
	check("no second factor", f, none, oauthflow.CodeStepUp)
	token := user
	token.Via = authn.ViaToken
	check("a bearer token instead of a session", f, token, oauthflow.CodeStepUp)
	noUser := user
	noUser.UserID = uuid.Nil
	check("no Fyniq user", f, noUser, oauthflow.CodeNoAccount)
	for _, code := range []string{"MCP-GATE-01", "MCP-GATE-02", "MCP-GATE-03"} {
		gate.code, gate.why = code, "plain words"
		check("the gate says "+code, f, user, code)
	}
	gate.code, gate.why, gate.err = "", "", errors.New("the database is down")
	check("the gate cannot answer", f, user, oauthflow.CodeFailure)

	// The API's own consent does not meet the MCP gate.
	api := consent
	api.RequestedAudiences = []string{"https://api.test/api"}
	api.Client = ports.OAuthClient{ID: "i", Class: authn.ClassIntegration, Audiences: []string{"https://api.test/api"}}
	gate.err = errors.New("must not be asked")
	if d := f.Admit(context.Background(), api, token); d.Problem != "" {
		t.Errorf("an API consent met the MCP gate: %+v", d)
	}
}
