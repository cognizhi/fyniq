package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ConnectorDeps are what the connector operations need (FR-INT-20, FR-INT-25…27): the release's catalogue, the store
// of the keys people type, which secret holds each installation connector key, and the runtime the connection test
// runs through. Nil Catalogue: the operations answer that connectors are not configured.
type ConnectorDeps struct {
	Catalogue *connector.Catalogue
	Secrets   ports.SecretStore
	Names     map[string]string
	Runtime   *connector.Runtime
}

// ConnectorCodes are the connector lifecycle's refusals.
var ConnectorCodes = []string{connector.CodeUnknown, connector.CodeScope, connector.CodeConfiguration, connector.CodeSecretMissing,
	connector.CodeSecretUnknown, connector.CodeState, connector.CodeNotInstalled, connector.CodeChange, storage.CodeConnector}

// connectorFixes say how to fix each refusal of the lifecycle (ENG-23).
var connectorFixes = map[string]string{
	connector.CodeUnknown:       "Choose a connector from the catalogue of this release",
	connector.CodeScope:         "Install it where its manifest says: installation-wide by a Super admin, or by an organisation",
	connector.CodeConfiguration: "Correct the field the message names, as the connector's configuration schema describes it",
	connector.CodeSecretMissing: "Set the secret, then try again",
	connector.CodeSecretUnknown: "Use one of the secrets the connector's manifest lists",
	connector.CodeState:         "Take the steps in order: install, configure, test, enable; pause and resume; disable; uninstall",
	connector.CodeNotInstalled:  "Check the ID: list the installed connectors",
	connector.CodeChange:        "Read the change again; ask for a new one if the connector changed since",
}

// DataClass is a class of data a connector sends out of Fyniq (PRD-001 AI-75).
type DataClass string

// Schema implements huma.SchemaProvider.
func (DataClass) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{"know-how", "business-figures", "personal-data", "payroll", "restricted"},
		Description: "A class of data (AI-75).", Examples: []any{"personal-data"}}
}

// RegionCode is where a provider processes what it is sent: a country code, EU, or configured.
type RegionCode string

// Schema implements huma.SchemaProvider.
func (RegionCode) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: "^([A-Z]{2}|EU|configured)$", Description: "A country code, EU, or configured.", Examples: []any{"US"}}
}

// ScopeName is a scope a connector asks of its provider or receives from Fyniq.
type ScopeName string

// Schema implements huma.SchemaProvider.
func (ScopeName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A scope.", Examples: []any{"fyniq.read.banking"}}
}

// WebhookPath is where a provider calls Fyniq.
type WebhookPath string

// Schema implements huma.SchemaProvider.
func (WebhookPath) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A webhook's path.", Examples: []any{"/api/webhooks/email/zeptomail"}}
}

// WebhookEvent is an event a provider's webhook sends.
type WebhookEvent string

// Schema implements huma.SchemaProvider.
func (WebhookEvent) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An event the webhook sends.", Examples: []any{"hardbounce"}}
}

// named is s as a slice of a named string type, through as, its conversion (no `~string` constraint: Semgrep's Go
// parser refuses one).
func named[T any](s []string, as func(string) T) []T {
	out := make([]T, 0, len(s))
	for _, x := range s {
		out = append(out, as(x))
	}
	return out
}

func asDataClass(s string) DataClass       { return DataClass(s) }
func asRegionCode(s string) RegionCode     { return RegionCode(s) }
func asWebhookEvent(s string) WebhookEvent { return WebhookEvent(s) }
func asScopeName(s string) ScopeName       { return ScopeName(s) }
func asWebhookPath(s string) WebhookPath   { return WebhookPath(s) }

// ConnectorCapability is a kind of service a connector implements, and how its work behaves (FR-INT-25…27).
type ConnectorCapability struct {
	Kind        string `json:"kind" enum:"sign-in,outbound-email,inbound-email,messaging,ticketing,payment-provider,bank-data,e-invoicing,document-ai" doc:"The kind of service." example:"outbound-email"`
	Trigger     string `json:"trigger" enum:"human-authority,system-rule,none" doc:"Who may make it act: a completed human authority, a rule of the platform, or nobody (FR-INT-27)." example:"system-rule"`
	TriggerRule string `json:"trigger_rule" doc:"The authority or rule, in plain words." example:"A message the outbox holds for sending."`
	OnDisable   string `json:"on_disable" enum:"hold,cancel" doc:"What disabling does to its queued work; pausing always holds it (FR-INT-26)." example:"hold"`
	InFlight    string `json:"in_flight" enum:"finish,reconcile" doc:"What happens to work already handed to the provider: it finishes, or is reconciled; never dropped." example:"reconcile"`
}

// ConnectorDestination is a place a connector connects to: a host, or the configuration field that names one.
type ConnectorDestination struct {
	Host       string `json:"host,omitempty" doc:"A host it may reach; *. allows its subdomains." example:"cpaas.zoho.com"`
	Configured string `json:"configured,omitempty" doc:"The configuration field that names a host the person chooses." example:"address"`
	Port       int    `json:"port,omitempty" doc:"Its port; 0 or absent for any." example:"443"`
}

// ConnectorSecretInfo is a secret a connector needs: never shown once set.
type ConnectorSecretInfo struct {
	Name                 string `json:"name" doc:"The secret's name in the manifest." example:"send_token"`
	Title                string `json:"title" doc:"What it is called." example:"Send token"`
	Description          string `json:"description" doc:"What it is." example:"The Mail Agent's send token."`
	RotationOverlapHours int    `json:"rotation_overlap_hours" doc:"How long a replaced value stays valid, so work in flight and webhooks still pass (FR-INT-26)." example:"24"`
}

// ConnectorWebhookInfo is a webhook the provider calls.
type ConnectorWebhookInfo struct {
	Path         string         `json:"path" doc:"Where the provider calls." example:"/api/webhooks/email/zeptomail"`
	Events       []WebhookEvent `json:"events" doc:"The events it sends."`
	RegisteredBy string         `json:"registered_by" enum:"connector,operator" doc:"operator: a person sets it in the provider's dashboard, and removes it after an uninstall." example:"operator"`
}

// CatalogueEntry is a connector of this release (FR-INT-25).
type CatalogueEntry struct {
	ID              string                 `json:"id" doc:"The connector's id." example:"zeptomail"`
	Name            string                 `json:"name" example:"Zoho ZeptoMail"`
	Publisher       string                 `json:"publisher" example:"Fyniq"`
	Version         string                 `json:"version" doc:"The release's manifest version." example:"1.0.0"`
	Summary         string                 `json:"summary" example:"Sends the platform's email through Zoho ZeptoMail."`
	Scope           string                 `json:"scope" enum:"installation,organisation" doc:"Installed by a Super admin for everyone, or by an organisation." example:"installation"`
	Capabilities    []ConnectorCapability  `json:"capabilities"`
	DataClasses     []DataClass            `json:"data_classes" doc:"The data classes it sends out of Fyniq."`
	Regions         []RegionCode           `json:"regions" doc:"Where the provider processes what it is sent."`
	Destinations    []ConnectorDestination `json:"destinations" doc:"Every place it connects to (SEC-36)."`
	Secrets         []ConnectorSecretInfo  `json:"secrets"`
	Webhooks        []ConnectorWebhookInfo `json:"webhooks"`
	Configuration   map[string]any         `json:"configuration" doc:"The JSON Schema of its configuration." example:"{\"type\":\"object\",\"properties\":{}}"`
	HealthCheck     string                 `json:"health_check" example:"Counts refusals and failed connections in recent sends."`
	TestAction      string                 `json:"test_action" example:"Sends one test message to the organisation's own email address."`
	LocalSubstitute string                 `json:"local_substitute" example:"Mailpit in the local stack."`
}

