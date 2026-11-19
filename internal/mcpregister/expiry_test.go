package mcpregister

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type expiring struct {
	storage.MCPClients
	ids          []string
	pending, use time.Duration
}

func (e *expiring) ExpireClients(_ context.Context, _ time.Time, pending, unused time.Duration) ([]string, error) {
	e.pending, e.use = pending, unused
	return e.ids, nil
}

type removing struct {
	ports.OAuthClients
	removed []string
	fail    map[string]bool
}

func (r *removing) RemoveClient(_ context.Context, id string) error {
	if r.fail[id] {
		return errors.New("the OAuth server is down")
	}
	r.removed = append(r.removed, id)
	return nil
}

// The job that ends lapsed registrations takes the periods the design gives (a week unapproved, a month unused), removes each client
// from the OAuth server, carries on past one it cannot remove, and with no OAuth server only marks them (AI-90, design D14).
// req: AI-90
// asvs: V10.4.1, V7.4.1
func TestTheExpiryJobEndsLapsedRegistrationsAndRemovesTheirClients(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := &expiring{ids: []string{"mcp-a", "mcp-b", "mcp-c"}}
	hydra := &removing{fail: map[string]bool{"mcp-b": true}}
	n, err := Expire(context.Background(), db, hydra, clock.System{}.Now(), log)
	if err != nil || n != 3 {
		t.Fatalf("expired %d, %v", n, err)
	}
	if db.pending != 7*24*time.Hour || db.use != 30*24*time.Hour {
		t.Errorf("the periods: %v and %v", db.pending, db.use)
	}
	if len(hydra.removed) != 2 || hydra.removed[0] != "mcp-a" || hydra.removed[1] != "mcp-c" {
		t.Errorf("removed %v: one that cannot be removed must not stop the rest", hydra.removed)
	}
	if n, err := Expire(context.Background(), db, nil, clock.System{}.Now(), log); err != nil || n != 3 {
		t.Errorf("with no OAuth server: %d, %v", n, err)
	}
}
