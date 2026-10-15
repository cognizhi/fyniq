package authn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
)

// DevPrefix starts every local dev token; the Introspector never sends one to the OAuth server.
const DevPrefix = "fyniq-dev."

// MaxCache is the longest an introspection is trusted (ADR-T3: AI-38 revokes within 60 seconds).
const MaxCache = 30 * time.Second

// MaxGrantAge is the absolute lifetime of a grant (AI-38: refresh tokens expire 90 days after consent).
// Hydra's Apache-2.0 build rotates refresh tokens with a sliding expiry only, so the API refuses any
// token whose consent is older, however recently it was refreshed: the client must ask again.
const MaxGrantAge = 90 * 24 * time.Hour

// ErrWrongAudience is an active token issued for another resource, such as the MCP server (AI-36).
var ErrWrongAudience = errors.Join(ErrUnauthenticated, errors.New("the token was issued for another audience"))

// Introspector accepts the OAuth server's opaque access tokens for one audience (ADR-T3, AI-36). Each
// token is introspected and the answer cached for at most TTL (≤ MaxCache), keyed by the token's
// SHA-256, never the token itself.
//
// The organisation, roles and client class come from the claims consent stamped on the token. That
// is P0's mechanism: a snapshot at consent, valid until the token expires or is revoked; a role change
// revokes the user's tokens (AI-38, the token lifecycle slice), and memberships replace the snapshot
// with a lookup when they arrive (the first-run slice).
type Introspector struct {
	OAuth    ports.OAuthServer
	Audience string
	Clock    ports.Clock
	TTL      time.Duration

	mu    sync.Mutex
	cache map[[32]byte]cached
}

type cached struct {
	p     Principal
	err   error
	until time.Time
}

var _ Authenticator = (*Introspector)(nil)

// Authenticate implements Authenticator.
func (i *Introspector) Authenticate(ctx context.Context, authorization string) (Principal, error) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || token == "" || strings.HasPrefix(token, DevPrefix) {
		return Principal{}, ErrUnauthenticated
	}
	key, now := sha256.Sum256([]byte(token)), i.Clock.Now()
	i.mu.Lock()
	if c, ok := i.cache[key]; ok && now.Before(c.until) {
		i.mu.Unlock()
		return c.p, c.err
	}
	i.mu.Unlock()
	in, err := i.OAuth.Introspect(ctx, token)
	if err != nil {
		return Principal{}, fmt.Errorf("introspection: %w", err) // not cached: the server may be back
	}
	p, perr := i.principal(in, now)
	ttl := min(i.TTL, MaxCache)
	until := now.Add(ttl)
	if perr == nil && in.ExpiresAt.Before(until) {
		until = in.ExpiresAt
	}
	i.mu.Lock()
	if i.cache == nil {
		i.cache = map[[32]byte]cached{}
	}
	for k, c := range i.cache { // keep the cache small: drop what has lapsed
		if !now.Before(c.until) {
			delete(i.cache, k)
		}
	}
	i.cache[key] = cached{p: p, err: perr, until: until}
	i.mu.Unlock()
	return p, perr
}

func (i *Introspector) principal(in ports.Introspection, now time.Time) (Principal, error) {
	switch {
	case !in.Active || !now.Before(in.ExpiresAt):
		return Principal{}, ErrUnauthenticated
	case !slices.Contains(in.Audiences, i.Audience):
		return Principal{}, ErrWrongAudience
	}
	granted, ok := in.Claims["granted_at"].(float64) // JSON numbers decode as float64; seconds since 1970
	if !ok || now.Sub(time.Unix(int64(granted), 0)) > MaxGrantAge {
		return Principal{}, errors.Join(ErrUnauthenticated, fmt.Errorf("the grant is older than 90 days, or undated: authorise the client again (AI-38)"))
	}
	p := Principal{Via: ViaToken, Subject: in.Subject, Client: in.ClientID, Scopes: in.Scopes}
	switch class, _ := in.Claims["class"].(string); class {
	case ClassUser, ClassIntegration, ClassAgent:
		p.Class = class
	default:
		return Principal{}, errors.Join(ErrUnauthenticated, fmt.Errorf("the token has no client class (AI-34)"))
	}
	if org, ok := in.Claims["org"].(string); ok {
		id, err := uuid.Parse(org)
		if err != nil {
			return Principal{}, errors.Join(ErrUnauthenticated, fmt.Errorf("the token's organisation is not an ID"))
		}
		p.Organisation = id
	}
	roles, _ := in.Claims["roles"].([]any)
	for _, r := range roles {
		if s, ok := r.(string); ok {
			if _, known := authz.Roles[authz.Role(s)]; known {
				p.Roles = append(p.Roles, authz.Role(s))
			}
		}
	}
	return p, nil
}

// Local is the local build's authenticator: a dev token goes to Dev, anything else to OAuth (nil when
// the OAuth server is not configured). The two never see each other's tokens.
type Local struct {
	Dev   Authenticator
	OAuth Authenticator
}

// Authenticate implements Authenticator.
func (l Local) Authenticate(ctx context.Context, authorization string) (Principal, error) {
	if strings.HasPrefix(authorization, "Bearer "+DevPrefix) {
		return l.Dev.Authenticate(ctx, authorization)
	}
	if l.OAuth == nil {
		return Principal{}, ErrUnauthenticated
	}
	return l.OAuth.Authenticate(ctx, authorization)
}
