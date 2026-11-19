package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/ports"
)

// fakeSignIn is a provider that signs in whoever the test says, and checks the secrets the flow derives.
type fakeSignIn struct {
	who    ports.SignedIn
	err    error
	secret ports.SignInSecrets
}

func (f *fakeSignIn) Providers() []ports.SignInProvider {
	return []ports.SignInProvider{{ID: "local", Name: "Local"}}
}
func (f *fakeSignIn) Start(_ context.Context, _, callback string, s ports.SignInSecrets) (string, error) {
	f.secret = s
	return "https://idp.example/authorize?state=" + url.QueryEscape(s.State) + "&redirect_uri=" + url.QueryEscape(callback), nil
}
func (f *fakeSignIn) Finish(_ context.Context, _ string, cb *url.URL, s ports.SignInSecrets) (ports.SignedIn, error) {
	if f.err != nil {
		return ports.SignedIn{}, f.err
	}
	if cb.Query().Get("state") != s.State {
		return ports.SignedIn{}, ports.ErrSignInRefused
	}
	return f.who, nil
}

type fakeStore struct {
	done        bool
	memberships []Membership
	started     int
	ended       []string
	startErr    error
	got         identity.Identity
}

func (s *fakeStore) SetupDone(context.Context) (bool, error) { return s.done, nil }
func (s *fakeStore) MembershipsOf(_ context.Context, who identity.Identity) ([]Membership, error) {
	s.got = who
	return s.memberships, nil
}
func (s *fakeStore) StartSession(_ context.Context, _, _ uuid.UUID, _ time.Time) (string, error) {
	s.started++
	return "tok-" + uuid.NewString(), s.startErr
}
func (s *fakeStore) EndSession(_ context.Context, token string, _ time.Time) error {
	s.ended = append(s.ended, token)
	return nil
}

func newFlow(origin string) (Flow, *fakeSignIn, *fakeStore, *http.ServeMux) {
	si := &fakeSignIn{who: ports.SignedIn{Provider: "local", Issuer: "https://idp.example", Subject: "sam", Email: "sam@example.com", EmailVerified: true}}
	st := &fakeStore{done: true, memberships: []Membership{{uuid.New(), uuid.New()}}}
	f := Flow{SignIn: si, Store: st, Clock: clock.NewFrozen(time.Date(2099, 1, 1, 9, 0, 0, 0, time.UTC)), Origin: origin, Base: "/api", Tenant: "t1"}
	mux := http.NewServeMux()
	f.Register(mux)
	return f, si, st, mux
}

func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// begin starts a sign-in and returns the request the provider would send back: the callback with the state the flow derived.
func begin(t *testing.T, mux *http.ServeMux, si *fakeSignIn, f Flow, returnTo string) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/signin?provider=local&return_to="+url.QueryEscape(returnTo), nil))
	c := cookieOf(rec, f.startCookie())
	if rec.Code != http.StatusSeeOther || c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || !strings.HasPrefix(rec.Header().Get("Location"), "https://idp.example/authorize") {
		t.Fatalf("start: %d, %v, %s", rec.Code, c, rec.Header().Get("Location"))
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state="+url.QueryEscape(si.secret.State), nil)
	req.AddCookie(c)
	return req
}

