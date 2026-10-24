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
	Purpose          string // test, sign_in_alert, notification or digest so far
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

// EmailMessageFilter narrows the delivery log: a status and a purpose when set, messages before (older than) Before
// when set, at most Limit.
type EmailMessageFilter struct {
	Status, Purpose string
	Before          uuid.UUID
	Limit           int
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
	// List is the delivery log: messages newest first, of a status or purpose when the filter names one (FR-NTF-05).
	List(ctx context.Context, f EmailMessageFilter) ([]EmailMessage, error)
	// Start marks an attempt through provider on a queued message and returns the message as it was before: a
	// mark already set means an earlier attempt (through the provider it names) may have handed it over, and
	// nothing is marked.
	Start(ctx context.Context, id uuid.UUID, now time.Time, provider string) (EmailMessage, error)
	// Release clears the mark after the provider refused the message, so a retry may send it.
	Release(ctx context.Context, id uuid.UUID, lastError string) (EmailMessage, error)
	// Finish records the outcome; CodeVersion when the message has one already.
	Finish(ctx context.Context, id uuid.UUID, o EmailOutcome) (EmailMessage, error)
	// RecordEvent records a provider's event about a message; false, and nothing recorded, when that delivery of
	// the provider's was recorded already.
	RecordEvent(ctx context.Context, e EmailEvent) (EmailEvent, bool, error)
	// Events are a message's events, oldest first.
	Events(ctx context.Context, message uuid.UUID) ([]EmailEvent, error)
}

// The kinds of event a provider reports about a message it took (FR-INT-21; PRD-001 rev 0.103).
const (
	EventDelivered  = "delivered"
	EventDelayed    = "delayed"
	EventSoftBounce = "soft_bounce"
	EventHardBounce = "hard_bounce"
	EventComplaint  = "complaint"
	EventSuppressed = "suppressed" // the provider suppressed it
	EventFailed     = "failed"
)

// EmailEvent is a provider's report about a message: never changed (FY180).
type EmailEvent struct {
	ID         uuid.UUID
	MessageID  uuid.UUID
	Provider   string
	DeliveryID string // the provider's ID of the event, the same on each retry
	Kind       string
	Recipient  string
	Detail     string
	OccurredAt time.Time
	ReceivedAt time.Time
}

// EmailSuppression keeps mail from an address that bounced hard, complained or was suppressed by the provider, until
// it is released (FY181).
type EmailSuppression struct {
	ID           uuid.UUID
	Address      string // in lower case
	Reason       string // EventHardBounce, EventComplaint or EventSuppressed
	EventID      uuid.UUID
	SuppressedAt time.Time
	ReleasedAt   *time.Time
	ReleasedBy   string
}

// EmailSuppressions are the organisation's suppressed addresses. Each suppression and release is audited.
type EmailSuppressions interface {
	// Suppress puts address on the list for reason, from event; false, and nothing recorded, when it is on it.
	Suppress(ctx context.Context, address, reason string, event uuid.UUID) (EmailSuppression, bool, error)
	// InForce is address's suppression in force; ports.ErrNotFound when there is none.
	InForce(ctx context.Context, address string) (EmailSuppression, error)
	// List is every suppression, newest first; released ones too when all.
	List(ctx context.Context, all bool) ([]EmailSuppression, error)
	// Release ends a suppression in force; CodeVersion when it was released already.
	Release(ctx context.Context, id uuid.UUID) (EmailSuppression, error)
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
