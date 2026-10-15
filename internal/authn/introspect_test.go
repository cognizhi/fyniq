package authn_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// fakeOAuth answers introspection from a table and counts the calls.
type fakeOAuth struct {
	ports.OAuthServer
	tokens map[string]ports.Introspection
	calls  int
}

func (f *fakeOAuth) Introspect(_ context.Context, token string) (ports.Introspection, error) {
	f.calls++
	return f.tokens[token], nil
}

// req: AI-36
// req: SEC-02
// asvs: V10.3.1, V10.3.2, V10.3.3
func TestTheIntrospectorAcceptsOnlyItsAudience(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	org := uuid.New()
	exp := c.Now().Add(time.Hour)
	granted := float64(c.Now().Add(-24 * time.Hour).Unix())
	claims := map[string]any{"class": "user", "org": org.String(), "roles": []any{"accountant", "not_a_role"}, "granted_at": granted}
	f := &fakeOAuth{tokens: map[string]ports.Introspection{
		"api-token": {Active: true, Subject: "user-1", ClientID: "cli", Audiences: []string{"https://api"}, Scopes: []string{"fyniq.read.accounting"}, ExpiresAt: exp, Claims: claims},
		"mcp-token": {Active: true, Subject: "user-1", ClientID: "cli", Audiences: []string{"https://mcp"}, ExpiresAt: exp, Claims: claims},
		"no-class":  {Active: true, Subject: "user-1", ClientID: "cli", Audiences: []string{"https://api"}, ExpiresAt: exp, Claims: map[string]any{"org": org.String()}},
		"revoked":   {},
		"agent-token": {Active: true, Subject: "user-1", ClientID: "agent", Audiences: []string{"https://api"}, ExpiresAt: exp,
			Claims: map[string]any{"class": "agent", "org": org.String(), "roles": []any{"accountant"}, "granted_at": granted}},
	}}
	in := &authn.Introspector{OAuth: f, Audience: "https://api", Clock: c, TTL: time.Hour} // capped at 30 s
	ctx := context.Background()
	p, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer api-token"})
	if err != nil || p.Subject != "user-1" || p.Organisation != org || len(p.Roles) != 1 || p.Roles[0] != authz.Accountant ||
		p.Class != authn.ClassUser || len(p.Scopes) != 1 {
		t.Fatalf("an API-audience token: %+v, %v", p, err)
	}
	if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer mcp-token"}); !errors.Is(err, authn.ErrWrongAudience) || !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("an MCP-audience token at the API: %v", err)
	}
	for name, header := range map[string]string{"revoked or unknown": "Bearer revoked", "no class": "Bearer no-class", "no scheme": "api-token",
		"a grant older than 90 days (AI-38)": "Bearer old-grant", "an undated grant": "Bearer undated"} {
		if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: header}); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if p, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer agent-token"}); err != nil || !p.AI() {
		t.Errorf("an agent-class token is the AI acting for the user: %+v, %v", p, err)
	}
	calls := f.calls
	if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + authn.DevPrefix + "abc.def"}); !errors.Is(err, authn.ErrUnauthenticated) || f.calls != calls {
		t.Fatalf("a dev token reached the OAuth server (%d calls): %v", f.calls-calls, err)
	}

	// Cached for at most 30 seconds: a token revoked meanwhile is refused once the cache lapses.
	calls = f.calls
	if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer api-token"}); err != nil || f.calls != calls {
		t.Fatalf("a second call within the cache introspected again (%d) or failed: %v", f.calls-calls, err)
	}
	f.tokens["api-token"] = ports.Introspection{}
	c.Advance(29 * time.Second)
	if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer api-token"}); err != nil {
		t.Fatalf("within 30 seconds the cached answer stands: %v", err)
	}
	c.Advance(2 * time.Second)
	if _, err := in.Authenticate(ctx, authn.Credentials{Authorization: "Bearer api-token"}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("a revoked token was accepted after 31 seconds: %v", err)
	}
}

