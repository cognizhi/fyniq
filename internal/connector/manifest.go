// Package connector is the integration framework (PRD-001 FR-INT-20, FR-INT-25…27): every outside service is a
// connector, an adapter compiled into the release with a manifest that declares what it does, what it needs, what it
// sends out and where, and what pause and disable do to its work. The manifests' format is
// contracts/connectors/manifest.schema.json; the first-party connectors are in connectors/<id>/.
package connector

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is a capability: the kind of service a connector implements (FR-INT-25).
type Kind string

// The kinds.
const (
	SignIn          Kind = "sign-in"
	OutboundEmail   Kind = "outbound-email"
	InboundEmail    Kind = "inbound-email"
	Messaging       Kind = "messaging"
	Ticketing       Kind = "ticketing"
	PaymentProvider Kind = "payment-provider"
	BankData        Kind = "bank-data"
	EInvoicing      Kind = "e-invoicing"
	DocumentAI      Kind = "document-ai"
)

// Kinds are every kind, in the catalogue's order.
var Kinds = []Kind{SignIn, OutboundEmail, InboundEmail, Messaging, Ticketing, PaymentProvider, BankData, EInvoicing, DocumentAI}

// The scopes a connector is installed at.
const (
	ScopeInstallation = "installation" // by the Super admin, for everyone (FR-ADM-07)
	ScopeOrganisation = "organisation" // by an organisation's Owner or Administrator
)

// The triggers: who may make a capability act (FR-INT-27).
const (
	TriggerHuman  = "human-authority" // a completed human authority, such as a payment batch approved under FR-BNK-12
	TriggerSystem = "system-rule"     // a rule of the platform, such as a message the outbox holds
	TriggerNone   = "none"            // nobody: the capability only answers, as a read-only bank feed does
)

// What pause and disable do to queued work, and what happens to work in flight (FR-INT-26).
const (
	Hold      = "hold"
	Cancel    = "cancel"
	Finish    = "finish"
	Reconcile = "reconcile"
)

// DataClasses are the classes of data a connector may send out (PRD-001 AI-75's classes).
var DataClasses = []string{"know-how", "business-figures", "personal-data", "payroll", "restricted"}

// Services are the services that may read a secret.
var Services = []string{"api", "worker", "jobs"}

// Manifest is a connector's manifest.
type Manifest struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Publisher       string          `json:"publisher"`
	Version         string          `json:"version"`
	MinPlatform     string          `json:"min_platform"`
	Summary         string          `json:"summary"`
	Scope           string          `json:"scope"`
	Capabilities    []Capability    `json:"capabilities"`
	Configuration   json.RawMessage `json:"configuration"`
	Secrets         []Secret        `json:"secrets"`
	ProviderScopes  []string        `json:"provider_scopes"`
	FyniqScopes     []string        `json:"fyniq_scopes"`
	DataClasses     []string        `json:"data_classes"`
	Regions         []string        `json:"regions"`
	Destinations    []Destination   `json:"destinations"`
	Webhooks        []Webhook       `json:"webhooks"`
	HealthCheck     string          `json:"health_check"`
	TestAction      string          `json:"test_action"`
	LocalSubstitute string          `json:"local_substitute"`
	Limits          Limits          `json:"limits"`

	hash   string
	schema *jsonschema.Schema
}

// Capability is one kind of service the connector implements, and how its work behaves.
type Capability struct {
	Kind          Kind   `json:"kind"`
	Trigger       string `json:"trigger"`
	TriggerRule   string `json:"trigger_rule"`
	OnPause       string `json:"on_pause"`
	OnDisable     string `json:"on_disable"`
	InFlight      string `json:"in_flight"`
	ParserVersion string `json:"parser_version,omitempty"`
}

