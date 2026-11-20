package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Webhook endpoints, their deliveries and attempts (PRD-001 FR-INT-12; p2-api-third-parties design D1 to D4). An endpoint is an organisation's; the secret is sealed
// under the field key, bound to the endpoint's ID, so the caller makes the ID before it seals.

// The states of an endpoint: pending (not yet verified), active, paused (by a person or after ten failures in a row) or disabled (by a person, or the endpoint said it is gone).
const (
	WebhookPending  = "pending"
	WebhookActive   = "active"
	WebhookPaused   = "paused"
	WebhookDisabled = "disabled"
)

// The states of a delivery: pending (to send, now or at next_attempt_at), delivered, or dead (the retries are used up; a person replays it).
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryDead      = "dead"
)

// WebhookEndpoint is one endpoint as stored. The sealed secrets are for the signer alone and are never read into an API answer.
type WebhookEndpoint struct {
	ID             uuid.UUID
	URL            string
	Label          string
	EventTypes     []string
	State          string
	SecretSealed   []byte
	SecretHint     string
	PreviousSealed []byte
	PreviousUntil  *time.Time
	ChallengeHash  string
	VerifiedAt     *time.Time
	FailureRun     int
	PausedReason   string
	Version        int
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewWebhookEndpoint makes a pending endpoint.
type NewWebhookEndpoint struct {
	ID           uuid.UUID
	URL          string
	Label        string
	EventTypes   []string
	SecretSealed []byte
	SecretHint   string
	At           time.Time
}

// WebhookEndpointChange is what a person changes: nil leaves a field as it is. A change of URL makes the endpoint pending again; the caller sets State and clears VerifiedAt.
type WebhookEndpointChange struct {
	URL            *string
	Label          *string
	EventTypes     []string
	State          *string
	SecretSealed   []byte // a new secret; the old one goes to PreviousSealed until PreviousUntil
	SecretHint     *string
	PreviousSealed []byte
	PreviousUntil  *time.Time
	ClearPrevious  bool
	ChallengeHash  *string
	ClearChallenge bool
	VerifiedAt     *time.Time
	ClearVerified  bool
	PausedReason   *string
	ClearPaused    bool
	ResetFailures  bool
	At             time.Time
}

// WebhookDelivery is one event for one endpoint.
type WebhookDelivery struct {
	ID             uuid.UUID
	EndpointID     uuid.UUID
	EventID        uuid.UUID
	EventType      string
	MessageID      string
	Payload        []byte
	State          string
	Attempts       int
	Replays        int
	NextAttemptAt  time.Time
	FirstAttemptAt *time.Time
	LastAttemptAt  *time.Time
	LastStatus     *int
	LastError      string
	DeliveredAt    *time.Time
	CreatedAt      time.Time
}

// NewWebhookDelivery queues an event for an endpoint, once per event.
type NewWebhookDelivery struct {
	EndpointID uuid.UUID
	EventID    uuid.UUID
	EventType  string
	MessageID  string
	Payload    []byte
	At         time.Time
}

// WebhookAttempt is the log of one try.
type WebhookAttempt struct {
	ID         uuid.UUID
	DeliveryID uuid.UUID
	Number     int
	At         time.Time
	Status     *int
	Error      string
	DurationMS int
}

// WebhookResult is what one try came to: Delivered, or Dead when the retries are used up, or the time of the next attempt.
type WebhookResult struct {
	At         time.Time
	Status     *int
	Error      string
	DurationMS int
	Delivered  bool
	Dead       bool
	NextAt     time.Time
}

// WebhookQuery filters the deliveries of an endpoint, newest first, from a cursor.
type WebhookQuery struct {
	EndpointID uuid.UUID
	State      string
	After      *uuid.UUID
	Limit      int
}

// Webhooks is the organisation's webhook records. Each change of an endpoint is audited in the same transaction (AUD-01).
type Webhooks interface {
	Create(ctx context.Context, n NewWebhookEndpoint, by string) (WebhookEndpoint, error)
	Get(ctx context.Context, id uuid.UUID) (WebhookEndpoint, error)
	// Lock is Get for update, so a verification or a roll does not race another change.
	Lock(ctx context.Context, id uuid.UUID) (WebhookEndpoint, error)
	List(ctx context.Context) ([]WebhookEndpoint, error)
	Change(ctx context.Context, id uuid.UUID, version int, c WebhookEndpointChange) (WebhookEndpoint, error)
	// Subscribed are the active endpoints that asked for eventType.
	Subscribed(ctx context.Context, eventType string) ([]WebhookEndpoint, error)
	// Queue records an event for an endpoint: false when it was queued before.
	Queue(ctx context.Context, n NewWebhookDelivery) (WebhookDelivery, bool, error)
	// Due are the pending deliveries whose time has come, oldest first.
	Due(ctx context.Context, now time.Time, limit int) ([]WebhookDelivery, error)
	Delivery(ctx context.Context, id uuid.UUID) (WebhookDelivery, error)
	Deliveries(ctx context.Context, q WebhookQuery) ([]WebhookDelivery, error)
	Attempts(ctx context.Context, delivery uuid.UUID) ([]WebhookAttempt, error)
	// Record logs a try and moves its delivery on (delivered, pending for next time, or dead) and counts the endpoint's failures: ten in a row pause it.
	Record(ctx context.Context, delivery uuid.UUID, r WebhookResult) (WebhookDelivery, error)
	// Replay puts a dead delivery back for another try now, for the person who asked.
	Replay(ctx context.Context, delivery uuid.UUID, at time.Time) (WebhookDelivery, error)
}
