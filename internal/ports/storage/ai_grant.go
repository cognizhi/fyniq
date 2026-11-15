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
	// Propose is whether the turn may make proposals (AI-P3, design D3): set as it is issued, while a task type is on.
	Propose bool
	// Run is a scheduled agent's run the grant is for, in place of a turn (AI-63; p1-scheduled-agents design D6), with the
	// only tools it may call and the only task types it may propose.
	Run       uuid.UUID
	Tools     []string
	TaskTypes []string
}

// AIGrants keeps an organisation's delegated read grants.
type AIGrants interface {
	// Issue keeps a turn's grant: one per turn.
	Issue(ctx context.Context, g AIGrant) (AIGrant, error)
	// End ends the grant of origin, a turn or a run, at now, once; ports.ErrNotFound when it has none live.
	End(ctx context.Context, origin uuid.UUID, now time.Time) error
	// Classes are the data classes the live grant of origin, a turn or a run, has read (AI-83); ports.ErrNotFound when it
	// has none live.
	Classes(ctx context.Context, origin uuid.UUID, now time.Time) ([]string, error)
	// Widen adds classes to what the live grant of origin, a turn or a run, has read; ports.ErrNotFound when it has none.
	Widen(ctx context.Context, origin uuid.UUID, classes []string, now time.Time) error
}

// AIGrantHolder is who a live grant reads as: the person, the turn, their roles in the grant's organisation now, and
// when it expires.
type AIGrantHolder struct {
	UserID    uuid.UUID
	Turn      uuid.UUID
	Roles     []string
	ExpiresAt time.Time
	Propose   bool
	Run       uuid.UUID // a scheduled agent's run, in place of a turn
	Tools     []string  // a run's tools: the only ones it may call
	TaskTypes []string  // a run's task types: the only ones it may propose
	Agent     string    // a run's agent as ai_agent names it, such as company_secretary
}

// AIGrantLookup resolves a grant's token outside any unit of work, for authentication: ports.ErrNotFound when it is
// unknown in that organisation, ended or expired, or the person is no longer an active member there.
type AIGrantLookup interface {
	AIGrant(ctx context.Context, org uuid.UUID, tokenHash []byte, now time.Time) (AIGrantHolder, error)
}
