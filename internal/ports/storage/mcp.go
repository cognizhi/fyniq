package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MCPOptIn is whether a person has opted in to AI apps acting for them over MCP in the organisation (AI-35): off until they
// do. The latest is in force.
type MCPOptIn struct {
	UserID  uuid.UUID
	OptedIn bool
	SetAt   time.Time
}

// MCPOptIns are each person's opt-in to MCP, audited. The MCP gates read the latest on every call.
type MCPOptIns interface {
	// Set records a person's choice.
	Set(ctx context.Context, o MCPOptIn) (MCPOptIn, error)
	// Latest is a person's choice in force; false when they never chose (which is off).
	Latest(ctx context.Context, user uuid.UUID) (MCPOptIn, bool, error)
}
