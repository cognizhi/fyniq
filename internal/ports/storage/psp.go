package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The payment-provider events (PRD-001 FR-PSP-01; p1-payment-providers design D3): what a provider's verified webhooks
// brought, kept as received, and where each stands. An event posts nothing: it only names an object to fetch.

// The statuses of a provider event: received until the worker works it; then processed, parked for a person (an unknown
// event type, or an object Fyniq cannot post) or failed (to be retried). A parked or failed one becomes processed once it
// is dealt with. The status moves forward only (FY621).
const (
	PSPReceived  = "received"
	PSPProcessed = "processed"
	PSPParked    = "parked"
	PSPFailed    = "failed"
)

// PSPEvent is a stored provider event, without its payload.
type PSPEvent struct {
	ID            uuid.UUID
	ConnectorID   uuid.UUID
	Provider      string
	EventID       string
	EventType     string
	APIVersion    string
	ParserVersion string
	ObjectKind    string // "" for an event that names no object
	ObjectID      string
	PayloadSHA256 string
	ReceivedAt    time.Time
	Status        string
	ParkedReason  string
	ProcessedAt   *time.Time
	LastError     string
}

// NewPSPEvent is a verified webhook to store: Payload is the raw body, sealed with the row's ID as its binding.
type NewPSPEvent struct {
	ID            uuid.UUID
	ConnectorID   uuid.UUID
	Provider      string
	EventID       string
	EventType     string
	APIVersion    string
	ParserVersion string
	ObjectKind    string
	ObjectID      string
	Payload       []byte // sealed
	PayloadSHA256 string
	ReceivedAt    time.Time
}

// PSPEventFilter narrows a list of events: by status ("" for any) and connector (uuid.Nil for any), newest first, after
// an event's ID.
type PSPEventFilter struct {
	Status    string
	Connector uuid.UUID
	Before    uuid.UUID
	Limit     int
}

// PSPEvents are the organisation's provider events. Each change is audited in the same transaction (AUD-01).
type PSPEvents interface {
	// Receive stores a verified webhook; false, and nothing changed, when its event ID was received already (a
	// redelivery, or Stripe's retry): the first row stands.
	Receive(ctx context.Context, n NewPSPEvent) (PSPEvent, bool, error)
	Get(ctx context.Context, id uuid.UUID) (PSPEvent, error)
	// Payload is the sealed raw body of an event, for a person's review or a re-read by a newer parser.
	Payload(ctx context.Context, id uuid.UUID) ([]byte, error)
	// Settle records where an event stands: processed, parked with a reason, or failed with the error (clipped, never a
	// payload). CodeVersion when the status would not move forward.
	Settle(ctx context.Context, id uuid.UUID, status, reason string, at time.Time) (PSPEvent, error)
	// List is a page of events, newest first.
	List(ctx context.Context, f PSPEventFilter) ([]PSPEvent, error)
}
