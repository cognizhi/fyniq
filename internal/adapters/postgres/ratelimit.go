package postgres

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Rate-limit windows are a counter, not a change of record: they carry no audit event, like the
// idempotency keys (migration 000017, AUD-08).

func (u *unitOfWork) RateLimits() storage.RateLimits { return rateLimits{u} }

type rateLimits struct{ u *unitOfWork }

func (r rateLimits) Count(ctx context.Context, client, kind string, start time.Time) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	if start.IsZero() {
		return 0, &storage.Error{Code: storage.CodeInvalid, Message: "a rate-limit window needs its start"}
	}
	used, err := r.u.q.CountRequest(ctx, sqlcdb.CountRequestParams{ClientID: client, Kind: kind, WindowStart: start})
	return int(used), mapError(err)
}

func (r rateLimits) Purge(ctx context.Context, before time.Time) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.PurgeRateLimitWindows(ctx, before)
	return n, mapError(err)
}
