// Package auth is the browser's way in and out of Nemura (PRD-002 NM-ARC-01, NM-ARC-06, ADR-N3): GET /auth/signin sends the browser to a
// sign-in provider, GET /auth/callback checks what came back and starts a session, POST /auth/signout ends it. It works with no Fyniq:
// the provider is the identity provider's (Identity Platform in the cloud, the local sign-in page locally, through the shared SignIn port),
// and who signed in is matched by issuer, subject and tenant, never by email. The session is an opaque token in an HttpOnly cookie on
// Nemura's own origin; the database keeps its SHA-256 only. Nothing here logs a token, a code or a cookie (SEC-15).
package auth

import (
	"context"
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

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The stable codes of the refusals. The browser lands on the web app with ?signin_error=<code>, which the web app explains in plain words.
const (
	CodeUnknownProvider = "NM_SIGNIN_01" // no such provider
	CodeExpired         = "NM_SIGNIN_02" // the sign-in took longer than 10 minutes, or began in another browser
	CodeRefused         = "NM_SIGNIN_03" // the provider's answer failed its checks
	CodeFailure         = "NM_SIGNIN_04" // the provider or the database did not answer
	CodeNoMembership    = "NM_SIGNIN_05" // the account signs in to no organisation here, and setup is done
	CodeCrossSite       = "NM_SIGNIN_06" // a sign-out sent from another site
	CodeNeedsSetup      = "NM_SIGNIN_07" // first-run setup is not done: the web app shows its form
	CodeJoinRefused     = "NM_SIGNIN_08" // the invitation or setup code is wrong, used or expired, or the form was incomplete
)

// joinLifetime is how long a code waits in the browser while the person signs in at the provider.
const joinLifetime = 15 * time.Minute

// Join is what a person brings to the sign-in to take a place in an organisation: the one-time setup code that makes the first
// organisation with them as its Owner, or an invitation's code. It travels in an HttpOnly cookie, never in a URL, and the callback uses
// it once, whatever happens, with the identity the provider proved.
type Join struct {
	Kind         string `json:"k"` // "setup" or "invitation"
	Code         string `json:"c"`
	Organisation string `json:"o,omitempty"` // setup: the organisation's name
	Name         string `json:"n,omitempty"` // setup: the Owner's display name
}

const (
	JoinSetup      = "setup"
	JoinInvitation = "invitation"
)

// startLifetime is how long a sign-in may take at the provider.
const startLifetime = 10 * time.Minute

var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Membership is one organisation a person signs in to.
type Membership struct{ Organisation, Member uuid.UUID }

// Store is what the flow needs of Nemura's database.
type Store interface {
	SetupDone(ctx context.Context) (bool, error)
	MembershipsOf(ctx context.Context, who identity.Identity) ([]Membership, error)
	// StartSession opens a session for a member and returns its token (only its hash is kept).
	StartSession(ctx context.Context, org, member uuid.UUID, now time.Time) (string, error)
	// EndSession ends the session a token opens, if any.
	EndSession(ctx context.Context, token string, now time.Time) error
	// FirstRun makes the first organisation and its Owner with the setup code's hash; AcceptInvitation links the identity to the invited
	// member. Either refuses with one answer for every wrong, used or expired code.
	FirstRun(ctx context.Context, code []byte, at time.Time, organisation, displayName string, who identity.Identity) (org, owner uuid.UUID, err error)
	AcceptInvitation(ctx context.Context, code string, at time.Time, who identity.Identity) (org, member uuid.UUID, err error)
}

// Flow is the browser's way in and out.
type Flow struct {
	SignIn ports.SignIn
	Store  Store
	Clock  ports.Clock
	Origin string // Nemura's origin as the browser sees it: http://localhost:8180, https://people.<domain>
	Base   string // where the API is mounted on that origin: /api
	// Tenant is the identity provider's tenant for the people who sign in here: one per organisation in the cloud (question 2), so
	// the identity is (issuer, subject, tenant). Locally it is one value for the whole local stack.
	Tenant string
	Log    *slog.Logger
}

// Register adds the routes to mux.
func (f Flow) Register(mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}) {
	mux.HandleFunc("GET /auth/signin", f.start)
	mux.HandleFunc("POST /auth/join", f.join)
	mux.HandleFunc("GET /auth/callback", f.callback)
	mux.HandleFunc("POST /auth/signout", f.signOut)
}

