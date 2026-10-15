package postgres

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var _ storage.Platform = (*Store)(nil)

func (s *Store) q() *sqlcdb.Queries { return sqlcdb.New(s.pool) }

// ClaimSlot implements storage.Platform.
func (s *Store) ClaimSlot(ctx context.Context, job string, slot time.Time, holder string, until, now time.Time) (bool, error) {
	ok, err := s.q().ClaimJobSlot(ctx, sqlcdb.ClaimJobSlotParams{Job: job, Slot: slot, Holder: holder, Until: until, Now: now})
	return ok, mapError(err)
}

// SkipSlots implements storage.Platform.
func (s *Store) SkipSlots(ctx context.Context, job string, slots []time.Time, now time.Time) (int, error) {
	n, err := s.q().SkipJobSlots(ctx, sqlcdb.SkipJobSlotsParams{Job: job, Slots: slots, Now: now})
	return int(n), mapError(err)
}

// StartedSlot implements storage.Platform.
func (s *Store) StartedSlot(ctx context.Context, job string, slot time.Time, execution string) error {
	return mapError(s.q().StartedJobSlot(ctx, sqlcdb.StartedJobSlotParams{Job: job, Slot: slot, Execution: execution}))
}

// FinishSlot implements storage.Platform.
func (s *Store) FinishSlot(ctx context.Context, job string, slot time.Time, ok bool, detail string, now time.Time) (bool, error) {
	done, err := s.q().FinishJobSlot(ctx, sqlcdb.FinishJobSlotParams{Job: job, Slot: slot, Ok: ok, Detail: detail, Now: now})
	return done, mapError(err)
}

// LatestSlot implements storage.Platform.
func (s *Store) LatestSlot(ctx context.Context, job string) (time.Time, bool, error) {
	t, err := s.q().LatestJobSlot(ctx, job)
	if err != nil {
		return time.Time{}, false, mapError(err)
	}
	return t, t.Unix() != 0, nil
}

// Slots implements storage.Platform.
func (s *Store) Slots(ctx context.Context, job string) ([]storage.JobSlot, error) {
	rows, err := s.q().ListJobSlots(ctx, job)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.JobSlot, len(rows))
	for i, r := range rows {
		out[i] = storage.JobSlot{Job: r.Job, Slot: r.Slot, State: r.State, Execution: r.Execution, Detail: r.Detail}
	}
	return out, nil
}

// Beat implements storage.Platform.
func (s *Store) Beat(ctx context.Context, name string, at time.Time, detail string) error {
	return mapError(s.q().Beat(ctx, sqlcdb.BeatParams{Name: name, At: at, Detail: detail}))
}

// Heartbeats implements storage.Platform.
func (s *Store) Heartbeats(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.q().ListHeartbeats(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		out[r.Name] = r.LastBeat
	}
	return out, nil
}
