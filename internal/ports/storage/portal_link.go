package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NewPortalLink is the link to the customer portal of one customer (FR-AR-14): the SHA-256 of its token, never the token, and when
// it expires (at most 90 days).
type NewPortalLink struct {
	ContactID uuid.UUID
	Label     string
	TokenHash string // hex
	ExpiresAt time.Time
	At        time.Time // when it is made, by the platform clock
}

// PortalLink is a link as recorded: it changes only in its first opening and its revocation, each once (FYP10).
type PortalLink struct {
	ID            uuid.UUID
	ContactID     uuid.UUID
	Label         string
	ExpiresAt     time.Time
	CreatedBy     string
	CreatedAt     time.Time
	FirstOpenedAt *time.Time
	RevokedAt     *time.Time
	RevokedBy     string
}

// PortalLinks are the organisation's customer portal links. Each creation, first opening and revocation is audited.
type PortalLinks interface {
	Create(ctx context.Context, n NewPortalLink) (PortalLink, error)
	Get(ctx context.Context, id uuid.UUID) (PortalLink, error)
	// OfContact are a customer's links, newest first.
	OfContact(ctx context.Context, contact uuid.UUID) ([]PortalLink, error)
	// Opened records a link's first opening at at; false, and nothing recorded, when it was opened before.
	Opened(ctx context.Context, id uuid.UUID, at time.Time) (PortalLink, bool, error)
	// Revoke ends a link; CodeLink when it was revoked already.
	Revoke(ctx context.Context, id uuid.UUID) (PortalLink, error)
}

// PortalRoutes find the organisation and link a portal token's hash opens, before any tenant is set (FR-AR-14): the public
// endpoints' only way in. ports.ErrNotFound when the hash opens none.
type PortalRoutes interface {
	PortalRoute(ctx context.Context, tokenHash string) (org, link uuid.UUID, err error)
}
