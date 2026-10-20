// Package authn says who is calling the API: the principal behind a bearer token or the web app's
// first-party session cookie (Sessions; SEC-05, SEC-08). The release build refuses every credential
// until the Identity Platform adapter of the SignIn port arrives (p0-identity-oauth-first-run); the
// local build accepts the local issuer's dev tokens (internal/adapters/local), sessions begun with its
// dev sign-in and, when the OAuth server is configured, Hydra's access tokens for the API's audience
// (Introspector), never one for the other.
package authn

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The client classes of AI-34. An agent-class client is proposal-only; the API treats it as the AI
// acting for the user behind it.
const (
	ClassUser        = "user"
	ClassIntegration = "integration"
	ClassAgent       = "agent"
)

// How a principal was authenticated: a first-party session (the web app's sign-in; locally, dev
// tokens) or an OAuth access token a client holds. Some operations, such as first-run setup, accept
// only a session.
const (
	ViaSession = "session"
	ViaToken   = "token"
)

// Principal is an authenticated caller.
type Principal struct {
	Via     string // ViaSession or ViaToken, set by the authenticator
	Issuer  string // who vouched for Subject: set by the authenticator, never read from a token
	Subject string // the sign-in identity's subject (a session), or the token's subject (a user ID or service account)
	// UserID is the Fyniq user behind the principal: set for a session whose identity has a user and for
	// an OAuth token issued to a user; Nil otherwise (before setup, and for the local dev tokens).
	UserID       uuid.UUID
	Provider     string       // the sign-in provider of a first-party session (FR-ADM-08); empty otherwise
	Client       string       // the OAuth client the call came through
	Class        string       // user, integration or agent
	Organisation uuid.UUID    // the organisation the session is in; Nil outside any
	Roles        []authz.Role // the subject's roles in that organisation
	// Scopes are what an OAuth token was granted (AI-37); empty for a first-party session. Mapping
	// scopes to operations arrives with the scope catalogue in P1; until then roles decide.
	Scopes []string
	// StepUpAt is when the person last passed step-up authentication in this session (FR-ADM-07;
	// passkey or TOTP from P1, FR-ADM-02, SEC-01); zero when never. Only a session has one. Set by
	// the authenticator: the release build sets it with the SignIn port, the local build from the dev
	// token.
	StepUpAt time.Time
	// StepUpMethod is how the session passed its last second factor: sign_in or passkey; empty when never.
	StepUpMethod string
	// SessionHash is the SHA-256 of a first-party session's token: the passkey ceremonies bind their challenges to
	// it. Set only by the session authenticator; nil for a dev token or an OAuth token.
	SessionHash []byte
}

// StepUpWindow is how recent a step-up must be for an operation that needs one.
const StepUpWindow = 5 * time.Minute

// SteppedUp reports whether the principal is a session that passed step-up within StepUpWindow of now.
func (p Principal) SteppedUp(now time.Time) bool {
	return p.Via == ViaSession && !p.StepUpAt.IsZero() && !p.StepUpAt.After(now) && now.Sub(p.StepUpAt) <= StepUpWindow
}

// AI reports whether the principal is the AI acting for its subject (AI-34).
func (p Principal) AI() bool { return p.Class == ClassAgent }

// ErrUnauthenticated is a request without valid credentials.
var ErrUnauthenticated = errors.New("no valid credentials")

// Credentials are what a request presents: its Authorization header and its Cookie header, either of
// which may be empty. A bearer token in Authorization wins; the session cookie counts only without one.
type Credentials struct {
	Authorization string
	Cookie        string
}

// FromRequest returns a request's credentials.
func FromRequest(r *http.Request) Credentials {
	return Credentials{Authorization: r.Header.Get("Authorization"), Cookie: r.Header.Get("Cookie")}
}

// Authenticator reads a request's credentials.
type Authenticator interface {
	Authenticate(ctx context.Context, c Credentials) (Principal, error)
}

// NotYet is the release build's authenticator until p0-identity-oauth-first-run: it accepts no one.
type NotYet struct{}

// Authenticate implements Authenticator.
func (NotYet) Authenticate(context.Context, Credentials) (Principal, error) {
	return Principal{}, errors.Join(ErrUnauthenticated,
		errors.New("sign-in arrives with the identity and OAuth server (ADR-T3); this build accepts no credentials yet"))
}

type principalKey struct{}

// WithPrincipal attaches the caller to ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// From returns the caller attached to ctx.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