// Secret is a secret the connector needs: never in its configuration, never shown again once set.
type Secret struct {
	Name                 string   `json:"name"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	UsedBy               []string `json:"used_by"`
	RotationOverlapHours int      `json:"rotation_overlap_hours"`
}

// Destination is a place the connector connects to: a host (with its subdomains when it starts with "*."), or the
// configuration property that names a host the person chooses.
type Destination struct {
	Host       string `json:"host,omitempty"`
	Configured string `json:"configured,omitempty"`
	Port       int    `json:"port,omitempty"`
}

// Webhook is an endpoint the provider calls.
type Webhook struct {
	Path         string   `json:"path"`
	Events       []string `json:"events"`
	RegisteredBy string   `json:"registered_by"`
	Secret       string   `json:"secret"`
}

// Limits are the connector's rate limit and circuit breaker, per instance (FR-INT-27).
type Limits struct {
	RequestsPerMinute      int `json:"requests_per_minute"`
	BreakerFailures        int `json:"breaker_failures"`
	BreakerCooldownSeconds int `json:"breaker_cooldown_seconds"`
}

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	semver        = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	hostPattern   = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	regionPattern = regexp.MustCompile(`^([A-Z]{2}|EU|configured)$`)
	hookPath      = regexp.MustCompile(`^/api/webhooks/[a-z0-9/-]+$`)
	parserPattern = regexp.MustCompile(`^[a-z0-9.-]+@[0-9]+$`)
	scopePattern  = regexp.MustCompile(`^fyniq\.[a-z.]+$`)
)

// Parse reads and checks a manifest. It refuses an unknown field and reports every problem at once, so a release
// with a malformed manifest does not start.
func Parse(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("connector manifest: %w", err)
	}
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if !idPattern.MatchString(m.ID) {
		bad("id %q is not 2 to 40 lower-case letters, digits and hyphens", m.ID)
	}
	for field, v := range map[string]string{"name": m.Name, "publisher": m.Publisher, "summary": m.Summary,
		"health_check": m.HealthCheck, "test_action": m.TestAction, "local_substitute": m.LocalSubstitute} {
		if strings.TrimSpace(v) == "" {
			bad("%s is empty", field)
		}
	}
	if !semver.MatchString(m.Version) {
		bad("version %q is not MAJOR.MINOR.PATCH", m.Version)
	}
	if !semver.MatchString(m.MinPlatform) {
		bad("min_platform %q is not MAJOR.MINOR.PATCH", m.MinPlatform)
	}
	if m.Scope != ScopeInstallation && m.Scope != ScopeOrganisation {
		bad("scope %q is neither installation nor organisation", m.Scope)
	}
	checkCapabilities(m.Capabilities, bad)
	secrets := checkSecrets(m.Secrets, bad)
	props := checkConfiguration(&m, secrets, bad)
	for _, c := range m.DataClasses {
		if !slices.Contains(DataClasses, c) {
			bad("data class %q is not one of %s", c, strings.Join(DataClasses, ", "))
		}
	}
	if len(m.Regions) == 0 {
		bad("regions is empty: say where the provider processes what it is sent")
	}
	for _, r := range m.Regions {
		if !regionPattern.MatchString(r) {
			bad("region %q is not a country code, EU or configured", r)
		}
	}
	for _, s := range m.FyniqScopes {
		if !scopePattern.MatchString(s) {
			bad("Fyniq scope %q is not fyniq.<name>", s)
		}
	}
	checkDestinations(m.Destinations, props, bad)
	for _, w := range m.Webhooks {
		switch {
		case !hookPath.MatchString(w.Path):
			bad("webhook path %q is not under /api/webhooks/", w.Path)
		case len(w.Events) == 0:
			bad("webhook %s lists no events", w.Path)
		case w.RegisteredBy != "connector" && w.RegisteredBy != "operator":
			bad("webhook %s: registered_by %q is neither connector nor operator", w.Path, w.RegisteredBy)
		case !slices.Contains(secrets, w.Secret):
			bad("webhook %s: secret %q is not one of the connector's secrets", w.Path, w.Secret)
		}
	}
	l := m.Limits
	if l.RequestsPerMinute < 1 || l.RequestsPerMinute > 100000 || l.BreakerFailures < 1 || l.BreakerFailures > 100 ||
		l.BreakerCooldownSeconds < 1 || l.BreakerCooldownSeconds > 3600 {
		bad("limits are outside 1–100000 requests a minute, 1–100 failures and 1–3600 seconds")
	}
	if len(problems) > 0 {
		return Manifest{}, fmt.Errorf("connector manifest %s: %s", m.ID, strings.Join(problems, "; "))
	}
	var doc any
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return Manifest{}, err
	}
	canon, err := approval.Canonical(doc)
	if err != nil {
		return Manifest{}, fmt.Errorf("connector manifest %s: %w", m.ID, err)
	}
	sum := sha256.Sum256(canon)
	m.hash = hex.EncodeToString(sum[:])
	return m, nil
}

func checkCapabilities(cs []Capability, bad func(string, ...any)) {
	if len(cs) == 0 {
		bad("capabilities is empty")
	}
	var seen []Kind
	for _, c := range cs {
		if !slices.Contains(Kinds, c.Kind) {
			bad("capability %q is not a kind of FR-INT-25", c.Kind)
		}
		if slices.Contains(seen, c.Kind) {
			bad("capability %s is listed twice", c.Kind)
		}
		seen = append(seen, c.Kind)
		if c.Trigger != TriggerHuman && c.Trigger != TriggerSystem && c.Trigger != TriggerNone {
			bad("capability %s: trigger %q is not human-authority, system-rule or none", c.Kind, c.Trigger)
		}
		if strings.TrimSpace(c.TriggerRule) == "" {
			bad("capability %s: trigger_rule is empty", c.Kind)
		}
		if c.OnPause != Hold {
			bad("capability %s: on_pause must be hold, as pause holds the queue", c.Kind)
		}
		if c.OnDisable != Hold && c.OnDisable != Cancel {
			bad("capability %s: on_disable %q is neither hold nor cancel", c.Kind, c.OnDisable)
		}
		if c.InFlight != Finish && c.InFlight != Reconcile {
			bad("capability %s: in_flight %q is neither finish nor reconcile", c.Kind, c.InFlight)
		}
		if c.ParserVersion != "" && !parserPattern.MatchString(c.ParserVersion) {
			bad("capability %s: parser_version %q is not <name>@<n>", c.Kind, c.ParserVersion)
		}
	}
}

func checkSecrets(ss []Secret, bad func(string, ...any)) []string {
	var names []string
	for _, s := range ss {
		if !namePattern.MatchString(s.Name) {
			bad("secret %q is not 2 to 40 lower-case letters, digits and underscores", s.Name)
		}
		if slices.Contains(names, s.Name) {
			bad("secret %s is listed twice", s.Name)
		}
		names = append(names, s.Name)
		if strings.TrimSpace(s.Title) == "" || strings.TrimSpace(s.Description) == "" {
			bad("secret %s needs a title and a description", s.Name)
		}
		if len(s.UsedBy) == 0 {
			bad("secret %s: used_by is empty", s.Name)
		}
		for _, u := range s.UsedBy {
			if !slices.Contains(Services, u) {
				bad("secret %s: used_by %q is not api, worker or jobs", s.Name, u)
			}
		}
		if s.RotationOverlapHours < 0 || s.RotationOverlapHours > 168 {
			bad("secret %s: rotation_overlap_hours is outside 0–168", s.Name)
		}
	}
	return names
}

// checkConfiguration compiles the configuration schema, and returns its properties and their types.
func checkConfiguration(m *Manifest, secrets []string, bad func(string, ...any)) map[string]string {
	var top struct {
		Type                 string                     `json:"type"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(m.Configuration, &top); err != nil {
		bad("configuration is not a JSON Schema object: %v", err)
		return nil
	}
	if top.Type != "object" || top.AdditionalProperties == nil || *top.AdditionalProperties || top.Properties == nil {
		bad("configuration must be an object schema with properties and additionalProperties false")
	}
	props := map[string]string{}
	for name, raw := range top.Properties {
		var p struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &p)
		props[name] = p.Type
		if slices.Contains(secrets, name) {
			bad("configuration property %s is a secret: secrets are listed under secrets and never configured", name)
		}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(m.Configuration))
	if err != nil {
		bad("configuration schema: %v", err)
		return props
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	url := "connector:" + m.ID + "/configuration"
	if err := c.AddResource(url, doc); err != nil {
		bad("configuration schema: %v", err)
		return props
	}
	s, err := c.Compile(url)
	if err != nil {
		bad("configuration schema does not compile: %v", err)
		return props
	}
	m.schema = s
	return props
}

