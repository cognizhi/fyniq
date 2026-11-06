package signin_test

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/signin"
)

type fakeSignIn struct {
	started  ports.SignInSecrets
	finished ports.SignInSecrets
	callback *url.URL
	who      ports.SignedIn
	err      error
}

func (f *fakeSignIn) Providers() []ports.SignInProvider {
	return []ports.SignInProvider{{ID: "google", Name: "Google"}}
}

func (f *fakeSignIn) Start(_ context.Context, provider, callback string, s ports.SignInSecrets) (string, error) {
	f.started = s
	return "https://provider.test/authorize?" + url.Values{"state": {s.State}, "redirect_uri": {callback}}.Encode(), nil
}

func (f *fakeSignIn) Finish(_ context.Context, provider string, cb *url.URL, s ports.SignInSecrets) (ports.SignedIn, error) {
	f.finished, f.callback = s, cb
	return f.who, f.err
}

type fakeSessions struct {
	started []storage.NewSession
	ended   [][]byte
	emails  []string                   // "issuer subject email" of each email recorded
	live    map[string]storage.Session // the live sessions, by token hash
	links   []storage.IdentityLink
	linkErr error
	methods []storage.SignInMethod
	// sessionEmails are the verified emails kept on new sessions, for joining by domain
	sessionEmails []string
}

func (f *fakeSessions) RecordIdentityEmail(_ context.Context, issuer, subject, email string, _ time.Time) error {
	f.emails = append(f.emails, issuer+" "+subject+" "+email)
	return nil
}

func (f *fakeSessions) StartSession(_ context.Context, s storage.NewSession) error {
	f.started = append(f.started, s)
	return nil
}

func (f *fakeSessions) Session(_ context.Context, h []byte, _ time.Time, _ time.Duration) (storage.Session, error) {
	if s, ok := f.live[string(h)]; ok {
		return s, nil
	}
	return storage.Session{}, ports.ErrNotFound
}

func (f *fakeSessions) EndSession(_ context.Context, h []byte, _ time.Time) error {
	f.ended = append(f.ended, h)
	return nil
}

func (f *fakeSessions) UserSessions(context.Context, uuid.UUID, time.Time, time.Duration) ([]storage.Device, error) {
	return nil, nil
}

func (f *fakeSessions) IdleTimeout(_ context.Context, _ uuid.UUID, _ time.Time, fallback time.Duration) (time.Duration, error) {
	return fallback, nil
}

func (f *fakeSessions) EndUserSession(context.Context, uuid.UUID, []byte, time.Time) error {
	return ports.ErrNotFound
}

func (f *fakeSessions) LinkIdentity(_ context.Context, l storage.IdentityLink) (uuid.UUID, error) {
	if f.linkErr != nil {
		return uuid.Nil, f.linkErr
	}
	f.links = append(f.links, l)
	return f.live[string(l.SessionHash)].UserID, nil
}

func (f *fakeSessions) UnlinkIdentity(context.Context, []byte, string, string, time.Time) (uuid.UUID, error) {
	return uuid.Nil, ports.ErrNotFound
}

func (f *fakeSessions) Identities(context.Context, uuid.UUID) ([]storage.Identity, error) {
	return nil, nil
}

func (f *fakeSessions) RecordSessionEmail(_ context.Context, h []byte, email string) error {
	f.sessionEmails = append(f.sessionEmails, email)
	return nil
}

func (f *fakeSessions) SignInMethods(context.Context) ([]storage.SignInMethod, error) {
	return f.methods, nil
}

func (f *fakeSessions) SignInEssential(context.Context) ([]string, error) { return nil, nil }

func (f *fakeSessions) SignInCounts(context.Context, time.Time) (map[string]int, error) {
	return nil, nil
}

type flowEnv struct {
	flow     signin.Flow
	mux      *http.ServeMux
	provider *fakeSignIn
	sessions *fakeSessions
}

func newFlow(origin string) *flowEnv {
	e := &flowEnv{mux: http.NewServeMux(), provider: &fakeSignIn{}, sessions: &fakeSessions{}}
	e.flow = signin.Flow{SignIn: e.provider, Sessions: e.sessions, Clock: clock.NewFrozen(now), Origin: origin, Base: "/api"}
	e.flow.Register(e.mux)
	return e
}

