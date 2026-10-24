package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Announcement is a banner (FR-NTF-03): the organisation's, or the installation's to everyone when Installation. It
// shows under the header for its roles (none: everyone) from StartsAt until EndsAt, unless withdrawn sooner; it changes
// only in being withdrawn, once (FY190).
type Announcement struct {
	ID           uuid.UUID
	Installation bool
	Message      string
	Link         string // a path in the web app or an https address; empty for none
	Severity     string // info, warning or critical
	Roles        []string
	StartsAt     time.Time
	EndsAt       time.Time
	Dismissible  bool // never for a critical one
	PublishedBy  string
	PublishedAt  time.Time
	WithdrawnAt  *time.Time
	WithdrawnBy  string
}

// NewAnnouncement is a banner to publish at At.
type NewAnnouncement struct {
	Message, Link, Severity string
	Roles                   []string
	StartsAt, EndsAt        time.Time
	Dismissible             bool
	At                      time.Time
}

// Announcements are the organisation's banners and its people's dismissals, and the installation's banners through the
// functions only an active Super admin may write with (FY085 otherwise). Publishing and withdrawing are audited.
type Announcements interface {
	Publish(ctx context.Context, n NewAnnouncement) (Announcement, error)
	// Withdraw ends a banner at at; ports.ErrNotFound for none, CodeVersion when it was withdrawn already.
	Withdraw(ctx context.Context, id uuid.UUID, at time.Time) (Announcement, error)
	Get(ctx context.Context, id uuid.UUID) (Announcement, error)
	// List is every banner of the organisation's, newest first.
	List(ctx context.Context) ([]Announcement, error)
	// PublishInstallation publishes a banner to everyone, as the Super admin by.
	PublishInstallation(ctx context.Context, by uuid.UUID, n NewAnnouncement) (Announcement, error)
	// WithdrawInstallation ends the installation's banner id at at, as the Super admin by; ports.ErrNotFound for none.
	WithdrawInstallation(ctx context.Context, by, id uuid.UUID, at time.Time) (Announcement, error)
	// Installation is every banner of the installation's, newest first.
	Installation(ctx context.Context) ([]Announcement, error)
	// Dismiss keeps that user dismissed the banner id (the installation's when installation), once; CodeBanner for one
	// that cannot be dismissed.
	Dismiss(ctx context.Context, user, id uuid.UUID, installation bool, at time.Time) error
	// Dismissed are the banners user dismissed.
	Dismissed(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error)
}
