package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeAuditEngagement is the database's refusal of an engagement, request or sample changed out of its course, deleted, or added to an engagement that is
// not active (FYR10; FR-UX-42).
const CodeAuditEngagement = "STORAGE-AUDIT-01"

// Engagement statuses.
const (
	EngagementPending = "pending"
	EngagementActive  = "active"
	EngagementClosed  = "closed"
)

// Request statuses.
const (
	AuditRequestOpen     = "open"
	AuditRequestAnswered = "answered"
	AuditRequestClosed   = "closed"
)

// AuditEngagement is an auditor's time-boxed engagement (FR-UX-42).
type AuditEngagement struct {
	ID          uuid.UUID
	Name, Firm  string
	AuditorUser uuid.UUID
	PeriodFrom  time.Time
	PeriodTo    time.Time
	OpensOn     time.Time
	ClosesOn    time.Time
	Status      string
	OpenedBy    string
	OpenedAt    time.Time
	ConfirmedBy string
	ConfirmedAt *time.Time
	ClosedBy    string
	ClosedAt    *time.Time
}

// AuditRequest is an item the auditor asks for, answered by a named person with evidence.
type AuditRequest struct {
	ID          uuid.UUID
	Engagement  uuid.UUID
	Number      int
	Title       string
	Detail      string
	DueOn       *time.Time
	Assignee    uuid.UUID
	Status      string
	RequestedBy string
	RequestedAt time.Time
	Answer      string
	EvidenceIDs []uuid.UUID
	AnsweredBy  string
	AnsweredAt  *time.Time
	ClosedBy    string
	ClosedAt    *time.Time
}

// AuditSample is a sample drawn from the ledger by the auditor's rule: insert-only, its seed making the draw reproducible.
type AuditSample struct {
	ID         uuid.UUID
	Engagement uuid.UUID
	Rule       json.RawMessage
	Seed       string
	Population int
	Items      json.RawMessage
	DrawnBy    string
	DrawnAt    time.Time
}

// AuditEngagements are the engagements, their requests and their samples.
type AuditEngagements interface {
	List(ctx context.Context) ([]AuditEngagement, error)
	Get(ctx context.Context, id uuid.UUID) (AuditEngagement, error)
	Open(ctx context.Context, e AuditEngagement) (AuditEngagement, error)
	Confirm(ctx context.Context, id uuid.UUID, by string, at time.Time) (AuditEngagement, error)
	Close(ctx context.Context, id uuid.UUID, by string, at time.Time) (AuditEngagement, error)

	Requests(ctx context.Context, engagement uuid.UUID) ([]AuditRequest, error)
	Request(ctx context.Context, id uuid.UUID) (AuditRequest, error)
	Ask(ctx context.Context, r AuditRequest) (AuditRequest, error)
	Answer(ctx context.Context, id uuid.UUID, answer string, evidence []uuid.UUID, by string, at time.Time) (AuditRequest, error)
	CloseRequest(ctx context.Context, id uuid.UUID, by string, at time.Time) (AuditRequest, error)

	Samples(ctx context.Context, engagement uuid.UUID) ([]AuditSample, error)
	Draw(ctx context.Context, s AuditSample) (AuditSample, error)
}
