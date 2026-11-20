package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeCashSnapshot is the database's refusal of a cash snapshot changed or deleted (FYR11; FR-PLN-04).
const CodeCashSnapshot = "STORAGE-CASHSNAP-01"

// CodeCashSnapshotWeek is the refusal of a second snapshot for one week (cash_snapshot_week_once).
const CodeCashSnapshotWeek = "STORAGE-CASHSNAP-02"

// CashSnapshot is the short-term projection as it stood in one week (FR-PLN-04).
type CashSnapshot struct {
	ID             uuid.UUID
	WeekOf         time.Time // the Monday
	TakenOn        time.Time
	Currency       string
	LedgerRevision int64
	Projection     json.RawMessage
	TakenAt        time.Time
}

// CashSnapshots are the weekly snapshots.
type CashSnapshots interface {
	// List are the snapshots, oldest week first.
	List(ctx context.Context) ([]CashSnapshot, error)
	// Take keeps a snapshot; a second for the week is refused (CodeCashSnapshotWeek).
	Take(ctx context.Context, s CashSnapshot) (CashSnapshot, error)
}
