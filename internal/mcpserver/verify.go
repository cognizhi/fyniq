package mcpserver

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Token is an MCP access token the server has checked: it was issued for this server alone (RFC 8707), to a client of
// the agent class, and it is live now.
type Token struct {
	Subject   string // the person's user ID
	ClientID  string // the OAuth client it was issued to
	Scopes    []string
	ExpiresAt time.Time
	// Organisation is the organisation the person consented in, as the consent app stamped it (a snapshot: the API reads
	// the person's membership now).
	Organisation string
	GrantedAt    time.Time
}

// Holds reports whether the token was granted scope.
func (t Token) Holds(scope string) bool { return slices.Contains(t.Scopes, scope) }

// HoldsAny reports whether the token was granted any of scopes.
func (t Token) HoldsAny(scopes []string) bool { return slices.ContainsFunc(scopes, t.Holds) }

// ErrInvalidToken is a token that is not valid for this server: unknown, expired, revoked, issued for another audience,
// or not a token an agent-class client was granted. Its detail never says which, so a probe learns nothing.
var ErrInvalidToken = errors.New("the access token is not valid for the MCP server")

// Verifier checks the bearer token of a request.
type Verifier interface {
	Verify(ctx context.Context, bearer string) (Token, error)
}

// Introspector is the one thing the server asks of the OAuth server: what an access token is. It is narrower than
// ports.OAuthServer on purpose, so the server's wiring is a function that can introspect and nothing else.
type Introspector interface {
	Introspect(ctx context.Context, token string) (ports.Introspection, error)
}

// ConsentLifetime is how long a consent lasts before it must be renewed (AI-37: 90 days).
const ConsentLifetime = 90 * 24 * time.Hour

// OAuthVerifier validates tokens by introspection: every call asks the OAuth server, so a revocation is seen at once
// (AI-38). The token's audiences must be this server's resource and nothing else: a token that is also good for the API
// could be passed through, which AI-36 forbids, so it is refused here too.
type OAuthVerifier struct {
	OAuth    Introspector
	Resource string // the MCP server's canonical URL, the audience (RFC 8707)
	Clock    ports.Clock
}

// Verify implements Verifier.
func (v OAuthVerifier) Verify(ctx context.Context, bearer string) (Token, error) {
	if bearer == "" {
		return Token{}, ErrInvalidToken
	}
	in, err := v.OAuth.Introspect(ctx, bearer)
	if err != nil {
		return Token{}, err // the OAuth server did not answer: not the client's fault, the caller says 503
	}
	now := v.Clock.Now()
	class, _ := in.Claims["class"].(string)
	org, _ := in.Claims["org"].(string)
	var granted time.Time
	if g, ok := in.Claims["granted_at"].(float64); ok {
		granted = time.Unix(int64(g), 0).UTC()
	}
	switch {
	case !in.Active, in.Subject == "", in.ClientID == "":
		return Token{}, ErrInvalidToken
	case len(in.Audiences) != 1 || in.Audiences[0] != v.Resource:
		return Token{}, ErrInvalidToken
	case !in.ExpiresAt.After(now):
		return Token{}, ErrInvalidToken
	case class != "agent": // AI-34: every MCP client is an agent
		return Token{}, ErrInvalidToken
	case granted.IsZero() || now.Sub(granted) > ConsentLifetime: // consent is renewed every 90 days (AI-37)
		return Token{}, ErrInvalidToken
	}
	return Token{Subject: in.Subject, ClientID: in.ClientID, Scopes: in.Scopes, ExpiresAt: in.ExpiresAt, Organisation: org, GrantedAt: granted}, nil
}