// req: AI-36
// asvs: V10.3.1
func TestTheLocalChainKeepsDevAndOAuthTokensApart(t *testing.T) {
	dev, oauth := &recorder{}, &recorder{}
	l := authn.Local{Dev: dev, OAuth: oauth}
	_, _ = l.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + authn.DevPrefix + "x.y"})
	_, _ = l.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer ory_at_x"})
	if dev.seen != 1 || oauth.seen != 1 {
		t.Fatalf("dev saw %d, oauth saw %d; want one each", dev.seen, oauth.seen)
	}
	if _, err := (authn.Local{Dev: dev}).Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer ory_at_x"}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("without an OAuth server a token must be refused: %v", err)
	}
}

type recorder struct{ seen int }

func (r *recorder) Authenticate(context.Context, authn.Credentials) (authn.Principal, error) {
	r.seen++
	return authn.Principal{}, authn.ErrUnauthenticated
}

// The release build never takes a local dev token, and sends a bearer token only to the OAuth server
// and a cookie only to the sessions (DEV-07, AI-36).
// req: AI-36
// req: SEC-08
// asvs: V10.3.1
func TestTheReleaseChainTakesNoDevToken(t *testing.T) {
	oauth, sessions := &recorder{}, &recorder{}
	r := authn.Release{OAuth: oauth, Sessions: sessions}
	ctx := context.Background()
	if _, err := r.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + authn.DevPrefix + "x.y", Cookie: "__Host-fyniq_session=s"}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("a dev token: %v", err)
	}
	_, _ = r.Authenticate(ctx, authn.Credentials{Authorization: "Bearer ory_at_x", Cookie: "__Host-fyniq_session=s"})
	_, _ = r.Authenticate(ctx, authn.Credentials{Cookie: "__Host-fyniq_session=s"})
	if oauth.seen != 1 || sessions.seen != 1 {
		t.Fatalf("oauth saw %d, sessions saw %d; want one each", oauth.seen, sessions.seen)
	}
	if _, err := (authn.Release{}).Authenticate(ctx, authn.Credentials{Authorization: "Bearer ory_at_x"}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("an unconfigured release build: %v", err)
	}
}

// A delegated token (open question 40, option (b)) is always the AI acting for its subject, with the
// person's organisation and roles read now, and is refused when it lives longer than 5 minutes or its
// subject is not a user.
// req: AI-36
// req: AI-34
// asvs: V10.3.1
func TestADelegatedTokenIsTheAIWithThePersonsRightsNow(t *testing.T) {
	user, org := uuid.New(), uuid.New()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	roles := []authz.Role{authz.Accountant}
	f := &fakeOAuth{tokens: map[string]ports.Introspection{
		"delegated": {Active: true, Subject: user.String(), ClientID: "mcp-delegation", Audiences: []string{"https://api"}, IssuedAt: now,
			ExpiresAt: now.Add(5 * time.Minute), Claims: map[string]any{"class": "user", "roles": []any{"owner"}}},
		"long": {Active: true, Subject: user.String(), ClientID: "mcp-delegation", Audiences: []string{"https://api"}, IssuedAt: now,
			ExpiresAt: now.Add(time.Hour)},
		"not-a-user": {Active: true, Subject: "someone", ClientID: "mcp-delegation", Audiences: []string{"https://api"}, IssuedAt: now,
			ExpiresAt: now.Add(time.Minute)},
	}}
	in := &authn.Introspector{OAuth: f, Audience: "https://api", Clock: clock.NewFrozen(now.Add(time.Second)), Delegation: &authn.Delegation{
		Client: "mcp-delegation",
		Lookup: func(_ context.Context, u uuid.UUID) (uuid.UUID, []authz.Role, error) { return org, roles, nil },
	}}
	p, err := in.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer delegated"})
	if err != nil || !p.AI() || p.UserID != user || p.Organisation != org || len(p.Roles) != 1 || p.Roles[0] != authz.Accountant {
		t.Fatalf("the delegated principal: %+v, %v (claims must not count: class user, roles owner)", p, err)
	}
	for _, tok := range []string{"long", "not-a-user"} {
		if _, err := in.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + tok}); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s: %v", tok, err)
		}
	}
}
