package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PackVersion is a loaded statutory pack version (PRD-001 §10.6, ENG-30), as every service reads it. Its
// tables and pinned cases are internal/domain/pack's to read; a loaded version never changes.
type PackVersion struct {
	ID, Pack, Digest string
	EffectiveFrom    time.Time  // a date
	EffectiveTo      *time.Time // a date; nil while no later version ends it
	ReviewDate       time.Time  // a date
	LoadedAt         time.Time
}

// PackActivation is the organisation's activation of a pack version (ENG-32): who proposed it, who
// approved it (never the proposer, SOD-10) and the approval engine's record.
type PackActivation struct {
	VersionID              string
	ProposedBy, ApprovedBy uuid.UUID
	Approval               uuid.UUID
	ActivatedAt            time.Time
}

// NewPackActivation is what Packs.Activate needs.
type NewPackActivation struct {
	VersionID              string
	ProposedBy, ApprovedBy uuid.UUID
	Approval               uuid.UUID
	ActivatedAt            time.Time
}

// Packs are the statutory pack versions the release loaded (global, read-only) and the organisation's
// activations of them.
type Packs interface {
	// Versions returns every loaded version, by id.
	Versions(ctx context.Context) ([]PackVersion, error)
	// Activations returns the organisation's activations, by version id.
	Activations(ctx context.Context) ([]PackActivation, error)
	// Activate records an activation, audited, as the approval engine executes its approved request: the database
	// refuses one without that request approved and signed by its approver, or approved by its proposer
	// (CodePack), and a version activated already (CodeDuplicate) (rev 0.41).
	Activate(ctx context.Context, n NewPackActivation) (PackActivation, error)
}
