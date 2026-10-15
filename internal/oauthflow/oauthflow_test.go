package oauthflow_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
