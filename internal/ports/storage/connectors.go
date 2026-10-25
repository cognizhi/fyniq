package storage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The states of an installed connector (FR-INT-26): installed → configured → tested → enabled; paused and resumed;
// disabled; uninstalled for good.
const (
	ConnectorInstalled   = "installed"
	ConnectorConfigured  = "configured"
	ConnectorTested      = "tested"
	ConnectorEnabled     = "enabled"
	ConnectorPaused      = "paused"
	ConnectorDisabled    = "disabled"
	ConnectorUninstalled = "uninstalled"
)

// The steps of the lifecycle, as an audit event and an installation change name them. Test and pause need no second
// person; every other step does once there are two Super admins (installation) or under FM-1 (an organisation's).
const (
	StepInstall   = "install"
	StepConfigure = "configure"
	StepSecret    = "secret"
	StepTest      = "test"
	StepEnable    = "enable"
	StepPause     = "pause"
	StepResume    = "resume"
	StepDisable   = "disable"
	StepUninstall = "uninstall"
	StepOrder     = "order"
	StepConsent   = "consent"
)

// InstalledConnector is a connector installed from the catalogue: the installation's, or an organisation's.
type InstalledConnector struct {
	ID              uuid.UUID
	Installation    bool
	Connector       string // the manifest's id
	ManifestVersion string // the manifest consented to
	ManifestHash    string
	SecretNames     []string // the consented manifest's secrets, which Configuration never holds
	Consent         ConnectorConsent
	State           string
	Configuration   map[string]any
	Position        int // among the connectors of its kind: 1 is the primary
	Test            *ConnectorTest
	InstalledBy     string
	InstalledAt     time.Time
	ChangedBy       string
	ChangedAt       time.Time
	Version         int
}

// ConnectorConsent is what an installed connector consented to send out and reach (FR-INT-26): a release whose manifest
// asks more runs under it until someone consents again.
type ConnectorConsent struct {
	DataClasses    []string             `json:"data_classes,omitempty"`
	ProviderScopes []string             `json:"provider_scopes,omitempty"`
	FyniqScopes    []string             `json:"fyniq_scopes,omitempty"`
	Destinations   []ConsentDestination `json:"destinations,omitempty"`
	Webhooks       []string             `json:"webhooks,omitempty"` // their paths
}

// ConsentDestination is a host (with its subdomains when it starts with "*."), or the configuration property that
// names one; and its port, 0 for any.
type ConsentDestination struct {
	Host       string `json:"host,omitempty"`
	Configured string `json:"configured,omitempty"`
	Port       int    `json:"port,omitempty"`
}

// ConnectorTest is a connection test's result: pass or fail, and the reason Fyniq wrote, never the provider's answer.
type ConnectorTest struct {
	Passed bool
	Reason string
	At     time.Time
}

// ConnectorSecret is one version of a connector's secret, by reference: current until replaced, then valid until the
// manifest's overlap ends, then destroyed.
type ConnectorSecret struct {
	ID          uuid.UUID
	ConnectorID uuid.UUID
	Name        string
	Ref         ports.SecretRef
	SetBy       string
	SetAt       time.Time
	ValidUntil  *time.Time
	DestroyedAt *time.Time
}

// NewConnector is a connector to install.
type NewConnector struct {
	Connector       string
	ManifestVersion string
	ManifestHash    string
	SecretNames     []string
	Consent         ConnectorConsent
	Configuration   map[string]any
	Position        int
	At              time.Time
}

// ConnectorChange is what a step leaves a connector as: every field, as it is after the step.
type ConnectorChange struct {
	Step            string
	State           string
	Configuration   map[string]any
	Position        int
	Test            *ConnectorTest
	ManifestVersion string
	ManifestHash    string
	SecretNames     []string
	Consent         ConnectorConsent
	At              time.Time
}

// BootstrapConnector is an email connector the first-start bootstrap installs from the deployment's settings, enabled,
// with its secrets as references (rev 0.139).
type BootstrapConnector struct {
	NewConnector
	Secrets map[string]ports.SecretRef
}

