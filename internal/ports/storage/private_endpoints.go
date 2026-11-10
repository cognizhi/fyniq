package storage

import (
	"context"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

// The registered private endpoints (SEC-37; p1-security-hardening design D3): a destination on a private network that a
// connection may reach, registered by a Super admin for one organisation, service and credential. Global rows, versioned
// and never changed; the installation's trail records each version.

// The services a private endpoint is for.
const (
	PrivateEndpointConnector = "connector" // a connector inside an organisation's network; its credential is the organisation's connector
	PrivateEndpointAIModel   = "ai_model"  // a self-hosted model (AI-79), from p1-ai-assistant
)

// PrivateEndpoint is the current version of a registered private endpoint.
type PrivateEndpoint struct {
	ID         uuid.UUID
	Version    int
	Address    netip.Prefix // the private address or range
	Port       int
	Pin        string // the base64 SHA-256 of the SubjectPublicKeyInfo its TLS certificate must present
	Service    string
	Serves     uuid.UUID // the organisation it is for
	Credential uuid.UUID // the credential bound to it: for a connector, the organisation's connector
	Retired    bool
	Reason     string
	RecordedBy uuid.UUID
	RecordedAt time.Time
	Change     *uuid.UUID // the installation change that approved it, with two Super admins
}

// PrivateEndpointChange is a version a Super admin registers: Version is the one it follows, 0 for a new endpoint.
type PrivateEndpointChange struct {
	ID         uuid.UUID
	Version    int
	Address    string
	Port       int
	Pin        string
	Service    string
	Serves     uuid.UUID
	Credential uuid.UUID
	Retired    bool
	Reason     string
}

// PrivateEndpoints keeps the registered private endpoints.
type PrivateEndpoints interface {
	// List is the current version of every endpoint, retired ones included: in a unit of work of the installation
	// organisation only.
	List(ctx context.Context) ([]PrivateEndpoint, error)
	// Register adds version c.Version+1 of endpoint c.ID by by, as approved by change (nil: none needed, with one Super
	// admin), in a unit of work of the installation organisation, whose trail records it: CodeVersion at another version;
	// CodePrivateEndpoint for a retired endpoint, an address that is not a private range of at most /24 or /64, a pin
	// that is not one, or an organisation that does not exist.
	Register(ctx context.Context, by uuid.UUID, change *uuid.UUID, c PrivateEndpointChange, at time.Time) (PrivateEndpoint, error)
	// For is what the unit of work's organisation may reach with one credential of a service: the current versions,
	// not retired, registered for it, and no other organisation's.
	For(ctx context.Context, service string, credential uuid.UUID) ([]PrivateEndpoint, error)
}
