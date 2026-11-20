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
	joined      []string // what FirstRun and AcceptInvitation were given
	joinErr     error
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
func (s *fakeStore) StartSession(_ context.Context, _, _ uuid.UUID, _ time.Time, _ identity.Via) (string, error) {
	s.started++
	return "tok-" + uuid.NewString(), s.startErr
}
func (s *fakeStore) EndSession(_ context.Context, token string, _ time.Time) error {
	s.ended = append(s.ended, token)
	return nil
}

func (s *fakeStore) FirstRun(_ context.Context, code []byte, _ time.Time, organisation, name string, who identity.Identity) (uuid.UUID, uuid.UUID, error) {
	s.joined = append(s.joined, "setup:"+string(code)+":"+organisation+":"+name+":"+who.Subject)
	return uuid.New(), uuid.New(), s.joinErr
}
func (s *fakeStore) AcceptInvitation(_ context.Context, code string, _ time.Time, who identity.Identity) (uuid.UUID, uuid.UUID, error) {
	s.joined = append(s.joined, "invitation:"+code+":"+who.Subject)
	return uuid.New(), uuid.New(), s.joinErr
}

func (s *fakeStore) RecoverOwner(_ context.Context, code string, _ time.Time, who identity.Identity) (uuid.UUID, uuid.UUID, error) {
	s.joined = append(s.joined, "recovery:"+code+":"+who.Subject)
	return uuid.New(), uuid.New(), s.joinErr
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

// join posts the sign-in form's code and returns the browser's next request, the one that starts the sign-in with the code's cookie.
func join(t *testing.T, mux *http.ServeMux, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/join", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// A person takes a place with a code: the invitation's, or the deployment's one-time setup code. The code waits in an HttpOnly cookie, never in
// a URL, and the callback uses it once with the identity the provider proved: the first organisation's Owner, or the invited member. Wrong
// forms, wrong codes and a code from another site are refused in plain codes, with no session (NM-ARC-01).
// req: NM-ARC-01
// asvs: V7.2.1, V3.3.1, V2.1.1
func TestACodeTakesAPlaceWithTheIdentityTheProviderProved(t *testing.T) {
	f, si, st, mux := newFlow("http://localhost:8180")
	st.memberships, st.done = nil, false
	rec := join(t, mux, url.Values{"provider": {"local"}, "kind": {"setup"}, "code": {" setup-code "}, "organisation": {"Acme Pte Ltd"}, "name": {"Sam"}, "return_to": {"/me"}})
	c := cookieOf(rec, f.joinCookie())
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(joinLifetime/time.Second) || loc != "/api/auth/signin?provider=local&return_to=%2Fme" {
		t.Fatalf("join: %d %+v %s", rec.Code, c, loc)
	}
	if strings.Contains(loc, "setup-code") {
		t.Error("the code is in a URL")
	}
	// The browser starts the sign-in, comes back with the join cookie, and the first organisation is made with the proved identity.
	cb := begin(t, mux, si, f, "/me")
	cb.AddCookie(c)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, cb)
	if rec.Header().Get("Location") != "/me" || cookieOf(rec, "nemura_session") == nil || st.started != 1 {
		t.Fatalf("callback: %s %v %d", rec.Header().Get("Location"), cookieOf(rec, "nemura_session"), st.started)
	}
	if len(st.joined) != 1 || st.joined[0] != "setup:"+string(identity.Hash("setup-code"))+":Acme Pte Ltd:Sam:sam" {
		t.Errorf("first run was given %v", st.joined)
	}
	if jc := cookieOf(rec, f.joinCookie()); jc == nil || jc.MaxAge != -1 {
		t.Error("the code is not used once")
	}

	// An invitation links the identity to the invited member, even for someone who belongs elsewhere already.
	st.memberships, st.joined = []Membership{{uuid.New(), uuid.New()}}, nil
	rec = join(t, mux, url.Values{"provider": {"local"}, "kind": {"invitation"}, "code": {"inv-1"}})
	cb = begin(t, mux, si, f, "/")
	cb.AddCookie(cookieOf(rec, f.joinCookie()))
	mux.ServeHTTP(httptest.NewRecorder(), cb)
	if len(st.joined) != 1 || st.joined[0] != "invitation:inv-1:sam" {
		t.Errorf("accepting an invitation was given %v", st.joined)
	}

	// A recovery code takes the member's place with the account signed in, even for an account that belongs elsewhere already.
	st.joined = nil
	rec = join(t, mux, url.Values{"provider": {"local"}, "kind": {"recovery"}, "code": {"rec-1"}})
	cb = begin(t, mux, si, f, "/")
	cb.AddCookie(cookieOf(rec, f.joinCookie()))
	mux.ServeHTTP(httptest.NewRecorder(), cb)
	if len(st.joined) != 1 || st.joined[0] != "recovery:rec-1:sam" {
		t.Errorf("a recovery was given %v", st.joined)
	}

	// Setup for an account that belongs to an organisation is refused before the database is asked.
	st.joined = nil
	rec = join(t, mux, url.Values{"provider": {"local"}, "kind": {"setup"}, "code": {"c"}, "organisation": {"X"}, "name": {"Y"}})
	cb = begin(t, mux, si, f, "/")
	cb.AddCookie(cookieOf(rec, f.joinCookie()))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, cb)
	if rec.Header().Get("Location") != "/sign-in?signin_error="+CodeJoinRefused || len(st.joined) != 0 {
		t.Errorf("setup by someone who belongs: %s %v", rec.Header().Get("Location"), st.joined)
	}

	// A wrong code is the database's one answer, a failing database is another, and neither leaves a session.
	st.memberships, st.started = nil, 0
	for err, want := range map[error]string{identity.Refused(): CodeJoinRefused, errors.New("the database is down"): CodeFailure} {
		st.joinErr = err
		rec = join(t, mux, url.Values{"provider": {"local"}, "kind": {"invitation"}, "code": {"nope"}})
		cb = begin(t, mux, si, f, "/")
		cb.AddCookie(cookieOf(rec, f.joinCookie()))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, cb)
		if rec.Header().Get("Location") != "/sign-in?signin_error="+want || cookieOf(rec, "nemura_session") != nil || st.started != 0 {
			t.Errorf("%v: %s", err, rec.Header().Get("Location"))
		}
	}
	st.joinErr = nil

	// Incomplete forms, an unknown provider and a form from another site set no cookie.
	for name, form := range map[string]url.Values{
		"no code":           {"provider": {"local"}, "kind": {"invitation"}},
		"no kind":           {"provider": {"local"}, "code": {"c"}},
		"setup with no org": {"provider": {"local"}, "kind": {"setup"}, "code": {"c"}, "name": {"Y"}},
		"unknown provider":  {"provider": {"nope"}, "kind": {"invitation"}, "code": {"c"}},
		"a long code":       {"provider": {"local"}, "kind": {"invitation"}, "code": {strings.Repeat("x", 201)}},
	} {
		rec = join(t, mux, form)
		if rec.Header().Get("Location") != "/sign-in?signin_error="+CodeJoinRefused || cookieOf(rec, f.joinCookie()) != nil {
			t.Errorf("%s: %s", name, rec.Header().Get("Location"))
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/join", strings.NewReader("provider=local&kind=invitation&code=c"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Header().Get("Location") != "/sign-in?signin_error="+CodeJoinRefused || cookieOf(rec, f.joinCookie()) != nil {
		t.Errorf("another site: %s", rec.Header().Get("Location"))
	}
	// A forged or truncated cookie is not a join.
	cb = begin(t, mux, si, f, "/")
	cb.AddCookie(&http.Cookie{Name: f.joinCookie(), Value: "not-base64-json"})
	st.joined = nil
	mux.ServeHTTP(httptest.NewRecorder(), cb)
	if len(st.joined) != 0 {
		t.Errorf("a forged join cookie was used: %v", st.joined)
	}
}