// CataloguePage is a page of the catalogue.
type CataloguePage struct {
	Items      []CatalogueEntry `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6InNtdHAifQ"`
}

// ConnectorSecretState is whether a connector's secret is set; never its value.
type ConnectorSecretState struct {
	Name       string     `json:"name" example:"send_token"`
	Set        bool       `json:"set" doc:"Whether a value is set." example:"true"`
	ReplacedAt *time.Time `json:"replaced_at,omitempty" doc:"When the current value was set." example:"2026-10-25T09:00:00Z"`
}

// ConnectorTestResult is a connection test's result, in Fyniq's words, never the provider's answer.
type ConnectorTestResult struct {
	Passed bool      `json:"passed" example:"true"`
	Reason string    `json:"reason" example:"Zoho ZeptoMail accepted the test message."`
	At     time.Time `json:"at" example:"2026-10-25T09:00:00Z"`
}

// ConsentChange is what a release's manifest asks beyond, or no longer asks of, what a connector consented to.
type ConsentChange struct {
	DataClasses  []DataClass            `json:"data_classes"`
	Scopes       []ScopeName            `json:"scopes" doc:"The provider's and Fyniq's scopes."`
	Destinations []ConnectorDestination `json:"destinations"`
	Webhooks     []WebhookPath          `json:"webhooks"`
}

// Connector is an installed connector (FR-INT-20, FR-INT-26).
type Connector struct {
	ID              uuid.UUID              `json:"id" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
	Connector       string                 `json:"connector" doc:"Its id in the catalogue." example:"zeptomail"`
	Name            string                 `json:"name" example:"Zoho ZeptoMail"`
	Installation    bool                   `json:"installation" doc:"Installed for everyone by a Super admin; false for the organisation's." example:"true"`
	State           string                 `json:"state" enum:"installed,configured,tested,enabled,paused,disabled,uninstalled" doc:"Where it is in its lifecycle." example:"enabled"`
	Health          string                 `json:"health" enum:"ok,failing,unknown" doc:"ok when enabled and its last test passed; failing when that test failed; unknown otherwise." example:"ok"`
	Position        int                    `json:"position" doc:"Its place among the connectors of its kind: 1 is the primary." example:"1"`
	ManifestVersion string                 `json:"manifest_version" doc:"The manifest it consented to." example:"1.0.0"`
	ReleaseVersion  string                 `json:"release_version" doc:"The release's manifest; differs when an upgrade is available." example:"1.0.0"`
	UpgradeWidens   bool                   `json:"upgrade_widens" doc:"The release's manifest asks for more: it runs under the old consent until someone consents (FR-INT-26)." example:"false"`
	Added           ConsentChange          `json:"added" doc:"What the release's manifest asks beyond the consent."`
	Removed         ConsentChange          `json:"removed" doc:"What it no longer asks."`
	Configuration   map[string]any         `json:"configuration" doc:"Its configuration; never a secret." example:"{\"url\":\"https://cpaas.zoho.com/v1.1/email\"}"`
	Secrets         []ConnectorSecretState `json:"secrets"`
	Test            *ConnectorTestResult   `json:"test,omitempty"`
	Webhooks        []ConnectorWebhookInfo `json:"webhooks_to_remove,omitempty" doc:"After an uninstall: the webhooks a person removes from the provider's dashboard."`
	InstalledBy     string                 `json:"installed_by" example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	InstalledAt     time.Time              `json:"installed_at" example:"2026-10-25T09:00:00Z"`
	ChangedAt       time.Time              `json:"changed_at" example:"2026-10-25T09:00:00Z"`
	Version         int                    `json:"version" doc:"Its version, for If-Match." example:"3"`
}

// ConnectorPage is a page of installed connectors.
type ConnectorPage struct {
	Items      []Connector `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6InNtdHAifQ"`
}

// InstallationChange is a change to an installation connector waiting for the other Super admin (FR-ADM-07).
type InstallationChange struct {
	ID           uuid.UUID  `json:"id" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e42"`
	ConnectorID  *uuid.UUID `json:"connector_id,omitempty" doc:"The installed connector; absent for an install." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
	Connector    string     `json:"connector" example:"zeptomail"`
	Step         string     `json:"step" enum:"install,configure,secret,enable,resume,disable,uninstall,order,consent" example:"resume"`
	Summary      string     `json:"summary" example:"Resume Zoho ZeptoMail"`
	Status       string     `json:"status" enum:"pending,approved,rejected,expired" example:"pending"`
	Envelope     string     `json:"envelope" doc:"The RFC 8785 envelope the other Super admin signs." example:"{\"change\":{\"step\":\"resume\"}}"`
	EnvelopeHash string     `json:"envelope_hash" doc:"Its SHA-256, hex: send it to approve or reject." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	RequestedBy  uuid.UUID  `json:"requested_by" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	RequestedAt  time.Time  `json:"requested_at" example:"2026-10-25T09:00:00Z"`
	ExpiresAt    time.Time  `json:"expires_at" example:"2026-11-01T09:00:00Z"`
	DecidedBy    *uuid.UUID `json:"decided_by,omitempty" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e43"`
	DecidedAt    *time.Time `json:"decided_at,omitempty" example:"2026-10-25T10:00:00Z"`
	Reason       string     `json:"reason,omitempty" example:"Not this key."`
}

func (InstallationChange) waitsForApproval() bool { return true }

// InstallationChangePage is a page of installation changes.
type InstallationChangePage struct {
	Items      []InstallationChange `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6IngifQ"`
}

// ------------------------------------------------------------------ requests

// ConnectorPath names an installed connector in the path.
type ConnectorPath struct {
	ID string `path:"connector_id" format:"uuid" doc:"The installed connector's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
}

// ConnectorSecretPath names a secret of an installed connector in the path.
type ConnectorSecretPath struct {
	ConnectorPath
	Secret string `path:"secret" pattern:"^[a-z][a-z0-9_]{1,39}$" doc:"The secret's name in the manifest." example:"send_token"`
}

// InstallConnectorRequest names a connector of the catalogue.
type InstallConnectorRequest struct {
	Connector string `json:"connector" pattern:"^[a-z][a-z0-9-]{1,39}$" doc:"Its id in the catalogue." example:"zeptomail"`
}

// ConfigureConnectorRequest is a connector's whole configuration, as its manifest's schema describes it.
type ConfigureConnectorRequest struct {
	Configuration map[string]any `json:"configuration" doc:"Every field, as the connector's configuration schema describes it; never a secret." example:"{\"url\":\"https://cpaas.zoho.com/v1.1/email\"}"`
}

// SetConnectorSecretRequest is a secret's new value: write-only.
type SetConnectorSecretRequest struct {
	Value string `json:"value" minLength:"1" maxLength:"8192" writeOnly:"true" doc:"The secret's value. It goes to Secret Manager at once and is never shown again (FR-INT-20)." example:"zepto-send-token-example"`
}

// ConnectorStepRequest is a step of the lifecycle.
type ConnectorStepRequest struct {
	Step     string `json:"step" enum:"enable,pause,resume,disable,uninstall,consent,order" doc:"The step: pause takes effect at once; the others need a second person once the rules ask for one (FR-INT-26)." example:"enable"`
	Position int    `json:"position,omitempty" minimum:"1" maximum:"1000" doc:"For order: the connector's new place among those of its kind, 1 for the primary." example:"1"`
}

// DecideChangeRequest signs an installation change's envelope.
type DecideChangeRequest struct {
	EnvelopeHash string `json:"envelope_hash" pattern:"^[0-9a-f]{64}$" doc:"The SHA-256 of the envelope you read, hex." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Reason       string `json:"reason,omitempty" maxLength:"500" doc:"Why, for a rejection." example:"Not this key."`
}

// ChangePath names an installation change in the path.
type ChangePath struct {
	ID string `path:"change_id" format:"uuid" doc:"The change's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e42"`
}

