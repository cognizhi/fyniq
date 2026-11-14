package authn_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type grantStore map[string]storage.AIGrantHolder

func (g grantStore) AIGrant(_ context.Context, org uuid.UUID, hash []byte, _ time.Time) (storage.AIGrantHolder, error) {
	h, ok := g[org.String()+string(hash)]
	if !ok {
		return storage.AIGrantHolder{}, ports.ErrNotFound
	}
	return h, nil
}

// A delegated AI read token is the organisation in the clear and 32 random bytes, kept only as its SHA-256; it
// authenticates as the AI acting for the person (class agent, AI-34) with the person's known roles, scoped to the AI's
// reads and its turn; anything else, or a token the store does not resolve, is unauthenticated, and the release and
// local builds route it only to this authenticator (PRD-001 §9.3 rev 0.277).
// req: AI-01
// req: ai/proposals The orchestrator holds no write credential
// asvs: V7.2.3, V8.2.1
func TestDelegatedAITokens(t *testing.T) {
	org, user, turn := uuid.New(), uuid.New(), uuid.New()
	token, hash, err := authn.NewAIToken(org)
	if err != nil {
		t.Fatal(err)
	}
	again, _, _ := authn.NewAIToken(org)
	if again == token || !strings.HasPrefix(token, authn.AITokenPrefix) || len(hash) != 32 {
		t.Fatalf("tokens: %q %q", token, again)
	}
	if o, h, ok := authn.ParseAIToken(token); !ok || o != org || string(h) != string(hash) {
		t.Fatalf("parsed: %v %x %v", o, h, ok)
	}
	for _, bad := range []string{"", "fyai_", "fyai_!!", token[:len(token)-4], "fyau_" + token[5:], authn.AITokenPrefix + strings.Repeat("A", 64)} {
		if _, _, ok := authn.ParseAIToken(bad); ok {
			t.Errorf("parsed %q", bad)
		}
	}

	store := grantStore{org.String() + string(hash): {UserID: user, Turn: turn, Roles: []string{"bookkeeper", "no_such_role"}}}
	a := authn.AIGrants{Store: store, Clock: clock.NewFrozen(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))}
	p, err := a.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + token})
	if err != nil || !p.AI() || p.UserID != user || p.Turn != turn || p.Organisation != org || p.Via != authn.ViaToken ||
		!slices.Equal(p.Roles, []authz.Role{authz.Bookkeeper}) || !slices.Equal(p.Scopes, []string{authn.AIScope}) || p.Client != authn.AssistantClient {
		t.Fatalf("the principal: %+v %v", p, err)
	}
	for name, c := range map[string]authn.Credentials{
		"an unknown token":   {Authorization: "Bearer " + again},
		"no bearer":          {Authorization: token},
		"a cookie":           {Cookie: "fyniq_session=" + token},
		"another credential": {Authorization: "Bearer someone-else"},
	} {
		if _, err := a.Authenticate(context.Background(), c); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The builds send the token only to it: without one, it authenticates nobody.
	for name, x := range map[string]authn.Authenticator{"release": authn.Release{}, "local": authn.Local{}} {
		if _, err := x.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + token}); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s without the AI authenticator: %v", name, err)
		}
	}
	for name, x := range map[string]authn.Authenticator{"release": authn.Release{AI: a}, "local": authn.Local{AI: a}} {
		if p, err := x.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + token}); err != nil || p.Turn != turn {
			t.Errorf("%s with it: %+v %v", name, p, err)
		}
	}
}