// InstallationChange is a change to an installation connector waiting for, or decided by, the other Super admin
// (FR-ADM-07): the change and the RFC 8785 envelope its decider signs.
type InstallationChange struct {
	ID             uuid.UUID
	ConnectorID    *uuid.UUID // none for an install
	Connector      string
	Step           string
	Summary        string
	Change         []byte // a JSON object
	Envelope       string
	EnvelopeHash   []byte
	RequestedBy    uuid.UUID
	RequestedAt    time.Time
	RequesterMFAAt time.Time
	ExpiresAt      time.Time
	Status         string // pending, approved, rejected or expired
	DecidedBy      *uuid.UUID
	DecidedAt      *time.Time
	DeciderMFAAt   *time.Time
	Reason         string
	AppliedAt      *time.Time
}

// NewInstallationChange is a change to request.
type NewInstallationChange struct {
	ConnectorID    *uuid.UUID
	Connector      string
	Step           string
	Summary        string
	Change         []byte
	Envelope       string
	EnvelopeHash   []byte
	RequestedBy    uuid.UUID
	RequesterMFAAt time.Time
	At             time.Time
	ExpiresAt      time.Time
}

// Connectors are the installed connectors (FR-INT-20, FR-INT-26). The installation's are global, changed only by an
// active Super admin (by), as approved by change once there are two (uuid.Nil for none: CodeConnectorChange when one is
// needed), and audited in the unit of work's organisation, the installation's. An organisation's are its own rows,
// audited in the same transaction. Every step is checked by the database: the lifecycle (CodeConnector), versions
// (CodeVersion) and dual control (CodeConnectorChange).
type Connectors interface {
	// Installation is every installation connector, live and uninstalled, in position order.
	Installation(ctx context.Context) ([]InstalledConnector, error)
	// InstallationSecrets is every version of the installation connectors' secrets.
	InstallationSecrets(ctx context.Context) ([]ConnectorSecret, error)
	InstallInstallation(ctx context.Context, by, change uuid.UUID, n NewConnector) (InstalledConnector, error)
	// BootstrapInstallation installs the email connectors from the deployment's settings, only into an empty catalogue
	// (CodeBootstrap otherwise), as the service.
	BootstrapInstallation(ctx context.Context, cs []BootstrapConnector, at time.Time) (int, error)
	// ChangeInstallation applies a step to installation connector id at version.
	ChangeInstallation(ctx context.Context, by, change, id uuid.UUID, version int, c ConnectorChange) (InstalledConnector, error)
	// SetInstallationSecret makes ref the current version of secret name; the one it replaces stays valid until
	// validUntil.
	SetInstallationSecret(ctx context.Context, by, change, id uuid.UUID, name string, ref ports.SecretRef, validUntil, at time.Time) error
	// DestroyedInstallationSecret records that the replaced version ref, past its overlap, was destroyed.
	DestroyedInstallationSecret(ctx context.Context, ref ports.SecretRef, at time.Time) error
	RequestInstallationChange(ctx context.Context, n NewInstallationChange) (InstallationChange, error)
	// DecideInstallationChange approves or rejects pending change id as the other Super admin, who signs its envelope's
	// hash.
	DecideInstallationChange(ctx context.Context, by, id uuid.UUID, hash []byte, approve bool, mfaAt time.Time, reason string,
		at time.Time) (InstallationChange, error)
	// ExpireInstallationChanges marks the pending changes past their expiry as expired, and returns them.
	ExpireInstallationChanges(ctx context.Context, at time.Time) ([]InstallationChange, error)
	// InstallationChanges is every installation change, newest first.
	InstallationChanges(ctx context.Context) ([]InstallationChange, error)

	// Organisation is every connector of the organisation's, live and uninstalled, in position order.
	Organisation(ctx context.Context) ([]InstalledConnector, error)
	// OrganisationSecrets is every version of the organisation connectors' secrets.
	OrganisationSecrets(ctx context.Context) ([]ConnectorSecret, error)
	InstallOrganisation(ctx context.Context, n NewConnector) (InstalledConnector, error)
	ChangeOrganisation(ctx context.Context, id uuid.UUID, version int, c ConnectorChange) (InstalledConnector, error)
	SetOrganisationSecret(ctx context.Context, id uuid.UUID, name string, ref ports.SecretRef, validUntil, at time.Time) error
	DestroyedOrganisationSecret(ctx context.Context, ref ports.SecretRef, at time.Time) error
}