type catalogueInput struct{ PageQuery }
type catalogueOutput struct{ Body CataloguePage }
type connectorListInput struct{ PageQuery }
type connectorListOutput struct{ Body ConnectorPage }
type connectorGetInput struct{ ConnectorPath }
type connectorReadOutput struct {
	ETag string `header:"ETag" doc:"The connector's version, for If-Match."`
	Body Connector
}
type installConnectorInput struct {
	KeyHeader
	PreferHeader
	Body InstallConnectorRequest
}
type configureConnectorInput struct {
	ConnectorPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ConfigureConnectorRequest
}
type setConnectorSecretInput struct {
	ConnectorSecretPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body SetConnectorSecretRequest
}
type connectorStepInput struct {
	ConnectorPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ConnectorStepRequest
}
type testConnectorInput struct {
	ConnectorPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}
type connectorTestOutput struct {
	ETag string `header:"ETag" doc:"The connector's version, for If-Match."`
	Body Connector
}

func testOutput(w *connectorWriteOutput, err error) (*connectorTestOutput, error) {
	if err != nil {
		return nil, err
	}
	c, _ := w.Body.(Connector)
	return &connectorTestOutput{ETag: w.ETag, Body: c}, nil
}

type connectorWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The connector's version, for If-Match; absent when the change waits for a second person."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     any    // a Connector, or the change (installation) or approval request (organisation) it waits for (202)
}
type changeListInput struct{ PageQuery }
type changeListOutput struct{ Body InstallationChangePage }
type decideChangeInput struct {
	ChangePath
	KeyHeader
	PreferHeader
	Body DecideChangeRequest
}
type changeOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     any    // the Connector it applied, or the InstallationChange rejected
}

// ------------------------------------------------------------------ views

func catalogueEntryOf(m connector.Manifest) CatalogueEntry {
	e := CatalogueEntry{ID: m.ID, Name: m.Name, Publisher: m.Publisher, Version: m.Version, Summary: m.Summary, Scope: m.Scope,
		Capabilities: []ConnectorCapability{}, DataClasses: named(m.DataClasses, asDataClass), Regions: named(m.Regions, asRegionCode), Destinations: []ConnectorDestination{},
		Secrets: []ConnectorSecretInfo{}, Webhooks: []ConnectorWebhookInfo{}, Configuration: map[string]any{}, HealthCheck: m.HealthCheck,
		TestAction: m.TestAction, LocalSubstitute: m.LocalSubstitute}
	for _, c := range m.Capabilities {
		e.Capabilities = append(e.Capabilities, ConnectorCapability{Kind: string(c.Kind), Trigger: c.Trigger, TriggerRule: c.TriggerRule,
			OnDisable: c.OnDisable, InFlight: c.InFlight})
	}
	for _, d := range m.Destinations {
		e.Destinations = append(e.Destinations, ConnectorDestination{Host: d.Host, Configured: d.Configured, Port: d.Port})
	}
	for _, s := range m.Secrets {
		e.Secrets = append(e.Secrets, ConnectorSecretInfo{Name: s.Name, Title: s.Title, Description: s.Description, RotationOverlapHours: s.RotationOverlapHours})
	}
	e.Webhooks = webhooksOf(m.Webhooks)
	_ = json.Unmarshal(m.Configuration, &e.Configuration)
	return e
}

func webhooksOf(ws []connector.Webhook) []ConnectorWebhookInfo {
	out := []ConnectorWebhookInfo{}
	for _, w := range ws {
		out = append(out, ConnectorWebhookInfo{Path: w.Path, Events: named(w.Events, asWebhookEvent), RegisteredBy: w.RegisteredBy})
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func consentChangeOf(c storage.ConnectorConsent) ConsentChange {
	out := ConsentChange{DataClasses: named(c.DataClasses, asDataClass), Scopes: named(append(slices.Clone(c.ProviderScopes), c.FyniqScopes...), asScopeName),
		Destinations: []ConnectorDestination{}, Webhooks: named(c.Webhooks, asWebhookPath)}
	for _, d := range c.Destinations {
		out.Destinations = append(out.Destinations, ConnectorDestination{Host: d.Host, Configured: d.Configured, Port: d.Port})
	}
	return out
}

func (a *API) connectorOf(c storage.InstalledConnector, secrets []storage.ConnectorSecret) Connector {
	out := Connector{ID: c.ID, Connector: c.Connector, Name: c.Connector, Installation: c.Installation, State: c.State, Health: "unknown",
		Position: c.Position, ManifestVersion: c.ManifestVersion, ReleaseVersion: c.ManifestVersion, Configuration: c.Configuration,
		Secrets: []ConnectorSecretState{}, InstalledBy: c.InstalledBy, InstalledAt: c.InstalledAt.UTC(), ChangedAt: c.ChangedAt.UTC(), Version: c.Version,
		Added: consentChangeOf(storage.ConnectorConsent{}), Removed: consentChangeOf(storage.ConnectorConsent{})}
	if out.Configuration == nil {
		out.Configuration = map[string]any{}
	}
	if b, ok := a.deps.Connectors.Catalogue.Get(c.Connector); ok {
		out.Name, out.ReleaseVersion = b.Manifest.Name, b.Manifest.Version
		d := connector.Diff(c.Consent, b.Manifest)
		out.UpgradeWidens, out.Added, out.Removed = d.Widens(), consentChangeOf(d.Added), consentChangeOf(d.Removed)
	}
	if c.Test != nil {
		out.Test = &ConnectorTestResult{Passed: c.Test.Passed, Reason: c.Test.Reason, At: c.Test.At.UTC()}
		switch {
		case !c.Test.Passed:
			out.Health = "failing"
		case c.State == storage.ConnectorEnabled:
			out.Health = "ok"
		}
	}
	for _, name := range c.SecretNames {
		st := ConnectorSecretState{Name: name}
		for _, s := range secrets {
			if s.ConnectorID == c.ID && s.Name == name && s.ValidUntil == nil && s.DestroyedAt == nil {
				at := s.SetAt.UTC()
				st.Set, st.ReplacedAt = true, &at
			}
		}
		out.Secrets = append(out.Secrets, st)
	}
	return out
}

func installationChangeOf(c storage.InstallationChange) InstallationChange {
	return InstallationChange{ID: c.ID, ConnectorID: c.ConnectorID, Connector: c.Connector, Step: c.Step, Summary: c.Summary, Status: c.Status,
		Envelope: c.Envelope, EnvelopeHash: hex.EncodeToString(c.EnvelopeHash), RequestedBy: c.RequestedBy, RequestedAt: c.RequestedAt.UTC(),
		ExpiresAt: c.ExpiresAt.UTC(), DecidedBy: c.DecidedBy, DecidedAt: c.DecidedAt, Reason: c.Reason}
}

// pendsChange declares a write that answers 200 with done, or 202 with the change for the other Super admin.
func pendsChange(a *API, op huma.Operation, done reflect.Type) huma.Operation {
	reg := a.OpenAPI().Components.Schemas
	status := op.DefaultStatus
	if status == 0 {
		status = http.StatusOK
	}
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	op.Responses[fmt.Sprint(status)] = &huma.Response{Description: http.StatusText(status),
		Content: map[string]*huma.MediaType{"application/json": {Schema: reg.Schema(done, true, "")}}}
	op.Responses["202"] = &huma.Response{Description: "Accepted: with two Super admins, the step waits for the other one's approval " +
		"(FR-ADM-07); nothing changed yet. The body is the change.",
		Content: map[string]*huma.MediaType{"application/json": {Schema: reg.Schema(reflect.TypeFor[InstallationChange](), true, "")}}}
	return op
}

func (a *API) installations() connector.Installations {
	return connector.Installations{Catalogue: a.deps.Connectors.Catalogue, Secrets: a.deps.Connectors.Secrets, Names: a.deps.Connectors.Names,
		Admins: a.deps.Installation, Clock: a.deps.Clock}
}

func (a *API) organisations() connector.Organisations {
	return connector.Organisations{Catalogue: a.deps.Connectors.Catalogue, Secrets: a.deps.Connectors.Secrets, Clock: a.deps.Clock}
}

func (a *API) connectorsReady() error {
	if a.deps.Connectors.Catalogue == nil || a.deps.Installation == nil {
		return problem(CodeUnavailable, "Connectors are not configured in this deployment.", "Configure the database and the connector catalogue, then try again.")
	}
	return nil
}

// installationResult is what an installation step answers: the connector, or the change it waits for.
func (a *API) installationResult(ctx context.Context, u storage.UnitOfWork, r connector.Result) (any, error) {
	if r.Pending != nil {
		return installationChangeOf(*r.Pending), nil
	}
	secrets, err := u.Connectors().InstallationSecrets(ctx)
	if err != nil {
		return nil, err
	}
	c := a.connectorOf(r.Connector, secrets)
	c.Webhooks = webhooksOf(r.Webhooks)
	return c, nil
}

func installedOne(ctx context.Context, u storage.UnitOfWork, installation bool, id uuid.UUID) (storage.InstalledConnector, error) {
	var all []storage.InstalledConnector
	var err error
	if installation {
		all, err = u.Connectors().Installation(ctx)
	} else {
		all, err = u.Connectors().Organisation(ctx)
	}
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	i := slices.IndexFunc(all, func(c storage.InstalledConnector) bool { return c.ID == id })
	if i < 0 {
		return storage.InstalledConnector{}, problem(connector.CodeNotInstalled, "No installed connector has that ID.", connectorFixes[connector.CodeNotInstalled])
	}
	return all[i], nil
}

func connectorOutput(w written[any]) *connectorWriteOutput {
	out := &connectorWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
	if c, ok := w.Body.(Connector); ok && !w.Previewed {
		out.ETag = etag(c.Version)
	}
	return out
}

// testAddress is where a connection test's message goes: the organisation's own email address, from its profile (the
// installation organisation's for a Super admin outside every organisation), so it reaches nobody else (FR-INT-21).
func (a *API) testAddress(ctx context.Context) (string, error) {
	p, _ := authn.From(ctx)
	org := p.Organisation
	if org == uuid.Nil && a.deps.Installation != nil {
		var err error
		if org, err = a.deps.Installation.InstallationOrganisation(ctx); err != nil {
			return "", problem(CodeNoEmailAddress, "There is no organisation email address to send the test message to.",
				"Sign in to an organisation whose profile has an email address, then test again.")
		}
	}
	var to string
	err := a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		to = o.Profile.Email
		return err
	})
	if err == nil && to == "" {
		return "", problem(CodeNoEmailAddress, "The organisation has no email address to send the test message to.",
			"Set the organisation's email in its profile, then test again.")
	}
	return to, err
}

