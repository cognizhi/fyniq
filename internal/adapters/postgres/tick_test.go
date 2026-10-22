//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Scheduler tick, platform jobs, heartbeats and the after-restore step
// (p0-outbox-background-work, part 2). job_slot and heartbeat are installation-wide, so each test
// uses job names of its own and asserts only on them.

// recordingJobs is a JobRunner that only records what it was asked to start.
type recordingJobs struct {
	mu      sync.Mutex
	started []string
}

func (j *recordingJobs) Start(_ context.Context, job string, slot time.Time) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	name := fmt.Sprintf("%s@%s", job, slot.Format(time.RFC3339))
	j.started = append(j.started, name)
	return "exec-" + name, nil
}

func slotStates(t *testing.T, p storage.Platform, job string) string {
	t.Helper()
	slots, err := p.Slots(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range slots {
		out = append(out, s.Slot.UTC().Format("15:04")+" "+s.State)
	}
	return strings.Join(out, ", ")
}

func ticker(t *testing.T, c *clock.Frozen, jobs *recordingJobs, schedules []outbox.Schedule) outbox.Ticker {
	t.Helper()
	api := store(t, "fyniq_api", postgres.WithClock(c))
	return outbox.Ticker{Store: api, Platform: api, Jobs: jobs, Clock: c, Holder: "tick-test", Schedules: schedules,
		Dispatcher: outbox.Dispatcher{Store: api, Queue: local.NewMemory(), Clock: c}}
}

// Every platform job the tick runs claims its slot and starts: a name the database refuses would leave that job never
// running, as journals.auto-reverse's hyphen did until 000079 (FR-AUT-01, FR-GL-02).
// req: Scheduler tick
// req: FR-AUT-01
func TestEveryPlatformJobClaimsItsSlotAndStarts(t *testing.T) {
	ctx := context.Background()
	c := clock.NewFrozen(time.Date(2031, 1, 6, 23, 59, 0, 0, time.UTC)) // after every slot the other tests use
	// A job's later slot stops the tick from starting an earlier one, so the slots this test claims go when it ends,
	// and the tests that tick in 2026 still start theirs.
	t.Cleanup(func() {
		if _, err := conn(t, "fyniq_migrator").Exec(context.Background(), "DELETE FROM fyniq.job_slot WHERE slot >= '2031-01-01'"); err != nil {
			t.Errorf("removing the 2031 slots: %v", err)
		}
	})
	jobs := &recordingJobs{}
	r, err := ticker(t, c, jobs, outbox.PlatformSchedules).Tick(ctx)
	if err != nil {
		t.Fatalf("the tick: %v", err)
	}
	if len(jobs.started) != len(outbox.PlatformSchedules) {
		t.Errorf("started %d of %d platform jobs: %v (busy %v)", len(jobs.started), len(outbox.PlatformSchedules), jobs.started, r.Busy)
	}
	for _, s := range outbox.PlatformSchedules {
		if !slices.ContainsFunc(jobs.started, func(x string) bool { return strings.HasPrefix(x, s.Job+"@") }) {
			t.Errorf("%s did not start", s.Job)
		}
	}
}

// req: Scheduler tick
// req: Missed slots and overlapping runs
// req: DEV-05
func TestTheTickRunsEachSlotOnceSkipsMissedOnesAndNeverOverlaps(t *testing.T) {
	ctx := context.Background()
	c := clock.NewFrozen(time.Date(2026, 10, 20, 10, 7, 0, 0, time.UTC))
	job := "test.hourly_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	jobs := &recordingJobs{}
	tk := ticker(t, c, jobs, []outbox.Schedule{{Job: job, Every: time.Hour, Lease: 2 * time.Hour, Stale: 3 * time.Hour}})
	worker := store(t, "fyniq_worker")

	if _, err := tk.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	c.Set(time.Date(2026, 10, 20, 10, 12, 0, 0, time.UTC))
	if _, err := tk.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := slotStates(t, tk.Platform, job); got != "10:00 running" || len(jobs.started) != 1 {
		t.Fatalf("after two ticks in one slot: %q, started %v; want the 10:00 slot started once", got, jobs.started)
	}
	if err := outbox.RunSlot(ctx, worker, c, job, time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	// A gap: the tick stopped from 10:12 to 13:20. Only the latest missed slot runs.
	c.Set(time.Date(2026, 10, 20, 13, 20, 0, 0, time.UTC))
	r, err := tk.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := slotStates(t, tk.Platform, job); got != "10:00 done, 11:00 skipped, 12:00 skipped, 13:00 running" || r.Skipped != 2 {
		t.Fatalf("after a gap: %q (skipped %d)", got, r.Skipped)
	}

	// The 13:00 run is still going at 14:05: the 14:00 slot waits rather than overlapping.
	c.Set(time.Date(2026, 10, 20, 14, 5, 0, 0, time.UTC))
	if r, _ := tk.Tick(ctx); len(r.Busy) != 1 || len(jobs.started) != 2 {
		t.Fatalf("an overlapping run started: %+v, started %v", r, jobs.started)
	}
	// At 15:30 the 13:00 run's lease (2 h) has expired: the watchdog gives it up, and the latest slot
	// runs; 14:00, missed while it ran, is recorded as skipped.
	c.Set(time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC))
	if _, err := tk.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := slotStates(t, tk.Platform, job); got != "10:00 done, 11:00 skipped, 12:00 skipped, 13:00 failed, 14:00 skipped, 15:00 running" {
		t.Fatalf("after the lease expired: %q", got)
	}
}

// req: Scheduler tick
// req: Unknown outcomes of outside calls
// req: Outbox row contract
func TestTheSweepResendsStrandedRowsAndLeavesUnknownOutcomesAlone(t *testing.T) {
	ctx := context.Background()
	// Far enough ahead that rows the database stamps now are past the grace.
	c := clock.NewFrozen(time.Date(2027, 1, 4, 9, 0, 0, 0, time.UTC))
	b := books(t, "Sweep Pte Ltd")
	jobs := &recordingJobs{}
	tk := ticker(t, c, jobs, nil)
	q := tk.Dispatcher.Queue.(*local.Memory)
	api := tk.Store

	var pending, overdue, unknown, spent storage.OutboxRow
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o := u.Outbox()
		var err error
		if pending, err = o.Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.stranded"}); err != nil {
			return err
		}
		if overdue, err = o.Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.overdue"}); err != nil {
			return err
		}
		if err = o.MarkDispatched(ctx, overdue.ID, outbox.TaskName(overdue.ID, 1), c.Now().Add(-time.Minute)); err != nil {
			return err
		}
		if unknown, err = o.Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.unknown"}); err != nil {
			return err
		}
		if spent, err = o.Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.spent", MaxAttempts: 1}); err != nil {
			return err
		}
		for _, row := range []storage.OutboxRow{unknown, spent} {
			if err = o.MarkDispatched(ctx, row.ID, outbox.TaskName(row.ID, 1), c.Now().Add(-time.Minute)); err != nil {
				return err
			}
			if _, _, err = o.Lease(ctx, row.ID, "h", c.Now(), c.Now().Add(time.Second)); err != nil {
				return err
			}
		}
		if err = o.Finish(ctx, unknown.ID, "h", storage.OutboxUnknownOutcome, "time-out after sending"); err != nil {
			return err
		}
		return o.Finish(ctx, spent.ID, "h", storage.OutboxFailed, "503")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tk.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	sent := map[string]bool{}
	for {
		task, err := q.Next(ctx, "worker")
		if err != nil {
			break
		}
		sent[task.Name] = true
	}
	if !sent[outbox.TaskName(pending.ID, 1)] || !sent[outbox.TaskName(overdue.ID, 2)] {
		t.Fatalf("the sweep sent %v; want the stranded row's first dispatch and the overdue row's second", sent)
	}
	if sent[outbox.TaskName(unknown.ID, 2)] {
		t.Fatal("the sweep re-dispatched a row waiting on reconciliation")
	}
	for id, want := range map[uuid.UUID]storage.OutboxState{pending.ID: storage.OutboxDispatched, overdue.ID: storage.OutboxDispatched,
		unknown.ID: storage.OutboxUnknownOutcome, spent.ID: storage.OutboxDead} {
		if got := outboxRow(t, b.Org, id); got.State != want {
			t.Errorf("row %s is %s; want %s", got.Kind, got.State, want)
		}
	}
}

