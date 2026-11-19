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
	"context"
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
	"github.com/cognizhi/fyniq/internal/proposal"
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
	CodeStepUp      = "AUTH-FLOW-08" // an MCP consent needs a second factor passed just now (AI-37)
	CodeMCPOff      = "AUTH-FLOW-09" // MCP consent is not available in this deployment
)

// MCPGate says whether an AI app may act for a person over MCP in their organisation now (AI-35): the organisation's switch,
// the allowlist and the person's opt-in (internal/mcpgate). Without one, no MCP consent is granted.
type MCPGate interface {
	Admit(ctx context.Context, org, user uuid.UUID, app string) (code, why string, err error)
}

// Flow is the login and consent app.
type Flow struct {
	OAuth       ports.OAuthServer
	SignedIn    authn.Authenticator
	Clock       ports.Clock
	APIAudience string
	MCPAudience string
	// MCP is the gate an MCP consent passes; nil refuses every MCP consent (closed by default).
	MCP MCPGate
	// Publishers names a first-party client's verified publisher by its client ID: a client absent from it is unverified.
	Publishers map[string]string
	Log        *slog.Logger
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
	Code          string   `json:"code,omitempty"`    // the stable code of the problem, when it has one
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
	audiences, problem := f.audiences(c)
	if problem != "" {
		d.Problem = problem
		return d
	}
	for _, a := range audiences {
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
	case slices.Contains(d.Audiences, f.MCPAudience) && len(d.Audiences) > 1:
		d.Problem = "A token for the MCP server is for it alone: the client asked for another audience too (AI-36, no token passthrough)"
	case slices.Contains(d.Audiences, f.MCPAudience) && c.Client.Class != authn.ClassAgent:
		d.Problem = "Only an AI app may be given a token for the MCP server (AI-34)"
	case p.Organisation == uuid.Nil:
		d.Problem = "You are not signed in to an organisation"
	}
	return d
}

// audiences are the audiences a request asks for: those it names, or, as an MCP client sends it, the resource of RFC 8707,
// which the OAuth server does not turn into an audience (G4-082). A resource must be this deployment's API or MCP server
// exactly, there may be one, and it must agree with any audience the request names.
func (f Flow) audiences(c ports.OAuthConsent) ([]string, string) {
	var resources []string
	if u, err := url.Parse(c.RequestURL); err == nil {
		resources = u.Query()["resource"]
	}
	switch {
	case len(resources) == 0:
		return c.RequestedAudiences, ""
	case len(resources) > 1:
		return nil, "The client named more than one resource: ask for the MCP server alone (RFC 8707)"
	case resources[0] != f.MCPAudience && resources[0] != f.APIAudience:
		return nil, "The client asked for a resource this server does not serve"
	case len(c.RequestedAudiences) > 0 && (len(c.RequestedAudiences) != 1 || c.RequestedAudiences[0] != resources[0]):
		return nil, "The client's resource and its audience differ"
	}
	return []string{resources[0]}, ""
}