func checkDestinations(ds []Destination, props map[string]string, bad func(string, ...any)) {
	if len(ds) == 0 {
		bad("destinations is empty: every place the connector connects to is declared (SEC-36)")
	}
	for _, d := range ds {
		switch {
		case (d.Host == "") == (d.Configured == ""):
			bad("a destination names either a host or the configuration property that names one")
		case d.Host != "" && !hostPattern.MatchString(d.Host):
			bad("destination host %q is not a lower-case host name", d.Host)
		case d.Configured != "" && props[d.Configured] != "string":
			bad("destination %q is not a string property of the configuration", d.Configured)
		}
		if d.Port < 0 || d.Port > 65535 {
			bad("destination port %d is outside 1–65535", d.Port)
		}
	}
}

// Hash is the SHA-256 of the manifest's RFC 8785 form: what an installation consented to.
func (m Manifest) Hash() string { return m.hash }

// Capability returns the manifest's capability of kind k.
func (m Manifest) Capability(k Kind) (Capability, bool) {
	for _, c := range m.Capabilities {
		if c.Kind == k {
			return c, true
		}
	}
	return Capability{}, false
}

// Secret returns the manifest's secret called name.
func (m Manifest) Secret(name string) (Secret, bool) {
	for _, s := range m.Secrets {
		if s.Name == name {
			return s, true
		}
	}
	return Secret{}, false
}

