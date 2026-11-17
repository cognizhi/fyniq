package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The first-run tour's states (FR-UX-34): started at a step, finished, or skipped at a step.
const (
	TourStarted  = "started"
	TourFinished = "finished"
	TourSkipped  = "skipped"
)

// HelpState is how far a person went with the first-run tour in the organisation (FR-UX-34, G4-104): the latest is in
// force.
type HelpState struct {
	UserID uuid.UUID
	Tour   string // TourStarted, TourFinished or TourSkipped
	Step   int    // the step shown last, from 0
	SetAt  time.Time
}

// HelpStates are each person's help state: the tour's only write, audited.
type HelpStates interface {
	// Set records a person's state.
	Set(ctx context.Context, s HelpState) (HelpState, error)
	// Latest is a person's state in force; false when they have none (the tour was never offered an answer).
	Latest(ctx context.Context, user uuid.UUID) (HelpState, bool, error)
}
