//go:build local

package wiring

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A refresh of the checks delivered while another that took its lease first runs is held for checks.Pace and runs
// nothing, so a burst of changes on a large ledger never stacks refreshes up; the one that leased first is not held by
// a later one, so refreshes delivered together never all wait (p1-operations-dr task 13.13; at 10 M lines eighteen
// refreshes ran at once). Once the other finished, none is ahead: TestWorkHeldForItsOwnWaitWhileAnotherOfItsKindRuns.
// req: NFR-PERF-01
// req: FR-CTL-01
func TestARefreshOfTheChecksWaitsWhileAnotherRuns(t *testing.T) {
	ctx := context.Background()
	w := startConnectors(t, false)
	k := &Kit{Store: w.worker}
	refresh := k.CheckKinds(w.clock)[checks.RefreshKind]
	d := outbox.Dispatcher{Store: w.api, Queue: local.NewMemory(), Clock: w.clock}
	var other storage.OutboxRow
	if err := w.api.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		other, err = d.Enqueue(ctx, u, w.org, checks.RefreshItem())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := w.clock.Now()
	if err := w.worker.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, ok, err := u.Outbox().Lease(ctx, other.ID, "worker-2", now, now.Add(10*time.Minute))
		if err == nil && !ok {
			err = errors.New("the other refresh could not be leased")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	later, earlier := now.Add(11*time.Minute), now.Add(9*time.Minute)
	var held outbox.Held
	if err := refresh(ctx, w.org, storage.OutboxRow{ID: uuid.New(), LeaseUntil: &later}); !errors.As(err, &held) ||
		held.For != checks.Pace || !errors.Is(err, outbox.ErrHeld) {
		t.Fatalf("a refresh while another runs: %v; want held for %s", err, checks.Pace)
	}
	if err := refresh(ctx, w.org, storage.OutboxRow{ID: uuid.New(), LeaseUntil: &earlier}); errors.Is(err, outbox.ErrHeld) {
		t.Fatalf("a refresh that leased before the one running was held: %v", err)
	}
}
