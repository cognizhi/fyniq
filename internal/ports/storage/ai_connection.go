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
	DataHandling    json.RawMessage // its data-handling declaration (AI-75, AI-88); {} until declared
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

// AIDataPolicy is a version of the organisation's data-class policy (AI-75): which connections each class may go to
// beyond the defaults, as approved.
type AIDataPolicy struct {
	ID       uuid.UUID
	Version  int // 0: none approved yet, the defaults apply
	Policy   json.RawMessage
	Approval uuid.UUID
	MadeBy   string
	MadeAt   time.Time
}

// AIDataPolicies keeps the organisation's data-class policy, one approved version after another (FY331).
type AIDataPolicies interface {
	// Latest is the policy in force: version 0, with an empty policy, before any is approved.
	Latest(ctx context.Context) (AIDataPolicy, error)
	// Save adds p as the next version, as approved by p.Approval: audited as ai_data_policy.changed. CodeVersion when
	// another version was saved since p.Version-1 was read.
	Save(ctx context.Context, p AIDataPolicy) (AIDataPolicy, error)
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

// AIModel is a model a connection's provider lists, as the organisation keeps it (AI-80, AI-81).
type AIModel struct {
	ConnectionID  uuid.UUID
	Model         string // the provider's ID
	Name          string
	AliasOf       string
	Created       *time.Time
	Shutdown      *time.Time
	ContextWindow int
	MaxOutput     int
	Reports       []string
	FirstListed   time.Time
	LastListed    time.Time
	Listed        bool
	NotCallableAt *time.Time
}

// AIModelListRead is one read of a connection's model list (AI-84).
type AIModelListRead struct {
	ID           uuid.UUID
	ConnectionID uuid.UUID
	Trigger      string
	Outcome      string // complete, partial or failed
	Pages        int
	Added        int
	Removed      int
	Failure      string // why it was not complete, in Fyniq's words; never a credential
	ReadBy       string
	ReadAt       time.Time
}

// AIModels keeps each connection's listed models and its reads of the list.
type AIModels interface {
	// Of are the models a connection's provider listed, by ID.
	Of(ctx context.Context, connection uuid.UUID) ([]AIModel, error)
	// Record keeps a read and the models it leaves: audited on the connection as ai_model.listed, a system event.
	Record(ctx context.Context, read AIModelListRead, models []AIModel) (AIModelListRead, error)
	// Reads are a connection's latest reads, newest first, at most limit.
	Reads(ctx context.Context, connection uuid.UUID, limit int) ([]AIModelListRead, error)
	// LastComplete is a connection's latest complete read, or ports.ErrNotFound when it has none.
	LastComplete(ctx context.Context, connection uuid.UUID) (AIModelListRead, error)
}