func (e *flowEnv) do(r *http.Request) *http.Response {
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w.Result()
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// A sign-in is bound to the browser that started it: the state, nonce and PKCE verifier come from a
// random value in an HttpOnly cookie, and a callback without it is refused. Success starts a session
// whose token only the cookie holds; the store gets its SHA-256 (SEC-05, SEC-08).
// req: SEC-05
// req: SEC-08
// req: FR-ADM-08
// asvs: V10.2.1, V7.2.3, V7.2.4, V3.3.2, V3.3.4
func TestTheSignInFlowStartsASessionOnlyForTheBrowserThatBegan(t *testing.T) {
	e := newFlow("http://localhost:8080")
	resp := e.do(httptest.NewRequest(http.MethodGet, "/auth/signin?provider=google&return_to=/journals?page=2", nil))
	start := cookieNamed(resp, "fyniq_signin")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "https://provider.test/") || start == nil {
		t.Fatalf("start: %d %s %v", resp.StatusCode, resp.Header.Get("Location"), resp.Cookies())
	}
	if !start.HttpOnly || start.SameSite != http.SameSiteLaxMode || start.Path != "/" || start.Secure || start.MaxAge != 600 {
		t.Errorf("the sign-in cookie: %+v", start)
	}
	s := e.provider.started
	if len(s.State) != 43 || len(s.CodeVerifier) != 43 || s.State == s.Nonce || s.Nonce == s.CodeVerifier ||
		strings.Contains(start.Value, s.State) || strings.Contains(start.Value, s.CodeVerifier) {
		t.Errorf("the secrets are not independent 256-bit values kept out of the cookie: %+v", s)
	}

	callback := func(cookies ...*http.Cookie) *http.Response {
		r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c-1&state="+s.State, nil)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		return e.do(r)
	}
	for name, cookies := range map[string][]*http.Cookie{
		"no sign-in cookie (another browser)": nil,
		"a cookie naming another provider":    {{Name: "fyniq_signin", Value: strings.Replace(start.Value, ".google.", ".github.", 1)}},
		"a cookie that is not ours":           {{Name: "fyniq_signin", Value: "garbage"}},
	} {
		if loc := callback(cookies...).Header.Get("Location"); loc != "/?signin_error="+signin.CodeExpired {
			t.Errorf("%s: %q", name, loc)
		}
	}
	e.provider.err = ports.ErrSignInRefused
	if loc := callback(start).Header.Get("Location"); loc != "/?signin_error="+signin.CodeRefused {
		t.Errorf("a refused sign-in: %q", loc)
	}
	if len(e.sessions.started) != 0 {
		t.Fatal("a refused sign-in started a session")
	}

	e.provider.err = nil
	e.provider.who = ports.SignedIn{Provider: "google", Issuer: "https://accounts.google.com", Subject: "110", AuthTime: now.Add(-time.Minute), SecondFactor: true,
		Email: "Mei@Example.com", EmailVerified: true}
	old := &http.Cookie{Name: "fyniq_session", Value: "an-earlier-session"}
	resp = callback(start, old)
	session := cookieNamed(resp, "fyniq_session")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/journals?page=2" || session == nil || session.Value == "" {
		t.Fatalf("callback: %d %s %v", resp.StatusCode, resp.Header.Get("Location"), resp.Cookies())
	}
	if e.provider.finished != s || e.provider.callback.String() != "http://localhost:8080/api/auth/callback?code=c-1&state="+s.State {
		t.Errorf("Finish got other secrets or callback: %+v %s", e.provider.finished, e.provider.callback)
	}
	if c := cookieNamed(resp, "fyniq_signin"); c == nil || c.MaxAge >= 0 {
		t.Error("the sign-in cookie was not used up")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.MaxAge != int(authn.SessionLifetime/time.Second) {
		t.Errorf("the session cookie: %+v", session)
	}
	got := e.sessions.started[0]
	if !bytes.Equal(got.TokenHash, authn.HashToken(session.Value)) || got.Issuer != "https://accounts.google.com" || got.Subject != "110" ||
		got.Provider != "google" || !got.StepUpAt.Equal(now.Add(-time.Minute)) || !got.Expires.Equal(now.Add(authn.SessionLifetime)) {
		t.Errorf("the session stored: %+v", got)
	}
	if len(e.sessions.ended) != 1 || !bytes.Equal(e.sessions.ended[0], authn.HashToken("an-earlier-session")) {
		t.Error("a new sign-in did not end the browser's earlier session")
	}

	// The verified email is kept on the identity, so mail the person sends to an email-in address is known (FR-INT-22).
	if len(e.sessions.sessionEmails) != 1 || e.sessions.sessionEmails[0] != "Mei@Example.com" {
		t.Errorf("the session's verified email (FR-ADM-08): %v", e.sessions.sessionEmails)
	}
	if len(e.sessions.emails) != 1 || e.sessions.emails[0] != "https://accounts.google.com 110 Mei@Example.com" {
		t.Errorf("the emails recorded: %v", e.sessions.emails)
	}

	e.provider.who.SecondFactor, e.provider.who.EmailVerified = false, false
	callback(start)
	if !e.sessions.started[1].StepUpAt.IsZero() {
		t.Error("a sign-in without a second factor counted as a step-up (FR-ADM-07)")
	}
	if len(e.sessions.emails) != 1 {
		t.Error("an email the provider did not verify was recorded")
	}
}

