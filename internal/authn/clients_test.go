package authn_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fakeClients answers the API clients Fyniq registered from a table.
type fakeClients map[string]storage.APIClientAccess

func (f fakeClients) APIClientAccess(_ context.Context, clientID string, _ time.Time) (storage.APIClientAccess, error) {
	if c, ok := f[clientID]; ok {
		return c, nil
	}
	if clientID == "broken" {
		return storage.APIClientAccess{}, errors.New("the database is away")
	}
	return storage.APIClientAccess{}, ports.ErrNotFound
}

// An integration client's token is what Fyniq's record of the client says, never what the token claims (FR-INT-02,
// AI-34): its organisation, class, scopes, networks and limits come from the record, and its rights from the roles its
// member holds now. It is refused once revoked or expired, when its member has gone, for a token issued to someone else,
// and for scopes the record no longer holds; a client Fyniq does not know falls back to consent's claims, and a database
// failure is never cached as a refusal.
// req: FR-INT-02
// req: SEC-07
// req: AI-34
// asvs: V10.3.1, V10.4.11, V8.2.1
func TestTheIntrospectorTakesAnAPIClientFromItsRecord(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	org, owner := uuid.New(), uuid.New()
	exp := c.Now().Add(time.Hour)
	live := storage.APIClientAccess{Organisation: org, ID: uuid.New(), Kind: storage.APIClientIntegration, Class: authn.ClassIntegration,
		OwnerID: owner, OwnerActive: true, Roles: []string{"accountant", "not_a_role"}, Scopes: []string{"fyniq.read.accounting", "fyniq.write.sales"},
		IPAllow: []string{"203.0.113.0/24"}, ReadsPerMinute: 60, WritesPerMinute: 10, ExpiresAt: c.Now().Add(24 * time.Hour)}
	revoked, expired, gone, unclassed := live, live, live, live
	revoked.Revoked, expired.ExpiresAt, gone.OwnerActive, gone.Roles, unclassed.Class = true, c.Now(), false, nil, "user"
	clients := fakeClients{"live": live, "revoked": revoked, "expired": expired, "gone": gone, "unclassed": unclassed}
	token := func(client, subject string, scopes ...string) ports.Introspection {
		// The claims say Owner of another organisation: they never count for a registered client.
		return ports.Introspection{Active: true, Subject: subject, ClientID: client, Audiences: []string{"https://api"}, Scopes: scopes, ExpiresAt: exp,
			Claims: map[string]any{"class": "user", "org": uuid.NewString(), "roles": []any{"owner"}, "granted_at": float64(c.Now().Unix())}}
	}
	f := &fakeOAuth{tokens: map[string]ports.Introspection{
		"live":        token("live", "live", "fyniq.read.accounting", "fyniq.write.sales", "fyniq.write.accounting"),
		"revoked":     token("revoked", "revoked", "fyniq.read.accounting"),
		"expired":     token("expired", "expired", "fyniq.read.accounting"),
		"gone":        token("gone", "gone", "fyniq.read.accounting"),
		"unclassed":   token("unclassed", "unclassed", "fyniq.read.accounting"),
		"someone":     token("live", uuid.NewString(), "fyniq.read.accounting"),
		"unknown":     token("not-ours", "not-ours"),
		"broken":      token("broken", "broken"),
		"other-place": {Active: true, Subject: "live", ClientID: "live", Audiences: []string{"https://mcp"}, ExpiresAt: exp},
	}}
	in := &authn.Introspector{OAuth: f, Audience: "https://api", Clock: c, TTL: time.Hour, Clients: clients}
	ctx := context.Background()
	p, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer live"})
	if err != nil || !p.Scoped || p.Organisation != org || p.UserID != owner || p.Class != authn.ClassIntegration || p.AI() ||
		!slices.Equal(p.Roles, []authz.Role{authz.Accountant}) || p.Client != "live" || p.Via != authn.ViaToken {
		t.Fatalf("a live integration client: %+v, %v", p, err)
	}
	if !slices.Equal(p.Scopes, []string{"fyniq.read.accounting", "fyniq.write.sales"}) {
		t.Errorf("scopes the record no longer holds must not count: %v", p.Scopes)
	}
	if p.ReadsPerMinute != 60 || p.WritesPerMinute != 10 || !p.AllowsIP(netip.MustParseAddr("203.0.113.7")) ||
		p.AllowsIP(netip.MustParseAddr("198.51.100.7")) || !p.AllowsIP(netip.MustParseAddr("::ffff:203.0.113.9")) {
		t.Errorf("its limits and networks: %+v", p)
	}
	for _, tok := range []string{"revoked", "expired", "gone", "unclassed", "someone", "other-place"} {
		if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + tok}); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s: want refused, got %v", tok, err)
		}
	}
	// A client Fyniq never registered is not scoped: it is a consented client, judged by consent's claims (AI-37).
	if p, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer unknown"}); err != nil || p.Scoped || p.Class != authn.ClassUser {
		t.Errorf("an unregistered client: %+v, %v", p, err)
	}
	calls := f.calls
	for range 2 {
		if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer broken"}); err == nil || errors.Is(err, authn.ErrUnauthenticated) {
			t.Fatalf("a database failure is an error, not a refusal: %v", err)
		}
	}
	if f.calls != calls+2 {
		t.Errorf("a failure must not be cached: %d introspections, want %d", f.calls, calls+2)
	}
	var none authn.Principal
	if !none.AllowsIP(netip.MustParseAddr("198.51.100.7")) {
		t.Error("a client with no networks calls from anywhere")
	}
}