func registerConnectors(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)
	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable}, locks...)
	versioned := []string{CodeIfMatchRequired, CodeStale, storage.CodeVersion}
	changeCodes := []string{storage.CodeConnectorChange}

	register(a, authz.ListConnectorCatalogue, huma.Operation{
		OperationID: "listConnectorCatalogue", Method: http.MethodGet, Path: "/v1/connector-catalogue", Tags: []string{"Connectors"},
		Summary: "List the connectors of this release",
		Description: "Every connector compiled into this release (FR-INT-25): what it implements and who may make it act, the data classes it " +
			"sends out and where, its destinations, the secrets it needs (never their values), its webhooks, health check, test action and " +
			"local substitute, and the JSON Schema of its configuration. Grouped by capability. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeUnavailable}, locks...), func(ctx context.Context, in *catalogueInput) (*catalogueOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		page, next, err := pageOf(a.deps.Connectors.Catalogue.All(), func(b connector.Binding) string { return b.Manifest.ID }, in.PageQuery)
		out := CataloguePage{Items: []CatalogueEntry{}, NextCursor: next}
		for _, b := range page {
			out.Items = append(out.Items, catalogueEntryOf(b.Manifest))
		}
		return &catalogueOutput{Body: out}, err
	})

	// ---------------------------------------------------------------- the installation's
	register(a, authz.ListInstallationConnectors, huma.Operation{
		OperationID: "listInstallationConnectors", Method: http.MethodGet, Path: "/v1/installation/connectors", Tags: []string{"Connectors"},
		Summary: "List the installation's connectors",
		Description: "The connectors a Super admin installed for everyone (FR-INT-20, FR-ADM-07), live and uninstalled, in position order: each " +
			"one's state, health, position, configuration, which secrets are set (never their values), last test, and whether the release's " +
			"manifest asks more than it consented to. " + authz.ListInstallationConnectors.Note + " Read-only; no side effects.",
	}, append([]string{CodeCursor}, admin...), func(ctx context.Context, in *connectorListInput) (*connectorListOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		by, err := a.superAdmin(ctx, "Listing the installation's connectors")
		if err != nil {
			return nil, err
		}
		out := ConnectorPage{Items: []Connector{}}
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			all, err := u.Connectors().Installation(ctx)
			if err != nil {
				return err
			}
			secrets, err := u.Connectors().InstallationSecrets(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(c storage.InstalledConnector) string { return c.ID.String() }, in.PageQuery)
			for _, c := range page {
				out.Items = append(out.Items, a.connectorOf(c, secrets))
			}
			out.NextCursor = next
			return err
		})
		return &connectorListOutput{Body: out}, err
	})

	register(a, authz.GetInstallationConnector, huma.Operation{
		OperationID: "getInstallationConnector", Method: http.MethodGet, Path: "/v1/installation/connectors/{connector_id}", Tags: []string{"Connectors"},
		Summary: "Read one of the installation's connectors",
		Description: "One installed connector with its ETag (FR-INT-20): as listed, and the manifest diff when the release's asks more. " +
			authz.GetInstallationConnector.Note + " Read-only; no side effects.",
	}, append([]string{connector.CodeNotInstalled}, admin...), func(ctx context.Context, in *connectorGetInput) (*connectorReadOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		by, err := a.superAdmin(ctx, "Reading a connector")
		if err != nil {
			return nil, err
		}
		id, err := parseID("connector_id", in.ID)
		if err != nil {
			return nil, err
		}
		var out connectorReadOutput
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			c, err := installedOne(ctx, u, true, id)
			if err != nil {
				return err
			}
			secrets, err := u.Connectors().InstallationSecrets(ctx)
			out.Body = a.connectorOf(c, secrets)
			out.ETag = etag(c.Version)
			return err
		})
		return &out, err
	})

	installationWrite := func(ctx context.Context, in any, key, prefer, ifMatch string, id uuid.UUID, what string, status int,
		run func(ctx context.Context, by uuid.UUID, mfaAt time.Time, u storage.UnitOfWork, c storage.InstalledConnector) (connector.Result, error)) (*connectorWriteOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		by, err := a.superAdmin(ctx, what)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		w, err := inInstallationWrite(ctx, a, by, key, prefer, in, status, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
			var c storage.InstalledConnector
			if id != uuid.Nil {
				var err error
				if c, err = installedOne(ctx, u, true, id); err != nil {
					return nil, err
				}
				if err := checkIfMatch(ifMatch, etag(c.Version)); err != nil {
					return nil, err
				}
			}
			r, err := run(ctx, by, p.StepUpAt, u, c)
			if err != nil {
				return nil, err
			}
			if what != testingConnector { // a test changes nothing anyone must hear of
				if err := a.tellOfInstallationStep(ctx, u, by, c, r); err != nil {
					return nil, err
				}
			}
			return a.installationResult(ctx, u, r)
		})
		if err != nil {
			return nil, err
		}
		return connectorOutput(w), nil
	}
	note := " Needs an active Super admin who passed step-up in the last 5 minutes. With two Super admins the answer is 202 with the change " +
		"the other one approves (FR-ADM-07); with one it applies at once and is marked so in the installation's trail. Side effects: the " +
		"connector's change, or the change request, and its event in the installation's trail. Idempotent with Idempotency-Key; " +
		"Prefer: return=preview checks it and changes nothing."

	register(a, authz.InstallInstallationConnector, pendsChange(a, huma.Operation{
		OperationID: "installInstallationConnector", Method: http.MethodPost, Path: "/v1/installation/connectors", Tags: []string{"Connectors"},
		DefaultStatus: http.StatusCreated, Summary: "Install a connector for everyone",
		Description: "Installs a connector of the catalogue whose scope is the installation, at the end of its kind's order, consenting to its " +
			"manifest: the data classes it sends out, its scopes, destinations and webhooks (FR-INT-26)." + note,
	}, reflect.TypeFor[Connector]()), append(append(append(append([]string{CodeInvalid}, admin...), writes...), changeCodes...), ConnectorCodes...),
		func(ctx context.Context, in *installConnectorInput) (*connectorWriteOutput, error) {
			return installationWrite(ctx, in, in.IdempotencyKey, in.Prefer, "", uuid.Nil, "Installing a connector", http.StatusCreated,
				func(ctx context.Context, by uuid.UUID, mfaAt time.Time, u storage.UnitOfWork, _ storage.InstalledConnector) (connector.Result, error) {
					return a.installations().Install(ctx, u, by, mfaAt, in.Body.Connector)
				})
		})

	register(a, authz.ConfigureInstallationConnector, pendsChange(a, huma.Operation{
		OperationID: "configureInstallationConnector", Method: http.MethodPut, Path: "/v1/installation/connectors/{connector_id}/configuration",
		Tags: []string{"Connectors"}, Summary: "Configure one of the installation's connectors",
		Description: "Replaces the connector's whole configuration, checked against its manifest's JSON Schema (CONNECTOR-03 names the field); " +
			"never a secret. A connector not yet enabled is tested again before it is enabled; an enabled one keeps running (FR-INT-26)." + note +
			" Needs If-Match with its ETag.",
	}, reflect.TypeFor[Connector]()), append(append(append(append(append([]string{CodeInvalid}, admin...), writes...), versioned...), changeCodes...), ConnectorCodes...),
		func(ctx context.Context, in *configureConnectorInput) (*connectorWriteOutput, error) {
			id, err := parseID("connector_id", in.ID)
			if err != nil {
				return nil, err
			}
			return installationWrite(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, id, "Configuring a connector", http.StatusOK,
				func(ctx context.Context, by uuid.UUID, mfaAt time.Time, u storage.UnitOfWork, c storage.InstalledConnector) (connector.Result, error) {
					return a.installations().Step(ctx, u, by, mfaAt, c.ID, c.Version, storage.StepConfigure, connector.Input{Configuration: in.Body.Configuration}, "")
				})
		})

	register(a, authz.SetInstallationConnectorSecret, pendsChange(a, huma.Operation{
		OperationID: "setInstallationConnectorSecret", Method: http.MethodPut, Path: "/v1/installation/connectors/{connector_id}/secrets/{secret}",
		Tags: []string{"Connectors"}, Summary: "Set a secret of one of the installation's connectors",
		Description: "Stores a new value of one of the connector's secrets in Secret Manager at once, as a new version, and records only its " +
			"reference (FR-INT-20); the value is never shown or kept anywhere else. When it applies, the value it replaces stays valid for " +
			"the manifest's overlap, then is destroyed; a change that is rejected or expires destroys the new value (FR-INT-26)." + note +
			" Needs If-Match with its ETag.",
	}, reflect.TypeFor[Connector]()), append(append(append(append(append([]string{CodeInvalid}, admin...), writes...), versioned...), changeCodes...), ConnectorCodes...),
		func(ctx context.Context, in *setConnectorSecretInput) (*connectorWriteOutput, error) {
			id, err := parseID("connector_id", in.ID)
			if err != nil {
				return nil, err
			}
			return installationWrite(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, id, "Setting a connector's secret", http.StatusOK,
				func(ctx context.Context, by uuid.UUID, mfaAt time.Time, u storage.UnitOfWork, c storage.InstalledConnector) (connector.Result, error) {
					return a.installations().Step(ctx, u, by, mfaAt, c.ID, c.Version, storage.StepSecret, connector.Input{Secret: in.Secret}, in.Body.Value)
				})
		})

	register(a, authz.StepInstallationConnector, pendsChange(a, huma.Operation{
		OperationID: "stepInstallationConnector", Method: http.MethodPost, Path: "/v1/installation/connectors/{connector_id}/steps",
		Tags: []string{"Connectors"}, Summary: "Take a step of an installation connector's lifecycle",
		Description: "enable, pause, resume, disable, uninstall, consent (to the release's manifest: an upgrade) or order (its position) " +
			"(FR-INT-26). Pause takes effect at once for one Super admin with step-up, even with two (the owner's decision of 25 Oct 2026): " +
			"it stops new work and holds the queue. Disable cancels the queued work its manifest marks cancellable; uninstall also lists " +
			"the webhooks to remove by hand, and its secrets are destroyed." + note + " Needs If-Match with its ETag.",
	}, reflect.TypeFor[Connector]()), append(append(append(append(append([]string{CodeInvalid}, admin...), writes...), versioned...), changeCodes...), ConnectorCodes...),
		func(ctx context.Context, in *connectorStepInput) (*connectorWriteOutput, error) {
			id, err := parseID("connector_id", in.ID)
			if err != nil {
				return nil, err
			}
			return installationWrite(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, id, "Changing a connector", http.StatusOK,
				func(ctx context.Context, by uuid.UUID, mfaAt time.Time, u storage.UnitOfWork, c storage.InstalledConnector) (connector.Result, error) {
					return a.installations().Step(ctx, u, by, mfaAt, c.ID, c.Version, in.Body.Step, connector.Input{Position: in.Body.Position}, "")
				})
		})

	register(a, authz.TestInstallationConnector, huma.Operation{
		OperationID: "testInstallationConnector", Method: http.MethodPost, Path: "/v1/installation/connectors/{connector_id}/test",
		Tags: []string{"Connectors"}, Summary: "Test one of the installation's connectors",
		Description: "Runs the connector's test action through the egress guard (SEC-36): for email, one test message to the organisation's own " +
			"email address, from its profile, so it reaches nobody else. The result is pass or fail with Fyniq's reason, never what the provider answered, and is " +
			"recorded on the connector: a passing test of a configured connector lets it be enabled. No second person is needed. " +
			authz.TestInstallationConnector.Note + " Side effects: the test message, the result and its event in the installation's trail. " +
			"Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.",
	}, append(append(append([]string{CodeNoEmailAddress}, admin...), versioned...), ConnectorCodes...),
		func(ctx context.Context, in *testConnectorInput) (*connectorTestOutput, error) {
			id, err := parseID("connector_id", in.ID)
			if err != nil {
				return nil, err
			}
			if err := a.connectorsReady(); err != nil {
				return nil, err
			}
			to, err := a.testAddress(ctx)
			if err != nil {
				return nil, err
			}
			return testOutput(installationWrite(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, id, testingConnector, http.StatusOK,
				func(ctx context.Context, by uuid.UUID, _ time.Time, u storage.UnitOfWork, c storage.InstalledConnector) (connector.Result, error) {
					if storage.IsPreview(ctx) { // a preview checks the request and sends nothing
						return connector.Result{Connector: c}, nil
					}
					secrets, err := u.Connectors().InstallationSecrets(ctx)
					if err != nil {
						return connector.Result{}, err
					}
					result := a.deps.Connectors.Runtime.TestEmail(ctx, c, secrets, to, a.deps.Mail.From)
					return a.installations().Test(ctx, u, by, c.ID, c.Version, result)
				}))
		})

	register(a, authz.ListInstallationChanges, huma.Operation{
		OperationID: "listInstallationChanges", Method: http.MethodGet, Path: "/v1/installation/changes", Tags: []string{"Connectors"},
		Summary: "List the changes waiting for a Super admin",
		Description: "Every change to an installation connector, newest first: pending ones wait for the other Super admin, who reads the " +
			"envelope and signs its SHA-256 (FR-ADM-07, FR-INT-26). " + authz.ListInstallationChanges.Note + " Read-only; no side effects.",
	}, append([]string{CodeCursor}, admin...), func(ctx context.Context, in *changeListInput) (*changeListOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		by, err := a.superAdmin(ctx, "Listing the installation's changes")
		if err != nil {
			return nil, err
		}
		out := InstallationChangePage{Items: []InstallationChange{}}
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			all, err := u.Connectors().InstallationChanges(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(c storage.InstallationChange) string { return c.ID.String() }, in.PageQuery)
			for _, c := range page {
				out.Items = append(out.Items, installationChangeOf(c))
			}
			out.NextCursor = next
			return err
		})
		return &changeListOutput{Body: out}, err
	})

	decide := func(ctx context.Context, in *decideChangeInput, approve bool) (*changeOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		what := "Approving a change"
		if !approve {
			what = "Rejecting a change"
		}
		by, err := a.superAdmin(ctx, what)
		if err != nil {
			return nil, err
		}
		id, err := parseID("change_id", in.ID)
		if err != nil {
			return nil, err
		}
		hash, err := hex.DecodeString(in.Body.EnvelopeHash)
		if err != nil {
			return nil, problem(CodeInvalid, "envelope_hash is not hex.", "Send the envelope_hash the change shows.")
		}
		p, _ := authn.From(ctx)
		w, err := inInstallationWrite(ctx, a, by, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
			if !approve {
				c, err := a.installations().Reject(ctx, u, by, p.StepUpAt, id, hash, in.Body.Reason)
				if err != nil {
					return nil, err
				}
				return installationChangeOf(c), a.tellOfDecision(ctx, u, c)
			}
			r, err := a.installations().Approve(ctx, u, by, p.StepUpAt, id, hash)
			if err != nil {
				return nil, err
			}
			changes, err := u.Connectors().InstallationChanges(ctx)
			if err != nil {
				return nil, err
			}
			if i := slices.IndexFunc(changes, func(c storage.InstallationChange) bool { return c.ID == id }); i >= 0 {
				if err := a.tellOfDecision(ctx, u, changes[i]); err != nil {
					return nil, err
				}
			}
			out, err := a.installationResult(ctx, u, r)
			if c, ok := out.(Connector); ok {
				return c, err
			}
			return out, err
		})
		if err != nil {
			return nil, err
		}
		status := w.Status
		if _, ok := w.Body.(InstallationChange); ok {
			status = http.StatusOK // a rejection answers with the change, decided
		}
		return &changeOutput{Status: status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	}
	decideCodes := append(append(append([]string{CodeInvalid}, admin...), append(writes, changeCodes...)...), ConnectorCodes...)
	register(a, authz.ApproveInstallationChange, decidedChange(a, huma.Operation{
		OperationID: "approveInstallationChange", Method: http.MethodPost, Path: "/v1/installation/changes/{change_id}/approve",
		Tags: []string{"Connectors"}, Summary: "Approve a change to an installation connector",
		Description: "The other Super admin approves a pending change by signing the SHA-256 of its envelope, and it applies in the same " +
			"transaction (FR-ADM-07, FR-INT-26); never the person who asked. Refused once it expired, was decided, or the connector changed " +
			"since it was asked for (CONNECTOR-08). " + authz.ApproveInstallationChange.Note + " Side effects: the decision, the connector's " +
			"change and their events in the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and decides nothing.",
	}, reflect.TypeFor[Connector]()), decideCodes, func(ctx context.Context, in *decideChangeInput) (*changeOutput, error) {
		return decide(ctx, in, true)
	})
	register(a, authz.RejectInstallationChange, decidedChange(a, huma.Operation{
		OperationID: "rejectInstallationChange", Method: http.MethodPost, Path: "/v1/installation/changes/{change_id}/reject",
		Tags: []string{"Connectors"}, Summary: "Reject a change to an installation connector",
		Description: "The other Super admin rejects a pending change, signing the SHA-256 of its envelope, with a reason (FR-ADM-07); a new " +
			"secret value it carried is destroyed. " + authz.RejectInstallationChange.Note + " Side effects: the decision and its event in " +
			"the installation's trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and decides nothing.",
	}, reflect.TypeFor[InstallationChange]()), decideCodes, func(ctx context.Context, in *decideChangeInput) (*changeOutput, error) {
		return decide(ctx, in, false)
	})

	registerOrganisationConnectors(a, writes, versioned, locks)
}

