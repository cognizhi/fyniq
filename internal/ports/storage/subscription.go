package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The kinds of amendment to a subscription contract (FR-AR-15).
const (
	AmendUpgrade   = "upgrade"
	AmendDowngrade = "downgrade"
	AmendCancel    = "cancel"
	AmendRenew     = "renew"
)

// SubscriptionContract is a customer's subscription: a start, an optional agreed end and the monthly value at the start.
type SubscriptionContract struct {
	ID        uuid.UUID
	ContactID uuid.UUID
	Name      string
	Currency  string
	StartsOn  time.Time
	EndsOn    *time.Time
	MRR       Decimal
	Version   int
	CreatedBy string
	CreatedAt time.Time
}

// NewSubscriptionContract is a contract to record.
type NewSubscriptionContract struct {
	ContactID uuid.UUID
	Name      string
	Currency  string
	StartsOn  time.Time
	EndsOn    *time.Time
	MRR       Decimal
	At        time.Time
}

// SubscriptionAmendment is a dated change of a contract's monthly value.
type SubscriptionAmendment struct {
	ID          uuid.UUID
	ContractID  uuid.UUID
	Kind        string
	EffectiveOn time.Time
	MRR         Decimal
	Reason      string
	CreatedBy   string
	CreatedAt   time.Time
}

// NewSubscriptionAmendment is an amendment to record.
type NewSubscriptionAmendment struct {
	ContractID  uuid.UUID
	Kind        string
	EffectiveOn time.Time
	MRR         Decimal
	Reason      string
	At          time.Time
}

// Subscriptions are the organisation's subscription contracts and their amendments. Each change is audited in the same transaction (AUD-01).
type Subscriptions interface {
	// Create records a contract.
	Create(ctx context.Context, n NewSubscriptionContract) (SubscriptionContract, error)
	Get(ctx context.Context, id uuid.UUID) (SubscriptionContract, error)
	// Lock is Get with the row locked until commit.
	Lock(ctx context.Context, id uuid.UUID) (SubscriptionContract, error)
	// List are the contracts after the one with ID after (uuid.Nil for the first page), oldest first, at most limit; contact uuid.Nil is any.
	List(ctx context.Context, contact, after uuid.UUID, limit int) ([]SubscriptionContract, error)
	// Amendments are a contract's, by day.
	Amendments(ctx context.Context, contract uuid.UUID) ([]SubscriptionAmendment, error)
	// AllAmendments are every amendment of the organisation, by contract and day.
	AllAmendments(ctx context.Context) ([]SubscriptionAmendment, error)
	// Amend records an amendment of the contract at version and moves the contract's version; CodeVersion when it moved on, CodeInvalid for a second
	// amendment of the same day.
	Amend(ctx context.Context, n NewSubscriptionAmendment, version int) (SubscriptionAmendment, error)
}
