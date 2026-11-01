package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeCheck is the database's refusal of a check's state or owner written other than at version 1, changed other than as
// its next version, or deleted; and of a check run changed, or deleted within its retention (FY245).
const CodeCheck = "STORAGE-CHECK-01"

// The triggers of a run of the checks (FR-CTL-01, design D5).
const (
	CheckOnChange  = "change"  // a change the checks react to
	CheckNightly   = "nightly" // the nightly run of every check
	CheckOnRequest = "request" // Run now
)

// CheckState is a check's latest result (FR-CTL-01, design D4): its status, how many items it found and the first of them
// as JSON, its words, and the run that wrote it.
type CheckState struct {
	ID             uuid.UUID
	Key            string
	Status         string // ok, warning, error, due, not_assessed or failed
	Count          int
	Items          []byte // a JSON array of at most 50 items
	Detail         string
	Fix            string
	Needs          string // what a check not assessed needs
	Setting        string // where that is set
	RunAt          time.Time
	Trigger        string
	LedgerRevision int64
	ChangedAt      time.Time // when its status last changed
	Version        int
}

// NewCheckRun is a run of the checks as it finished.
type NewCheckRun struct {
	Trigger        string
	RequestedBy    string // "type:id", for a run on request only
	StartedAt      time.Time
	FinishedAt     time.Time
	LedgerRevision int64
	AuditSeq       int64    // the audit trail's last event the run read
	Keys           []string // the checks it ran
	Results        []byte   // a nightly run's results, as a JSON array; nil for the others
}

// CheckRun is a recorded run.
type CheckRun struct {
	ID uuid.UUID
	NewCheckRun
}

// CheckOwner is the person named as a check's owner instead of the catalogue's role (D8); Person nil gives it back to
// the role.
type CheckOwner struct {
	ID      uuid.UUID
	Key     string
	Person  *uuid.UUID // the member's user
	SetBy   string     // "type:id"
	SetAt   time.Time
	Version int
}

// Checks are the checks' results, their runs and their owners (FR-CTL-01, FR-CTL-05). The worker writes the results and
// the runs; the API names the owners.
type Checks interface {
	// States are every check's latest result, by key.
	States(ctx context.Context) ([]CheckState, error)
	// Save writes a check's result: its first when version is 0, else its next after version (CodeVersion when the
	// state moved since). A first result and a change of status are audited on the check (check.status_changed); a
	// result with the same status as before adds nothing to the trail.
	Save(ctx context.Context, s CheckState, version int) (CheckState, error)
	// RecordRun records a finished run.
	RecordRun(ctx context.Context, r NewCheckRun) (CheckRun, error)
	// LatestRun is the latest run of a trigger ("" for any), or ports.ErrNotFound when there is none.
	LatestRun(ctx context.Context, trigger string) (CheckRun, error)
	// Watermark is the last audit event any run read: a run on a change reads the events after it.
	Watermark(ctx context.Context) (int64, error)
	// Purge deletes the runs on a change or request that finished before before, and the nightly runs that finished
	// before nightlyBefore; the database refuses a run within its retention (CodeCheck).
	Purge(ctx context.Context, before, nightlyBefore time.Time) (int64, error)
	// Owners are the checks with a person named, or once named, by key.
	Owners(ctx context.Context) ([]CheckOwner, error)
	// SetOwner names person as the check's owner (nil: the catalogue's role again): its first when version is 0, else
	// its next after version (CodeVersion when it changed since); audited check.owner_set with the person before and
	// after.
	SetOwner(ctx context.Context, key string, person *uuid.UUID, setBy string, at time.Time, version int) (CheckOwner, error)
}

// Changed is what a unit of work calls once its work is done, before its audit events are written, with its organisation
// and the object types of those events: it may add to the unit of work, such as the outbox item that reruns the checks a
// change concerns (FR-CTL-01, design D5). It is not called for a unit of work that recorded no event.
type Changed func(ctx context.Context, u UnitOfWork, org uuid.UUID, objectTypes []string) error
