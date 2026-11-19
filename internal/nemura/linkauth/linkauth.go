// Package linkauth is Nemura's resource server for the one caller that is not a person: Fyniq's link client (PRD-002 NM-INT-10, NM-ARC-04; p1-nemura-fyniq-link design D1). Fyniq
// calls the link's event inbox with an access token its client got from Nemura's own OAuth server by the client-credentials grant; the token is introspected here at every call
// (cached for at most 30 seconds, keyed by its hash), is accepted for Nemura's audience only (never one made for Fyniq's API: no passthrough, AI-36), must be the client's own token and
// carry the link scope, and the client must be the one the live link names. What it returns is who may call the inbox: the organisation and the link, never a person and never a role.
// A session cookie, a personal token or an ordinary client's token is not this caller.
package linkauth

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

	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/ports"
)

// MaxCache is the longest an introspection is trusted.
const MaxCache = 30 * time.Second

// The refusals. ErrUnauthenticated is a token that is not valid for this API at all; ErrNotLink is a valid token of a client that is not the link's, or without the link scope.
var (
	ErrUnauthenticated = errors.New("the token is not valid for this API")
	ErrWrongAudience   = errors.Join(ErrUnauthenticated, errors.New("the token was issued for another audience"))
	ErrNotLink         = errors.New("the token is not the link's client's, or lacks the link scope")
)

// Route is what Nemura's database says about a client ID before any organisation is known: the link it belongs to.
type Route struct {
	Organisation, Link, FyniqOrganisation uuid.UUID
	State                                 string
	Epoch                                 int
	Purpose                               string // events: the client that sends the link's events; ask: the one that reads a hold's status
}

// Routes finds the link a client belongs to; ports.ErrNotFound when it belongs to none.
type Routes interface {
	LinkClientRoute(ctx context.Context, client string) (Route, error)
}

// Caller is the link's client, authenticated.
type Caller struct {
	Organisation, Link, FyniqOrganisation uuid.UUID
	Epoch                                 int
	ClientID                              string
	Scope                                 string // the scope this call was authenticated for
}

// Introspector authenticates bearer tokens as the link's client.
type Introspector struct {
	OAuth    ports.OAuthServer
	Routes   Routes
	Audience string // Nemura's API audience
	Clock    ports.Clock
	TTL      time.Duration

	mu    sync.Mutex
	cache map[[32]byte]cached
}

type cached struct {
	c     Caller
	err   error
	until time.Time
}

// Events authenticates the client that sends the link's events (the scope apiclient.LinkScope); Ask the one that reads a hold's status (apiclient.AskScope). A token of one is no token of the other.
func (i *Introspector) Events(ctx context.Context, authorization string) (Caller, error) {
	return i.Authenticate(ctx, authorization, apiclient.LinkScope)
}

// Ask is Events for the read-only client.
func (i *Introspector) Ask(ctx context.Context, authorization string) (Caller, error) {
	return i.Authenticate(ctx, authorization, apiclient.AskScope)
}

// Authenticate reads an Authorization header value for one scope.
func (i *Introspector) Authenticate(ctx context.Context, authorization, scope string) (Caller, error) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || token == "" {
		return Caller{}, ErrUnauthenticated
	}
	key, now := sha256.Sum256([]byte(scope+"\n"+token)), i.Clock.Now()
	i.mu.Lock()
	if c, ok := i.cache[key]; ok && now.Before(c.until) {
		i.mu.Unlock()
		return c.c, c.err
	}
	i.mu.Unlock()
	in, err := i.OAuth.Introspect(ctx, token)
	if err != nil {
		return Caller{}, fmt.Errorf("introspection: %w", err) // not cached: the server may be back
	}
	caller, perr := i.caller(ctx, in, now, scope)
	if perr != nil && !errors.Is(perr, ErrUnauthenticated) && !errors.Is(perr, ErrNotLink) {
		return Caller{}, perr // not cached: the database may be back
	}
	until := now.Add(min(i.TTL, MaxCache))
	if perr == nil && in.ExpiresAt.Before(until) {
		until = in.ExpiresAt
	}
	i.mu.Lock()
	if i.cache == nil {
		i.cache = map[[32]byte]cached{}
	}
	for k, c := range i.cache {
		if !now.Before(c.until) {
			delete(i.cache, k)
		}
	}
	i.cache[key] = cached{c: caller, err: perr, until: until}
	i.mu.Unlock()
	return caller, perr
}

func (i *Introspector) caller(ctx context.Context, in ports.Introspection, now time.Time, scope string) (Caller, error) {
	switch {
	case !in.Active || !now.Before(in.ExpiresAt):
		return Caller{}, ErrUnauthenticated
	case !slices.Contains(in.Audiences, i.Audience):
		return Caller{}, ErrWrongAudience
	case in.Subject != in.ClientID: // a client-credentials token's subject is the client itself
		return Caller{}, ErrUnauthenticated
	case !slices.Contains(in.Scopes, scope):
		return Caller{}, ErrNotLink
	}
	r, err := i.Routes.LinkClientRoute(ctx, in.ClientID)
	if errors.Is(err, ports.ErrNotFound) {
		return Caller{}, ErrNotLink
	}
	if err != nil {
		return Caller{}, err
	}
	if (scope == apiclient.LinkScope) != (r.Purpose == "events") || (scope == apiclient.AskScope) != (r.Purpose == "ask") {
		return Caller{}, ErrNotLink // the events' client may not ask, and the asking client may not send
	}
	if r.State != "active" {
		return Caller{}, ErrNotLink // a proposed, suspended or ended link has no client to call with
	}
	return Caller{Organisation: r.Organisation, Link: r.Link, FyniqOrganisation: r.FyniqOrganisation, Epoch: r.Epoch, ClientID: in.ClientID, Scope: scope}, nil
}
