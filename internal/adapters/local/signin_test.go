//go:build local

package local

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/signin"
)

const devCallback = "http://localhost:8080/api/auth/callback"

var (
	devGoogle = signin.Provider{ID: "google", Name: "Google (local)", Issuer: "urn:fyniq:local-dev:google", ClientID: "fyniq-web"}
	devZoho   = signin.Provider{ID: "zoho", Name: "Zoho (local)", Issuer: "urn:fyniq:local-dev:zoho", ClientID: "fyniq-web"}
)

type devPage struct {
	t     *testing.T
	d     *DevSignIn
	mux   *http.ServeMux
	clock *clock.Frozen
}

func newDevPage(t *testing.T) *devPage {
	t.Helper()
	c := clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	d, err := NewDevSignIn([]signin.Provider{devGoogle, devZoho}, "/api", devCallback, c)
	if err != nil {
		t.Fatal(err)
	}
	p := &devPage{t: t, d: d, mux: http.NewServeMux(), clock: c}
	d.Register(p.mux)
	return p
}

func secretsFor(n string) ports.SignInSecrets {
	return ports.SignInSecrets{State: "state-" + n, Nonce: "nonce-" + n, CodeVerifier: strings.Repeat(n, 43)[:43]}
}

// signIn starts with provider, fills the page as subject and returns the callback the browser reaches.
func (p *devPage) signIn(provider, subject string, s ports.SignInSecrets, secondFactor bool, decision string) *url.URL {
	p.t.Helper()
	start, err := p.d.Start(context.Background(), provider, devCallback, s)
	if err != nil {
		p.t.Fatal(err)
	}
	u, _ := url.Parse(start)
	if u.Path != "/api/auth/dev/authorize" {
		p.t.Fatalf("the page is at %s", u.Path)
	}
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/dev/authorize?"+u.RawQuery, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Local mode only") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		p.t.Fatalf("the page: %d %s", w.Code, w.Body)
	}
	form := u.Query()
	form.Set("subject", subject)
	form.Set("name", "Mei Ling Tan")
	form.Set("decision", decision)
	if secondFactor {
		form.Set("second_factor", "yes")
	}
	r := httptest.NewRequest(http.MethodPost, "/auth/dev/authorize", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	p.mux.ServeHTTP(w, r)
	back, err := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusSeeOther || err != nil || !strings.HasPrefix(back.String(), devCallback+"?") {
		p.t.Fatalf("the page sent the browser to %q (%d)", w.Header().Get("Location"), w.Code)
	}
	return back
}

// The local sign-in is an OpenID Connect issuer that checks what a provider checks: the registered
// redirect URI, the state, a one-time code bound to PKCE, and an ID token whose issuer is the
// provider's own. A code from one provider finished as another is refused (FR-ADM-08).
// req: FR-ADM-08
// req: SEC-05
// asvs: V10.2.1, V10.5.1, V10.5.3
func TestTheLocalSignInChecksWhatAProviderChecks(t *testing.T) {
	p := newDevPage(t)
	ctx := context.Background()
	s := secretsFor("a")
	cb := p.signIn("google", "founder-a", s, true, "sign-in")
	who, err := p.d.Finish(ctx, "google", cb, s)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if who.Provider != "google" || who.Issuer != devGoogle.Issuer || who.Subject != "founder-a" || who.Name != "Mei Ling Tan" || !who.SecondFactor ||
		!who.AuthTime.Equal(p.clock.Now()) {
		t.Fatalf("signed in as %+v", who)
	}
	if _, err := p.d.Finish(ctx, "google", cb, s); !errors.Is(err, ports.ErrSignInRefused) {
		t.Errorf("a code used twice: %v", err)
	}

	refused := map[string]func() error{
		"another browser's state": func() error {
			s := secretsFor("b")
			cb := p.signIn("google", "founder-a", s, false, "sign-in")
			other := s
			other.State = "state-x"
			_, err := p.d.Finish(ctx, "google", cb, other)
			return err
		},
		"another code verifier (PKCE)": func() error {
			s := secretsFor("c")
			cb := p.signIn("google", "founder-a", s, false, "sign-in")
			other := s
			other.CodeVerifier = strings.Repeat("z", 43)
			_, err := p.d.Finish(ctx, "google", cb, other)
			return err
		},
		"another sign-in's nonce": func() error {
			s := secretsFor("d")
			cb := p.signIn("google", "founder-a", s, false, "sign-in")
			other := s
			other.Nonce = "nonce-x"
			_, err := p.d.Finish(ctx, "google", cb, other)
			return err
		},
		"a Zoho sign-in finished as Google's": func() error {
			s := secretsFor("e")
			cb := p.signIn("zoho", "founder-a", s, false, "sign-in")
			_, err := p.d.Finish(ctx, "google", cb, s)
			return err
		},
		"an expired code": func() error {
			s := secretsFor("f")
			cb := p.signIn("google", "founder-a", s, false, "sign-in")
			p.clock.Advance(codeLifetime)
			_, err := p.d.Finish(ctx, "google", cb, s)
			return err
		},
		"a cancelled sign-in": func() error {
			s := secretsFor("g")
			cb := p.signIn("google", "founder-a", s, false, "cancel")
			_, err := p.d.Finish(ctx, "google", cb, s)
			return err
		},
	}
	for name, try := range refused {
		if err := try(); !errors.Is(err, ports.ErrSignInRefused) {
			t.Errorf("%s: not refused (%v)", name, err)
		}
	}

	start, _ := p.d.Start(ctx, "google", devCallback, secretsFor("h"))
	u, _ := url.Parse(start)
	for name, change := range map[string]func(q url.Values){
		"another redirect URI":  func(q url.Values) { q.Set("redirect_uri", "https://evil.test/cb") },
		"another client":        func(q url.Values) { q.Set("client_id", "someone-else") },
		"an unknown provider":   func(q url.Values) { q.Set("provider", "github") },
		"no PKCE":               func(q url.Values) { q.Del("code_challenge") },
		"PKCE in plain":         func(q url.Values) { q.Set("code_challenge_method", "plain") },
		"no state":              func(q url.Values) { q.Del("state") },
		"no nonce":              func(q url.Values) { q.Del("nonce") },
		"a subject over limits": func(q url.Values) { q.Set("subject", strings.Repeat("x", 201)); q.Set("decision", "sign-in") },
	} {
		q := u.Query()
		change(q)
		r := httptest.NewRequest(http.MethodPost, "/auth/dev/authorize", strings.NewReader(q.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		p.mux.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Header().Get("Location"))
		}
	}
	if _, err := p.d.Start(ctx, "github", devCallback, secretsFor("i")); !errors.Is(err, ports.ErrUnknownProvider) {
		t.Errorf("an unknown provider started: %v", err)
	}
}
