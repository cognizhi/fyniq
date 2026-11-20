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

	// PlaceHold places a hold in force (AUD-11, 001300), audited as retention_hold.placed; CodeHold when the subject already
	// has one in force or the hold is outside its rules.
	PlaceHold(ctx context.Context, h NewHold) (Hold, error)
	// LiftHold lifts the hold with its reason, audited as retention_hold.lifted; ErrNotFound for none, CodeHold when it was lifted already.
	LiftHold(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) (Hold, error)
	// Holds are the organisation's holds, in force or lifted, the newest placed first.
	Holds(ctx context.Context, inForceOnly bool) ([]Hold, error)
	// HeldLedgerDates are the posting dates of the entries the holds in force cover (a held year gives its first day).
	HeldLedgerDates(ctx context.Context) ([]time.Time, error)
	// HeldEvents are the sequence numbers, from first to last inclusive, of the audit events the holds in force cover.
	HeldEvents(ctx context.Context, first, last int64) ([]int64, error)
	// HeldFiles are the final evidence files the holds in force cover.
	HeldFiles(ctx context.Context) ([]HeldFile, error)
	// FreeFiles are the final evidence files no hold in force covers (the files the reconcile may free).
	FreeFiles(ctx context.Context) ([]HeldFile, error)
}

// CodeHold is the database's refusal of a hold outside its rules, a second one on a subject, or a lift twice (001300, FY360).
const CodeHold = "STORAGE-HOLD-01"

// CodeHeld is the database's refusal to delete a row a hold in force covers (001300, FY361).
const CodeHeld = "STORAGE-HOLD-02"

// Hold kinds (AUD-11).
const (
	HoldRecord        = "record"
	HoldContact       = "contact"
	HoldFinancialYear = "financial_year"
)

// NewHold is a hold to place: its kind and what it names, and why.
type NewHold struct {
	Kind       string
	DocumentID uuid.UUID // a record: its source document
	ContactID  uuid.UUID // a contact
	Year       *HoldYear // a financial year
	Reason     string
	PlacedBy   string // "user:<id>"
	PlacedAt   time.Time
}

// HoldYear is the financial year a hold names, with the dates it covers.
type HoldYear struct {
	Number           int
	StartsOn, EndsOn time.Time
}

// Hold is a placed hold; LiftedAt is nil while it stands.
type Hold struct {
	ID         uuid.UUID
	Kind       string
	DocumentID *uuid.UUID
	ContactID  *uuid.UUID
	Year       *HoldYear
	Reason     string
	PlacedBy   string
	PlacedAt   time.Time
	LiftedBy   string
	LiftedAt   *time.Time
	LiftReason string
}

// HeldFile is an evidence file's locked key, with its evidence ID.
type HeldFile struct {
	EvidenceID uuid.UUID
	LockedKey  string
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