// Consent is what installing m, or consenting to it, agrees to send out and reach (FR-INT-26).
func (m Manifest) Consent() storage.ConnectorConsent {
	c := storage.ConnectorConsent{DataClasses: slices.Clone(m.DataClasses), ProviderScopes: slices.Clone(m.ProviderScopes),
		FyniqScopes: slices.Clone(m.FyniqScopes)}
	for _, d := range m.Destinations {
		c.Destinations = append(c.Destinations, storage.ConsentDestination{Host: d.Host, Configured: d.Configured, Port: d.Port})
	}
	for _, w := range m.Webhooks {
		c.Webhooks = append(c.Webhooks, w.Path)
	}
	return c
}

// ErrConfiguration is a configuration the manifest's schema refuses.
var ErrConfiguration = errors.New("configuration does not match the connector's schema")

// Validate checks a configuration against the manifest's schema: an object of the properties it declares, never a
// secret.
func (m Manifest) Validate(config map[string]any) error {
	if m.schema == nil {
		return fmt.Errorf("connector %s: the manifest was not parsed", m.ID)
	}
	for _, s := range m.Secrets {
		if _, ok := config[s.Name]; ok {
			return fmt.Errorf("%w: %s is a secret, which is set on its own and never in the configuration", ErrConfiguration, s.Name)
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := m.schema.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return fmt.Errorf("%w: %s", ErrConfiguration, firstCause(ve))
		}
		return fmt.Errorf("%w: %v", ErrConfiguration, err)
	}
	return nil
}

// firstCause is the deepest first reason a validation failed, in a line a person can read.
func firstCause(ve *jsonschema.ValidationError) string {
	for len(ve.Causes) > 0 {
		ve = ve.Causes[0]
	}
	loc := "/" + strings.Join(ve.InstanceLocation, "/")
	return loc + ": " + ve.ErrorKind.LocalizedString(message.NewPrinter(language.English))
}
