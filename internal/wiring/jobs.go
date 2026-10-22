package wiring

import (
	"context"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/compliance"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/superadmin"
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
		"ratelimit.purge": func(ctx context.Context) error {
			_, err := PurgeRateLimitWindows(ctx, k.Store, clock.Now())
			return err
		},
		"ledger.verify": func(ctx context.Context) error {
			_, err := balance.VerifyAll(ctx, k.Store)
			return err
		},
		"ledger.replay": func(ctx context.Context) error {
			_, err := journal.ReplayAll(ctx, k.Store)
			return err
		},
		"superadmins.verify": func(ctx context.Context) error {
			_, err := superadmin.Verify(ctx, k.Store, k.Store)
			return err
		},
		"compliance.evidence": func(ctx context.Context) error {
			_, err := compliance.RecordEvidence(ctx, k.Store, k.Blobs, clock.Now())
			return err
		},
		"evidence.commit": func(ctx context.Context) error {
			c, ok := k.Committer(clock)
			if !ok {
				return nil // no evidence stores: nothing can have been attached
			}
			_, err := c.CommitAll(ctx)
			return err
		},
		"journals.auto-reverse": func(ctx context.Context) error {
			_, err := journal.ReverseDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"journals.recurring": func(ctx context.Context) error {
			_, err := journal.MakeDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"fx.feed": func(ctx context.Context) error {
			if k.Rates == nil {
				return nil // no feed configured: organisations enter their own rates
			}
			_, err := fx.RecordFeed(ctx, k.Store, k.Rates, clock.Now())
			return err
		},
		"evidence.verify": func(ctx context.Context) error {
			if k.Evidence == nil {
				return nil
			}
			_, err := evidence.Verify(ctx, k.Store, k.Evidence)
			return err
		},
		"evidence.sweep": func(ctx context.Context) error {
			if k.Staging == nil || k.Evidence == nil {
				return nil
			}
			_, err := evidence.Sweep(ctx, k.Store, k.Staging, k.Evidence, clock.Now())
			return err
		},
	}
}

// Committer commits attached files of records that are no longer drafts to the locked store (FR-DOC-13);
// ok is false when the kit has no evidence stores.
func (k *Kit) Committer(clock ports.Clock) (evidence.Committer, bool) {
	if k.Staging == nil || k.Evidence == nil {
		return evidence.Committer{}, false
	}
	return evidence.Committer{Store: k.Store, Staging: k.Staging, Locked: k.Evidence, Clock: clock, Zone: appclock.Singapore}, true
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

// rateLimitKept is how long an ended rate-limit window is kept: a margin for API instances whose clocks
// differ a little.
const rateLimitKept = 5 * time.Minute

// PurgeRateLimitWindows deletes every organisation's rate-limit windows that started more than
// rateLimitKept before now (FR-INT-16) and returns how many. A window is only a count for its minute.
func PurgeRateLimitWindows(ctx context.Context, store storage.Store, now time.Time) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, org := range orgs {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.RateLimits().Purge(ctx, now.Add(-rateLimitKept))
			total += n
			return err
		})
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}
