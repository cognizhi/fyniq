package wiring

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Checks is the checks engine's runner (FR-CTL-01, p1-checks design D5): a check whose run fails is logged here, and its
// result only says it failed (SEC-15).
func (k *Kit) Checks(clock ports.Clock) checks.Runner {
	var jobs []checks.WatchedJob
	for _, s := range outbox.PlatformSchedules {
		if _, ok := checks.ScheduledJobs[s.Job]; ok {
			jobs = append(jobs, checks.WatchedJob{Name: s.Job, Stale: s.Stale})
		}
	}
	return checks.Runner{Store: k.Store, Clock: clock, Platform: k.Store, Jobs: jobs, Failed: func(ctx context.Context, org uuid.UUID, key string, err error) {
		slog.Default().ErrorContext(ctx, "checks.failed", "organisation", org.String(), "check", key, "error", err.Error())
	}}
}

// CheckKinds are the checks' background work: a refresh after a change, and Run now.
func (k *Kit) CheckKinds(clock ports.Clock) map[string]func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	r := k.Checks(clock)
	return map[string]func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error{
		checks.RefreshKind: func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
			// One refresh at a time: while one that took its lease first runs, this one waits a minute and is delivered
			// again, so a burst of changes on a large ledger never stacks refreshes up (NFR-PERF-01; p1-operations-dr task
			// 13.13). A row without a lease waits behind any that runs.
			until := clock.Now().Add(24 * time.Hour)
			if row.LeaseUntil != nil {
				until = *row.LeaseUntil
			}
			var ahead *storage.OutboxRow
			if err := k.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				ahead, err = u.Outbox().Ahead(ctx, checks.RefreshKind, row.ID, until, clock.Now())
				return err
			}); err != nil {
				return err
			}
			if ahead != nil {
				return outbox.Held{For: checks.Pace, Reason: "another refresh of the checks is running"}
			}
			_, err := r.Paced(ctx, org) // at most once a minute through a burst of changes
			return err
		},
		checks.RequestKind: func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
			by, err := checks.RequestedBy(row.Payload)
			if err != nil {
				return err
			}
			_, err = r.Request(ctx, org, by)
			return err
		},
	}
}

// watchChanges makes every unit of work that changes something the checks react to ask for a refresh, once while one
// waits (FR-CTL-01, design D5): the API's commands, the worker's and the jobs' writes alike, so no path is missed.
func (k *Kit) watchChanges(clock ports.Clock) {
	defs := checks.Catalogue()
	d := k.dispatcher(clock)
	k.Store.OnChange(func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, types []string) error {
		if !checks.Reacts(defs, types) {
			return nil
		}
		if waiting, err := u.Outbox().Waiting(ctx, checks.RefreshKind); err != nil || waiting != nil {
			return err
		}
		_, err := d.Enqueue(ctx, u, org, checks.RefreshItem())
		return err
	})
}
