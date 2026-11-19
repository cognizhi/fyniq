package storage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/preference"
)

// SavedPreferences are a person's saved preferences in the organisation (FR-UX-23): the latest save is in force.
type SavedPreferences struct {
	UserID uuid.UUID
	preference.Preferences
	SetAt time.Time
}

// Preferences are each person's own preferences: presentation only, audited, the same on every device.
type Preferences interface {
	// Save records a person's preferences as the whole state after a change.
	Save(ctx context.Context, user uuid.UUID, p preference.Preferences) (SavedPreferences, error)
	// Latest is a person's preferences in force; false when they never saved any.
	Latest(ctx context.Context, user uuid.UUID) (SavedPreferences, bool, error)
}
