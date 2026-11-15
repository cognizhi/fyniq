package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeAIConnection is the database's refusal of an AI connection's change (FY330): one removed, one that starts
// switched on, a version skipped or rewritten, its provider or creation changed, or a change with no record of its
// version (PRD-001 AI-72; p1-ai-providers task 3.1).
const CodeAIConnection = "STORAGE-AI-CONNECTION-01"

// The steps of an AI connection's life, each a version of it.
const (
	AIConnectionCreated    = "created"
	AIConnectionChanged    = "changed"
	AIConnectionCredential = "credential"
	AIConnectionEnabled    = "enabled"
	AIConnectionDisabled   = "disabled"
)

// AIConnection is the current version of a connection to an AI model provider (AI-72): never its credential's value,
// which the secret store keeps under CredentialRef.
type AIConnection struct {
	ID              uuid.UUID
	Name            string
	Connector       string
	Preset          string
	Auth            string
	Configuration   json.RawMessage
	Host            string
	SelfHosted      bool
	CredentialRef   string // the secret version; "" when none
	CredentialSetAt *time.Time
	Billing         string
	Quota           json.RawMessage // a subscription's; nil for usage-based billing
	State           string
	Version         int
	CreatedBy       string
	CreatedAt       time.Time
	ChangedBy       string
	ChangedAt       time.Time
}

// AIConnectionVersion is one version of a connection, as it was made.
type AIConnectionVersion struct {
	Version  int
	Step     string
	Snapshot json.RawMessage
	Approval *uuid.UUID
	MadeBy   string
	MadeAt   time.Time
}

// AIConnections keeps the organisation's connections to AI model providers and every version of each.
type AIConnections interface {
	// List is every connection, by name.
	List(ctx context.Context) ([]AIConnection, error)
	// Get is a connection, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (AIConnection, error)
	// Create adds c, switched off, at its first version, as approved by approval: audited as ai_connection.created.
	// CodeAIConnection when the database refuses it; a name the organisation uses already is CodeDuplicate.
	Create(ctx context.Context, c AIConnection, approval uuid.UUID) (AIConnection, error)
	// Step writes c as the next version of the connection it names, which must be at c.Version, as step: approved by
	// approval, which only a switch off goes without. Audited as ai_connection.<step> (the credential's as
	// ai_connection.credential_stored), never with the credential's value. CodeVersion when the connection moved on.
	Step(ctx context.Context, c AIConnection, step string, approval *uuid.UUID) (AIConnection, error)
	// Versions are a connection's versions, oldest first.
	Versions(ctx context.Context, id uuid.UUID) ([]AIConnectionVersion, error)
}
