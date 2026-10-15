// Package authn says who is calling the API: the principal behind a bearer token. The release build
// refuses every token until memberships and the SignIn port arrive (p0-identity-oauth-first-run); the
// local build accepts the local issuer's dev tokens (internal/adapters/local) and, when the OAuth
// server is configured, Hydra's access tokens for the API's audience (Introspector), never one for
// the other.
package authn

import (
	"context"
	"errors"

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
	Via          string       // ViaSession or ViaToken, set by the authenticator
	Issuer       string       // who vouched for Subject: set by the authenticator, never read from a token
	Subject      string       // the user (or service account)
	Client       string       // the OAuth client the call came through
	Class        string       // user, integration or agent
	Organisation uuid.UUID    // the organisation the session is in; Nil outside any
	Roles        []authz.Role // the subject's roles in that organisation
	// Scopes are what an OAuth token was granted (AI-37); empty for a first-party session. Mapping
	// scopes to operations arrives with the scope catalogue in P1; until then roles decide.
	Scopes []string
}

// AI reports whether the principal is the AI acting for its subject (AI-34).
func (p Principal) AI() bool { return p.Class == ClassAgent }

// ErrUnauthenticated is a request without valid credentials.
var ErrUnauthenticated = errors.New("no valid credentials")

// Authenticator reads a request's credentials: its Authorization header.
type Authenticator interface {
	Authenticate(ctx context.Context, authorization string) (Principal, error)
}

// NotYet is the release build's authenticator until p0-identity-oauth-first-run: it accepts no one.
type NotYet struct{}

// Authenticate implements Authenticator.
func (NotYet) Authenticate(context.Context, string) (Principal, error) {
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
