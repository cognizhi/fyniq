// Package oauthflow serves the steps the OAuth server delegates to the API (ADR-T3): GET /auth/login
// signs the user in for an authorisation request, and GET and POST /auth/consent show and record what
// the user grants a client (AI-36, AI-37).
//
// Who is at the browser comes from SignedIn: the first-party session cookie (internal/signin), or in the
// local build a dev token in the Authorization header, which only tests and local tools send. A browser
// without a session is sent to sign in and comes back. The OAuth server knows a person by their user ID,
// the subject a role change or a recovery revokes (AI-38), so a session whose identity has no user yet
// cannot sign in to a client. The consent screen (P1) reads GET /auth/consent's JSON. Consent is never
// remembered, so every client asks every time; a POST /auth/consent the session cookie authenticates
// must come from the web app's own origin (Fetch Metadata), since the cookie is ambient.
package oauthflow

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The stable codes of the flow's refusals (ENG-23).
const (
	CodeNoChallenge = "AUTH-FLOW-01" // the request has no challenge
	CodeExpired     = "AUTH-FLOW-02" // the server no longer knows the challenge
	CodeSignIn      = "AUTH-FLOW-03" // nobody is signed in
	CodeOtherUser   = "AUTH-FLOW-04" // the signed-in user is not the request's
	CodeFailure     = "AUTH-FLOW-05" // the OAuth server failed
	CodeNoAccount   = "AUTH-FLOW-06" // the signed-in identity has no Fyniq user yet
	CodeCrossSite   = "AUTH-FLOW-07" // a consent sent from another site with the session cookie
)

// Flow is the login and consent app.
type Flow struct {
	OAuth       ports.OAuthServer
	SignedIn    authn.Authenticator
	Clock       ports.Clock
	APIAudience string
	MCPAudience string
	Log         *slog.Logger
	// SignInPage is where a browser without a session signs in (the web app, "/"), and Base where the
	// API is mounted for the browser ("/api"), so the browser comes back to the login. Without a page, a
	// request without a session is refused with 401.
	SignInPage, Base string
}

// subject is how the OAuth server knows the person: their user ID. Only a dev token or a test's
// credential, sent in the Authorization header, may sign in without one, under its own subject.
func subject(p authn.Principal, c authn.Credentials) (string, bool) {
	if p.UserID != uuid.Nil {
		return p.UserID.String(), true
	}
	return p.Subject, c.Authorization != "" && p.Subject != ""
}

// Register adds the routes to mux.
func (f Flow) Register(mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}) {
	mux.HandleFunc("GET /auth/login", f.login)
	mux.HandleFunc("GET /auth/consent", f.showConsent)
	mux.HandleFunc("POST /auth/consent", f.recordConsent)
}

// readScopes are the areas of `fyniq.read.<area>` a client may be granted (AI-37). Payroll and equity
// need settings that arrive in P1 (AI-02), so P0 never grants them.
var readScopes = []string{"reports", "sales", "purchases", "banking", "accounting", "planning", "compliance", "proposals"}

