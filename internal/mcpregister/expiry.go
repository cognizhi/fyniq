package mcpregister

import (
	"context"
	"log/slog"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The periods after which a registration lapses (design D14): one nobody approves within 7 days, one approved and never used within 30.
const (
	PendingFor = 7 * 24 * time.Hour
	UnusedFor  = 30 * 24 * time.Hour
)

// ExpireJob is the platform job that ends the registrations that lapsed, every hour (outbox.PlatformSchedules). The API also does it
// as registrations come in and when an Owner looks at the pending ones; the job makes it happen when nobody does.
const ExpireJob = "mcp.registrations.expire"

// Expire marks the registrations that lapsed as expired, and removes their clients from the OAuth server so a lapsed registration
// cannot sign anyone in. It gives how many lapsed. A client that cannot be removed is logged and the next lapse is still taken: the
// registration is expired already, which the consent gate refuses, so the client is of no use meanwhile. oauth may be nil (the
// worker without the OAuth server's admin address): then the registrations are marked and nothing is removed.
func Expire(ctx context.Context, clients storage.MCPClients, oauth ports.OAuthClients, now time.Time, log *slog.Logger) (int, error) {
	ids, err := clients.ExpireClients(ctx, now, PendingFor, UnusedFor)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		log.InfoContext(ctx, "AI app registration expired", "event", "mcp.registration_expired", "client", id)
		if oauth == nil {
			continue
		}
		if err := oauth.RemoveClient(ctx, id); err != nil {
			log.WarnContext(ctx, "mcp.expired_client_not_removed", "client", id, "error", err.Error())
		}
	}
	return len(ids), nil
}