// Secure reports whether the origin is https: the cookies then carry Secure and the __Host- prefix.
func (f Flow) Secure() bool { return strings.HasPrefix(f.Origin, "https://") }

// SessionCookie is the name of the session cookie on Nemura's own origin, never Fyniq's.
func SessionCookie(secure bool) string {
	if secure {
		return "__Host-nemura_session"
	}
	return "nemura_session"
}

func (f Flow) joinCookie() string {
	if f.Secure() {
		return "__Host-nemura_join"
	}
	return "nemura_join"
}

func (f Flow) startCookie() string {
	if f.Secure() {
		return "__Host-nemura_signin"
	}
	return "nemura_signin"
}

func (f Flow) callbackURL() string { return f.Origin + f.Base + "/auth/callback" }

func (f Flow) start(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if !f.known(provider) {
		f.fail(w, r, CodeUnknownProvider, "unknown provider", nil)
		return
	}
	back := SafeReturn(r.URL.Query().Get("return_to"))
	seed := rand.Text() + rand.Text() // 260 random bits
	to, err := f.SignIn.Start(r.Context(), provider, f.callbackURL(), secrets(seed))
	if err != nil {
		f.fail(w, r, CodeFailure, "start", err)
		return
	}
	http.SetCookie(w, f.cookie(f.startCookie(), seed+"."+provider+"."+base64.RawURLEncoding.EncodeToString([]byte(back)), startLifetime))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// join takes a setup or invitation code from the sign-in form into a cookie and goes on to the provider. A code that is the wrong shape
// is refused here; whether it is right is only known with the identity, at the callback.
func (f Flow) join(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		f.fail(w, r, CodeJoinRefused, "join sent from another site", nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		f.fail(w, r, CodeJoinRefused, "unreadable form", nil)
		return
	}
	j := Join{Kind: r.PostForm.Get("kind"), Code: strings.TrimSpace(r.PostForm.Get("code")), Organisation: strings.TrimSpace(r.PostForm.Get("organisation")), Name: strings.TrimSpace(r.PostForm.Get("name"))}
	bad := j.Code == "" || len(j.Code) > 200 || len(j.Organisation) > 200 || len(j.Name) > 200
	switch j.Kind {
	case JoinSetup:
		bad = bad || j.Organisation == "" || j.Name == ""
	case JoinInvitation:
	default:
		bad = true
	}
	provider := r.PostForm.Get("provider")
	if bad || !f.known(provider) {
		f.fail(w, r, CodeJoinRefused, "incomplete join", nil)
		return
	}
	raw, _ := json.Marshal(j)
	http.SetCookie(w, f.cookie(f.joinCookie(), base64.RawURLEncoding.EncodeToString(raw), joinLifetime))
	http.Redirect(w, r, f.Base+"/auth/signin?provider="+url.QueryEscape(provider)+"&return_to="+url.QueryEscape(SafeReturn(r.PostForm.Get("return_to"))), http.StatusSeeOther)
}

// joinOf reads the join cookie, if there is one and it is ours.
func (f Flow) joinOf(r *http.Request) *Join {
	c, err := r.Cookie(f.joinCookie())
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	var j Join
	if err != nil || json.Unmarshal(raw, &j) != nil || j.Code == "" || (j.Kind != JoinSetup && j.Kind != JoinInvitation) {
		return nil
	}
	return &j
}

func (f Flow) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c, err := r.Cookie(f.startCookie())
	http.SetCookie(w, f.cookie(f.startCookie(), "", -1)) // used once, whatever happens
	joining := f.joinOf(r)
	if joining != nil {
		http.SetCookie(w, f.cookie(f.joinCookie(), "", -1)) // a code is used once, whatever happens
	}
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
	signed, err := f.SignIn.Finish(r.Context(), provider, cb, secrets(seed))
	if errors.Is(err, ports.ErrSignInRefused) {
		f.fail(w, r, CodeRefused, "refused", err)
		return
	}
	if err != nil {
		f.fail(w, r, CodeFailure, "finish", err)
		return
	}
	who := identity.Identity{Issuer: signed.Issuer, Subject: signed.Subject, Tenant: f.Tenant}
	memberships, err := f.Store.MembershipsOf(r.Context(), who)
	if err != nil {
		f.fail(w, r, CodeFailure, "memberships", err)
		return
	}
	if joining != nil {
		// Whoever signed in now takes the place the code opens: an invitation's member, or the first organisation's Owner. A wrong, used or
		// expired code is one answer. The new place comes first in the list, so the session is in it.
		var org, member uuid.UUID
		var err error
		switch {
		case joining.Kind == JoinInvitation:
			org, member, err = f.Store.AcceptInvitation(r.Context(), joining.Code, f.Clock.Now(), who)
		case len(memberships) == 0:
			org, member, err = f.Store.FirstRun(r.Context(), identity.Hash(joining.Code), f.Clock.Now(), joining.Organisation, joining.Name, who)
		default:
			err = identity.Refused() // setup makes the first organisation: an account that belongs to one has no use for it
		}
		if err != nil {
			var refusal *identity.Refusal
			if errors.As(err, &refusal) {
				f.fail(w, r, CodeJoinRefused, "join refused", err)
			} else {
				f.fail(w, r, CodeFailure, "join", err)
			}
			return
		}
		memberships = append([]Membership{{org, member}}, memberships...)
	}
	if len(memberships) == 0 {
		done, err := f.Store.SetupDone(r.Context())
		switch {
		case err != nil:
			f.fail(w, r, CodeFailure, "setup", err)
		case !done:
			f.fail(w, r, CodeNeedsSetup, "setup is not done", nil)
		default:
			f.fail(w, r, CodeNoMembership, "no membership", nil)
		}
		return
	}
	// The first organisation the person joined; choosing among several is the web app's organisation switcher.
	token, err := f.Store.StartSession(r.Context(), memberships[0].Organisation, memberships[0].Member, f.Clock.Now())
	if err != nil {
		// A deactivated member, a refusal: the same plain answer for all (it tells an attacker nothing).
		f.fail(w, r, CodeNoMembership, "session refused", nil)
		return
	}
	http.SetCookie(w, f.cookie(SessionCookie(f.Secure()), token, identity.SessionTTL))
	f.log().Info("auth.signed_in", "provider", provider, "issuer", signed.Issuer, "second_factor", signed.SecondFactor)
	http.Redirect(w, r, SafeReturn(string(back)), http.StatusSeeOther)
}

