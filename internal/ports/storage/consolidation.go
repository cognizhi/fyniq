package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// GroupPair says two accounts of the parent's chart mirror each other: A normally holds a debit and B a credit (FR-ENT-06).
type GroupPair struct{ A, B uuid.UUID }

// GroupMapping maps an entity's account code to an account of the parent's chart (FR-ENT-05). EntityRow is uuid.Nil for the parent's own accounts. The counterparty is the other entity
// of an intercompany account, or the parent itself.
type GroupMapping struct {
	ID                 uuid.UUID
	EntityRow          uuid.UUID
	EntityAccountCode  string
	GroupAccountID     uuid.UUID
	CounterpartyRow    uuid.UUID // uuid.Nil for none
	CounterpartyParent bool
	Version            int
	UpdatedAt          time.Time
}

// NewGroupMapping maps an account, or maps it again: the next version of the one mapping an account has.
type NewGroupMapping struct {
	EntityRow          uuid.UUID
	EntityAccountCode  string
	GroupAccountID     uuid.UUID
	CounterpartyRow    uuid.UUID
	CounterpartyParent bool
	By                 uuid.UUID
	At                 time.Time
}

// GroupRate is the group's translation rates for a currency in a month: group currency per one unit of the entity's currency. A newer entry corrects an older.
type GroupRate struct {
	ID               uuid.UUID
	Currency         string
	Month            time.Time
	Closing, Average Decimal
	Note             string
	EnteredBy        uuid.UUID
	EnteredAt        time.Time
}

// NewGroupRate is a rate to enter.
type NewGroupRate struct {
	Currency         string
	Month            time.Time
	Closing, Average Decimal
	Note             string
	By               uuid.UUID
	At               time.Time
}

// ConsolidationRun is a frozen run: its figures as read, the rates, the entities' revisions and period states (Inputs) and the engine's result (Result), as JSON documents.
type ConsolidationRun struct {
	ID          uuid.UUID
	From, To    time.Time
	Currency    string
	Provisional bool
	Inputs      []byte
	Result      []byte
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
}

// NewConsolidationRun is a run to keep.
type NewConsolidationRun struct {
	From, To    time.Time
	Currency    string
	Provisional bool
	Inputs      []byte
	Result      []byte
	By          uuid.UUID
	At          time.Time
}

// Consolidations are the parent's set-up and frozen runs (FR-ENT-05, FR-ENT-06): all in the parent's own tenant, none reaching into another.
type Consolidations interface {
	// Pairs returns the elimination pairs and their version (0 when none were ever set).
	Pairs(ctx context.Context) ([]GroupPair, int, error)
	// SetPairs replaces the pairs as the next version over version.
	SetPairs(ctx context.Context, pairs []GroupPair, version int, by uuid.UUID, at time.Time) error
	// Mappings lists every mapping: the parent's own first, then each entity's by code.
	Mappings(ctx context.Context) ([]GroupMapping, error)
	// Map makes or changes the mapping of an account.
	Map(ctx context.Context, m NewGroupMapping) (GroupMapping, error)
	// Rates lists every entry, newest month first, a correction before the entry it corrects.
	Rates(ctx context.Context) ([]GroupRate, error)
	AddRate(ctx context.Context, r NewGroupRate) (GroupRate, error)
	// Runs lists the runs without their documents, newest first.
	Runs(ctx context.Context) ([]ConsolidationRun, error)
	// Run returns a run with its documents, or ports.ErrNotFound.
	Run(ctx context.Context, id uuid.UUID) (ConsolidationRun, error)
	SaveRun(ctx context.Context, r NewConsolidationRun) (ConsolidationRun, error)
}