// Linking a sign-in starts only from Fyniq itself, in a session that confirmed it is the person in the last 5 minutes,
// and sends the browser to the provider as signing in does; what comes back is added to the session's user, never a new
// session, and the person is told. Refusals land on the profile with their code.
// req: FR-ADM-08
// asvs: V6.8.1, V7.5.1
func TestLinkingASignInAddsItToTheSessionsUser(t *testing.T) {
	e := newFlow("http://localhost:8080")
	user := uuid.New()
	e.sessions.live = map[string]storage.Session{
		string(authn.HashToken("fresh")): {UserID: user, StepUpAt: now.Add(-time.Minute)},
		string(authn.HashToken("stale")): {UserID: user, StepUpAt: now.Add(-6 * time.Minute)},
		string(authn.HashToken("none")):  {UserID: user},
	}
	var told []string
	e.flow.Linked = func(_ context.Context, u uuid.UUID, _ []byte, provider string, _ time.Time) error {
		told = append(told, u.String()+" "+provider)
		return nil
	}
	e.mux = http.NewServeMux() // the flow is a value: register the one with Linked
	e.flow.Register(e.mux)
	start := func(session, site string) *http.Response {
		r := httptest.NewRequest(http.MethodGet, "/auth/signin?provider=google&link=1&return_to=/elsewhere", nil)
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "fyniq_session", Value: session})
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		return e.do(r)
	}
	for name, c := range map[string]struct{ session, site, code string }{
		"from another site":                   {"fresh", "cross-site", signin.CodeCrossSite},
		"with no Sec-Fetch-Site":              {"fresh", "", signin.CodeCrossSite},
		"signed out":                          {"", "same-origin", signin.CodeLinkStepUp},
		"an unknown session":                  {"gone", "same-origin", signin.CodeLinkStepUp},
		"a second factor 6 minutes ago":       {"stale", "same-origin", signin.CodeLinkStepUp},
		"a session that passed no second one": {"none", "same-origin", signin.CodeLinkStepUp},
	} {
		resp := start(c.session, c.site)
		if loc := resp.Header.Get("Location"); loc != "/settings/profile?link_error="+c.code || cookieNamed(resp, "fyniq_signin") != nil {
			t.Errorf("%s: %q", name, loc)
		}
	}

	resp := start("fresh", "same-origin")
	cookie := cookieNamed(resp, "fyniq_signin")
	if !strings.HasPrefix(resp.Header.Get("Location"), "https://provider.test/") || cookie == nil || !strings.HasSuffix(cookie.Value, ".link") {
		t.Fatalf("start: %s %v", resp.Header.Get("Location"), resp.Cookies())
	}
	callback := func(session string) *http.Response {
		r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c-1&state="+e.provider.started.State, nil)
		r.AddCookie(cookie)
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "fyniq_session", Value: session})
		}
		return e.do(r)
	}
	e.provider.who = ports.SignedIn{Provider: "google", Issuer: "https://accounts.google.com", Subject: "220", AuthTime: now, SecondFactor: true,
		Email: "mei@example.com", EmailVerified: true}
	e.provider.err = ports.ErrSignInRefused
	if loc := callback("fresh").Header.Get("Location"); loc != "/settings/profile?link_error="+signin.CodeRefused {
		t.Errorf("a refused sign-in: %q", loc)
	}
	e.provider.err = nil
	if loc := callback("").Header.Get("Location"); loc != "/settings/profile?link_error="+signin.CodeLinkStepUp {
		t.Errorf("a callback without the session: %q", loc)
	}
	e.sessions.linkErr = &storage.Error{Code: storage.CodeIdentity}
	if loc := callback("fresh").Header.Get("Location"); loc != "/settings/profile?link_error="+signin.CodeLinked {
		t.Errorf("a sign-in linked already: %q", loc)
	}
	e.sessions.linkErr = &storage.Error{Code: storage.CodeIdentityStepUp}
	if loc := callback("fresh").Header.Get("Location"); loc != "/settings/profile?link_error="+signin.CodeLinkStepUp {
		t.Errorf("a session the database finds without a recent second factor: %q", loc)
	}
	e.sessions.linkErr = nil

	resp = callback("fresh")
	if loc := resp.Header.Get("Location"); loc != "/settings/profile?linked=google" || cookieNamed(resp, "fyniq_session") != nil {
		t.Fatalf("linking: %q %v", loc, resp.Cookies())
	}
	if len(e.sessions.links) != 1 || len(e.sessions.started) != 0 || len(e.sessions.ended) != 0 {
		t.Fatalf("linking started or ended a session, or linked %d", len(e.sessions.links))
	}
	l := e.sessions.links[0]
	if !bytes.Equal(l.SessionHash, authn.HashToken("fresh")) || l.Issuer != "https://accounts.google.com" || l.Subject != "220" || l.Provider != "google" ||
		!l.Now.Equal(now) {
		t.Errorf("the link: %+v", l)
	}
	if len(told) != 1 || told[0] != user.String()+" google" {
		t.Errorf("told: %v", told)
	}
	if len(e.sessions.emails) != 1 {
		t.Errorf("the linked sign-in's verified email was not recorded: %v", e.sessions.emails)
	}
}

