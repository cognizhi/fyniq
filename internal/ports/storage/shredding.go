package storage

import (
	"time"

	"github.com/google/uuid"
)

// CodeShredding is the database's refusal of a shredding run or an anonymisation out of order: a run listed before its
// year's retention ended, approved twice, shredded unapproved or unregistered, a person key deleted outside its run, or
// a contact anonymised with no recorded anonymisation (FY317, FY318).
const CodeShredding = "STORAGE-SHREDDING-01"

// ShreddingRun is a financial year's person keys to shred once its retention has ended (§11.8, design D9): listed by
// the worker, approved by the DPO, registered in the signed register, then done.
type ShreddingRun struct {
	ID         uuid.UUID
	Cohort     time.Time   // the year's last day
	Keys       []uuid.UUID // the person keys it shreds
	ListedAt   time.Time
	Status     string // listed, approved or done
	ApprovedBy string
	ApprovedAt *time.Time
	Register   string // the signed register entry's key
	DoneAt     *time.Time
	Reapplied  bool // recreated from the register by a restore
	Version    int
}

// KeyCohort is a financial year with person keys, and its keys.
type KeyCohort struct {
	Cohort time.Time
	Keys   []uuid.UUID
}

// Anonymisation is a contact's personal details replaced once a request is honoured or their purpose ends (§11.9,
// design D10): its signed register entry is written first.
type Anonymisation struct {
	ID           uuid.UUID
	ContactID    uuid.UUID
	RequestID    *uuid.UUID
	Reason       string
	Register     string
	AnonymisedBy string
	AnonymisedAt time.Time
	Reapplied    bool
}
