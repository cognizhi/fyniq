package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Operation is long work a person or client asked for, which a job does in the background (FR-INT-11): the caller
// polls it. Its status is its job's.
type Operation struct {
	ID          uuid.UUID
	Kind        string // report_export or checks_run
	JobID       uuid.UUID
	Resource    string // the path of what the work makes or changes, such as /v1/report-exports/{id}
	RequestedBy string
	RequestedAt time.Time
	JobState    OutboxState // the job's state now
	JobUpdated  time.Time   // when the job's state last changed
}

// NewOperation is an operation to record, with the job it names.
type NewOperation struct {
	Kind        string
	JobID       uuid.UUID
	Resource    string
	RequestedBy string
	RequestedAt time.Time
}

// Operations keeps the organisation's operations; one never changes (FY284).
type Operations interface {
	Create(ctx context.Context, n NewOperation) (Operation, error)
	// Get is an operation with its job's state now: CodeNotFound when there is none.
	Get(ctx context.Context, id uuid.UUID) (Operation, error)
}