// decidedChange declares a decision's answer: what it applied, or the change it rejected.
func decidedChange(a *API, op huma.Operation, done reflect.Type) huma.Operation {
	reg := a.OpenAPI().Components.Schemas
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	op.Responses["200"] = &huma.Response{Description: http.StatusText(http.StatusOK),
		Content: map[string]*huma.MediaType{"application/json": {Schema: reg.Schema(done, true, "")}}}
	return op
}

// ------------------------------------------------------------------ an organisation's

// connectorChange is what an organisation connector's approval request executes.
type connectorChange struct {
	Step          string          `json:"step"`
	Connector     string          `json:"connector"`
	Installed     string          `json:"installed,omitempty"`
	Version       int             `json:"version,omitempty"`
	Configuration map[string]any  `json:"configuration,omitempty"`
	Slot          string          `json:"slot,omitempty"`
	Ref           ports.SecretRef `json:"ref,omitempty"`
	Position      int             `json:"position,omitempty"`
}

func registerOrganisationConnectors(a *API, writes, versioned, locks []string) {
	asks := []string{CodeSessionRequired, CodeStepUpRequired, storage.CodeApproval, approval.CodePending, CodeUnavailable}

	register(a, authz.ListOrganisationConnectors, huma.Operation{
		OperationID: "listOrganisationConnectors", Method: http.MethodGet, Path: "/v1/organisation/connectors", Tags: []string{"Connectors"},
		Summary: "List the organisation's connectors",
		Description: "The connectors the organisation installed, such as its bank feeds (FR-INT-20), live and uninstalled, in position order, " +
			"as the installation's are listed. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeUnavailable}, locks...), func(ctx context.Context, in *connectorListInput) (*connectorListOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := ConnectorPage{Items: []Connector{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Connectors().Organisation(ctx)
			if err != nil {
				return err
			}
			secrets, err := u.Connectors().OrganisationSecrets(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(c storage.InstalledConnector) string { return c.ID.String() }, in.PageQuery)
			for _, c := range page {
				out.Items = append(out.Items, a.connectorOf(c, secrets))
			}
			out.NextCursor = next
			return err
		})
		return &connectorListOutput{Body: out}, err
	})

	register(a, authz.GetOrganisationConnector, huma.Operation{
		OperationID: "getOrganisationConnector", Method: http.MethodGet, Path: "/v1/organisation/connectors/{connector_id}", Tags: []string{"Connectors"},
		Summary: "Read one of the organisation's connectors", Description: "One of the organisation's installed connectors with its ETag " +
			"(FR-INT-20). Read-only; no side effects.",
	}, append([]string{connector.CodeNotInstalled, CodeUnavailable}, locks...), func(ctx context.Context, in *connectorGetInput) (*connectorReadOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		id, err := parseID("connector_id", in.ID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out connectorReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := installedOne(ctx, u, false, id)
			if err != nil {
				return err
			}
			secrets, err := u.Connectors().OrganisationSecrets(ctx)
			out.Body, out.ETag = a.connectorOf(c, secrets), etag(c.Version)
			return err
		})
		return &out, err
	})

	// ask records a step for a second approver (FM-1): the approval engine executes it on the final approval.
	ask := func(ctx context.Context, u storage.UnitOfWork, ch connectorChange, key, summary string, diff []ApprovalDiff) (any, error) {
		raw, err := json.Marshal(ch)
		if err != nil {
			return nil, err
		}
		var execute map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber() // an envelope holds integers, never a float (approval.Canonical)
		if err := dec.Decode(&execute); err != nil {
			return nil, err
		}
		return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectConnector, ObjectKey: key, Operation: approval.OpConnectorChange,
			Summary: summary, Execute: execute, Diff: diff})
	}
	note := " Asking needs a person signed in who passed step-up in the last 5 minutes: the answer is 202 with the approval request, which " +
		"another member who holds the approval right (an Owner) approves under FM-1, and the step is taken then. Side effects: the approval " +
		"request and its event."

	register(a, authz.InstallOrganisationConnector, pendsApproval(a, huma.Operation{
		OperationID: "installOrganisationConnector", Method: http.MethodPost, Path: "/v1/organisation/connectors", Tags: []string{"Connectors"},
		Summary: "Install a connector for the organisation",
		Description: "Asks to install a connector of the catalogue whose scope is the organisation, consenting to its manifest (FR-INT-26)." + note +
			" Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, reflect.TypeFor[Connector]()), append(append(append([]string{CodeInvalid}, asks...), writes...), ConnectorCodes...),
		func(ctx context.Context, in *installConnectorInput) (*connectorWriteOutput, error) {
			if err := a.connectorsReady(); err != nil {
				return nil, err
			}
			b, ok := a.deps.Connectors.Catalogue.Get(in.Body.Connector)
			switch {
			case !ok:
				return nil, problem(connector.CodeUnknown, in.Body.Connector+" is not a connector of this release.", connectorFixes[connector.CodeUnknown])
			case b.Manifest.Scope != connector.ScopeOrganisation:
				return nil, problem(connector.CodeScope, b.Manifest.Name+" is installed installation-wide by a Super admin.", connectorFixes[connector.CodeScope])
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				return ask(ctx, u, connectorChange{Step: storage.StepInstall, Connector: b.Manifest.ID}, b.Manifest.ID, "Install "+b.Manifest.Name,
					[]ApprovalDiff{{Field: "connector", After: b.Manifest.ID}})
			})
			if err != nil {
				return nil, err
			}
			return connectorOutput(w), nil
		})

	orgStep := func(ctx context.Context, in any, key, prefer, ifMatch, rawID string, run func(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector) (any, error)) (*connectorWriteOutput, error) {
		if err := a.connectorsReady(); err != nil {
			return nil, err
		}
		id, err := parseID("connector_id", rawID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, key, prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
			c, err := installedOne(ctx, u, false, id)
			if err != nil {
				return nil, err
			}
			if err := checkIfMatch(ifMatch, etag(c.Version)); err != nil {
				return nil, err
			}
			return run(ctx, u, c)
		})
		if err != nil {
			return nil, err
		}
		return connectorOutput(w), nil
	}
	orgCodes := append(append(append(append([]string{CodeInvalid}, asks...), writes...), versioned...), ConnectorCodes...)

	register(a, authz.ConfigureOrganisationConnector, pendsApproval(a, huma.Operation{
		OperationID: "configureOrganisationConnector", Method: http.MethodPut, Path: "/v1/organisation/connectors/{connector_id}/configuration",
		Tags: []string{"Connectors"}, Summary: "Configure one of the organisation's connectors",
		Description: "Asks to replace the connector's whole configuration, checked against its manifest's JSON Schema now (CONNECTOR-03) and " +
			"again when approved (FR-INT-26)." + note + " Needs If-Match with its ETag.",
	}, reflect.TypeFor[Connector]()), orgCodes, func(ctx context.Context, in *configureConnectorInput) (*connectorWriteOutput, error) {
		return orgStep(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, in.ID, func(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector) (any, error) {
			b, _ := a.deps.Connectors.Catalogue.Get(c.Connector)
			if _, err := connector.Plan(b.Manifest, c, storage.StepConfigure, connector.Input{Configuration: in.Body.Configuration}, nil, a.deps.Clock.Now()); err != nil {
				return nil, err
			}
			return ask(ctx, u, connectorChange{Step: storage.StepConfigure, Connector: c.Connector, Installed: c.ID.String(), Version: c.Version,
				Configuration: in.Body.Configuration}, c.ID.String(), "Configure "+b.Manifest.Name, []ApprovalDiff{{Field: "configuration", After: "changed"}})
		})
	})

	register(a, authz.SetOrganisationConnectorSecret, pendsApproval(a, huma.Operation{
		OperationID: "setOrganisationConnectorSecret", Method: http.MethodPut, Path: "/v1/organisation/connectors/{connector_id}/secrets/{secret}",
		Tags: []string{"Connectors"}, Summary: "Set a secret of one of the organisation's connectors",
		Description: "Stores the new value in Secret Manager at once, as a new version never shown again, and asks to make it current (FR-INT-20, " +
			"FR-INT-26): approved, the value it replaces stays valid for the manifest's overlap; rejected or expired, the new value is " +
			"destroyed." + note + " Needs If-Match with its ETag.",
	}, reflect.TypeFor[Connector]()), orgCodes, func(ctx context.Context, in *setConnectorSecretInput) (*connectorWriteOutput, error) {
		p, _ := authn.From(ctx)
		return orgStep(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, in.ID, func(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector) (any, error) {
			ref, err := a.organisations().PutSecret(ctx, u, p.Organisation, c, in.Secret, in.Body.Value)
			if err != nil {
				return nil, err
			}
			b, _ := a.deps.Connectors.Catalogue.Get(c.Connector)
			return ask(ctx, u, connectorChange{Step: storage.StepSecret, Connector: c.Connector, Installed: c.ID.String(), Version: c.Version,
				Slot: in.Secret, Ref: ref}, c.ID.String(), "Replace "+b.Manifest.Name+"'s "+in.Secret, []ApprovalDiff{{Field: "secret." + in.Secret, After: "replaced"}})
		})
	})

	register(a, authz.StepOrganisationConnector, pendsApproval(a, huma.Operation{
		OperationID: "stepOrganisationConnector", Method: http.MethodPost, Path: "/v1/organisation/connectors/{connector_id}/steps",
		Tags: []string{"Connectors"}, Summary: "Take a step of an organisation connector's lifecycle",
		Description: "enable, pause, resume, disable, uninstall, consent or order (FR-INT-26). Pause takes effect at once for one person with " +
			"step-up and answers 200 (the owner's decision of 25 Oct 2026); every other step is asked for." + note + " Needs If-Match with its ETag.",
	}, reflect.TypeFor[Connector]()), orgCodes, func(ctx context.Context, in *connectorStepInput) (*connectorWriteOutput, error) {
		return orgStep(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, in.ID, func(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector) (any, error) {
			b, _ := a.deps.Connectors.Catalogue.Get(c.Connector)
			secrets, err := u.Connectors().OrganisationSecrets(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := connector.Plan(b.Manifest, c, in.Body.Step, connector.Input{Position: in.Body.Position}, secrets, a.deps.Clock.Now()); err != nil {
				return nil, err
			}
			if in.Body.Step == storage.StepPause {
				p, err := a.steppedUpPerson(ctx, "Pausing a connector")
				if err != nil {
					return nil, err
				}
				r, err := a.organisations().Apply(ctx, u, c.ID, c.Version, storage.StepPause, connector.Input{})
				if err != nil {
					return nil, err
				}
				if a.deps.Outbox != nil { // the others hear of it at once (the owner's decision of 25 Oct 2026)
					link := "/settings/integrations/" + c.ID.String() + "?scope=organisation"
					if err := notify.ConnectorPaused(ctx, *a.deps.Outbox, u, p.Organisation, p.UserID, c.ID, b.Manifest.Name, link, a.deps.Clock.Now()); err != nil {
						return nil, err
					}
				}
				return a.connectorOf(r.Connector, secrets), nil
			}
			return ask(ctx, u, connectorChange{Step: in.Body.Step, Connector: c.Connector, Installed: c.ID.String(), Version: c.Version,
				Position: in.Body.Position}, c.ID.String(), stepWord(in.Body.Step)+" "+b.Manifest.Name, []ApprovalDiff{{Field: "state", Before: c.State, After: in.Body.Step}})
		})
	})

	register(a, authz.TestOrganisationConnector, huma.Operation{
		OperationID: "testOrganisationConnector", Method: http.MethodPost, Path: "/v1/organisation/connectors/{connector_id}/test",
		Tags: []string{"Connectors"}, Summary: "Test one of the organisation's connectors",
		Description: "Runs the connector's test action through the egress guard (SEC-36) and records the result: pass or fail with Fyniq's " +
			"reason, never what the provider answered. No second person is needed. Side effects: the result and its event. Needs If-Match " +
			"with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and sends nothing.",
	}, append(append(append([]string{CodeNoEmailAddress, CodeSessionRequired, CodeUnavailable}, locks...), versioned...), ConnectorCodes...),
		func(ctx context.Context, in *testConnectorInput) (*connectorTestOutput, error) {
			to, err := a.testAddress(ctx)
			if err != nil {
				return nil, err
			}
			return testOutput(orgStep(ctx, in, in.IdempotencyKey, in.Prefer, in.IfMatch, in.ID, func(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector) (any, error) {
				secrets, err := u.Connectors().OrganisationSecrets(ctx)
				if err != nil {
					return nil, err
				}
				if storage.IsPreview(ctx) { // a preview checks the request and sends nothing
					return a.connectorOf(c, secrets), nil
				}
				result := a.deps.Connectors.Runtime.TestEmail(ctx, c, secrets, to, a.deps.Mail.From)
				r, err := a.organisations().Apply(ctx, u, c.ID, c.Version, storage.StepTest, connector.Input{Test: &result})
				if err != nil {
					return nil, err
				}
				return a.connectorOf(r.Connector, secrets), nil
			}))
		})
}

