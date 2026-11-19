package authn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
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
	// Delegation, when set, also accepts the tokens the MCP server obtains for a person (open question
	// 40, option (b)); nil accepts none.
	Delegation *Delegation
	// Clients, when set, accepts the tokens of the API clients Fyniq registered: integration clients and personal access
	// tokens (FR-INT-02). What such a client may do is Fyniq's record of it, read at introspection, never the token's
	// claims; nil accepts none of them.
	Clients storage.APIClientRoutes

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
func (i *Introspector) Authenticate(ctx context.Context, c Credentials) (Principal, error) {
	authorization := c.Authorization
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
	p, perr := i.principal(ctx, in, now)
	if perr != nil && !errors.Is(perr, ErrUnauthenticated) {
		return Principal{}, perr // not cached: the database may be back
	}
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

func (i *Introspector) principal(ctx context.Context, in ports.Introspection, now time.Time) (Principal, error) {
	switch {
	case !in.Active || !now.Before(in.ExpiresAt):
		return Principal{}, ErrUnauthenticated
	case !slices.Contains(in.Audiences, i.Audience):
		return Principal{}, ErrWrongAudience
	}
	if i.Delegation != nil && in.ClientID == i.Delegation.Client {
		return i.Delegation.principal(ctx, in)
	}
	if i.Clients != nil {
		switch c, err := i.Clients.APIClientAccess(ctx, in.ClientID, now); {
		case err == nil:
			return clientPrincipal(c, in, now)
		case !errors.Is(err, ports.ErrNotFound):
			return Principal{}, fmt.Errorf("the API client %s: %w", in.ClientID, err)
		}
	}
	granted, ok := in.Claims["granted_at"].(float64) // JSON numbers decode as float64; seconds since 1970
	if !ok || now.Sub(time.Unix(int64(granted), 0)) > MaxGrantAge {
		return Principal{}, errors.Join(ErrUnauthenticated, fmt.Errorf("the grant is older than 90 days, or undated: authorise the client again (AI-38)"))
	}
	p := Principal{Via: ViaToken, Subject: in.Subject, Client: in.ClientID, Scopes: in.Scopes}
	if id, err := uuid.Parse(in.Subject); err == nil { // consent names the user by their ID (the session's)
		p.UserID = id
	}
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

// Local is the local build's authenticator: a dev token goes to Dev, any other bearer token to OAuth
// (nil when the OAuth server is not configured), and a request without an Authorization header to
// Sessions (nil without a database). None sees another's credentials.
type Local struct {
	Dev      Authenticator
	OAuth    Authenticator
	Sessions Authenticator
	// AI takes the assistant's delegated read tokens (AIGrants); nil refuses them.
	AI Authenticator
}

// Authenticate implements Authenticator.
func (l Local) Authenticate(ctx context.Context, c Credentials) (Principal, error) {
	switch {
	case strings.HasPrefix(c.Authorization, "Bearer "+DevPrefix):
		return l.Dev.Authenticate(ctx, Credentials{Authorization: c.Authorization})
	case strings.HasPrefix(c.Authorization, "Bearer "+AITokenPrefix):
		if l.AI == nil {
			return Principal{}, ErrUnauthenticated
		}
		return l.AI.Authenticate(ctx, Credentials{Authorization: c.Authorization})
	case c.Authorization != "":
		if l.OAuth == nil {
			return Principal{}, ErrUnauthenticated
		}
		return l.OAuth.Authenticate(ctx, Credentials{Authorization: c.Authorization})
	case l.Sessions != nil:
		return l.Sessions.Authenticate(ctx, Credentials{Cookie: c.Cookie})
	}
	return Principal{}, ErrUnauthenticated
}

// MaxDelegated is the longest a delegated token may live (open question 40: at most 5 minutes).
const MaxDelegated = 5 * time.Minute

// Delegation accepts the tokens the MCP server's delegation client obtains with the JWT-bearer grant
// (internal/mcpdelegation). No consent stamped them, so nothing in them is trusted but the subject: the
// class is always agent (AI-34), whatever the token says, and the person's organisation and roles are
// read now with Lookup, so the AI never gets more than the person may do at this moment (the API's
// intersection). A token that lives longer than MaxDelegated is refused.
type Delegation struct {
	Client string
	Lookup func(ctx context.Context, user uuid.UUID) (org uuid.UUID, roles []authz.Role, err error)
	// LookupIn, when set, serves a subject that names the organisation too, "<organisation>:<user>" (DelegatedSubject): the
	// MCP server names the organisation the person consented in, and the API checks the person is an active member of it
	// and reads their roles there now. A person who is in no such organisation gets no token. Lookup serves the plain form.
	LookupIn func(ctx context.Context, org, user uuid.UUID) (roles []authz.Role, err error)
}

// DelegatedSubject is the subject of an assertion for a person in an organisation.
func DelegatedSubject(org, user uuid.UUID) string { return org.String() + ":" + user.String() }

func (d *Delegation) principal(ctx context.Context, in ports.Introspection) (Principal, error) {
	orgPart, userPart, named := strings.Cut(in.Subject, ":")
	if !named {
		orgPart, userPart = "", in.Subject
	}
	user, err := uuid.Parse(userPart)
	switch {
	case err != nil:
		return Principal{}, errors.Join(ErrUnauthenticated, errors.New("a delegated token's subject is not a user"))
	case in.IssuedAt.IsZero() || in.ExpiresAt.Sub(in.IssuedAt) > MaxDelegated+time.Second: // iat and exp are whole seconds
		return Principal{}, errors.Join(ErrUnauthenticated, fmt.Errorf("a delegated token lives at most %s", MaxDelegated))
	}
	var org uuid.UUID
	var roles []authz.Role
	if named {
		if d.LookupIn == nil {
			return Principal{}, errors.Join(ErrUnauthenticated, errors.New("this server takes no delegated subject that names an organisation"))
		}
		if org, err = uuid.Parse(orgPart); err != nil || org == uuid.Nil {
			return Principal{}, errors.Join(ErrUnauthenticated, errors.New("a delegated token's organisation is not an organisation"))
		}
		if roles, err = d.LookupIn(ctx, org, user); err != nil {
			return Principal{}, errors.Join(ErrUnauthenticated, err)
		}
	} else if org, roles, err = d.Lookup(ctx, user); err != nil {
		return Principal{}, errors.Join(ErrUnauthenticated, err)
	}
	return Principal{Via: ViaToken, Subject: in.Subject, UserID: user, Client: in.ClientID, Class: ClassAgent, Organisation: org,
		Roles: roles, Scopes: in.Scopes}, nil
}

// clientPrincipal is an API client Fyniq registered (FR-INT-02, SEC-07): its class, organisation, scopes, networks and
// limits come from Fyniq's record, and its rights from the roles its member holds now, which its scopes then narrow
// (authz.ScopeAllows). It is refused once revoked or expired here, when its member is no longer an active member, or for a
// token not issued to the client itself (the client-credentials grant's subject is the client).
func clientPrincipal(c storage.APIClientAccess, in ports.Introspection, now time.Time) (Principal, error) {
	refuse := func(why string) (Principal, error) {
		return Principal{}, errors.Join(ErrUnauthenticated, fmt.Errorf("the API client %s %s (FR-INT-02)", in.ClientID, why))
	}
	switch {
	case c.Revoked:
		return refuse("was revoked")
	case !now.Before(c.ExpiresAt):
		return refuse("has expired")
	case !c.OwnerActive || len(c.Roles) == 0:
		return refuse("acts for a member who is no longer an active member with a role")
	case in.Subject != in.ClientID:
		return refuse("presented a token issued to someone else")
	case c.Class != ClassIntegration && c.Class != ClassAgent:
		return refuse("has no client class (AI-34)")
	}
	p := Principal{Via: ViaToken, Subject: in.ClientID, UserID: c.OwnerID, Client: in.ClientID, Class: c.Class, Organisation: c.Organisation,
		Scoped: true, ReadsPerMinute: c.ReadsPerMinute, WritesPerMinute: c.WritesPerMinute, Scopes: []string{}}
	for _, s := range in.Scopes { // what the token was granted and the record still holds
		if slices.Contains(c.Scopes, s) {
			p.Scopes = append(p.Scopes, s)
		}
	}
	for _, r := range c.Roles {
		if _, known := authz.Roles[authz.Role(r)]; known {
			p.Roles = append(p.Roles, authz.Role(r))
		}
	}
	for _, a := range c.IPAllow {
		prefix, err := netip.ParsePrefix(a)
		if err != nil {
			return refuse("has a network that is not a CIDR prefix")
		}
		p.AllowedIPs = append(p.AllowedIPs, prefix.Masked())
	}
	return p, nil
}

// AllowsIP reports whether a scoped client may call from ip: anywhere when it names no network.
func (p Principal) AllowsIP(ip netip.Addr) bool {
	if len(p.AllowedIPs) == 0 {
		return true
	}
	ip = ip.Unmap()
	for _, n := range p.AllowedIPs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
