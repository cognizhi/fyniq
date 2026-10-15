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
	// MarkDispatched records the task a row was sent as, and when it is overdue.
	MarkDispatched(ctx context.Context, id uuid.UUID, taskName string, deadline time.Time) error
	// Lease gives the row to holder until until, counting an attempt. ok is false when the row is
	// not available: done, dead, waiting on reconciliation, or leased by another holder still in time.
	Lease(ctx context.Context, id uuid.UUID, holder string, now, until time.Time) (row OutboxRow, ok bool, err error)
	// Finish ends holder's lease with an outcome: done, failed, unknown_outcome or dead.
	Finish(ctx context.Context, id uuid.UUID, holder string, state OutboxState, lastError string) error
}
