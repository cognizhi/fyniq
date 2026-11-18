package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Every saved change is audited in the same transaction (AUD-01, AUD-08). The unit of work writes
// an event for each change its repositories make, and Audit().Record adds events that are not a row
// change. Who acted, and why, comes from the request's context.

// Actor is who made a change: type user, service, integration, scheduler or ai, and an id.
type Actor struct {
	Type string
	ID   string
}

// String is the actor as records name it: "type:id", such as "user:0192…" (FR-GL-01, FM-4).
func (a Actor) String() string { return a.Type + ":" + a.ID }

// Request is where a change came from.
type Request struct {
	ID, IP, UserAgent, Session string
	// Country and Region are where the edge says the request came from (SEC-31): kept, as the IP address is, beside a
	// security event's chain only (AUD-06).
	Country, Region string
}

// AuditContext is what a request knows about the change it makes.
type AuditContext struct {
	Actor       *Actor // nil: the store's own service identity
	OnBehalfOf  *Actor
	Originator  string   // the AI proposal a change carries out, if any
	ApprovalIDs []string // the approvals it carries out
	Reason      string
	Request     Request
}

type auditKey struct{}

// WithAudit attaches the audit context of a request.
func WithAudit(ctx context.Context, a AuditContext) context.Context {
	return context.WithValue(ctx, auditKey{}, a)
}

// WithReason sets the reason recorded with the changes made under ctx.
func WithReason(ctx context.Context, reason string) context.Context {
	a := AuditFrom(ctx)
	a.Reason = reason
	return WithAudit(ctx, a)
}

// AuditFrom returns the audit context of ctx (empty when none was attached).
func AuditFrom(ctx context.Context) AuditContext {
	a, _ := ctx.Value(auditKey{}).(AuditContext)
	return a
}

// AuditRecord is an event that is not a row change, such as an anchor written or a verification run.
type AuditRecord struct {
	Action     string
	ObjectType string
	ObjectID   uuid.UUID
	Changes    map[string][2]any
}

// AuditHead is the latest event of the organisation's chain.
type AuditHead struct {
	Seq    int64
	Hash   []byte
	Action string
}

// AuditEvent is a stored event with its chain fields.
type AuditEvent struct {
	ID       uuid.UUID
	Seq      int64
	Body     string
	PrevHash []byte
	Hash     []byte
}

// Audit is the organisation's audit trail.
type Audit interface {
	// Record adds an event; it is written with the unit of work's others, just before commit.
	Record(ctx context.Context, r AuditRecord) error
	// Head returns the chain head; Seq 0 when there are no events.
	Head(ctx context.Context) (AuditHead, error)
	// Events returns up to limit events after sequence number after, in order.
	Events(ctx context.Context, after int64, limit int) ([]AuditEvent, error)
	// Find returns the events q matches, newest first (the explorer, FR-CTL-03).
	Find(ctx context.Context, q AuditQuery) ([]AuditEvent, error)
	// Event returns one event by its ID, or ports.ErrNotFound.
	Event(ctx context.Context, id uuid.UUID) (AuditEvent, error)
	// Details returns the request details kept beside the chain for those of events that are security events and
	// still have them (AUD-06), by event. The worker and the read-only role read them.
	Details(ctx context.Context, events []uuid.UUID) (map[uuid.UUID]AuditDetail, error)
	// DetailsBefore counts the details of events that occurred before cutoff.
	DetailsBefore(ctx context.Context, cutoff time.Time) (int64, error)
	// Boundaries are the organisation's purge boundaries, oldest first (AUD-09).
	Boundaries(ctx context.Context) ([]AuditBoundary, error)
	// RecordBoundary writes a purge boundary at an event of the trail, after every earlier one: the worker only.
	RecordBoundary(ctx context.Context, b NewAuditBoundary) (AuditBoundary, error)
	// Purge deletes the events up to lastSeq and their details, behind a boundary at exactly lastSeq, while the purge
	// after retention is switched on, never an event less than 7 years old (CodeAudit otherwise): the worker only.
	Purge(ctx context.Context, lastSeq int64) (int64, error)
	// EraseDetails erases the details of events that occurred before cutoff (AUD-06): the worker only, and only for a
	// cutoff at least 30 days ago by the database's clock (CodeAudit otherwise).
	EraseDetails(ctx context.Context, cutoff time.Time) (AuditErasure, error)
	// Recoveries are the trail's recoveries from restores behind its anchors, oldest first (AUD-09, design D8); the
	// current anchor epoch is the last one's.
	Recoveries(ctx context.Context) ([]AuditRecovery, error)
	// RecordRecovery records a recovery, which starts the next epoch, and its event audit.restored in the chain: the
	// worker only.
	RecordRecovery(ctx context.Context, r NewAuditRecovery) (AuditRecovery, error)
	// AcknowledgeRecovery records who acknowledged a recovery, once (ports.ErrNotFound when there is none to
	// acknowledge). It is audited.
	AcknowledgeRecovery(ctx context.Context, id, by uuid.UUID, at time.Time) (AuditRecovery, error)
	// OpenSealed opens personal values the organisation's events sealed under person keys (§11.8, fyniq-audit-event@3),
	// in order: a value whose key was shredded comes back Shredded, with its financial year.
	OpenSealed(ctx context.Context, values []SealedValue) ([]OpenedValue, error)
}

