package wiring

import (
	"context"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PlatformJobs are the functions behind the platform's schedules (outbox.PlatformSchedules), run by
// the worker locally and by the jobs image as Cloud Run jobs in production.
func (k *Kit) PlatformJobs(clock ports.Clock) map[string]func(ctx context.Context) error {
	return map[string]func(ctx context.Context) error{
		"audit.anchor": func(ctx context.Context) error {
			_, err := audit.AnchorAll(ctx, k.Store, k.Blobs, k.Signer, clock)
			return err
		},
		"audit.verify": func(ctx context.Context) error {
			_, err := audit.VerifyAll(ctx, k.Store, k.Blobs, k.Signer)
			return err
		},
		"idempotency.purge": func(ctx context.Context) error {
			_, err := PurgeIdempotencyKeys(ctx, k.Store, clock.Now())
			return err
		},
		"ledger.verify": func(ctx context.Context) error {
			_, err := balance.VerifyAll(ctx, k.Store)
			return err
		},
		"compliance.evidence": func(ctx context.Context) error {
			_, err := compliance.RecordEvidence(ctx, k.Store, k.Blobs, clock.Now())
			return err
		},
	}
}

// PurgeIdempotencyKeys deletes every organisation's idempotency keys that expired at or before now
// (FR-INT-08) and returns how many. An expired key that is not yet purged is refused, not replayed.
func PurgeIdempotencyKeys(ctx context.Context, store storage.Store, now time.Time) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, org := range orgs {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.Idempotency().Purge(ctx, now)
			total += n
			return err
		})
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}
