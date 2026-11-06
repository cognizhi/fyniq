package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BreakGlass is an Owner's break-glass (FM-8): who started it and why, its 24 hours, its cap in the base currency as it
// stood when it started, its end if an Owner ended it sooner, and the batches it released.
type BreakGlass struct {
	ID        uuid.UUID
	StartedBy uuid.UUID
	Reason    string
	StartedAt time.Time
	ExpiresAt time.Time
	EndedAt   *time.Time
	EndedBy   *uuid.UUID
	Currency  string
	Cap       Decimal
	Version   int
	Uses      []BreakGlassUse // oldest first
}

// Ends is when it ends, or ended: its expiry, or sooner when an Owner ended it.
func (b BreakGlass) Ends() time.Time {
	if b.EndedAt != nil {
		return *b.EndedAt
	}
	return b.ExpiresAt
}

// BreakGlassUse is a batch a break-glass approved, by its Owner, at a moment, for its total.
type BreakGlassUse struct {
	ID           uuid.UUID
	BreakGlassID uuid.UUID
	BatchID      uuid.UUID
	BatchVersion int
	UsedBy       uuid.UUID
	UsedAt       time.Time
	Currency     string
	Total        Decimal
}

// NewBreakGlass is what BreakGlasses.Start needs: it lasts 24 hours from StartedAt.
type NewBreakGlass struct {
	StartedBy uuid.UUID
	Reason    string
	StartedAt time.Time
	Currency  string
	Cap       Decimal
}

// BreakGlasses are the organisation's break-glasses (FM-8). Each change is audited on the break-glass. The database
// refuses a break-glass outside Founder mode, by a member who is not an Owner or while an earlier one waits for its
// review; one not logged for review in its transaction; any change but ending it once inside its window; and a use by
// anyone but its Owner, outside its window, of a batch not waiting for approval at its current version, or above its
// cap (CodeBreakGlass).
type BreakGlasses interface {
	// List returns every break-glass with its uses, oldest first.
	List(ctx context.Context) ([]BreakGlass, error)
	// Get returns a break-glass with its uses, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (BreakGlass, error)
	// Start records a break-glass.
	Start(ctx context.Context, n NewBreakGlass) (BreakGlass, error)
	// End ends a break-glass at version, by by at at: CodeVersion when it changed since.
	End(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time) (BreakGlass, error)
	// Use records that a break-glass approves a batch at its version.
	Use(ctx context.Context, u BreakGlassUse) (BreakGlassUse, error)
}