// A sign-in goes to the provider and back: it matches the person by issuer, subject and tenant (never email), starts a session in an
// HttpOnly cookie on Nemura's own origin, and sends the browser to where it was going. It needs no Fyniq (NM-ARC-01).
// req: NM-ARC-01
// asvs: V7.2.1, V3.3.1
func TestASignInMatchesByIssuerSubjectAndTenantAndStartsASession(t *testing.T) {
	f, si, st, mux := newFlow("http://localhost:8180")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, begin(t, mux, si, f, "/me/pay"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/me/pay" {
		t.Fatalf("callback: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	c := cookieOf(rec, "nemura_session")
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || !strings.HasPrefix(c.Value, "tok-") || c.MaxAge != int(identity.SessionTTL/time.Second) {
		t.Fatalf("the session cookie: %+v", c)
	}
	if st.got != (identity.Identity{Issuer: "https://idp.example", Subject: "sam", Tenant: "t1"}) {
		t.Errorf("the identity looked up: %+v (the email must not be part of it)", st.got)
	}
	if sc := cookieOf(rec, f.startCookie()); sc == nil || sc.MaxAge != -1 {
		t.Error("the start cookie is not used once")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the callback may be cached")
	}
}

// Over https the cookies are Secure with the __Host- prefix and are Nemura's own, never Fyniq's.
// asvs: V3.3.1, V3.3.2
func TestOverHTTPSTheCookiesAreHostPrefixedAndNemuras(t *testing.T) {
	f, si, _, mux := newFlow("https://people.example.com")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, begin(t, mux, si, f, "/"))
	c := cookieOf(rec, "__Host-nemura_session")
	if c == nil || !c.Secure || cookieOf(rec, "fyniq_session") != nil || strings.Contains(SessionCookie(true), "fyniq") {
		t.Errorf("the session cookie over https: %+v", c)
	}
}

// A return that the browser did not start, a code with the wrong state, a provider that is not ours, a sign-in the provider refused, and
// an account that signs in nowhere each end at the sign-in page with a stable code, and no session is started.
// asvs: V7.2.1, V10.1.1
func TestEveryFailureEndsAtTheSignInPageWithItsCodeAndNoSession(t *testing.T) {
	landing := func(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Location") }
	f, si, st, mux := newFlow("http://localhost:8180")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=x", nil)) // no start cookie
	if landing(rec) != "/sign-in?signin_error="+CodeExpired {
		t.Errorf("a return with no sign-in in this browser: %s", landing(rec))
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/signin?provider=nope", nil))
	if landing(rec) != "/sign-in?signin_error="+CodeUnknownProvider {
		t.Errorf("an unknown provider: %s", landing(rec))
	}
	req := begin(t, mux, si, f, "/")
	req.URL.RawQuery = "code=c&state=forged"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if landing(rec) != "/sign-in?signin_error="+CodeRefused {
		t.Errorf("a forged state: %s", landing(rec))
	}
	si.err = errors.New("the provider is down")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, begin(t, mux, si, f, "/"))
	if landing(rec) != "/sign-in?signin_error="+CodeFailure {
		t.Errorf("a provider that did not answer: %s", landing(rec))
	}
	si.err = nil
	st.memberships = nil
	st.done = false
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, begin(t, mux, si, f, "/"))
	if landing(rec) != "/sign-in?signin_error="+CodeNeedsSetup {
		t.Errorf("before setup: %s", landing(rec))
	}
	st.done = true
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, begin(t, mux, si, f, "/"))
	if landing(rec) != "/sign-in?signin_error="+CodeNoMembership {
		t.Errorf("an account that signs in nowhere: %s", landing(rec))
	}
	st.memberships = []Membership{{uuid.New(), uuid.New()}}
	st.startErr = errors.New("deactivated")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, begin(t, mux, si, f, "/"))
	if landing(rec) != "/sign-in?signin_error="+CodeNoMembership || cookieOf(rec, "nemura_session") != nil {
		t.Errorf("a deactivated member: %s, %v", landing(rec), cookieOf(rec, "nemura_session"))
	}
}

// Where the browser goes after signing in is a path on this site: an absolute URL, a scheme-relative one and backslash tricks become "/".
// asvs: V3.7.1
func TestTheReturnAddressIsNeverAnOpenRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/me/pay": "/me/pay", "": "/", "https://evil.example/": "/", "//evil.example": "/", "/\\evil.example": "/", "javascript:alert(1)": "/",
		"/ok\r\nSet-Cookie: x=1": "/", "/a?b=c#d": "/a?b=c#d",
	} {
		if got := SafeReturn(in); got != want {
			t.Errorf("SafeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}

// Signing out ends the session the cookie names and clears the cookie; from another site it is refused (no logout CSRF).
// asvs: V7.4.1, V3.5.1
func TestSigningOutEndsTheSessionAndRefusesAnotherSite(t *testing.T) {
	_, _, st, mux := newFlow("http://localhost:8180")
	out := func(site string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/signout", nil)
		req.AddCookie(&http.Cookie{Name: "nemura_session", Value: "tok-1"})
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := out("cross-site"); rec.Code != http.StatusForbidden || len(st.ended) != 0 {
		t.Errorf("from another site: %d, ended %v", rec.Code, st.ended)
	}
	rec := out("same-origin")
	if c := cookieOf(rec, "nemura_session"); rec.Code != http.StatusSeeOther || c == nil || c.MaxAge != -1 || len(st.ended) != 1 || st.ended[0] != "tok-1" {
		t.Errorf("signing out: %d, %v, ended %v", rec.Code, c, st.ended)
	}
}
