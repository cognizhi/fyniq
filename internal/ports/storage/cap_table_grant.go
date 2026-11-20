package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeCapTableGrant is the database's refusal of a cap table grant changed other than by one revocation, or deleted (FYR09).
const CodeCapTableGrant = "STORAGE-CAPGRANT-01"

// CodeCapTableGrantLive is the refusal of a second live grant for one person (cap_table_grant_one_live).
const CodeCapTableGrantLive = "STORAGE-CAPGRANT-02"

// CapTableGrant is an Owner's grant of the full cap table to one member (FR-EQ-24).
type CapTableGrant struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Reason       string
	GrantedBy    string
	GrantedAt    time.Time
	RevokedBy    string
	RevokedAt    *time.Time
	RevokeReason string
}

// CapTableGrants are the grants.
type CapTableGrants interface {
	List(ctx context.Context) ([]CapTableGrant, error)
	// LiveOf is a person's live grant, or ports.ErrNotFound.
	LiveOf(ctx context.Context, user uuid.UUID) (CapTableGrant, error)
	Get(ctx context.Context, id uuid.UUID) (CapTableGrant, error)
	Grant(ctx context.Context, user uuid.UUID, reason, by string, at time.Time) (CapTableGrant, error)
	// Revoke ends a live grant, once.
	Revoke(ctx context.Context, id uuid.UUID, reason, by string, at time.Time) (CapTableGrant, error)
}
