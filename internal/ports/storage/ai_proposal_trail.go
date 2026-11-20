package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AIProposalUndo is the record of an undo (PRD-001 AI-31, AI-91): who undid an executed proposal, when, why, and what was reversed. It is
// appended once and never changed; the proposal stays Executed.
type AIProposalUndo struct {
	ID         uuid.UUID
	ProposalID uuid.UUID
	UndoneBy   uuid.UUID
	UndoneAt   time.Time
	Reason     string
	Reversed   json.RawMessage
}

// AIQualityRow is what one proposal tells of how it was decided, for the month's approval-quality metrics (CTL-AI-07, AI-92): its outcome, who decided it and when,
// whether what was approved is the whole payload, the reason of a rejection, and when the decider first opened its review.
type AIQualityRow struct {
	ID            uuid.UUID
	TaskType      string
	Status        string
	ProposedAt    time.Time
	DecidedBy     *uuid.UUID
	DecidedAt     *time.Time
	ApprovedWhole bool
	Reason        string
	FirstViewedAt *time.Time
	UndoneAt      *time.Time // when a person undid the executed proposal (AI-91), if they did
}

// AIQualityReview is a month's signed review of the approval-quality metrics (CTL-AI-07), with the metrics as they stood when it was signed.
type AIQualityReview struct {
	ID       uuid.UUID
	Month    time.Time
	SignedBy uuid.UUID
	SignedAt time.Time
	Report   json.RawMessage
	Note     string
}

// AIProposalTrail keeps what is appended about proposals: their undos, the first view of each by each person, and the monthly review of how they were decided.
// Nothing here is updated or deleted (FY951); each insert is audited in its transaction. The AI's role holds none of it.
type AIProposalTrail interface {
	// Undo records the undo of an executed proposal; CodeVersion when it was undone already.
	Undo(ctx context.Context, proposal, by uuid.UUID, at time.Time, reason string, reversed json.RawMessage) (AIProposalUndo, error)
	// UndoOf is a proposal's undo, if it was undone.
	UndoOf(ctx context.Context, proposal uuid.UUID) (AIProposalUndo, bool, error)
	// Viewed records the first time user opened a proposal's review; a later look changes nothing.
	Viewed(ctx context.Context, proposal, user uuid.UUID, at time.Time) error
	// QualityRows are the proposals proposed in [from, to), oldest first, as they were decided.
	QualityRows(ctx context.Context, from, to time.Time) ([]AIQualityRow, error)
	// Review signs a month's review (month is its first day); CodeVersion when it was signed already.
	Review(ctx context.Context, month time.Time, by uuid.UUID, at time.Time, report json.RawMessage, note string) (AIQualityReview, error)
	// ReviewOf is a month's signed review, if there is one.
	ReviewOf(ctx context.Context, month time.Time) (AIQualityReview, bool, error)
}
