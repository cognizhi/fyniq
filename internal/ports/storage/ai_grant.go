package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAIGrant is the database's refusal of a delegated read grant's change (FY340): one changed, removed or ended twice
// (PRD-001 §9.3 rev 0.277; p1-ai-assistant task 1.1).
const CodeAIGrant = "STORAGE-AI-GRANT-01"

// AIGrant is the read grant the API hands the AI service for one turn (AI-01, AI-36): the bearer token's SHA-256, the
// person it reads as, the turn, and its life of at most 5 minutes.
type AIGrant struct {
	ID        uuid.UUID
	TokenHash []byte
	UserID    uuid.UUID
	Turn      uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
	EndedAt   *time.Time
}

// AIGrants keeps an organisation's delegated read grants.
type AIGrants interface {
	// Issue keeps a turn's grant: one per turn.
	Issue(ctx context.Context, g AIGrant) (AIGrant, error)
	// End ends a turn's grant at now, once; ports.ErrNotFound when the turn has none live.
	End(ctx context.Context, turn uuid.UUID, now time.Time) error
	// Classes are the data classes a turn's live grant has read (AI-83); ports.ErrNotFound when the turn has none live.
	Classes(ctx context.Context, turn uuid.UUID, now time.Time) ([]string, error)
	// Widen adds classes to what a turn's live grant has read; ports.ErrNotFound when the turn has none live.
	Widen(ctx context.Context, turn uuid.UUID, classes []string, now time.Time) error
}

// AIGrantHolder is who a live grant reads as: the person, the turn, their roles in the grant's organisation now, and
// when it expires.
type AIGrantHolder struct {
	UserID    uuid.UUID
	Turn      uuid.UUID
	Roles     []string
	ExpiresAt time.Time
}

// AIGrantLookup resolves a grant's token outside any unit of work, for authentication: ports.ErrNotFound when it is
// unknown in that organisation, ended or expired, or the person is no longer an active member there.
type AIGrantLookup interface {
	AIGrant(ctx context.Context, org uuid.UUID, tokenHash []byte, now time.Time) (AIGrantHolder, error)
}