// req: Long work as Cloud Run jobs
// req: Heartbeat outside the request path
func TestTheTickStartsTheNightlyVerificationAsAJob(t *testing.T) {
	ctx := context.Background()
	c := clock.NewFrozen(time.Date(2026, 10, 21, 18, 4, 0, 0, time.UTC)) // 02:04 in Singapore
	worker := store(t, "fyniq_worker", postgres.WithClock(c))
	blobs, signer := anchoring(t)
	b := books(t, "Nightly Pte Ltd")
	var wg sync.WaitGroup
	runner := jobFunc(func(ctx context.Context, job string, slot time.Time) (string, error) {
		wg.Add(1)
		go func() { // the job runs on its own, after the tick's request ended
			defer wg.Done()
			_ = outbox.RunSlot(context.WithoutCancel(ctx), worker, c, job, slot, func(ctx context.Context) error {
				// Every organisation is verified; earlier tests tampered with some trails on purpose,
				// so this run is judged by its own organisation's report.
				reports, _ := audit.VerifyAll(ctx, worker, blobs, signer)
				for _, rep := range reports {
					if rep.Organisation == b.Org && !rep.OK() {
						return fmt.Errorf("verification failed: %s", rep.Problem)
					}
				}
				return nil
			})
		}()
		return "local-" + job, nil
	})
	api := store(t, "fyniq_api", postgres.WithClock(c))
	verify := outbox.PlatformSchedules[1]
	tk := outbox.Ticker{Store: api, Platform: api, Jobs: runner, Clock: c, Holder: "tick-test", Schedules: []outbox.Schedule{verify},
		Dispatcher: outbox.Dispatcher{Store: api, Queue: local.NewMemory(), Clock: c}}
	r, err := tk.Tick(ctx)
	if err != nil || len(r.Started) != 1 || r.Started[0] != "audit.verify@2026-10-21T18:00:00Z" {
		t.Fatalf("Tick = %+v, %v; want the 18:00 UTC verification started", r, err)
	}
	wg.Wait()
	if got := slotStates(t, api, "audit.verify"); !strings.Contains(got, "18:00 done") { // UTC
		t.Fatalf("the verification slot: %q", got)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer)
	if rep := reportFor(t, reports, b.Org); !rep.OK() {
		t.Fatalf("the job verified a trail that fails: %+v", rep)
	}
	beats, _ := api.Heartbeats(ctx)
	// The table is installation-wide and keeps each beat's latest time, so another test's later tick
	// may already be there; the verification's own beat is this run's.
	if !beats["audit.verify"].Equal(c.Now()) || beats["tick"].Before(c.Now()) {
		t.Fatalf("heartbeats %v; want the verification at %s and a tick no earlier", beats, c.Now())
	}
}