// SealedValue is a personal value an event sealed (§11.8): the event, the field (a path for a nested one, such as
// payee.name), the person key and the ciphertext.
type SealedValue struct {
	EventID uuid.UUID
	Field   string
	Key     uuid.UUID
	Text    []byte
}

// OpenedValue is a sealed value opened: the value, or Shredded once its person's key is gone at the end of retention,
// with the financial year (its last day) and the day its run shredded it, when a run did.
type OpenedValue struct {
	Value      string
	Shredded   bool
	Cohort     *time.Time
	ShreddedOn *time.Time
}

// NewAuditRecovery is a recovery from a restore behind the anchors (design D8): the epoch it starts, the restored head
// (the last event whose hash matches every anchor; 0 and the genesis or boundary hash when none does), the anchored
// range no longer proven, when it was found and where its signed copy is.
type NewAuditRecovery struct {
	Epoch        int
	RestoredSeq  int64
	RestoredHash []byte
	MissingFrom  int64
	MissingTo    int64
	DetectedAt   time.Time
	AnchorKey    string
}

// AuditRecovery is a recorded recovery and its acknowledgement, if any.
type AuditRecovery struct {
	ID uuid.UUID
	NewAuditRecovery
	AcknowledgedBy *uuid.UUID
	AcknowledgedAt *time.Time
}

// AuditQuery filters the trail for the explorer (FR-CTL-03). Every filter is optional.
type AuditQuery struct {
	Actor      string     // the actor's ID
	Originator string     // an AI proposal's ID, or "any" for every change an AI proposal carried out
	ObjectType string     // the record's type
	ObjectID   *uuid.UUID // the record
	Field      string     // a field of the changes
	Action     string     // an action, or every action a prefix ending in "." starts
	ApprovalID string     // an approval the change carried out
	From, To   *time.Time // occurred at From or later, and before To
	Before     int64      // events before this sequence number; 0 from the newest
	Limit      int        // at most this many, 1 to 1,000
}

// AuditDetail is a security event's request details, kept beside the chain until the security-log retention ends
// (AUD-06): the IP address and the user agent its body's request.detail fingerprints.
type AuditDetail struct {
	EventID    uuid.UUID
	OccurredAt time.Time
	IP         string
	UserAgent  string
	Country    string // ISO 3166-1 alpha-2, as the edge said; "" when it did not (SEC-31)
	Region     string
}

// NewAuditBoundary is a purge boundary to write: the last purged event, the setting change that switched the purge on,
// the counts, and where its signed copy is.
type NewAuditBoundary struct {
	LastSeq        int64
	LastHash       []byte
	LastOccurredAt time.Time
	Approval       string
	Counts         map[string]int64
	AnchorKey      string
	WrittenAt      time.Time
}

// AuditBoundary is a written purge boundary.
type AuditBoundary struct {
	NewAuditBoundary
	ID uuid.UUID
}

// AuditErasure is what an erasure removed: how many events' details, and the first and last of those events.
type AuditErasure struct {
	Erased            int64
	FirstSeq, LastSeq int64
}

type linesKey struct{}

// WithLines marks reads as at a revision that must sum the ledger's lines, never the balances cache: the checks that
// compare the statements with the balances (C.1 #1 to #3, INV-08) keep the two sources apart, so a cache that drifted
// from its lines shows. Reports read the cache when nothing later is in a period (NFR-PERF-02).
func WithLines(ctx context.Context) context.Context { return context.WithValue(ctx, linesKey{}, true) }

// FromLines says whether reads as at a revision must sum the lines (WithLines).
func FromLines(ctx context.Context) bool { b, _ := ctx.Value(linesKey{}).(bool); return b }