// A provider a Super admin turned off is refused when a sign-in starts and when it comes back; one that accepts only
// some domains refuses an account whose verified email is on none of them.
// req: FR-ADM-08
// asvs: V6.8.1
func TestTheFlowFollowsTheProvidersSwitches(t *testing.T) {
	e := newFlow("http://localhost:8080")
	e.sessions.methods = []storage.SignInMethod{{Provider: "google", Enabled: false, Version: 1}}
	if loc := e.do(httptest.NewRequest(http.MethodGet, "/auth/signin?provider=google", nil)).Header.Get("Location"); loc != "/?signin_error="+signin.CodeUnknownProvider {
		t.Errorf("a sign-in with a provider turned off: %q", loc)
	}

	e.sessions.methods = []storage.SignInMethod{{Provider: "google", Enabled: true, AllowedDomains: []string{"cognizhi.com"}, Version: 2}}
	start := cookieNamed(e.do(httptest.NewRequest(http.MethodGet, "/auth/signin?provider=google", nil)), "fyniq_signin")
	callback := func() string {
		r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c-1&state="+e.provider.started.State, nil)
		r.AddCookie(start)
		return e.do(r).Header.Get("Location")
	}
	for name, who := range map[string]ports.SignedIn{
		"another domain":                       {Email: "mei@example.com", EmailVerified: true},
		"an email the provider did not verify": {Email: "mei@cognizhi.com"},
		"no email":                             {},
	} {
		who.Provider, who.Issuer, who.Subject, who.AuthTime, who.SecondFactor = "google", "https://accounts.google.com", "110", now, true
		e.provider.who = who
		if loc := callback(); loc != "/?signin_error="+signin.CodeDomain {
			t.Errorf("%s: %q", name, loc)
		}
	}
	e.provider.who = ports.SignedIn{Provider: "google", Issuer: "https://accounts.google.com", Subject: "110", AuthTime: now, SecondFactor: true,
		Email: "Mei@Cognizhi.com", EmailVerified: true}
	if loc := callback(); loc != "/" || len(e.sessions.started) != 1 {
		t.Errorf("an account on the domain: %q, %d sessions", loc, len(e.sessions.started))
	}
	e.sessions.methods[0].Enabled = false // turned off while the browser was at the provider
	if loc := callback(); loc != "/?signin_error="+signin.CodeUnknownProvider || len(e.sessions.started) != 1 {
		t.Errorf("a provider turned off during the sign-in: %q", loc)
	}
}

