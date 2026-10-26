package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ClaimCategory is a line of the expense policy (FR-AP-15): what a claim line of it costs, and its limits in the base
// currency. A limit or threshold that is nil is not set. One live category may be the mileage category (FR-AP-16).
type ClaimCategory struct {
	ID             uuid.UUID
	Name           string
	AccountID      uuid.UUID
	Mileage        bool
	Currency       string
	LineLimit      *Decimal // above it, a line needs finance's approval too (FR-AP-17)
	ReceiptAbove   *Decimal // above it, a line needs its receipt
	AttendeesAbove *Decimal // above it, a line names who attended
	Active         bool
	CreatedBy      string
	CreatedAt      time.Time
	ChangedAt      time.Time
	Version        int
}

// NewClaimCategory creates a category; a change to one carries its version (the mileage flag and the currency are kept).
type NewClaimCategory struct {
	Name           string
	AccountID      uuid.UUID
	Mileage        bool
	Currency       string
	LineLimit      *Decimal
	ReceiptAbove   *Decimal
	AttendeesAbove *Decimal
	Active         bool
	At             time.Time
}

// ClaimCategories are the organisation's claim categories. Every change is audited in its transaction (AUD-01); the
// database refuses a category on a money, control, system, group or inactive account, or one not in the base
// currency, a second live category of a name or a second live mileage category, and deleting one (CodeClaim).
type ClaimCategories interface {
	Create(ctx context.Context, n NewClaimCategory) (ClaimCategory, error)
	// Change changes a category at version: CodeVersion when it changed since.
	Change(ctx context.Context, id uuid.UUID, version int, n NewClaimCategory) (ClaimCategory, error)
	// List is every category, live ones first, by name; Get is one, ports.ErrNotFound for none.
	List(ctx context.Context) ([]ClaimCategory, error)
	Get(ctx context.Context, id uuid.UUID) (ClaimCategory, error)
}

// ClaimPlace is a place a member often drives between, kept for their mileage claims (FR-AP-16): their own, by name and
// address. A place out of use is kept, as claims made with it name its address.
type ClaimPlace struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Address   string
	Active    bool
	CreatedAt time.Time
	ChangedAt time.Time
	Version   int
}

// NewClaimPlace creates a place of a person's; a change to one carries its version (the person is kept).
type NewClaimPlace struct {
	UserID  uuid.UUID
	Name    string
	Address string
	Active  bool
	At      time.Time
}

// ClaimPlaces are the members' saved places. Every change is audited in its transaction (AUD-01); the database refuses
// a second live place of a name for one person, a change of its person, and deleting one (CodeClaim).
type ClaimPlaces interface {
	Create(ctx context.Context, n NewClaimPlace) (ClaimPlace, error)
	// Change changes a place at version: CodeVersion when it changed since.
	Change(ctx context.Context, id uuid.UUID, version int, n NewClaimPlace) (ClaimPlace, error)
	// List is a person's places, live ones first, by name; Get is one, ports.ErrNotFound for none.
	List(ctx context.Context, user uuid.UUID) ([]ClaimPlace, error)
	Get(ctx context.Context, id uuid.UUID) (ClaimPlace, error)
}
