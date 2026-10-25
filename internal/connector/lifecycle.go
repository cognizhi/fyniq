package connector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The refusals of the lifecycle, each naming what is wrong and how to fix it (ENG-23).
const (
	CodeUnknown       = "CONNECTOR-01" // the connector is not in this release's catalogue
	CodeScope         = "CONNECTOR-02" // it is installed at the other scope
	CodeConfiguration = "CONNECTOR-03" // the configuration does not match the connector's manifest
	CodeSecretMissing = "CONNECTOR-04" // a secret the connector needs is not set
	CodeSecretUnknown = "CONNECTOR-05" // not one of the connector's secrets
	CodeState         = "CONNECTOR-06" // the step cannot be taken from the connector's state
	CodeNotInstalled  = "CONNECTOR-07" // no such installed connector
	CodeChange        = "CONNECTOR-08" // the change is not one that can be decided or applied now
)

// Error is a refusal of the lifecycle.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func (e *Error) Unwrap() error { return e.Err }

func refuse(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ChangeTTL is how long an installation change waits for the other Super admin, as approvals do (FR-APR-10).
const ChangeTTL = approval.TTL

// ChangeSchema names the installation change envelope's format.
const ChangeSchema = "fyniq.installation-change/1"

// Input is what a step needs besides the connector: a configuration (configure), a secret's name and the reference of
// its new version (secret), a position (order), or a connection test's result (test).
type Input struct {
	Configuration map[string]any
	Secret        string
	Ref           ports.SecretRef
	Position      int
	Test          *storage.ConnectorTest
}

// Plan is what step leaves connector c as, running manifest m (the release's), with its secret versions: the change to
// store, or a refusal in plain words. The database checks the same lifecycle (connector_guard); Plan says why first.
func Plan(m Manifest, c storage.InstalledConnector, step string, in Input, secrets []storage.ConnectorSecret, at time.Time) (storage.ConnectorChange, error) {
	out := storage.ConnectorChange{Step: step, State: c.State, Configuration: c.Configuration, Position: c.Position, Test: c.Test,
		ManifestVersion: c.ManifestVersion, ManifestHash: c.ManifestHash, SecretNames: c.SecretNames, Consent: c.Consent, At: at}
	if c.State == storage.ConnectorUninstalled {
		return out, refuse(CodeState, "%s is uninstalled: install it again to use it", m.Name)
	}
	from := func(states ...string) error {
		if !slices.Contains(states, c.State) {
			return refuse(CodeState, "%s is %s, so it cannot be %s now", m.Name, c.State, stepEvent(step))
		}
		return nil
	}
	switch step {
	case storage.StepConfigure:
		if err := m.Validate(in.Configuration); err != nil {
			return out, &Error{Code: CodeConfiguration, Message: err.Error(), Err: err}
		}
		out.Configuration = in.Configuration
		if slices.Contains([]string{storage.ConnectorInstalled, storage.ConnectorTested, storage.ConnectorDisabled}, c.State) {
			out.State, out.Test = storage.ConnectorConfigured, nil // a changed configuration is tested again before it is enabled
		}
	case storage.StepTest:
		if in.Test == nil {
			return out, refuse(CodeState, "a test needs its result")
		}
		if err := from(storage.ConnectorConfigured, storage.ConnectorTested, storage.ConnectorEnabled, storage.ConnectorPaused, storage.ConnectorDisabled); err != nil {
			return out, err
		}
		out.Test = in.Test
		if in.Test.Passed && slices.Contains([]string{storage.ConnectorConfigured, storage.ConnectorDisabled}, c.State) {
			out.State = storage.ConnectorTested
		}
	case storage.StepEnable:
		if err := from(storage.ConnectorTested, storage.ConnectorDisabled); err != nil {
			return out, err
		}
		if c.State == storage.ConnectorDisabled && (c.Test == nil || !c.Test.Passed) {
			return out, refuse(CodeState, "%s's last test did not pass: test it again before enabling it", m.Name)
		}
		for _, s := range m.Secrets {
			if !current(secrets, c.ID, s.Name) {
				return out, refuse(CodeSecretMissing, "%s needs its %s: set it before enabling the connector", m.Name, s.Title)
			}
		}
		out.State = storage.ConnectorEnabled
	case storage.StepPause:
		if err := from(storage.ConnectorEnabled); err != nil {
			return out, err
		}
		out.State = storage.ConnectorPaused
	case storage.StepResume:
		if err := from(storage.ConnectorPaused); err != nil {
			return out, err
		}
		out.State = storage.ConnectorEnabled
	case storage.StepDisable:
		if err := from(storage.ConnectorEnabled, storage.ConnectorPaused); err != nil {
			return out, err
		}
		out.State = storage.ConnectorDisabled
	case storage.StepUninstall:
		out.State = storage.ConnectorUninstalled
	case storage.StepOrder:
		if in.Position < 1 || in.Position > 1000 {
			return out, refuse(CodeState, "a position is from 1 (the primary) to 1000")
		}
		out.Position = in.Position
	case storage.StepConsent:
		if m.Hash() == c.ManifestHash {
			return out, refuse(CodeState, "%s already runs the release's manifest: there is nothing to consent to", m.Name)
		}
		out.ManifestVersion, out.ManifestHash = m.Version, m.Hash()
		out.SecretNames, out.Consent = names(m.Secrets), m.Consent()
	default:
		return out, refuse(CodeState, "%q is not a step of the lifecycle", step)
	}
	return out, nil
}

func stepEvent(step string) string {
	switch step {
	case storage.StepConfigure:
		return "configured"
	case storage.StepTest:
		return "tested"
	case storage.StepEnable:
		return "enabled"
	case storage.StepPause:
		return "paused"
	case storage.StepResume:
		return "resumed"
	case storage.StepDisable:
		return "disabled"
	case storage.StepUninstall:
		return "uninstalled"
	case storage.StepOrder:
		return "reordered"
	case storage.StepConsent:
		return "upgraded"
	}
	return step
}

func names(ss []Secret) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

// current reports whether connector id has a current version of secret name.
func current(secrets []storage.ConnectorSecret, id uuid.UUID, name string) bool {
	return slices.ContainsFunc(secrets, func(s storage.ConnectorSecret) bool {
		return s.ConnectorID == id && s.Name == name && s.ValidUntil == nil && s.DestroyedAt == nil
	})
}

// Canceller cancels the queued work of an installed connector's capability, when its manifest says disable cancels it
// (FR-INT-26). A capability whose work is held needs none: the runtime skips a disabled connector.
type Canceller func(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector) error

// cancel runs the cancellers of the capabilities m says disable cancels.
func cancel(ctx context.Context, u storage.UnitOfWork, m Manifest, c storage.InstalledConnector, cancellers map[Kind]Canceller) error {
	for _, cap := range m.Capabilities {
		if cap.OnDisable != Cancel {
			continue
		}
		f, ok := cancellers[cap.Kind]
		if !ok {
			return fmt.Errorf("connector %s: its manifest cancels %s work on disable, but nothing cancels it", m.ID, cap.Kind)
		}
		if err := f(ctx, u, c); err != nil {
			return err
		}
	}
	return nil
}

// ------------------------------------------------------------------ the installation's

// SuperAdmins counts the active Super admins, which decides whether a step needs the other one (FR-ADM-07).
type SuperAdmins interface {
	CountSuperAdmins(ctx context.Context) (int, error)
}

// Installations runs the lifecycle of the installation's connectors for the Super admins (FR-INT-20, FR-INT-26): with
// one Super admin a step applies at once; with two, every step but pause and test becomes a change the other one
// approves (the owner's decision of 25 Oct 2026 for pause). The unit of work is the installation organisation's, whose
// trail records every step.
type Installations struct {
	Catalogue  *Catalogue
	Secrets    ports.SecretStore
	Names      map[string]string // FYNIQ_CONNECTOR_SECRETS: <connector>.<secret> → secret name
	Admins     SuperAdmins
	Clock      ports.Clock
	Cancellers map[Kind]Canceller
}

// Result is a step's outcome: the connector as it now is, or the change waiting for the other Super admin. Webhooks are
// the ones a person must remove from the provider's dashboard after an uninstall.
type Result struct {
	Connector storage.InstalledConnector
	Pending   *storage.InstallationChange
	Webhooks  []Webhook
}

// SecretName is the Secret Manager secret that holds secret name of installation connector id.
func (s Installations) SecretName(id, name string) string {
	if n, ok := s.Names[id+"."+name]; ok {
		return n
	}
	return "connector-" + id + "-" + name
}

// change is what an installation change carries: everything its apply needs, and the version of the connector it was
// asked of, so a change asked of an older version never applies.
type change struct {
	Step          string                 `json:"step"`
	Connector     string                 `json:"connector"`
	Installed     *uuid.UUID             `json:"installed,omitempty"`
	Version       int                    `json:"version,omitempty"`
	Configuration map[string]any         `json:"configuration,omitempty"`
	Slot          string                 `json:"slot,omitempty"` // a secret change: the name of the secret it sets
	Ref           ports.SecretRef        `json:"ref,omitempty"`
	Position      int                    `json:"position,omitempty"`
	Test          *storage.ConnectorTest `json:"test,omitempty"`     // a test, which never waits for a second person
	Manifest      string                 `json:"manifest,omitempty"` // consent: the version consented to
	ManifestHash  string                 `json:"manifest_hash,omitempty"`
}

func (s Installations) manifest(id string) (Binding, error) {
	b, ok := s.Catalogue.Get(id)
	if !ok {
		return Binding{}, refuse(CodeUnknown, "%s is not a connector of this release", id)
	}
	if b.Manifest.Scope != ScopeInstallation {
		return Binding{}, refuse(CodeScope, "%s is installed by an organisation, not installation-wide", b.Manifest.Name)
	}
	return b, nil
}

func (s Installations) dual(ctx context.Context, step string) (bool, error) {
	if step == storage.StepPause || step == storage.StepTest {
		return false, nil
	}
	n, err := s.Admins.CountSuperAdmins(ctx)
	return n >= 2, err
}

func installed(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.InstalledConnector, []storage.ConnectorSecret, error) {
	all, err := u.Connectors().Installation(ctx)
	if err != nil {
		return storage.InstalledConnector{}, nil, err
	}
	i := slices.IndexFunc(all, func(c storage.InstalledConnector) bool { return c.ID == id })
	if i < 0 {
		return storage.InstalledConnector{}, nil, refuse(CodeNotInstalled, "no installed connector %s", id)
	}
	secrets, err := u.Connectors().InstallationSecrets(ctx)
	return all[i], secrets, err
}

// Install installs connector id, at the end of the order, or asks the other Super admin.
func (s Installations) Install(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, id string) (Result, error) {
	b, err := s.manifest(id)
	if err != nil {
		return Result{}, err
	}
	all, err := u.Connectors().Installation(ctx)
	if err != nil {
		return Result{}, err
	}
	position := 1
	for _, c := range all {
		if c.State != storage.ConnectorUninstalled {
			position = max(position, c.Position+1)
		}
	}
	ch := change{Step: storage.StepInstall, Connector: id, Position: position}
	if dual, err := s.dual(ctx, storage.StepInstall); err != nil || dual {
		if err != nil {
			return Result{}, err
		}
		return s.request(ctx, u, by, mfaAt, ch, "Install "+b.Manifest.Name)
	}
	return s.apply(ctx, u, by, uuid.Nil, ch)
}

// Step takes step on installed connector id at version, or asks the other Super admin. A new secret's value goes to
// the secret store at once; the change carries only its reference, and a refused change's version is destroyed.
func (s Installations) Step(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, id uuid.UUID, version int, step string,
	in Input, value string) (Result, error) {
	c, secrets, err := installed(ctx, u, id)
	if err != nil {
		return Result{}, err
	}
	b, err := s.manifest(c.Connector)
	if err != nil {
		return Result{}, err
	}
	if c.Version != version {
		return Result{}, &storage.Error{Code: storage.CodeVersion, Message: "the connector changed since it was read"}
	}
	ch := change{Step: step, Connector: c.Connector, Installed: &c.ID, Version: c.Version, Configuration: in.Configuration, Position: in.Position,
		Test: in.Test}
	if step == storage.StepSecret {
		sec, ok := b.Manifest.Secret(in.Secret)
		if !ok {
			return Result{}, refuse(CodeSecretUnknown, "%s has no secret %q", b.Manifest.Name, in.Secret)
		}
		if c.State == storage.ConnectorUninstalled {
			return Result{}, refuse(CodeState, "%s is uninstalled: install it again to use it", b.Manifest.Name)
		}
		if value == "" {
			return Result{}, refuse(CodeSecretMissing, "%s's %s is empty: paste its value", b.Manifest.Name, sec.Title)
		}
		ref, err := s.Secrets.Put(ctx, s.SecretName(c.Connector, sec.Name), value)
		if err != nil {
			return Result{}, err
		}
		u.AfterRollback(func(ctx context.Context) { _ = s.Secrets.Destroy(ctx, ref) })
		ch.Slot, ch.Ref = sec.Name, ref
	} else {
		if step == storage.StepConsent {
			ch.Manifest, ch.ManifestHash = b.Manifest.Version, b.Manifest.Hash()
		}
		if _, err := Plan(b.Manifest, c, step, in, secrets, s.Clock.Now()); err != nil {
			return Result{}, err
		}
	}
	dual, err := s.dual(ctx, step)
	if err != nil {
		return Result{}, err
	}
	if dual {
		return s.request(ctx, u, by, mfaAt, ch, fmt.Sprintf("%s %s", stepVerb(step), b.Manifest.Name))
	}
	return s.apply(ctx, u, by, uuid.Nil, ch)
}

func stepVerb(step string) string {
	switch step {
	case storage.StepSecret:
		return "Replace a secret of"
	case storage.StepConsent:
		return "Upgrade"
	case storage.StepOrder:
		return "Reorder"
	}
	if len(step) == 0 {
		return step
	}
	return string(step[0]-'a'+'A') + step[1:]
}

// request records ch as a change for the other Super admin, with the RFC 8785 envelope they sign.
func (s Installations) request(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, ch change, summary string) (Result, error) {
	now := s.Clock.Now()
	body, err := json.Marshal(ch)
	if err != nil {
		return Result{}, err
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // the canonical form takes integers, never a float
	if err := dec.Decode(&doc); err != nil {
		return Result{}, err
	}
	envelope, err := approval.Canonical(map[string]any{"schema": ChangeSchema, "change": doc, "requested_by": by.String(),
		"requested_at": now.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(envelope)
	p, err := u.Connectors().RequestInstallationChange(ctx, storage.NewInstallationChange{ConnectorID: ch.Installed, Connector: ch.Connector,
		Step: ch.Step, Summary: summary, Change: body, Envelope: string(envelope), EnvelopeHash: sum[:], RequestedBy: by, RequesterMFAAt: mfaAt,
		At: now, ExpiresAt: now.Add(ChangeTTL)})
	if err != nil {
		return Result{}, err
	}
	out := Result{Pending: &p}
	if ch.Installed != nil {
		out.Connector, _, err = installed(ctx, u, *ch.Installed)
	}
	return out, err
}

// apply applies ch, as approved by changeID (uuid.Nil: none needed).
func (s Installations) apply(ctx context.Context, u storage.UnitOfWork, by, changeID uuid.UUID, ch change) (Result, error) {
	b, err := s.manifest(ch.Connector)
	if err != nil {
		return Result{}, err
	}
	m, now := b.Manifest, s.Clock.Now()
	if ch.Step == storage.StepInstall {
		c, err := u.Connectors().InstallInstallation(ctx, by, changeID, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version,
			ManifestHash: m.Hash(), SecretNames: names(m.Secrets), Consent: m.Consent(), Position: ch.Position, At: now})
		return Result{Connector: c}, err
	}
	if ch.Installed == nil {
		return Result{}, refuse(CodeChange, "the change names no installed connector")
	}
	c, secrets, err := installed(ctx, u, *ch.Installed)
	if err != nil {
		return Result{}, err
	}
	if c.Version != ch.Version {
		return Result{}, refuse(CodeChange, "%s changed since the change was asked for: ask again", m.Name)
	}
	if ch.Step == storage.StepSecret {
		sec, ok := m.Secret(ch.Slot)
		if !ok {
			return Result{}, refuse(CodeSecretUnknown, "%s has no secret %q", m.Name, ch.Slot)
		}
		until := now.Add(time.Duration(sec.RotationOverlapHours) * time.Hour)
		if err := u.Connectors().SetInstallationSecret(ctx, by, changeID, c.ID, ch.Slot, ch.Ref, until, now); err != nil {
			return Result{}, err
		}
		c, _, err = installed(ctx, u, c.ID)
		return Result{Connector: c}, err
	}
	if ch.Step == storage.StepConsent && (ch.ManifestHash != m.Hash()) {
		return Result{}, refuse(CodeChange, "the release's manifest of %s changed since the upgrade was asked for: ask again", m.Name)
	}
	next, err := Plan(m, c, ch.Step, Input{Configuration: ch.Configuration, Position: ch.Position, Test: ch.Test}, secrets, now)
	if err != nil {
		return Result{}, err
	}
	if ch.Step == storage.StepDisable {
		if err := cancel(ctx, u, m, c, s.Cancellers); err != nil {
			return Result{}, err
		}
	}
	c, err = u.Connectors().ChangeInstallation(ctx, by, changeID, c.ID, c.Version, next)
	out := Result{Connector: c}
	if ch.Step == storage.StepUninstall {
		for _, w := range m.Webhooks {
			if w.RegisteredBy == "operator" {
				out.Webhooks = append(out.Webhooks, w)
			}
		}
	}
	return out, err
}

// Pause pauses installed connector id at once (one person with step-up, the owner's decision of 25 Oct 2026).
func (s Installations) Pause(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, id uuid.UUID, version int) (Result, error) {
	return s.Step(ctx, u, by, time.Time{}, id, version, storage.StepPause, Input{}, "")
}

// Test records a connection test's result on installed connector id; no second person is needed.
func (s Installations) Test(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, id uuid.UUID, version int, t storage.ConnectorTest) (Result, error) {
	return s.Step(ctx, u, by, time.Time{}, id, version, storage.StepTest, Input{Test: &t}, "")
}

// Approve approves pending change id, whose envelope's hash the approver signs, and applies it in the same unit of work:
// both happen, or neither.
func (s Installations) Approve(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, id uuid.UUID, hash []byte) (Result, error) {
	p, ch, err := s.pending(ctx, u, id)
	if err != nil {
		return Result{}, err
	}
	if p.Status != "pending" {
		return Result{}, refuse(CodeChange, "the change is %s and cannot be approved", p.Status)
	}
	if ch.Installed != nil {
		c, _, err := installed(ctx, u, *ch.Installed)
		if err != nil {
			return Result{}, err
		}
		if c.Version != ch.Version {
			return Result{}, refuse(CodeChange, "%s changed since the change was asked for: reject it and ask again", ch.Connector)
		}
	}
	if _, err := u.Connectors().DecideInstallationChange(ctx, by, id, hash, true, mfaAt, "", s.Clock.Now()); err != nil {
		return Result{}, err
	}
	return s.apply(ctx, u, by, id, ch)
}

// Reject rejects pending change id; a new secret version it carried is destroyed once the rejection commits.
func (s Installations) Reject(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, id uuid.UUID, hash []byte, reason string) (storage.InstallationChange, error) {
	_, ch, err := s.pending(ctx, u, id)
	if err != nil {
		return storage.InstallationChange{}, err
	}
	out, err := u.Connectors().DecideInstallationChange(ctx, by, id, hash, false, mfaAt, reason, s.Clock.Now())
	if err == nil && ch.Ref != "" {
		ref := ch.Ref
		u.AfterCommit(func(ctx context.Context) { _ = s.Secrets.Destroy(ctx, ref) })
	}
	return out, err
}

func (s Installations) pending(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.InstallationChange, change, error) {
	all, err := u.Connectors().InstallationChanges(ctx)
	if err != nil {
		return storage.InstallationChange{}, change{}, err
	}
	i := slices.IndexFunc(all, func(c storage.InstallationChange) bool { return c.ID == id })
	if i < 0 {
		return storage.InstallationChange{}, change{}, refuse(CodeChange, "no change %s", id)
	}
	var ch change
	if err := json.Unmarshal(all[i].Change, &ch); err != nil {
		return storage.InstallationChange{}, change{}, err
	}
	return all[i], ch, nil
}

// ------------------------------------------------------------------ an organisation's

// Organisations runs the lifecycle of an organisation's connectors. Every step but pause and test goes through the
// approval engine (object connector, FM-1) before the API calls Apply; Apply itself takes the step.
type Organisations struct {
	Catalogue  *Catalogue
	Secrets    ports.SecretStore
	Clock      ports.Clock
	Cancellers map[Kind]Canceller
}

func (s Organisations) manifest(id string) (Binding, error) {
	b, ok := s.Catalogue.Get(id)
	if !ok {
		return Binding{}, refuse(CodeUnknown, "%s is not a connector of this release", id)
	}
	if b.Manifest.Scope != ScopeOrganisation {
		return Binding{}, refuse(CodeScope, "%s is installed installation-wide by the Super admin, not by an organisation", b.Manifest.Name)
	}
	return b, nil
}

// SecretName is the Secret Manager secret that holds secret name of organisation org's connector id.
func (s Organisations) SecretName(org uuid.UUID, id, name string) string {
	return "org-" + org.String() + "-connector-" + id + "-" + name
}

// PutSecret stores a new version of secret name of installed connector c of organisation org, before the change that
// sets it is approved, and returns its reference; the version is destroyed if this unit of work rolls back.
func (s Organisations) PutSecret(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, c storage.InstalledConnector, name, value string) (ports.SecretRef, error) {
	b, err := s.manifest(c.Connector)
	if err != nil {
		return "", err
	}
	sec, ok := b.Manifest.Secret(name)
	if !ok {
		return "", refuse(CodeSecretUnknown, "%s has no secret %q", b.Manifest.Name, name)
	}
	if value == "" {
		return "", refuse(CodeSecretMissing, "%s's %s is empty: paste its value", b.Manifest.Name, sec.Title)
	}
	ref, err := s.Secrets.Put(ctx, s.SecretName(org, c.Connector, name), value)
	if err != nil {
		return "", err
	}
	u.AfterRollback(func(ctx context.Context) { _ = s.Secrets.Destroy(ctx, ref) })
	return ref, nil
}

// Install installs connector id for the unit of work's organisation, at the end of its order.
func (s Organisations) Install(ctx context.Context, u storage.UnitOfWork, id string) (storage.InstalledConnector, error) {
	b, err := s.manifest(id)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	position := 1
	for _, c := range all {
		if c.State != storage.ConnectorUninstalled {
			position = max(position, c.Position+1)
		}
	}
	m := b.Manifest
	return u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
		SecretNames: names(m.Secrets), Consent: m.Consent(), Position: position, At: s.Clock.Now()})
}

// Apply takes step on the organisation's installed connector id at version; for a secret, in carries its name and the
// reference PutSecret returned.
func (s Organisations) Apply(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version int, step string, in Input) (Result, error) {
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return Result{}, err
	}
	i := slices.IndexFunc(all, func(c storage.InstalledConnector) bool { return c.ID == id })
	if i < 0 {
		return Result{}, refuse(CodeNotInstalled, "no installed connector %s", id)
	}
	c := all[i]
	b, err := s.manifest(c.Connector)
	if err != nil {
		return Result{}, err
	}
	m, now := b.Manifest, s.Clock.Now()
	if c.Version != version {
		return Result{}, &storage.Error{Code: storage.CodeVersion, Message: "the connector changed since it was read"}
	}
	if step == storage.StepSecret {
		sec, ok := m.Secret(in.Secret)
		if !ok || in.Ref == "" {
			return Result{}, refuse(CodeSecretUnknown, "%s has no secret %q", m.Name, in.Secret)
		}
		until := now.Add(time.Duration(sec.RotationOverlapHours) * time.Hour)
		if err := u.Connectors().SetOrganisationSecret(ctx, c.ID, in.Secret, in.Ref, until, now); err != nil {
			return Result{}, err
		}
		return Result{Connector: c}, nil
	}
	secrets, err := u.Connectors().OrganisationSecrets(ctx)
	if err != nil {
		return Result{}, err
	}
	next, err := Plan(m, c, step, in, secrets, now)
	if err != nil {
		return Result{}, err
	}
	if step == storage.StepDisable {
		if err := cancel(ctx, u, m, c, s.Cancellers); err != nil {
			return Result{}, err
		}
	}
	c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, next)
	out := Result{Connector: c}
	if step == storage.StepUninstall {
		for _, w := range m.Webhooks {
			if w.RegisteredBy == "operator" {
				out.Webhooks = append(out.Webhooks, w)
			}
		}
	}
	return out, err
}

