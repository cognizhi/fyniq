package signin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of the flow's refusals (ENG-23). The browser lands on the web app with
// ?signin_error=<code>, which the web app explains in plain words.
const (
	CodeUnknownProvider = "AUTH-SIGNIN-01" // no such provider, or it is not enabled
	CodeExpired         = "AUTH-SIGNIN-02" // the sign-in took longer than 10 minutes, or began in another browser
	CodeRefused         = "AUTH-SIGNIN-03" // the provider's answer failed its checks (FR-ADM-08)
	CodeFailure         = "AUTH-SIGNIN-04" // the provider or the database did not answer
	CodeCrossSite       = "AUTH-SIGNIN-05" // a sign-out sent from another site
)

// startLifetime is how long a sign-in may take at the provider.
const startLifetime = 10 * time.Minute

var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Flow is the browser's way in and out: GET /auth/signin sends the browser to a provider, GET
// /auth/callback checks what came back and starts a session, POST /auth/signout ends it. The session is
// an opaque token in an HttpOnly cookie (SEC-05, SEC-08); the store keeps its SHA-256 only. Nothing the
// flow logs holds a token, a code or a cookie (SEC-15).
type Flow struct {
	SignIn   ports.SignIn
	Sessions storage.Sessions
	Clock    ports.Clock
	Origin   string // the web app's origin as the browser sees it: http://localhost:8080, https://…
	Base     string // where the API is mounted on that origin: /api
	Log      *slog.Logger
}

// Register adds the routes to mux.
func (f Flow) Register(mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}) {
	mux.HandleFunc("GET /auth/signin", f.start)
	mux.HandleFunc("GET /auth/callback", f.callback)
	mux.HandleFunc("POST /auth/signout", f.signOut)
}

// Secure reports whether the origin is https: the cookies then carry Secure and the __Host- prefix.
func (f Flow) Secure() bool { return strings.HasPrefix(f.Origin, "https://") }

func (f Flow) startCookie() string {
	if f.Secure() {
		return "__Host-fyniq_signin"
	}
	return "fyniq_signin"
}

func (f Flow) callbackURL() string { return f.Origin + f.Base + "/auth/callback" }

// Callback is where a provider sends the browser back: a provider registers it as the redirect URI.
func (f Flow) Callback() string { return f.callbackURL() }

func (f Flow) start(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if !f.known(provider) {
		f.fail(w, r, CodeUnknownProvider, "unknown provider", nil)
		return
	}
	seed := rand.Text() + rand.Text() // 260 random bits
	to, err := f.SignIn.Start(r.Context(), provider, f.callbackURL(), secrets(seed))
	if err != nil {
		f.fail(w, r, CodeFailure, "start", err)
		return
	}
	value := seed + "." + provider + "." + base64.RawURLEncoding.EncodeToString([]byte(SafeReturn(r.URL.Query().Get("return_to"))))
	http.SetCookie(w, f.cookie(f.startCookie(), value, startLifetime))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (f Flow) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c, err := r.Cookie(f.startCookie())
	http.SetCookie(w, f.cookie(f.startCookie(), "", -1)) // used once, whatever happens
	if err != nil {
		f.fail(w, r, CodeExpired, "no sign-in in this browser", nil)
		return
	}
	seed, rest, _ := strings.Cut(c.Value, ".")
	provider, encoded, _ := strings.Cut(rest, ".")
	back, err := base64.RawURLEncoding.DecodeString(encoded)
	if seed == "" || err != nil || !f.known(provider) {
		f.fail(w, r, CodeExpired, "the sign-in cookie is not ours", nil)
		return
	}
	cb, err := url.Parse(f.callbackURL())
	if err != nil {
		f.fail(w, r, CodeFailure, "callback", err)
		return
	}
	cb.RawQuery = r.URL.RawQuery
	who, err := f.SignIn.Finish(r.Context(), provider, cb, secrets(seed))
	if errors.Is(err, ports.ErrSignInRefused) {
		f.fail(w, r, CodeRefused, "refused", err)
		return
	}
	if err != nil {
		f.fail(w, r, CodeFailure, "finish", err)
		return
	}
	now := f.Clock.Now()
	var stepUp time.Time
	if who.SecondFactor && !who.AuthTime.After(now) {
		stepUp = who.AuthTime
	}
	token := rand.Text() + rand.Text()
	if err := f.Sessions.StartSession(r.Context(), storage.NewSession{TokenHash: authn.HashToken(token), Issuer: who.Issuer,
		Subject: who.Subject, Provider: who.Provider, StepUpAt: stepUp, Now: now, Expires: now.Add(authn.SessionLifetime)}); err != nil {
		f.fail(w, r, CodeFailure, "session", err)
		return
	}
	f.endCurrent(r) // a new sign-in replaces the browser's session, never adds to it
	http.SetCookie(w, f.cookie(authn.SessionCookie(f.Secure()), token, authn.SessionLifetime))
	f.log().Info("auth.signed_in", "provider", who.Provider, "issuer", who.Issuer, "subject", who.Subject,
		"second_factor", who.SecondFactor)
	http.Redirect(w, r, SafeReturn(string(back)), http.StatusSeeOther)
}