func stepWord(step string) string {
	if step == "" {
		return step
	}
	return strings.ToUpper(step[:1]) + step[1:]
}

// executeConnector takes an organisation connector's approved step (FR-INT-26): the change as asked, which applies only
// if the connector is still at the version it was asked of.
func (a *API) executeConnector(ctx context.Context, u storage.UnitOfWork, s stored) error {
	raw, err := json.Marshal(s.Execute)
	if err != nil {
		return err
	}
	var ch connectorChange
	if err := json.Unmarshal(raw, &ch); err != nil {
		return err
	}
	if ch.Step == storage.StepInstall {
		_, err := a.organisations().Install(ctx, u, ch.Connector)
		return err
	}
	id, err := uuid.Parse(ch.Installed)
	if err != nil {
		return err
	}
	_, err = a.organisations().Apply(ctx, u, id, ch.Version, ch.Step, connector.Input{Configuration: ch.Configuration, Secret: ch.Slot,
		Ref: ch.Ref, Position: ch.Position})
	return err
}

// connectorDependencies are what an organisation connector's envelope holds besides its change: the installed
// connector's version and state, or for an install whether it is installed already.
func connectorDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	deps := map[string]string{}
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return 0, nil, err
	}
	if id, err := uuid.Parse(key); err == nil {
		for _, c := range all {
			if c.ID == id {
				deps["connector_version"], deps["state"] = fmt.Sprint(c.Version), c.State
				return c.Version, deps, nil
			}
		}
		return 0, nil, ports.ErrNotFound
	}
	deps["connector_version"], deps["state"] = "0", "not_installed"
	for _, c := range all {
		if c.Connector == key && c.State != storage.ConnectorUninstalled {
			deps["state"] = "installed"
		}
	}
	return 0, deps, nil
}

