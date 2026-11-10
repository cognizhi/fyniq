package postgres

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The per-IP windows (SEC-23; migration 000312): a global count, like the organisations' rate-limit windows no change of
// record and so not audited, through count_ip_request and purge_ip_rate_windows.

var _ storage.IPLimits = (*Store)(nil)

// CountIP implements storage.IPLimits.
func (s *Store) CountIP(ctx context.Context, client, kind string, start time.Time) (int, error) {
	used, err := s.q().CountIPRequest(ctx, sqlcdb.CountIPRequestParams{Client: client, Kind: kind, WindowStart: start})
	return int(used), mapError(err)
}

// PurgeIP implements storage.IPLimits.
func (s *Store) PurgeIP(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q().PurgeIPRateWindows(ctx, before)
	return n, mapError(err)
}
