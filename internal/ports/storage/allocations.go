package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Allocation is what a settling document applied to a document it settles (FR-AR-18): the amount settled in the settled
// document's currency, that amount at the settled document's stored rate, and the realised FX it made (ENG-41). It
// never changes or goes; a reversing allocation negates it, once.
type Allocation struct {
	ID            uuid.UUID
	SourceID      uuid.UUID // the settling document: a bill payment
	SourceVersion int
	DocumentID    uuid.UUID // the settled document: a bill
	On            time.Time
	Currency      string
	Amount        Decimal // positive; a reversing allocation's negative
	BaseCurrency  string
	BaseAmount    Decimal
	RealisedFX    Decimal // the gain (+) or loss (−), in the base currency
	Tolerance     Decimal // the carrying amount a settlement tolerance cleared, in the base currency (FR-BNK-08); 0 for none
	Tax           Decimal // a use of a customer deposit: the part of the output tax it bore that the use takes, in the base currency (FR-AR-09)
	ReversesID    *uuid.UUID
	WrittenBy     string
	WrittenAt     time.Time
}

// NewAllocation is what Allocations.Add records. The writer is the unit of work's actor, at its clock's now.
type NewAllocation struct {
	SourceID      uuid.UUID
	SourceVersion int
	DocumentID    uuid.UUID
	On            time.Time
	Currency      string
	Amount        Decimal
	BaseCurrency  string
	BaseAmount    Decimal
	RealisedFX    Decimal
	Tolerance     Decimal // empty for none
	Tax           Decimal // empty for none
	ReversesID    *uuid.UUID
}

// Allocations are the organisation's allocations (FR-AR-18). Each is audited.
type Allocations interface {
	// Add records an allocation: CodeAllocation when it does not settle a bill from a bill payment, or a reversing one
	// does not negate the allocation it names exactly; CodeDuplicate when that allocation is reversed already.
	Add(ctx context.Context, n NewAllocation) (Allocation, error)
	// OfDocument is what has settled a document, earliest first.
	OfDocument(ctx context.Context, id uuid.UUID) ([]Allocation, error)
	// OfSource is what a settling document allocated, earliest first.
	OfSource(ctx context.Context, id uuid.UUID) ([]Allocation, error)
	// Settled is the amount settled of each document named, in its currency, net of reversals; a document nothing settled
	// is not in it.
	Settled(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Decimal, error)
	// Tolerance is the carrying amount settlement tolerances cleared on allocations dated from from to to, net of
	// reversals, in the base currency (FR-BNK-08).
	Tolerance(ctx context.Context, from, to time.Time) (Decimal, error)
}
