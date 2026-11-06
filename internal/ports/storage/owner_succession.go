package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OwnerSuccession is a succession of an Owner who is permanently unavailable (FR-ADM-09; rev 0.181): the outgoing Owner,
// the member it names to become an Owner, if any, why and on what evidence, its 7 days of notice, and how it ended.
type OwnerSuccession struct {
	ID                    uuid.UUID
	DocumentID            uuid.UUID // holds its evidence files
	OutgoingMembershipID  uuid.UUID
	OutgoingUserID        uuid.UUID
	SuccessorMembershipID *uuid.UUID
	Reason                string
	EvidenceKind          string // board_resolution or accountant_confirmation
	StartedBy             uuid.UUID
	StartedAt             time.Time
	DueAt                 time.Time // 7 days after it started: when a Super admin may complete it
	Status                string    // notice, objected, withdrawn or completed
	EndedBy               *uuid.UUID
	EndedAt               *time.Time
	Version               int
}

// The statuses of a succession.
const (
	SuccessionNotice    = "notice"
	SuccessionObjected  = "objected"
	SuccessionWithdrawn = "withdrawn"
	SuccessionCompleted = "completed"
)

// NewOwnerSuccession is what OwnerSuccessions.Start needs: it is due 7 days after StartedAt.
type NewOwnerSuccession struct {
	DocumentID            uuid.UUID
	OutgoingMembershipID  uuid.UUID
	OutgoingUserID        uuid.UUID
	SuccessorMembershipID *uuid.UUID
	Reason                string
	EvidenceKind          string
	StartedBy             uuid.UUID
	StartedAt             time.Time
}

// OwnerSuccessions are the organisation's Owner successions (FR-ADM-09). Each change is audited on the succession. The
// database refuses one not started by an active Owner of another active Owner, a second one open for the same Owner,
// one ended other than once from notice (objected by the outgoing Owner, withdrawn by the one who started it, completed
// by a Super admin from its due time with a file attached), and any other change (CodeSuccession).
type OwnerSuccessions interface {
	// List returns every succession, latest first.
	List(ctx context.Context) ([]OwnerSuccession, error)
	// Get returns a succession, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (OwnerSuccession, error)
	// Start records a succession in notice.
	Start(ctx context.Context, n NewOwnerSuccession) (OwnerSuccession, error)
	// End ends a succession at version with status, by by at at: CodeVersion when it changed or ended since.
	End(ctx context.Context, id uuid.UUID, version int, status string, by uuid.UUID, at time.Time) (OwnerSuccession, error)
}