// Admit is Decide, and for the MCP server the gates that need more than the request (AI-35, AI-37): the deployment has a
// gate at all, the person passed a second factor just now, and the organisation, the allowlist and the person's opt-in allow
// the app. Any failure to ask refuses (fail closed).
func (f Flow) Admit(ctx context.Context, c ports.OAuthConsent, p authn.Principal) Decision {
	d := f.Decide(c, p)
	if d.Problem != "" || !slices.Contains(d.Audiences, f.MCPAudience) {
		return d
	}
	refuse := func(code, why string) Decision {
		d.Problem, d.Code = why, code
		return d
	}
	switch {
	case f.MCP == nil:
		return refuse(CodeMCPOff, "AI apps cannot connect to this deployment yet.")
	case !p.SteppedUp(f.Clock.Now()):
		return refuse(CodeStepUp, "Confirm it is you with your second factor, then allow the app (AI-37).")
	case p.UserID == uuid.Nil:
		return refuse(CodeNoAccount, "AI apps act for a person with a Fyniq account.")
	}
	code, why, err := f.MCP.Admit(ctx, p.Organisation, p.UserID, c.Client.ID)
	if err != nil {
		if f.Log != nil {
			f.Log.Error("oauth.flow.mcp_gate_failure", "error", err.Error())
		}
		return refuse(CodeFailure, "Fyniq could not check whether AI apps may connect. Try again in a moment.")
	}
	if code != "" {
		return refuse(code, why)
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
	_ = json.NewEncoder(w).Encode(f.view(r.Context(), c, p))
}

// view is what the consent screen shows for a pending consent and the person it is for.
func (f Flow) view(ctx context.Context, c ports.OAuthConsent, p authn.Principal) map[string]any {
	d := f.Admit(ctx, c, p)
	client := map[string]any{"id": c.Client.ID, "name": c.Client.Name, "class": c.Client.Class}
	out := map[string]any{"client": client, "requested_scopes": c.RequestedScopes, "decision": d}
	if slices.Contains(d.Audiences, f.MCPAudience) {
		// What an MCP consent shows (AI-37): who, whether Fyniq has verified them, what each scope reaches, and how long it lasts.
		publisher := f.Publishers[c.Client.ID]
		client["publisher"], client["verified"] = publisher, publisher != ""
		client["proposal_only"] = c.Client.Class == authn.ClassAgent
		if publisher == "" { // an app that registered itself: Fyniq has not verified who it is, so the person sees where it sends them
			client["redirect_uris"] = c.Client.RedirectURIs
		}
		cats := []map[string]string{}
		for _, s := range d.Scopes {
			if what := scopeWord(s); what != "" {
				cats = append(cats, map[string]string{"scope": s, "reaches": what})
			}
		}
		out["data_categories"] = cats
		out["expiry"] = map[string]any{"consent_valid_days": int(ConsentDays), "access_token_minutes": 60}
	}
	return out
}

// ErrOtherUser is a consent that belongs to another person than the one answering.
var ErrOtherUser = errors.New("the consent belongs to another user")

// ConsentView is the consent screen's data for challenge, for the person p (their first-party session): the web app's way to the
// consent app's first step, as an operation of the API's contract. A consent that is not p's is ErrOtherUser.
func (f Flow) ConsentView(ctx context.Context, challenge string, p authn.Principal) (map[string]any, error) {
	c, err := f.pending(ctx, challenge, p)
	if err != nil {
		return nil, err
	}
	return f.view(ctx, c, p), nil
}

// AnswerConsent records the person's answer to challenge and returns where to send the browser next: the OAuth server's own address,
// to which the client's authorisation request carries on. A refusal, or a consent the gates refuse, is answered to the client as
// access_denied.
//
// only, when not nil, narrows what is granted to those of the scopes the screen showed that the person kept (and always "openid"): a
// scope the gates would not grant is never added by it.
func (f Flow) AnswerConsent(ctx context.Context, challenge string, accept bool, only []string, p authn.Principal) (string, error) {
	c, err := f.pending(ctx, challenge, p)
	if err != nil {
		return "", err
	}
	return f.answer(ctx, challenge, accept, only, c, p)
}

func (f Flow) pending(ctx context.Context, challenge string, p authn.Principal) (ports.OAuthConsent, error) {
	c, err := f.OAuth.ConsentRequest(ctx, challenge)
	if err != nil {
		return c, err
	}
	if p.UserID == uuid.Nil || p.UserID.String() != c.Subject {
		return c, ErrOtherUser
	}
	return c, nil
}

func (f Flow) answer(ctx context.Context, challenge string, accept bool, only []string, c ports.OAuthConsent, p authn.Principal) (string, error) {
	d := f.Admit(ctx, c, p)
	if only != nil {
		d.Scopes = slices.DeleteFunc(slices.Clone(d.Scopes), func(s string) bool { return s != "openid" && !slices.Contains(only, s) })
	}
	switch {
	case !accept:
		return f.OAuth.RejectConsent(ctx, challenge, ports.OAuthRejection{Error: "access_denied", Description: "The user refused"})
	case d.Problem != "":
		return f.OAuth.RejectConsent(ctx, challenge, ports.OAuthRejection{Error: "access_denied", Description: d.Problem})
	}
	roles := make([]string, len(p.Roles))
	for i, role := range p.Roles {
		roles[i] = string(role)
	}
	// The claims the API reads back through introspection: a snapshot at consent (authn.Introspector).
	// granted_at dates the grant: the API refuses it after 90 days, refreshed or not (AI-38).
	claims := map[string]any{"class": c.Client.Class, "org": p.Organisation.String(), "roles": roles, "granted_at": f.Clock.Now().Unix()}
	return f.OAuth.AcceptConsent(ctx, challenge, ports.ConsentAcceptance{Scopes: d.Scopes, Audiences: d.Audiences, AccessTokenClaims: claims})
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
	f.redirect(w, r, func() (string, error) {
		return f.answer(r.Context(), challenge, r.PostForm.Get("decision") == "accept", nil, c, p)
	})
}

// ConsentDays is how long a consent lasts before it must be renewed (AI-37).
const ConsentDays = 90

// scopeWords say in plain words what each scope reaches, for the consent screen (AI-37).
var scopeWords = map[string]string{
	"fyniq.read.reports":    "Reports and key figures: profit and loss, balance sheet, cash, runway and KPIs",
	"fyniq.read.sales":      "Invoices, quotes, credit notes and items",
	"fyniq.read.purchases":  "Bills, vendor credits and purchase orders",
	"fyniq.read.banking":    "Bank accounts and transactions",
	"fyniq.read.accounting": "Journals, accounts and the books' balances",
	"fyniq.read.planning":   "Budgets, forecasts and scenarios",
	"fyniq.read.compliance": "Checks, deadlines and statutory values",
	"fyniq.read.proposals":  "The status of changes the app proposed for you",
	"openid":                "Who you are, so the app acts as you",
	"offline_access":        "Staying connected for up to 90 days without asking again",
}

// scopeWord is what a scope reaches in plain words: the fixed ones, and for fyniq.propose.<task type> the catalogue's name of the task
// type, which a person approves in Fyniq before anything changes (AI-16, AI-37). "" for a scope it has no words for.
func scopeWord(scope string) string {
	if what, ok := scopeWords[scope]; ok {
		return what
	}
	if id, ok := cutPrefix(scope, "fyniq.propose."); ok {
		if tt, ok := proposal.Default().Get(id); ok {
			return "Suggest: " + tt.Name + ". A person approves it in Fyniq before anything changes"
		}
	}
	return ""
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
