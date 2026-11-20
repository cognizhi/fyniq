package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The states of a task made by an integration (FR-INT-24, rev 0.607): open, then finished for good.
const (
	IntegrationTaskOpen      = "open"
	IntegrationTaskDone      = "done"
	IntegrationTaskCancelled = "cancelled"
)

// IntegrationTask is something an API client with the scope fyniq.write.tasks asked a person to do. It is the client's own: only the client that made it
// (ClientID, the OAuth client behind the call) updates it, and it is not a compliance task.
type IntegrationTask struct {
	ID          uuid.UUID
	ClientID    string
	ExternalRef string
	Title       string
	Detail      string
	DueOn       *time.Time // a date
	LinkURL     string
	Status      string
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    *time.Time
	Version     int
}

// NewIntegrationTask is a task for a client to make.
type NewIntegrationTask struct {
	ClientID    string
	ExternalRef string
	Title       string
	Detail      string
	DueOn       *time.Time
	LinkURL     string
	CreatedBy   uuid.UUID
	At          time.Time
}

// IntegrationTaskChange is a client's update of its own task over the version it read: the whole of the fields it may change.
type IntegrationTaskChange struct {
	Title   string
	Detail  string
	DueOn   *time.Time
	LinkURL string
	Status  string // open, done or cancelled
	At      time.Time
}

// IntegrationTaskQuery pages tasks newest first: in these states (any when empty), made by ClientID (every client's when empty).
type IntegrationTaskQuery struct {
	Statuses []string
	ClientID string
	After    uuid.UUID
	Limit    int
}

// IntegrationTasks keeps the tasks API clients made. Each change is audited in its transaction. Nothing here reaches a compliance task.
type IntegrationTasks interface {
	// Make records a client's task, or returns the one the client already made with this reference (created false): a retried create.
	Make(ctx context.Context, n NewIntegrationTask) (t IntegrationTask, created bool, err error)
	Get(ctx context.Context, id uuid.UUID) (IntegrationTask, error)
	List(ctx context.Context, q IntegrationTaskQuery) ([]IntegrationTask, error)
	// Update changes the task client made, over the version read: ports.ErrNotFound for a task of another client, a CodeVersion error when it has moved on.
	Update(ctx context.Context, client string, id uuid.UUID, version int, c IntegrationTaskChange) (IntegrationTask, error)
}
