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
	Purpose          string // "test" or "sign_in_alert" so far
	Recipient        string
	Subject          string
	Status           string
	Provider         string // the provider of the attempt under way, or of the outcome
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
	// Start marks an attempt through provider on a queued message and returns the message as it was before: a
	// mark already set means an earlier attempt (through the provider it names) may have handed it over, and
	// nothing is marked.
	Start(ctx context.Context, id uuid.UUID, now time.Time, provider string) (EmailMessage, error)
	// Release clears the mark after the provider refused the message, so a retry may send it.
	Release(ctx context.Context, id uuid.UUID, lastError string) (EmailMessage, error)
	// Finish records the outcome; CodeVersion when the message has one already.
	Finish(ctx context.Context, id uuid.UUID, o EmailOutcome) (EmailMessage, error)
}

// The purposes an organisation chooses a sender for (FR-INT-21).
const (
	SenderNotifications = "notifications"
	SenderInvoices      = "invoices" // invoices and quotes
	SenderReminders     = "reminders"
	SenderSecurity      = "security"
)

// EmailSender is an organisation's choice of the address a purpose's email is sent from (FR-INT-21; PRD-001 rev
// 0.102): the latest of a purpose is in force, and one with no address returns to the platform's sender. A choice
// never changes or goes (FY179).
type EmailSender struct {
	ID          uuid.UUID
	Purpose     string
	Address     string // "" for the platform's sender
	DisplayName string
	SetBy       string
	SetAt       time.Time
}

// EmailSenders are the organisation's sender choices. Each is audited.
type EmailSenders interface {
	// Set chooses the sender of purpose: address "" returns it to the platform's.
	Set(ctx context.Context, purpose, address, displayName string) (EmailSender, error)
	// InForce are the choices in force, one per purpose ever chosen.
	InForce(ctx context.Context) ([]EmailSender, error)
}