// testingConnector names the test of an installation connector, which no one is told of.
const testingConnector = "Testing a connector"

// tellOfInstallationStep tells the Super admins of a step of an installation connector (FR-ADM-07, FR-INT-26; the owner's
// decision of 25 Oct 2026), through the relay, since the installation organisation has no members: the others, of a
// change that waits for one of them, or of a pause, which takes effect without them; and a lone Super admin, of every
// step that applied at once, as a security notice, so a stolen session's change does not go unseen.
func (a *API) tellOfInstallationStep(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, before storage.InstalledConnector, r connector.Result) error {
	if r.Pending != nil {
		return a.relay(ctx, u, notify.Event{Type: "approval.requested", RecordType: "installation_change", RecordID: &r.Pending.ID,
			Link: "/settings/integrations", Title: r.Pending.Summary + ": a change to an installation connector waits for another Super admin's approval",
			Audience: notify.SuperAdmins(by)})
	}
	n, err := a.deps.Installation.CountSuperAdmins(ctx)
	if err != nil {
		return err
	}
	b, _ := a.deps.Connectors.Catalogue.Get(r.Connector.Connector)
	e := notify.Event{Type: notify.ConnectorType, RecordType: "installation_connector", RecordID: &r.Connector.ID,
		Link: "/settings/integrations/" + r.Connector.ID.String() + "?scope=installation"}
	switch {
	case n <= 1:
		e.Title = b.Manifest.Name + " changed at once, since there is one Super admin: " + stepDone(before, r.Connector) +
			". If it was not you, sign out your other devices and replace the connector's keys."
		e.Audience = notify.SuperAdmins()
	case before.State == storage.ConnectorEnabled && r.Connector.State == storage.ConnectorPaused:
		e.Title = b.Manifest.Name + " was paused by another Super admin: its work is held until it is resumed, which needs a second Super admin"
		e.Audience = notify.SuperAdmins(by)
	default:
		return nil
	}
	return a.relay(ctx, u, e)
}

