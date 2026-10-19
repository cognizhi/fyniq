package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The organisation's settings with effective dates (FR-ORG-03; data/data-model "Effective-dated reference
// data"): each version is valid from a date, the database refuses overlapping versions, and every change
// is recorded with the time it was recorded (system time) and an audit event.

// The GST return frequencies and accounting bases (FR-ORG-03).
const (
	ReturnQuarterly = "quarterly"
	ReturnMonthly   = "monthly"
	BasisInvoice    = "invoice" // cash basis only once the Cash Accounting Scheme's rules are pinned (ENG-45)
)

// GSTRegistration is one GST registration: valid from RegisteredFrom until the day before
// DeregisteredFrom (nil while it lasts).
type GSTRegistration struct {
	ID                uuid.UUID
	RegisteredFrom    time.Time
	DeregisteredFrom  *time.Time
	GSTNumber         string
	ReturnFrequency   string // ReturnQuarterly or ReturnMonthly
	QuarterStartMonth int    // quarters start in this month and every third after it: 1, 2 or 3
	AccountingBasis   string // BasisInvoice
	Version           int
	RecordedAt        time.Time // system time: when this version was recorded
}

// NewGSTRegistration is a registration to record, or what a registration becomes.
type NewGSTRegistration struct {
	RegisteredFrom    time.Time
	DeregisteredFrom  *time.Time
	GSTNumber         string
	ReturnFrequency   string
	QuarterStartMonth int
	AccountingBasis   string
	RecordedAt        time.Time
}

// GSTRegistrations are the organisation's GST registrations. Overlapping validity is refused with
// CodeOverlap; every create and update is audited.
type GSTRegistrations interface {
	// List returns every registration, by RegisteredFrom.
	List(ctx context.Context) ([]GSTRegistration, error)
	Get(ctx context.Context, id uuid.UUID) (GSTRegistration, error)
	Create(ctx context.Context, r NewGSTRegistration) (GSTRegistration, error)
	// Update replaces a registration at its version, and returns it at its next; CodeVersion when it
	// changed since.
	Update(ctx context.Context, id uuid.UUID, version int, r NewGSTRegistration) (GSTRegistration, error)
}