type jobFunc func(ctx context.Context, job string, slot time.Time) (string, error)

func (f jobFunc) Start(ctx context.Context, job string, slot time.Time) (string, error) {
	return f(ctx, job, slot)
}

// req: Heartbeat outside the request path
func TestTheAbsenceAlertFiresWhenTheTickStops(t *testing.T) {
	ctx := context.Background()
	c := clock.NewFrozen(time.Date(2031, 3, 3, 3, 0, 0, 0, time.UTC)) // later than every other test's beat
	tk := ticker(t, c, &recordingJobs{}, nil)
	if _, err := tk.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Minute)
	if alerts, err := outbox.Absences(ctx, tk.Platform, c.Now(), nil); err != nil || len(alerts) != 0 {
		t.Fatalf("a minute after a tick: %v, %v; want no alert", alerts, err)
	}
	c.Advance(16 * time.Minute) // the tick stopped: three intervals pass without a beat
	alerts, err := outbox.Absences(ctx, tk.Platform, c.Now(), nil)
	if err != nil || len(alerts) != 1 || !strings.HasPrefix(alerts[0], "tick last beat 17m") {
		t.Fatalf("after 17 minutes without a tick: %v, %v", alerts, err)
	}
	if alerts, _ := outbox.Absences(ctx, tk.Platform, c.Now(), []outbox.Schedule{{Job: "test.never_ran", Stale: time.Hour}}); len(alerts) != 2 {
		t.Fatalf("a job that never beat must alert too: %v", alerts)
	}
}

// req: Outbox after a database restore
func TestAfterARestoreUnfinishedWorkWaitsForReconciliation(t *testing.T) {
	ctx := context.Background()
	c := clock.NewFrozen(time.Date(2027, 2, 1, 9, 0, 0, 0, time.UTC))
	b := books(t, "Restored Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	var done, waiting storage.OutboxRow
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o := u.Outbox()
		var err error
		if done, err = o.Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.done"}); err != nil {
			return err
		}
		_ = o.MarkDispatched(ctx, done.ID, outbox.TaskName(done.ID, 1), c.Now().Add(time.Hour))
		_, _, _ = o.Lease(ctx, done.ID, "h", c.Now(), c.Now().Add(time.Minute))
		if err = o.Finish(ctx, done.ID, "h", storage.OutboxDone, ""); err != nil {
			return err
		}
		waiting, err = o.Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.in_flight"})
		if err == nil {
			err = o.MarkDispatched(ctx, waiting.ID, outbox.TaskName(waiting.ID, 1), c.Now().Add(-time.Hour))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		n, err = u.Sweeping().AfterRestore(ctx)
		return err
	}); err != nil || n != 1 {
		t.Fatalf("AfterRestore = %d, %v; want the one unfinished row", n, err)
	}
	if got := outboxRow(t, b.Org, waiting.ID); got.State != storage.OutboxUnknownOutcome {
		t.Fatalf("the in-flight row is %s; want unknown_outcome", got.State)
	}
	if got := outboxRow(t, b.Org, done.ID); got.State != storage.OutboxDone {
		t.Fatalf("the finished row is %s; want done", got.State)
	}
	tk := ticker(t, c, &recordingJobs{}, nil)
	if _, err := tk.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tk.Dispatcher.Queue.(*local.Memory).Next(ctx, "worker"); err == nil {
		t.Fatal("the tick re-dispatched work after a restore without reconciliation")
	}
}
