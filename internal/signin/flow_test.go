package signin_test

import (
	"bytes"
	"context"
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
}

func (f *fakeSessions) StartSession(_ context.Context, s storage.NewSession) error {
	f.started = append(f.started, s)
	return nil
}

func (f *fakeSessions) Session(context.Context, []byte, time.Time, time.Duration) (storage.Session, error) {
	return storage.Session{}, ports.ErrNotFound
}

func (f *fakeSessions) EndSession(_ context.Context, h []byte, _ time.Time) error {
	f.ended = append(f.ended, h)
	return nil
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
	e.provider.who = ports.SignedIn{Provider: "google", Issuer: "https://accounts.google.com", Subject: "110", AuthTime: now.Add(-time.Minute), SecondFactor: true}
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

	e.provider.who.SecondFactor = false
	callback(start)
	if !e.sessions.started[1].StepUpAt.IsZero() {
		t.Error("a sign-in without a second factor counted as a step-up (FR-ADM-07)")
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
