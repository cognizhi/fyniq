package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var _ storage.MCPClients = (*Store)(nil)

// RegistrationOpen implements storage.MCPClients.
func (s *Store) RegistrationOpen(ctx context.Context, now time.Time) (bool, error) {
	open, err := s.q().MCPRegistrationOpen(ctx, now)
	return open, mapError(err)
}

// Register implements storage.MCPClients.
func (s *Store) Register(ctx context.Context, c storage.MCPClientRegistration, l storage.MCPClientLimits) (string, error) {
	refused, err := s.q().RegisterMCPClient(ctx, sqlcdb.RegisterMCPClientParams{ID: c.ID, Name: c.Name, Uris: c.RedirectURIs, Network: c.Network, Now: c.At,
		PerHour: int32(l.PerHour), PerDay: int32(l.PerDay), PendingCap: int32(l.PendingCap)}) // #nosec G115 -- small limits
	return refused, mapError(err)
}

// ClientStatus implements storage.MCPClients.
func (s *Store) ClientStatus(ctx context.Context, id string) (string, error) {
	st, err := s.q().MCPClientStatus(ctx, id)
	return st, mapError(err)
}

// DecideClient implements storage.MCPClients.
func (s *Store) DecideClient(ctx context.Context, id, status string, now time.Time) (bool, error) {
	ok, err := s.q().DecideMCPClient(ctx, sqlcdb.DecideMCPClientParams{ID: id, Status: status, Now: now})
	return ok, mapError(err)
}

// TouchClient implements storage.MCPClients.
func (s *Store) TouchClient(ctx context.Context, id string, now time.Time) error {
	return mapError(s.q().TouchMCPClient(ctx, sqlcdb.TouchMCPClientParams{ID: id, Now: now}))
}

// PendingClients implements storage.MCPClients.
func (s *Store) PendingClients(ctx context.Context) ([]storage.MCPPendingClient, error) {
	rows, err := s.q().PendingMCPClients(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MCPPendingClient, len(rows))
	for i, r := range rows {
		out[i] = storage.MCPPendingClient{ID: r.ClientID, Name: r.Name, RedirectURIs: r.RedirectUris, RegisteredAt: r.RegisteredAt}
	}
	return out, nil
}

// ExpireClients implements storage.MCPClients.
func (s *Store) ExpireClients(ctx context.Context, now time.Time, pending, unused time.Duration) ([]string, error) {
	ids, err := s.q().ExpireMCPClients(ctx, sqlcdb.ExpireMCPClientsParams{Now: now, Pending: interval(pending), Unused: interval(unused)})
	return ids, mapError(err)
}

func interval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
