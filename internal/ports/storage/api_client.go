package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAPIClient is the database refusing an API client's change: removed, rewritten, revoked twice or changed after its
// revocation (PRD-001 FR-INT-02, SEC-07; p1-public-api task 3.1).
const CodeAPIClient = "STORAGE-API-CLIENT-01"

// The kinds of API client: an integration client (OAuth client credentials with private_key_jwt) and a developer's
// personal access token (FR-INT-02).
const (
	APIClientIntegration = "integration"
	APIClientPersonal    = "personal"
)

// APIClient is an OAuth client of the organisation, as Fyniq holds it (ADR-T3): the OAuth server holds the client and
// its tokens, and Fyniq what the client is to the organisation. The API accepts its tokens only while it is live here.
type APIClient struct {
	ID       uuid.UUID
	Kind     string // APIClientIntegration or APIClientPersonal
	ClientID string // the OAuth server's client ID
	Name     string
	Class    string    // AI-34: integration or agent
	OwnerID  uuid.UUID // the member behind it, whose rights it never exceeds
	Scopes   []string  // fyniq.read.<area> and fyniq.write.<area>
	IPAllow  []string  // the networks it may call from, as CIDR prefixes; none: anywhere
	// ReadsPerMinute and WritesPerMinute are its own rate limits (FR-INT-16); 0: the platform's.
	ReadsPerMinute, WritesPerMinute int
	KeyThumbprint                   string // an integration client's: the RFC 7638 thumbprint of its public key
	ExpiresAt                       time.Time
	ApprovalID                      *uuid.UUID // the approval that registered it
	Version                         int
	CreatedBy                       string
	CreatedAt                       time.Time
	RevokedAt                       *time.Time
	RevokedBy, RevokeReason         string
}

// Live reports whether the client may call the API at now: not revoked and not expired.
func (c APIClient) Live(now time.Time) bool { return c.RevokedAt == nil && now.Before(c.ExpiresAt) }

// APIClients are the organisation's API clients.
type APIClients interface {
	// List is every client of a kind, oldest first.
	List(ctx context.Context, kind string) ([]APIClient, error)
	// Get is a client, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (APIClient, error)
	// Create registers c at its first version, audited as api_client.registered; its client ID is the OAuth server's.
	// CodeAPIClient or a check's code when the database refuses it.
	Create(ctx context.Context, c APIClient) (APIClient, error)
	// Revoke ends the client at version, once, audited as api_client.revoked with the reason. CodeVersion when it moved
	// on; CodeAPIClient when it is revoked already.
	Revoke(ctx context.Context, id uuid.UUID, version int, by, reason string, at time.Time) (APIClient, error)
}

// APIClientAccess is what the API needs to accept an access token of an API client: the client as Fyniq holds it, and
// the roles its member holds now in its organisation.
type APIClientAccess struct {
	Organisation, ID                uuid.UUID
	Kind, Class                     string
	OwnerID                         uuid.UUID
	OwnerActive                     bool     // the member is still an active member of the organisation
	Roles                           []string // the member's roles now; none when no longer a member
	Scopes, IPAllow                 []string
	ReadsPerMinute, WritesPerMinute int
	ExpiresAt                       time.Time
	Revoked                         bool
}

// APIClientRoutes finds an API client by its OAuth client ID before any tenant is set.
type APIClientRoutes interface {
	// APIClientAccess is the client with that OAuth client ID, at now; ports.ErrNotFound for a client Fyniq does not know.
	APIClientAccess(ctx context.Context, clientID string, now time.Time) (APIClientAccess, error)
}
