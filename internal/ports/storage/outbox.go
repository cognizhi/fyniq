package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The transactional outbox (§12.11, ADR-T7): background work is a row written in the same unit of
// work as the change it belongs to, dispatched after commit, leased by its handler, and finished.

// WorkerClass is a queue plus a service with its own identity (§12.3): the general worker, the
// proposal executor (approved proposals only) and the long-running jobs.
type WorkerClass string

// The worker classes.
const (
	ClassWorker   WorkerClass = "worker"
	ClassExecutor WorkerClass = "executor"
	ClassJobs     WorkerClass = "jobs"
)

// OutboxState is where a row is in the contract.
type OutboxState string

// The outbox states.
const (
	OutboxPending        OutboxState = "pending"
	OutboxDispatched     OutboxState = "dispatched"
	OutboxRunning        OutboxState = "running"
	OutboxDone           OutboxState = "done"
	OutboxFailed         OutboxState = "failed"
	OutboxUnknownOutcome OutboxState = "unknown_outcome"
	OutboxDead           OutboxState = "dead"
)

// NewOutboxItem is work to do after the change commits. BusinessKey, when set, is unique per kind:
// the same work is never queued twice.
type NewOutboxItem struct {
	Class       WorkerClass
	Kind        string // e.g. "audit.verify", "notification.send"
	Payload     []byte // a JSON object
	BusinessKey string
	MaxAttempts int // 0 means the default, 5
}

// OutboxRow is a row of the outbox.
type OutboxRow struct {
	ID          uuid.UUID
	Class       WorkerClass
	Kind        string
	Payload     []byte
	BusinessKey string
	State       OutboxState
	Attempts    int
	MaxAttempts int
	Dispatches  int
	TaskName    string
	LeaseHolder string
	LeaseUntil  *time.Time
	Deadline    *time.Time
	LastError   string
}

// Outbox is the organisation's outbox.
type Outbox interface {
	// Enqueue writes a row in this unit of work; it is dispatched only after commit.
	Enqueue(ctx context.Context, item NewOutboxItem) (OutboxRow, error)
	Get(ctx context.Context, id uuid.UUID) (OutboxRow, error)
	// ByKey is the row of a kind written with a business key, nil when there is none: what a job keyed once checks before
	// it is written, so asking again in a later unit of work does nothing.
	ByKey(ctx context.Context, kind, key string) (*OutboxRow, error)
	// MarkDispatched records the task a row was sent as, and when it is overdue.
	MarkDispatched(ctx context.Context, id uuid.UUID, taskName string, deadline time.Time) error
	// Lease gives the row to holder until until, counting an attempt. ok is false when the row is
	// not available: done, dead, waiting on reconciliation, or leased by another holder still in time.
	Lease(ctx context.Context, id uuid.UUID, holder string, now, until time.Time) (row OutboxRow, ok bool, err error)
	// Finish ends holder's lease with an outcome: done, failed, unknown_outcome or dead.
	Finish(ctx context.Context, id uuid.UUID, holder string, state OutboxState, lastError string) error
	// Hold ends holder's lease without an outcome: the row waits until until, dispatched again then, and the attempt
	// its lease counted is given back (FR-INT-26: work held while its connectors are paused spends no attempt).
	Hold(ctx context.Context, id uuid.UUID, holder string, until time.Time, why string) error
}

// Sweeping is what the tick needs from an organisation's outbox (§12.11).
type Sweeping interface {
	// Due returns rows never dispatched (created before pendingBefore) and rows dispatched, running
	// or failed past their deadline. Unknown outcomes and dead rows are never returned.
	Due(ctx context.Context, now, pendingBefore time.Time) ([]OutboxRow, error)
	// MarkDead ends a row whose attempts ran out.
	MarkDead(ctx context.Context, id uuid.UUID, reason string) error
	// AfterRestore marks every unfinished row unknown_outcome, to be reconciled before any retry, and
	// returns how many (Outbox after a database restore).
	AfterRestore(ctx context.Context) (int64, error)
}

// Platform is the installation's own schedules and heartbeats (§12.11): job slots that run at most
// once and never overlap, and the heartbeats an absence alert watches.
type Platform interface {
	// ClaimSlot starts job's run for slot; false when the slot already ran or was skipped, or another
	// run of the job still holds its lease.
	ClaimSlot(ctx context.Context, job string, slot time.Time, holder string, until, now time.Time) (bool, error)
	// SkipSlots records slots a gap made the job miss; they never run.
	SkipSlots(ctx context.Context, job string, slots []time.Time, now time.Time) (int, error)
	StartedSlot(ctx context.Context, job string, slot time.Time, execution string) error
	FinishSlot(ctx context.Context, job string, slot time.Time, ok bool, detail string, now time.Time) (bool, error)
	// LatestSlot is the latest slot the job claimed or skipped; ok is false when it never ran.
	LatestSlot(ctx context.Context, job string) (slot time.Time, ok bool, err error)
	Slots(ctx context.Context, job string) ([]JobSlot, error)
	Beat(ctx context.Context, name string, at time.Time, detail string) error
	Heartbeats(ctx context.Context) (map[string]time.Time, error)
}

// JobSlot is one run (or skipped slot) of a platform job.
type JobSlot struct {
	Job       string
	Slot      time.Time
	State     string // running, done, failed or skipped
	Execution string
	Detail    string
}