// stepDone says in a few words what a step did to a connector.
func stepDone(before, after storage.InstalledConnector) string {
	switch {
	case before.ID == uuid.Nil:
		return "installed"
	case before.State != after.State:
		return after.State
	case before.Position != after.Position:
		return "moved in its order"
	default:
		return "its configuration or a key replaced"
	}
}

// tellOfDecision tells the Super admin who asked for an installation change how it was decided.
func (a *API) tellOfDecision(ctx context.Context, u storage.UnitOfWork, c storage.InstallationChange) error {
	title := c.Summary + ": approved by another Super admin, and done"
	if c.Status == "rejected" {
		title = c.Summary + ": rejected by another Super admin: " + c.Reason
	}
	return a.relay(ctx, u, notify.Event{Type: "approval.decided", RecordType: "installation_change", RecordID: &c.ID, Link: "/settings/integrations",
		Title: title, Audience: notify.Audience{Users: []uuid.UUID{c.RequestedBy}}})
}

// relay queues e in u for the people it names, from the caller's organisation (the installation's, in an installation
// write); nothing without an outbox.
func (a *API) relay(ctx context.Context, u storage.UnitOfWork, e notify.Event) error {
	if a.deps.Outbox == nil {
		return nil
	}
	if e.At.IsZero() {
		e.At = a.deps.Clock.Now()
	}
	p, _ := authn.From(ctx)
	return notify.Relay(ctx, *a.deps.Outbox, u, p.Organisation, e)
}
