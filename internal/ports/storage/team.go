package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TeamMode is a version of the organisation's team mode (PRD-001 §6.4, rev 0.42). The organisation is in Founder mode
// until its first version.
type TeamMode struct {
	ID        uuid.UUID
	Version   int
	Mode      string
	RequestID uuid.UUID // the approved switch
	CreatedBy uuid.UUID // who asked for it
	CreatedAt time.Time
}

// NewTeamMode is what TeamModes.Add needs.
type NewTeamMode struct {
	Version   int
	Mode      string
	RequestID uuid.UUID
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

// SoDOverride lets a member keep a conflict of §6.3 that the team mode blocks, for one version of the team mode,
// with the reason a second approver accepted.
type SoDOverride struct {
	ID              uuid.UUID
	TeamModeVersion int
	UserID          uuid.UUID
	Conflict        string // such as "SOD-6"
	Reason          string
	RequestID       uuid.UUID // the approved switch or role change that holds it
	CreatedBy       uuid.UUID // who asked for it
	CreatedAt       time.Time
}

// NewSoDOverride is what TeamModes.Override needs.
type NewSoDOverride struct {
	TeamModeVersion int
	UserID          uuid.UUID
	Conflict        string
	Reason          string
	RequestID       uuid.UUID
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
}

// TeamModes are the versions of the organisation's team mode and the overrides of conflicts. The database adds a
// version only as the next, with the approved request that holds exactly it, and an override only for the version in
// force, with the approved request that holds exactly it; it never changes either (CodeApproval). Adding a version
// voids every pending approval request. Add and Override audit what they add, and Add the voids.
type TeamModes interface {
	// List returns every version, oldest first.
	List(ctx context.Context) ([]TeamMode, error)
	// Add records a version.
	Add(ctx context.Context, n NewTeamMode) (TeamMode, error)
	// Overrides returns every override, oldest first.
	Overrides(ctx context.Context) ([]SoDOverride, error)
	// Override records an override.
	Override(ctx context.Context, n NewSoDOverride) (SoDOverride, error)
}