// ------------------------------------------------------------------ rotation and clean-up

// Cleanup destroys the secret versions whose time is over: a replaced version once its overlap has ended, every version
// of an uninstalled connector, and the new version a rejected or expired installation change carried; and expires the
// installation changes nobody decided in time (FR-INT-26). Secret Manager destroys first, then the database records it,
// so a failure in between is retried on the next run; destroying is idempotent.
type Cleanup struct {
	Store        storage.Store
	Installation interface {
		InstallationOrganisation(ctx context.Context) (uuid.UUID, error)
	}
	Secrets ports.SecretStore
	Clock   ports.Clock
}

// CleanupJob is the platform job that runs Cleanup.
const CleanupJob = "connectors.cleanup"

// Run runs the clean-up once, and returns how many versions it destroyed.
func (c Cleanup) Run(ctx context.Context) (int, error) {
	now := c.Clock.Now()
	n := 0
	org, err := c.Installation.InstallationOrganisation(ctx)
	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return 0, err
	default:
		var due []ports.SecretRef
		if err := c.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Connectors().ExpireInstallationChanges(ctx, now); err != nil {
				return err
			}
			all, err := u.Connectors().Installation(ctx)
			if err != nil {
				return err
			}
			secrets, err := u.Connectors().InstallationSecrets(ctx)
			if err != nil {
				return err
			}
			due = overdue(all, secrets, now)
			changes, err := u.Connectors().InstallationChanges(ctx)
			if err != nil {
				return err
			}
			for _, p := range changes {
				var ch change
				ended := p.ExpiresAt
				if p.DecidedAt != nil {
					ended = *p.DecidedAt
				}
				if (p.Status == "rejected" || p.Status == "expired") && now.Sub(ended) < refusedFor && json.Unmarshal(p.Change, &ch) == nil && ch.Ref != "" {
					if err := c.Secrets.Destroy(ctx, ch.Ref); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return n, err
		}
		for _, ref := range due {
			if err := c.Secrets.Destroy(ctx, ref); err != nil {
				return n, err
			}
			if err := c.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Connectors().DestroyedInstallationSecret(ctx, ref, now)
			}); err != nil {
				return n, err
			}
			n++
		}
	}
	orgs, err := c.Store.Organisations(ctx)
	if err != nil {
		return n, err
	}
	for _, o := range orgs {
		var due, refused []ports.SecretRef
		if err := c.Store.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Connectors().Organisation(ctx)
			if err != nil {
				return err
			}
			secrets, err := u.Connectors().OrganisationSecrets(ctx)
			if err != nil {
				return err
			}
			due = overdue(all, secrets, now)
			requests, err := u.Approvals().List(ctx, "")
			refused = refusedRefs(requests, now)
			return err
		}); err != nil {
			return n, err
		}
		for _, ref := range refused { // a new key a refused or withdrawn request carried, never recorded
			if err := c.Secrets.Destroy(ctx, ref); err != nil {
				return n, err
			}
		}
		for _, ref := range due {
			if err := c.Secrets.Destroy(ctx, ref); err != nil {
				return n, err
			}
			if err := c.Store.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Connectors().DestroyedOrganisationSecret(ctx, ref, now)
			}); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// refusedFor is how long after a change was refused, withdrawn or expired the clean-up destroys the new key it carried:
