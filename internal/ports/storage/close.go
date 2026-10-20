package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Closes are the records of the period close (FR-GL-07, FM-4, AUD-04): each run of the close checks, and each close
// with its attestation and snapshot. Neither ever changes (FY152); the database refuses a close attested by the one
// who ran its checks, or on checks that are not clean or not of that period (FY153).
type Closes interface {
	// RecordCheck records a run of the close checks, by the unit of work's actor, now.
	RecordCheck(ctx context.Context, c NewCloseCheck) (CloseCheck, error)
	Check(ctx context.Context, id uuid.UUID) (CloseCheck, error)
	// LatestCheck is the period's latest run of the checks, or ports.ErrNotFound.
	LatestCheck(ctx context.Context, period uuid.UUID) (CloseCheck, error)
	// Record records a close, attested by the unit of work's actor, now. The caller closes the period in the same
	// unit of work, under the ledger lock (ENG-47).
	Record(ctx context.Context, c NewClose) (PeriodClose, error)
	// Latest is the period's latest close, or ports.ErrNotFound.
	Latest(ctx context.Context, period uuid.UUID) (PeriodClose, error)
	// ChangedSince reports whether an entry dated on or before through has a revision later than revision: the checks
	// that saw revision are then stale (FR-GL-07, G4-053).
	ChangedSince(ctx context.Context, through time.Time, revision int64) (bool, error)
}

// NewCloseCheck is a run of the close checks.
type NewCloseCheck struct {
	PeriodID       uuid.UUID
	LedgerRevision int64
	Result         []byte // a JSON array of the checks
	Clean          bool
}

// CloseCheck is a recorded run of the close checks.
type CloseCheck struct {
	NewCloseCheck
	ID        uuid.UUID
	CheckedBy string // "type:id"
	CheckedAt time.Time
}

// NewClose is a close: the checks it rests on, the attestation, the revision at the close and the snapshot.
type NewClose struct {
	PeriodID       uuid.UUID
	CheckID        uuid.UUID
	Statement      string
	ClosedRevision int64
	Snapshot       []byte // canonical JSON (RFC 8785)
	SnapshotHash   []byte // its SHA-256, which the database recomputes
}

// PeriodClose is a recorded close.
type PeriodClose struct {
	NewClose
	ID         uuid.UUID
	AttestedBy string
	AttestedAt time.Time
}
