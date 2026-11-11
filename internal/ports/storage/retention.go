package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Retention is the ledger's purge after retention (PRD-001 AUD-10; p1-audit-explorer slice 9, design D7b): its
// boundaries, what it reads of the years it purges, and the one call that deletes them (fyniq.ledger_purge, 000292).
// Only the worker's role writes; the boundary and the purge happen in one transaction, which commits only with the
// purge done.
type Retention interface {
	// LedgerBoundaries are the organisation's ledger boundaries, oldest first.
	LedgerBoundaries(ctx context.Context) ([]LedgerBoundary, error)
	// RecordLedgerBoundary records a boundary; the database checks the switch, that the year is the oldest on the
	// books, at least 7 years past and closed (FY305). It is audited.
	RecordLedgerBoundary(ctx context.Context, b NewLedgerBoundary) (LedgerBoundary, error)
	// EarliestEntryFrom is the posting date of the earliest entry dated on or after day; ok is false when there is none.
	EarliestEntryFrom(ctx context.Context, day time.Time) (earliest time.Time, ok bool, err error)
	// PurgeLines are the lines of every entry dated before a boundary, in posting order.
	PurgeLines(ctx context.Context, boundary time.Time) ([]PurgeLine, error)
	// ReversalLinks are the entries dated on or after a boundary that a reversal links to one dated before it.
	ReversalLinks(ctx context.Context, boundary time.Time) ([]EntryLink, error)
	// PurgeLedger deletes the entries given, all dated before the boundary this transaction recorded, once the
	// carry-forward this transaction posted replaces them exactly (FY305 otherwise); it keeps their tombstones, rebuilds
	// the balances cache, marks the purged periods and completes the boundary, and returns how many entries went.
	PurgeLedger(ctx context.Context, boundary time.Time, entries []uuid.UUID) (int64, error)
}

// NewLedgerBoundary is what a ledger boundary records.
type NewLedgerBoundary struct {
	Boundary   time.Time        // the first day kept whole
	Approval   string           // the setting change that switched the purge on
	Counts     map[string]int64 // entries and lines purged and kept, carry-forward entries
	PurgedHash []byte           // SHA-256 of the purged lines in canonical form
	AnchorKey  string           // where its signed copy is in the anchors' bucket
	WrittenAt  time.Time
}

// LedgerBoundary is a recorded ledger boundary.
type LedgerBoundary struct {
	ID uuid.UUID
	NewLedgerBoundary
	CarriedRevision int64 // the ledger revision after its carry-forward
}

// PurgeLine is a line of an entry dated before a boundary, with what the purge's rule reads of its entry.
type PurgeLine struct {
	EntryID         uuid.UUID
	PostingDate     time.Time
	Source          Source
	ReversesEntryID *uuid.UUID
	Named           bool // a share issue, the equity register, share capital, or the bank's reconciliation names it
	AccountID       uuid.UUID
	Currency        string
	Amount          Decimal
	BaseAmount      Decimal
	PartyID         *uuid.UUID
	InstrumentID    *uuid.UUID
	DimensionValues []uuid.UUID
}

// EntryLink is an entry, its date and the entry it reverses (nil for none).
type EntryLink struct {
	ID              uuid.UUID
	PostingDate     time.Time
	ReversesEntryID *uuid.UUID
}