// long enough for a failed run to be retried, short enough not to call Secret Manager about it every hour for good.
const refusedFor = 8 * 24 * time.Hour

// refusedRefs are the new keys of an organisation's connector approval requests that ended without applying: rejected,
// withdrawn (void) or expired, recently.
func refusedRefs(requests []storage.ApprovalRequest, now time.Time) []ports.SecretRef {
	var out []ports.SecretRef
	for _, r := range requests {
		if r.ObjectType != "connector" {
			continue
		}
		ended := r.ExpiresAt
		if r.DecidedAt != nil {
			ended = *r.DecidedAt
		}
		over := r.Status == "rejected" || r.Status == "void" || r.Status == "expired" || (r.Status == "pending" && !r.ExpiresAt.After(now))
		if !over || now.Sub(ended) >= refusedFor {
			continue
		}
		var stored struct {
			Execute struct {
				Ref ports.SecretRef `json:"ref"`
			} `json:"execute"`
		}
		if json.Unmarshal(r.Change, &stored) == nil && stored.Execute.Ref != "" {
			out = append(out, stored.Execute.Ref)
		}
	}
	return out
}

// overdue are the secret versions to destroy at now: replaced and past their overlap, or of an uninstalled connector.
func overdue(all []storage.InstalledConnector, secrets []storage.ConnectorSecret, now time.Time) []ports.SecretRef {
	gone := map[uuid.UUID]bool{}
	for _, c := range all {
		gone[c.ID] = c.State == storage.ConnectorUninstalled
	}
	var out []ports.SecretRef
	for _, s := range secrets {
		if s.DestroyedAt == nil && ((s.ValidUntil != nil && !s.ValidUntil.After(now)) || gone[s.ConnectorID]) {
			out = append(out, s.Ref)
		}
	}
	return out
}