var proposeScope = regexp.MustCompile(`^fyniq\.propose\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// Grantable reports whether consent may grant a scope.
func Grantable(scope string) bool {
	if scope == "openid" || scope == "offline_access" || proposeScope.MatchString(scope) {
		return true
	}
	area, ok := cutPrefix(scope, "fyniq.read.")
	return ok && slices.Contains(readScopes, area)
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

// Decision is what consent may grant for a request.
type Decision struct {
	Scopes        []string `json:"scopes"`
	RefusedScopes []string `json:"refused_scopes"`
	Audiences     []string `json:"audiences"`
	Problem       string   `json:"problem,omitempty"` // when set, consent is refused whatever the user says
}

// Decide applies the consent rules: only grantable scopes; audiences only the API's or the MCP
// server's, only those the client's record allows, and all of those the request names; the client's
// class from its record (AI-34); and a user who is in an organisation.
func (f Flow) Decide(c ports.OAuthConsent, p authn.Principal) Decision {
	d := Decision{Scopes: []string{}, RefusedScopes: []string{}, Audiences: []string{}}
	for _, s := range c.RequestedScopes {
		if Grantable(s) {
			d.Scopes = append(d.Scopes, s)
		} else {
			d.RefusedScopes = append(d.RefusedScopes, s)
		}
	}
	for _, a := range c.RequestedAudiences {
		if (a != f.APIAudience && a != f.MCPAudience) || !slices.Contains(c.Client.Audiences, a) {
			d.Problem = "The client asked for an audience it may not use: " + a
			return d
		}
		d.Audiences = append(d.Audiences, a)
	}
	switch {
	case !slices.Contains([]string{authn.ClassUser, authn.ClassIntegration, authn.ClassAgent}, c.Client.Class):
		d.Problem = "The client has no class (AI-34): register it again"
	case len(d.Audiences) == 0:
		d.Problem = "The client asked for no audience: it must name the API or the MCP server"
	case p.Organisation == uuid.Nil:
		d.Problem = "You are not signed in to an organisation"
	}
	return d
}

func (f Flow) login(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		writeProblem(w, http.StatusBadRequest, CodeNoChallenge, "The sign-in request has no login_challenge.", "Start again from the app that sent you here.")
		return
	}
	req, err := f.OAuth.LoginRequest(r.Context(), challenge)
	if err != nil {
		f.failed(w, err)
		return
	}
	if !pkceS256(req.RequestURL) {
		f.redirect(w, r, func() (string, error) {
			return f.OAuth.RejectLogin(r.Context(), challenge, ports.OAuthRejection{Error: "invalid_request",
				Description: "Fyniq accepts only the authorisation code with PKCE (S256)"})
		})
		return
	}
	creds := authn.FromRequest(r)
	p, err := f.SignedIn.Authenticate(r.Context(), creds)
	if err != nil {
		if creds.Authorization == "" && f.SignInPage != "" { // a browser: sign in, then come back here
			back := f.Base + "/auth/login?" + url.Values{"login_challenge": {challenge}}.Encode()
			http.Redirect(w, r, f.SignInPage+"?"+url.Values{"return_to": {back}}.Encode(), http.StatusSeeOther)
			return
		}
		writeProblem(w, http.StatusUnauthorized, CodeSignIn, "Nobody is signed in to Fyniq.", "Sign in to Fyniq, then return to the app that sent you here.")
		return
	}
	sub, ok := subject(p, creds)
	if !ok {
		writeProblem(w, http.StatusForbidden, CodeNoAccount, "You are signed in, but you have no Fyniq account yet.",
			"Ask a Fyniq administrator for an invitation, then start again.")
		return
	}
	if req.Skip && req.Subject != sub {
		writeProblem(w, http.StatusForbidden, CodeOtherUser, "Another user is signed in for this request.", "Sign out, then start again.")
		return
	}
	f.redirect(w, r, func() (string, error) {
		return f.OAuth.AcceptLogin(r.Context(), challenge, ports.LoginAcceptance{Subject: sub})
	})
}

// consent returns the pending consent and checks that the signed-in user is its subject.
func (f Flow) consent(w http.ResponseWriter, r *http.Request, challenge string) (ports.OAuthConsent, authn.Principal, bool) {
	if challenge == "" {
		writeProblem(w, http.StatusBadRequest, CodeNoChallenge, "The consent request has no consent_challenge.", "Start again from the app that sent you here.")
		return ports.OAuthConsent{}, authn.Principal{}, false
	}
	c, err := f.OAuth.ConsentRequest(r.Context(), challenge)
	if err != nil {
		f.failed(w, err)
		return c, authn.Principal{}, false
	}
	creds := authn.FromRequest(r)
	p, err := f.SignedIn.Authenticate(r.Context(), creds)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, CodeSignIn, "Nobody is signed in to Fyniq.", "Sign in to Fyniq, then return to the app that sent you here.")
		return c, p, false
	}
	if r.Method == http.MethodPost && creds.Authorization == "" && !authn.SameOrigin(r.Header) {
		writeProblem(w, http.StatusForbidden, CodeCrossSite, "The consent was sent from another site.", "Give consent on Fyniq's own consent screen.")
		return c, p, false
	}
	if sub, ok := subject(p, creds); !ok || sub != c.Subject {
		writeProblem(w, http.StatusForbidden, CodeOtherUser, "The consent belongs to another user.", "Sign in as the user who started it.")
		return c, p, false
	}
	return c, p, true
}

func (f Flow) showConsent(w http.ResponseWriter, r *http.Request) {
	c, p, ok := f.consent(w, r, r.URL.Query().Get("consent_challenge"))
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"client":           map[string]any{"id": c.Client.ID, "name": c.Client.Name, "class": c.Client.Class},
		"requested_scopes": c.RequestedScopes, "decision": f.Decide(c, p),
	})
}

func (f Flow) recordConsent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeProblem(w, http.StatusBadRequest, CodeNoChallenge, "The consent form cannot be read.", "Send consent_challenge and decision as a form.")
		return
	}
	challenge := r.PostForm.Get("consent_challenge")
	c, p, ok := f.consent(w, r, challenge)
	if !ok {
		return
	}
	d := f.Decide(c, p)
	switch {
	case r.PostForm.Get("decision") != "accept":
		f.redirect(w, r, func() (string, error) {
			return f.OAuth.RejectConsent(r.Context(), challenge, ports.OAuthRejection{Error: "access_denied", Description: "The user refused"})
		})
	case d.Problem != "":
		f.redirect(w, r, func() (string, error) {
			return f.OAuth.RejectConsent(r.Context(), challenge, ports.OAuthRejection{Error: "access_denied", Description: d.Problem})
		})
	default:
		roles := make([]string, len(p.Roles))
		for i, role := range p.Roles {
			roles[i] = string(role)
		}
		// The claims the API reads back through introspection: a snapshot at consent (authn.Introspector).
		// granted_at dates the grant: the API refuses it after 90 days, refreshed or not (AI-38).
		claims := map[string]any{"class": c.Client.Class, "org": p.Organisation.String(), "roles": roles, "granted_at": f.Clock.Now().Unix()}
		f.redirect(w, r, func() (string, error) {
			return f.OAuth.AcceptConsent(r.Context(), challenge, ports.ConsentAcceptance{Scopes: d.Scopes, Audiences: d.Audiences, AccessTokenClaims: claims})
		})
	}
}

// pkceS256 reports whether an authorisation request carries a PKCE challenge with S256 (AI-36).
func pkceS256(requestURL string) bool {
	u, err := url.Parse(requestURL)
	if err != nil {
		return false
	}
	q := u.Query()
	return q.Get("code_challenge") != "" && q.Get("code_challenge_method") == "S256"
}

func (f Flow) redirect(w http.ResponseWriter, r *http.Request, next func() (string, error)) {
	to, err := next()
	if err != nil {
		f.failed(w, err)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (f Flow) failed(w http.ResponseWriter, err error) {
	if errors.Is(err, ports.ErrNoChallenge) {
		writeProblem(w, http.StatusNotFound, CodeExpired, "The sign-in request expired or was already used.", "Start again from the app that sent you here.")
		return
	}
	if f.Log != nil {
		f.Log.Error("oauth.flow.failure", "error", err.Error())
	}
	writeProblem(w, http.StatusBadGateway, CodeFailure, "The sign-in service did not answer.", "Try again in a moment.")
}

func writeProblem(w http.ResponseWriter, status int, code, detail, fix string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:fyniq:problem:" + code, "title": http.StatusText(status),
		"status": status, "detail": detail, "code": code, "fix": fix})
}
