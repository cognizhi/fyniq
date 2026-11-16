package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAICall is the database's refusal of a model call's record (FY335): one changed or removed, or recorded outside
// an organisation (PRD-001 AI-12; p1-ai-providers task 5.2).
const CodeAICall = "STORAGE-AI-CALL-01"

// AICall is one attempt the router made along an agent's route (AI-76, AI-83, AI-87).
type AICall struct {
	ID              uuid.UUID
	Run             uuid.UUID
	Agent           string
	RouteVersion    int
	Step            int
	FellBackFrom    int // 0 when it is the run's first attempt
	ConnectionID    uuid.UUID
	ModelRequested  string
	ModelReported   string
	VersionReported string
	Actor           string
	Classes         []string
	Outcome         string
	Detail          string
	Input           int64
	Output          int64
	CacheRead       int64
	CacheWrite      int64
	Reasoning       int64
	Cost            string // exact decimal text; "" when unpriced
	Currency        string
	Price           *uuid.UUID
	LatencyMS       int
	StartedAt       time.Time
	RecordedAt      time.Time
}

// AICalls keeps the model calls, as the AI activity log reads them: written only through record_ai_call, never changed.
type AICalls interface {
	// Record keeps one attempt in the organisation.
	Record(ctx context.Context, c AICall) (AICall, error)
	// OfRun are a run's attempts, in order.
	OfRun(ctx context.Context, run uuid.UUID) ([]AICall, error)
}
