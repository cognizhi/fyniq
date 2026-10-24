package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NewDocumentLink is the online link to a version of a sent quote or issued invoice (FR-AR-06), made with the email
// message that carries it: the SHA-256 of its token, never the token, and when it expires.
type NewDocumentLink struct {
	DocumentID      uuid.UUID
	DocumentVersion int
	MessageID       uuid.UUID
	TokenHash       string // hex
	ExpiresAt       time.Time
}

// DocumentLink is a link as recorded: it changes only in its first view and its revocation, each once (FY197).
type DocumentLink struct {
	ID              uuid.UUID
	DocumentID      uuid.UUID
	DocumentVersion int
	MessageID       uuid.UUID
	ExpiresAt       time.Time
	CreatedBy       string
	CreatedAt       time.Time
	FirstViewedAt   *time.Time
	RevokedAt       *time.Time
	RevokedBy       string
}

// DocumentLinks are the organisation's online links. Each creation, first view and revocation is audited.
type DocumentLinks interface {
	Create(ctx context.Context, l NewDocumentLink) (DocumentLink, error)
	Get(ctx context.Context, id uuid.UUID) (DocumentLink, error)
	// OfDocument are a document's links, newest first.
	OfDocument(ctx context.Context, documentID uuid.UUID) ([]DocumentLink, error)
	// Viewed records a link's first view at at; false, and nothing recorded, when it was viewed before.
	Viewed(ctx context.Context, id uuid.UUID, at time.Time) (DocumentLink, bool, error)
	// Revoke ends a link; CodeLink when it was revoked already.
	Revoke(ctx context.Context, id uuid.UUID) (DocumentLink, error)
}

// LinkRoutes find the organisation and link a token's hash opens, before any tenant is set (FR-AR-06): the public
// read endpoint's only way in. ports.ErrNotFound when the hash opens none.
type LinkRoutes interface {
	LinkRoute(ctx context.Context, tokenHash string) (org, link uuid.UUID, err error)
}