func (f Flow) signOut(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"urn:nemura:problem:` + CodeCrossSite + `","title":"Forbidden","status":403,"detail":"Sign-out was sent from another site.","code":"` + CodeCrossSite + `","fix":"Sign out from Nemura itself."}`))
		return
	}
	if c, err := r.Cookie(SessionCookie(f.Secure())); err == nil && c.Value != "" {
		if err := f.Store.EndSession(r.Context(), c.Value, f.Clock.Now()); err != nil {
			f.log().Error("auth.signout.failure", "error", err.Error())
		}
	}
	http.SetCookie(w, f.cookie(SessionCookie(f.Secure()), "", -1))
	http.Redirect(w, r, "/", http.StatusSeeOther)
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

// cookie is one of the flow's cookies: HttpOnly, on every path, SameSite=Lax (so it comes back with the provider's redirect, a top-level
// navigation), Secure over https. A negative maxAge deletes it.
func (f Flow) cookie(name, value string, maxAge time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: f.Secure(), SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge / time.Second)}
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
	http.Redirect(w, r, "/sign-in?signin_error="+code, http.StatusSeeOther)
}

func (f Flow) log() *slog.Logger {
	if f.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return f.Log
}

// secrets derives the sign-in's state, nonce and PKCE verifier from the random seed in the browser's cookie. The state travels in URLs;
// the seed and the verifier never do, and no derived value reveals another.
func secrets(seed string) ports.SignInSecrets {
	derive := func(label string) string {
		sum := sha256.Sum256([]byte(label + ":" + seed))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return ports.SignInSecrets{State: derive("state"), Nonce: derive("nonce"), CodeVerifier: derive("pkce")}
}

// SafeReturn returns where to send the browser after signing in: a path on this origin, or "/". An absolute URL, a scheme-relative
// "//host" or a backslash trick would send it elsewhere (an open redirect), so they become "/".
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
