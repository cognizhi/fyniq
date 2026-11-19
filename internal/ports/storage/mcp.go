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

// MCPCall is one request an AI app made over MCP that reached the API's gates (AI-44), as it is kept: who, through which app,
// which tool, what became of it. It holds no token, argument or result, only hashes.
type MCPCall struct {
	ID                     uuid.UUID
	UserID                 uuid.UUID
	App, AppVersion        string
	TokenID                string // the first 16 hex characters of the MCP token's SHA-256
	Tool                   string
	InputSHA256, OutputSHA string
	Facts, DurationMS      int
	Outcome                string // read, refused, limited or failed
	Code                   string // the refusal's or failure's stable code; empty for a read
	Network                string // the caller's /24 or /48
	At                     time.Time
}

// The outcomes of an MCP call.
const (
	MCPRead    = "read"
	MCPRefused = "refused"
	MCPLimited = "limited"
	MCPFailed  = "failed"
)

// MCPCalls is the trail of AI apps' calls, append-only; the limits of AI-43 count from it.
type MCPCalls interface {
	// Record keeps a call.
	Record(ctx context.Context, c MCPCall) (MCPCall, error)
	// Count is how many calls the person's app made in (since, until], and the time of the oldest of them (zero when none).
	Count(ctx context.Context, user uuid.UUID, app string, since, until time.Time) (int, time.Time, error)
	// List are the latest calls, newest first, of one person or, with uuid.Nil, of everyone in the organisation.
	List(ctx context.Context, user uuid.UUID, limit int) ([]MCPCall, error)
	// FirstSeen records that the person used the app, or came from the network, and says whether it was the first time (AI-45).
	FirstSeen(ctx context.Context, user uuid.UUID, kind, value string, at time.Time) (bool, error)
	// Purge removes the calls made before a time, for the purge after retention (the worker's alone), and gives how many.
	Purge(ctx context.Context, before time.Time) (int, error)
}