func (f Flow) signOut(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !authn.SameOrigin(r.Header) {
		writeProblem(w, http.StatusForbidden, CodeCrossSite, "Sign-out was sent from another site.",
			"Sign out from Fyniq itself.")
		return
	}
	f.endCurrent(r)
	http.SetCookie(w, f.cookie(authn.SessionCookie(f.Secure()), "", -1))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// endCurrent ends the session the request's cookie names, if any.
func (f Flow) endCurrent(r *http.Request) {
	token := authn.CookieValue(r.Header.Get("Cookie"), authn.SessionCookie(f.Secure()))
	if token == "" {
		return
	}
	if err := f.Sessions.EndSession(r.Context(), authn.HashToken(token), f.Clock.Now()); err != nil {
		f.log().Error("auth.signout.failure", "error", err.Error())
	}
}

func (f Flow) known(provider string) bool {
	if !providerID.MatchString(provider) {
		return false
	}
	for _, p := range f.SignIn.Providers() {
		if p.ID == provider {
			return true
		}
	}
	return false
}

// cookie is one of the flow's cookies: HttpOnly, on every path, SameSite=Lax (so it comes back with
// the provider's redirect, a top-level navigation), Secure over https. A negative maxAge deletes it.
func (f Flow) cookie(name, value string, maxAge time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: f.Secure(), SameSite: http.SameSiteLaxMode,
		MaxAge: int(maxAge / time.Second)}
	if maxAge < 0 {
		c.MaxAge = -1
	}
	return c
}

// fail sends the browser to the web app with the refusal's code, and logs why without secrets.
func (f Flow) fail(w http.ResponseWriter, r *http.Request, code, why string, err error) {
	attrs := []any{"code", code, "why", why}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	f.log().Warn("auth.signin.refused", attrs...)
	http.Redirect(w, r, "/?signin_error="+code, http.StatusSeeOther)
}

func (f Flow) log() *slog.Logger {
	if f.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return f.Log
}

// secrets derives the sign-in's state, nonce and PKCE verifier from the random seed in the browser's
// cookie. The state travels in URLs; the seed and the verifier never do, and no derived value reveals
// another (each is a SHA-256 of a different label and the seed).
func secrets(seed string) ports.SignInSecrets {
	derive := func(label string) string {
		sum := sha256.Sum256([]byte(label + ":" + seed))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return ports.SignInSecrets{State: derive("state"), Nonce: derive("nonce"), CodeVerifier: derive("pkce")}
}

// SafeReturn returns where to send the browser after signing in: a path on this origin, or "/". An
// absolute URL, a scheme-relative "//host" or a backslash trick would send it elsewhere (an open
// redirect), so they become "/".
func SafeReturn(s string) string {
	if s == "" || len(s) > 2048 || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.ContainsAny(s, "\\\r\n\t") {
		return "/"
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return s
}

func writeProblem(w http.ResponseWriter, status int, code, detail, fix string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:fyniq:problem:" + code, "title": http.StatusText(status),
		"status": status, "detail": detail, "code": code, "fix": fix})
}