// Over https the cookies carry Secure and the __Host- prefix; an unknown provider is refused.
// req: SEC-05
// asvs: V3.3.1, V3.3.3
func TestOverHTTPSTheCookiesAreHostOnlyAndSecure(t *testing.T) {
	e := newFlow("https://fyniq.example.test")
	resp := e.do(httptest.NewRequest(http.MethodGet, "/auth/signin?provider=google", nil))
	c := cookieNamed(resp, "__Host-fyniq_signin")
	if c == nil || !c.Secure || c.Domain != "" || c.Path != "/" {
		t.Fatalf("the sign-in cookie over https: %v", resp.Cookies())
	}
	if authn.SessionCookie(e.flow.Secure()) != "__Host-fyniq_session" {
		t.Error("the session cookie over https lacks the __Host- prefix")
	}
	for _, provider := range []string{"github", "", "GOOGLE", "google.evil", "../google"} {
		if loc := e.do(httptest.NewRequest(http.MethodGet, "/auth/signin?provider="+url.QueryEscape(provider), nil)).Header.Get("Location"); loc != "/?signin_error="+signin.CodeUnknownProvider {
			t.Errorf("provider %q: %q", provider, loc)
		}
	}
}

// Signing out ends the session and clears the cookie, but only when the web app itself asks: another
// site cannot sign a person out (the cookie is ambient).
// req: SEC-05
// asvs: V7.4.1, V3.5.3
func TestSignOutEndsTheSessionOnlyFromTheWebApp(t *testing.T) {
	e := newFlow("http://localhost:8080")
	signOut := func(site string) *http.Response {
		r := httptest.NewRequest(http.MethodPost, "/auth/signout", nil)
		r.AddCookie(&http.Cookie{Name: "fyniq_session", Value: "tok"})
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		return e.do(r)
	}
	for _, site := range []string{"cross-site", "same-site", "none", ""} {
		if resp := signOut(site); resp.StatusCode != http.StatusForbidden {
			t.Errorf("a sign-out with Sec-Fetch-Site %q: %d", site, resp.StatusCode)
		}
	}
	if len(e.sessions.ended) != 0 {
		t.Fatal("a refused sign-out ended the session")
	}
	resp := signOut("same-origin")
	if c := cookieNamed(resp, "fyniq_session"); resp.StatusCode != http.StatusSeeOther || c == nil || c.MaxAge >= 0 {
		t.Fatalf("sign-out: %d %v", resp.StatusCode, resp.Cookies())
	}
	if len(e.sessions.ended) != 1 || !bytes.Equal(e.sessions.ended[0], authn.HashToken("tok")) {
		t.Error("sign-out did not end the session")
	}
}

// After signing in, the browser goes only to a path on Fyniq's own origin: never to another site.
// req: SEC-05
// asvs: V3.7.2
func TestTheReturnAddressStaysOnTheOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"/":                                  "/",
		"/journals?page=2":                   "/journals?page=2",
		"/api/auth/login?login_challenge=ab": "/api/auth/login?login_challenge=ab",
		"":                                   "/",
		"https://evil.test/":                 "/",
		"//evil.test/":                       "/",
		"/\\evil.test":                       "/",
		"\\\\evil.test":                      "/",
		"javascript:alert(1)":                "/",
		"journals":                           "/",
		"/x\r\nSet-Cookie: a=b":              "/",
		"/" + strings.Repeat("a", 3000):      "/",
	} {
		if got := signin.SafeReturn(in); got != want {
			t.Errorf("SafeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}
