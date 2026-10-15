package storage

import (
	"context"

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

// Request is where a change came from.
type Request struct {
	ID, IP, UserAgent, Session string
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
}
