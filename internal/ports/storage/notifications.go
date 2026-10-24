package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// NotificationEvent is something that happened, recorded once for every channel to be produced from (FR-NTF-05;
// PRD-001 rev 0.108): its type, the record it is about, plain words, a link into the web app, and whom it concerns.
// It never changes but for its fan-out, recorded once (FY187).
type NotificationEvent struct {
	ID          uuid.UUID
	Type        string
	RecordType  string
	RecordID    *uuid.UUID
	Title       string
	Link        string
	Actor       string
	Audience    json.RawMessage
	OccurredAt  time.Time
	FannedOutAt *time.Time
}

// NewNotificationEvent is an event to record.
type NewNotificationEvent struct {
	Type       string
	RecordType string
	RecordID   *uuid.UUID
	Title      string
	Link       string
	Audience   json.RawMessage
	OccurredAt time.Time
}

// How a notification goes by email.
const (
	EmailImmediate = "immediate"
	EmailDaily     = "daily"
	EmailWeekly    = "weekly"
	EmailOff       = "off"
)

// Notification is an event as one person sees it in their centre (FR-NTF-01): it changes only in being read, unread or
// archived, and in the email that carried it (FY188).
type Notification struct {
	ID, EventID    uuid.UUID
	UserID         uuid.UUID
	Type           string
	Severity       string
	Actionable     bool
	Email          string
	EmailMessageID *uuid.UUID
	DigestID       *uuid.UUID
	CreatedAt      time.Time
	ReadAt         *time.Time
	ArchivedAt     *time.Time
	// From its event.
	Title, Link string
}

// NewNotification is a notification to add at fan-out.
type NewNotification struct {
	EventID    uuid.UUID
	UserID     uuid.UUID
	Type       string
	Severity   string
	Actionable bool
	Email      string
}

// NotificationFilter narrows a person's notifications, newest first.
type NotificationFilter struct {
	UserID   uuid.UUID
	Unread   bool // only unread
	Archived bool // archived ones instead of the others
	Before   uuid.UUID
	Limit    int
}

// NotificationPreference is a person's choice of how a type goes by email, or their quiet hours (Type "quiet_hours"):
// the latest is in force (FY189).
type NotificationPreference struct {
	UserID             uuid.UUID
	Type               string
	Email              string
	QuietFrom, QuietTo *int // minutes since midnight, Singapore time; nil: none
	SetAt              time.Time
}

// QuietHours is the type of the quiet-hours preference.
const QuietHours = "quiet_hours"

// Notifications are the organisation's notification events, each person's notifications, digests and preferences.
type Notifications interface {
	// Emit records an event.
	Emit(ctx context.Context, e NewNotificationEvent) (NotificationEvent, error)
	Event(ctx context.Context, id uuid.UUID) (NotificationEvent, error)
	// FannedOut records an event's fan-out, once.
	FannedOut(ctx context.Context, id uuid.UUID, at time.Time) error
	// Add adds a person's notification of an event; false when they have it already.
	Add(ctx context.Context, n NewNotification) (Notification, bool, error)
	// Emailed records the email that carried a notification at once.
	Emailed(ctx context.Context, id, message uuid.UUID) error
	Get(ctx context.Context, id uuid.UUID) (Notification, error)
	List(ctx context.Context, f NotificationFilter) ([]Notification, error)
	// Unread counts a person's unread notifications that are not archived.
	Unread(ctx context.Context, user uuid.UUID) (int, error)
	// Mark marks a person's notification read or unread; Archive archives it, once.
	Mark(ctx context.Context, id, user uuid.UUID, read bool, at time.Time) (Notification, error)
	Archive(ctx context.Context, id, user uuid.UUID, at time.Time) (Notification, error)
	// MarkAllRead marks every unread notification of a person read, and says how many.
	MarkAllRead(ctx context.Context, user uuid.UUID, at time.Time) (int, error)
	// Held are the notifications going by email that no email carried yet: of cadence immediate (held in quiet hours),
	// daily or weekly, oldest first.
	Held(ctx context.Context, cadence string) ([]Notification, error)
	// Digested records a digest of a person's notifications, sent as message.
	Digested(ctx context.Context, user uuid.UUID, cadence string, message uuid.UUID, ids []uuid.UUID, at time.Time) error
	// Prefer records a person's preference.
	Prefer(ctx context.Context, p NotificationPreference) (NotificationPreference, error)
	// Preferences are a person's preferences in force, by type.
	Preferences(ctx context.Context, user uuid.UUID) ([]NotificationPreference, error)
}
