package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The email message statuses: queued until an outcome, then accepted, failed or unknown, never changed again.
const (
	EmailQueued   = "queued"
	EmailAccepted = "accepted"
	EmailFailed   = "failed"
	EmailUnknown  = "unknown"
)

// EmailMessage is the record of an outbound email (FR-INT-21): never its body.
type EmailMessage struct {
	ID               uuid.UUID
	Purpose          string // "test" so far
	Recipient        string
	Subject          string
	Status           string
	Provider         string
	ProviderID       string
	Attempts         int
	AttemptStartedAt *time.Time // set while an attempt may have handed the message over
	AcceptedAt       *time.Time
	LastError        string
	CreatedAt        time.Time
}

// EmailOutcome is how a message ended.
type EmailOutcome struct {
	Status               string // EmailAccepted, EmailFailed or EmailUnknown
	Provider, ProviderID string
	AcceptedAt           *time.Time
	LastError            string
}

// EmailMessages are the organisation's outbound email records. Queuing and every outcome are audited.
type EmailMessages interface {
	Queue(ctx context.Context, purpose, recipient, subject string) (EmailMessage, error)
	Get(ctx context.Context, id uuid.UUID) (EmailMessage, error)
	// Start marks an attempt on a queued message and returns the message as it was before: a mark already set
	// means an earlier attempt may have handed it over, and nothing is marked.
	Start(ctx context.Context, id uuid.UUID, now time.Time) (EmailMessage, error)
	// Release clears the mark after the provider refused the message, so a retry may send it.
	Release(ctx context.Context, id uuid.UUID, lastError string) (EmailMessage, error)
	// Finish records the outcome; CodeVersion when the message has one already.
	Finish(ctx context.Context, id uuid.UUID, o EmailOutcome) (EmailMessage, error)
}
