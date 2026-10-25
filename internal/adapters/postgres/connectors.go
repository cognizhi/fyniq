package postgres

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Connectors() storage.Connectors { return connectors{u} }

type connectors struct{ u *unitOfWork }

// stepEvents name each step's audit event: <object>.<event>.
var stepEvents = map[string]string{
	storage.StepInstall: "installed", storage.StepConfigure: "configured", storage.StepSecret: "secret_replaced",
	storage.StepTest: "tested", storage.StepEnable: "enabled", storage.StepPause: "paused", storage.StepResume: "resumed",
	storage.StepDisable: "disabled", storage.StepUninstall: "uninstalled", storage.StepOrder: "reordered",
	storage.StepConsent: "consented",
}

// small converts a connector's position (1–1000, the table's check) or version for the database; one outside int32 is
// sent as 0, which every check refuses, rather than wrapped into another number.
func small(n int) int32 {
	if n < 0 || n > math.MaxInt32 {
		return 0
	}
	return int32(n)
}

func ptrChange(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func configurationJSON(c map[string]any) ([]byte, error) {
	if c == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(c)
}

func consentJSON(c storage.ConnectorConsent) ([]byte, error) { return json.Marshal(c) }

func toConsent(raw []byte) storage.ConnectorConsent {
	var c storage.ConnectorConsent
	_ = json.Unmarshal(raw, &c)
	return c
}

func toConfiguration(raw []byte) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func secretNames(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}

func toTest(passed *bool, reason *string, at *time.Time) *storage.ConnectorTest {
	if passed == nil || at == nil {
		return nil
	}
	t := &storage.ConnectorTest{Passed: *passed, At: *at}
	if reason != nil {
		t.Reason = *reason
	}
	return t
}

func testArgs(t *storage.ConnectorTest) (*bool, *string, *time.Time) {
	if t == nil {
		return nil, nil, nil
	}
	return &t.Passed, opt(t.Reason), &t.At
}

func toInstallationConnector(r sqlcdb.FyniqInstallationConnector) storage.InstalledConnector {
	return storage.InstalledConnector{ID: r.ID, Installation: true, Connector: r.Connector, ManifestVersion: r.ManifestVersion,
		ManifestHash: r.ManifestHash, SecretNames: secretNames(r.SecretNames), Consent: toConsent(r.Consent), State: r.State,
		Configuration: toConfiguration(r.Configuration),
		Position:      int(r.Position), Test: toTest(r.TestPassed, r.TestReason, r.TestedAt), InstalledBy: r.InstalledBy, InstalledAt: r.InstalledAt,
		ChangedBy: r.ChangedBy, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func toOrganisationConnector(r sqlcdb.OrganisationConnectorsRow) storage.InstalledConnector {
	c := toInstallationConnector(sqlcdb.FyniqInstallationConnector{ID: r.ID, Connector: r.Connector, ManifestVersion: r.ManifestVersion,
		ManifestHash: r.ManifestHash, SecretNames: r.SecretNames, Consent: r.Consent, State: r.State, Configuration: r.Configuration, Position: r.Position,
		TestPassed: r.TestPassed, TestReason: r.TestReason, TestedAt: r.TestedAt, InstalledBy: r.InstalledBy, InstalledAt: r.InstalledAt,
		ChangedBy: r.ChangedBy, ChangedAt: r.ChangedAt, Version: r.Version})
	c.Installation = false
	return c
}

func toConnectorSecret(r sqlcdb.FyniqInstallationConnectorSecret) storage.ConnectorSecret {
	return storage.ConnectorSecret{ID: r.ID, ConnectorID: r.ConnectorID, Name: r.Name, Ref: ports.SecretRef(r.Ref), SetBy: r.SetBy,
		SetAt: r.SetAt, ValidUntil: r.ValidUntil, DestroyedAt: r.DestroyedAt}
}

func toInstallationChange(r sqlcdb.FyniqInstallationChange) storage.InstallationChange {
	c := storage.InstallationChange{ID: r.ID, ConnectorID: r.ConnectorID, Connector: r.Connector, Step: r.Step, Summary: r.Summary,
		Change: r.Change, Envelope: r.Envelope, EnvelopeHash: r.EnvelopeHash, RequestedBy: r.RequestedBy, RequestedAt: r.RequestedAt,
		RequesterMFAAt: r.RequesterMfaAt, ExpiresAt: r.ExpiresAt, Status: r.Status, DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt,
		DeciderMFAAt: r.DeciderMfaAt, AppliedAt: r.AppliedAt}
	if r.Reason != nil {
		c.Reason = *r.Reason
	}
	return c
}

// connectorChanges are an audit event's before and after values of a step: never a secret value, which the row cannot
// hold.
func connectorChanges(before, after storage.InstalledConnector) map[string][2]any {
	out := map[string][2]any{}
	add := func(k string, b, a any) {
		bj, _ := json.Marshal(b)
		aj, _ := json.Marshal(a)
		if string(bj) != string(aj) {
			out[k] = [2]any{b, a}
		}
	}
	add("state", nilIfEmpty(before.State), after.State)
	add("configuration", before.Configuration, after.Configuration)
	add("position", before.Position, after.Position)
	add("manifest_version", nilIfEmpty(before.ManifestVersion), after.ManifestVersion)
	add("manifest_hash", nilIfEmpty(before.ManifestHash), after.ManifestHash)
	add("consent", before.Consent, after.Consent)
	var bt, at any
	if before.Test != nil {
		bt = map[string]any{"passed": before.Test.Passed, "reason": before.Test.Reason}
	}
	if after.Test != nil {
		at = map[string]any{"passed": after.Test.Passed, "reason": after.Test.Reason}
	}
	add("test", bt, at)
	return out
}

// ------------------------------------------------------------------ the installation's

func (r connectors) Installation(ctx context.Context) ([]storage.InstalledConnector, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InstallationConnectors(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InstalledConnector, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInstallationConnector(x))
	}
	return out, nil
}

func (r connectors) installationOne(ctx context.Context, id uuid.UUID) (storage.InstalledConnector, error) {
	all, err := r.Installation(ctx)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return storage.InstalledConnector{}, ports.ErrNotFound
}

func (r connectors) InstallationSecrets(ctx context.Context) ([]storage.ConnectorSecret, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InstallationConnectorSecrets(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ConnectorSecret, 0, len(rows))
	for _, x := range rows {
		out = append(out, toConnectorSecret(x))
	}
	return out, nil
}

func (r connectors) InstallInstallation(ctx context.Context, by, change uuid.UUID, n storage.NewConnector) (storage.InstalledConnector, error) {
	if err := r.u.check(); err != nil {
		return storage.InstalledConnector{}, err
	}
	config, err := configurationJSON(n.Configuration)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	consent, err := consentJSON(n.Consent)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	id, err := r.u.q.InstallInstallationConnector(ctx, sqlcdb.InstallInstallationConnectorParams{By: by, Change: ptrChange(change),
		Connector: n.Connector, ManifestVersion: n.ManifestVersion, ManifestHash: n.ManifestHash, SecretNames: secretNames(n.SecretNames), Consent: consent,
		Configuration: config, Position: small(n.Position), At: n.At})
	if err != nil {
		return storage.InstalledConnector{}, mapError(err)
	}
	out, err := r.installationOne(ctx, id)
	if err != nil {
		return out, err
	}
	changes := connectorChanges(storage.InstalledConnector{}, out)
	changes["connector"] = [2]any{nil, out.Connector}
	if change != uuid.Nil {
		changes["change"] = [2]any{nil, change.String()}
	}
	return out, r.u.event(ctx, "installation_connector.installed", "installation_connector", id, &out.Version, changes)
}

func (r connectors) BootstrapInstallation(ctx context.Context, cs []storage.BootstrapConnector, at time.Time) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	type row struct {
		Connector       string                   `json:"connector"`
		ManifestVersion string                   `json:"manifest_version"`
		ManifestHash    string                   `json:"manifest_hash"`
		SecretNames     []string                 `json:"secret_names"`
		Consent         storage.ConnectorConsent `json:"consent"`
		Configuration   map[string]any           `json:"configuration"`
		Position        int                      `json:"position"`
		Secrets         map[string]string        `json:"secrets"`
	}
	rows := make([]row, 0, len(cs))
	for _, c := range cs {
		secrets := map[string]string{}
		for k, v := range c.Secrets {
			secrets[k] = string(v)
		}
		config := c.Configuration
		if config == nil {
			config = map[string]any{}
		}
		rows = append(rows, row{Connector: c.Connector, ManifestVersion: c.ManifestVersion, ManifestHash: c.ManifestHash,
			SecretNames: secretNames(c.SecretNames), Consent: c.Consent, Configuration: config, Position: c.Position, Secrets: secrets})
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return 0, err
	}
	n, err := r.u.q.BootstrapInstallationConnectors(ctx, sqlcdb.BootstrapInstallationConnectorsParams{Rows: raw, At: at})
	if err != nil {
		return 0, mapError(err)
	}
	installed, err := r.Installation(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range installed {
		changes := connectorChanges(storage.InstalledConnector{}, c)
		changes["connector"] = [2]any{nil, c.Connector}
		changes["source"] = [2]any{nil, "the deployment's settings"}
		for _, b := range cs {
			if b.Connector == c.Connector {
				for name, ref := range b.Secrets {
					changes["secret."+name] = [2]any{nil, string(ref)}
				}
			}
		}
		if err := r.u.event(ctx, "installation_connector.installed", "installation_connector", c.ID, &c.Version, changes); err != nil {
			return 0, err
		}
	}
	return int(n), nil
}

func (r connectors) ChangeInstallation(ctx context.Context, by, change, id uuid.UUID, version int, c storage.ConnectorChange) (storage.InstalledConnector, error) {
	before, err := r.installationOne(ctx, id)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	config, err := configurationJSON(c.Configuration)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	consent, err := consentJSON(c.Consent)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	passed, reason, tested := testArgs(c.Test)
	_, err = r.u.q.ChangeInstallationConnector(ctx, sqlcdb.ChangeInstallationConnectorParams{By: by, Change: ptrChange(change), Step: c.Step,
		ID: id, Version: small(version), State: c.State, Configuration: config, Position: small(c.Position),
		TestPassed: passed, TestReason: reason, TestedAt: tested, ManifestVersion: c.ManifestVersion, ManifestHash: c.ManifestHash,
		SecretNames: secretNames(c.SecretNames), Consent: consent, At: c.At})
	if err != nil {
		return storage.InstalledConnector{}, mapError(err)
	}
	after, err := r.installationOne(ctx, id)
	if err != nil {
		return after, err
	}
	changes := connectorChanges(before, after)
	if change != uuid.Nil {
		changes["change"] = [2]any{nil, change.String()}
	}
	return after, r.u.event(ctx, "installation_connector."+stepEvents[c.Step], "installation_connector", id, &after.Version, changes)
}

func (r connectors) SetInstallationSecret(ctx context.Context, by, change, id uuid.UUID, name string, ref ports.SecretRef, validUntil, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	previous := r.currentSecret(ctx, id, name, true)
	if err := r.u.q.SetInstallationConnectorSecret(ctx, sqlcdb.SetInstallationConnectorSecretParams{By: by, Change: ptrChange(change), ID: id,
		Name: name, Ref: string(ref), ValidUntil: validUntil, At: at}); err != nil {
		return mapError(err)
	}
	changes := map[string][2]any{"secret." + name: {previous, string(ref)}}
	if previous != nil {
		changes["valid_until"] = [2]any{nil, validUntil.UTC().Format(time.RFC3339)}
	}
	if change != uuid.Nil {
		changes["change"] = [2]any{nil, change.String()}
	}
	return r.u.event(ctx, "installation_connector.secret_replaced", "installation_connector", id, nil, changes)
}

// currentSecret is the reference of the current version of a connector's secret, for an audit event's "before": nil
// when none.
func (r connectors) currentSecret(ctx context.Context, id uuid.UUID, name string, installation bool) any {
	var all []storage.ConnectorSecret
	var err error
	if installation {
		all, err = r.InstallationSecrets(ctx)
	} else {
		all, err = r.OrganisationSecrets(ctx)
	}
	if err != nil {
		return nil
	}
	for _, s := range all {
		if s.ConnectorID == id && s.Name == name && s.ValidUntil == nil && s.DestroyedAt == nil {
			return string(s.Ref)
		}
	}
	return nil
}

func (r connectors) DestroyedInstallationSecret(ctx context.Context, ref ports.SecretRef, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	return mapError(r.u.q.DestroyedInstallationConnectorSecret(ctx, sqlcdb.DestroyedInstallationConnectorSecretParams{Ref: string(ref), At: at}))
}

func (r connectors) RequestInstallationChange(ctx context.Context, n storage.NewInstallationChange) (storage.InstallationChange, error) {
	if err := r.u.check(); err != nil {
		return storage.InstallationChange{}, err
	}
	id, err := r.u.q.RequestInstallationChange(ctx, sqlcdb.RequestInstallationChangeParams{By: n.RequestedBy, ConnectorID: n.ConnectorID,
		Connector: n.Connector, Step: n.Step, Summary: n.Summary, Change: n.Change, Envelope: n.Envelope, EnvelopeHash: n.EnvelopeHash,
		MfaAt: n.RequesterMFAAt, At: n.At, ExpiresAt: n.ExpiresAt})
	if err != nil {
		return storage.InstallationChange{}, mapError(err)
	}
	out, err := r.changeOne(ctx, id)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "installation_change.requested", "installation_change", id, nil, map[string][2]any{
		"connector": {nil, out.Connector}, "step": {nil, out.Step}, "summary": {nil, out.Summary}})
}

func (r connectors) changeOne(ctx context.Context, id uuid.UUID) (storage.InstallationChange, error) {
	all, err := r.InstallationChanges(ctx)
	if err != nil {
		return storage.InstallationChange{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return storage.InstallationChange{}, ports.ErrNotFound
}

func (r connectors) DecideInstallationChange(ctx context.Context, by, id uuid.UUID, hash []byte, approve bool, mfaAt time.Time, reason string,
	at time.Time) (storage.InstallationChange, error) {
	if err := r.u.check(); err != nil {
		return storage.InstallationChange{}, err
	}
	if err := r.u.q.DecideInstallationChange(ctx, sqlcdb.DecideInstallationChangeParams{By: by, ID: id, EnvelopeHash: hash, Approve: approve,
		MfaAt: mfaAt, Reason: opt(reason), At: at}); err != nil {
		return storage.InstallationChange{}, mapError(err)
	}
	out, err := r.changeOne(ctx, id)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "installation_change."+out.Status, "installation_change", id, nil, map[string][2]any{
		"status": {"pending", out.Status}, "reason": {nil, nilIfEmpty(reason)}})
}

func (r connectors) ExpireInstallationChanges(ctx context.Context, at time.Time) ([]storage.InstallationChange, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.ExpireInstallationChanges(ctx, at)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InstallationChange, 0, len(ids))
	for _, id := range ids {
		c, err := r.changeOne(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := r.u.event(ctx, "installation_change.expired", "installation_change", id, nil, map[string][2]any{"status": {"pending", "expired"}}); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r connectors) InstallationChanges(ctx context.Context) ([]storage.InstallationChange, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InstallationChanges(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InstallationChange, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInstallationChange(x))
	}
	return out, nil
}

// ------------------------------------------------------------------ the organisation's

func (r connectors) Organisation(ctx context.Context) ([]storage.InstalledConnector, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.OrganisationConnectors(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InstalledConnector, 0, len(rows))
	for _, x := range rows {
		out = append(out, toOrganisationConnector(x))
	}
	return out, nil
}

func (r connectors) organisationOne(ctx context.Context, id uuid.UUID) (storage.InstalledConnector, error) {
	all, err := r.Organisation(ctx)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return storage.InstalledConnector{}, ports.ErrNotFound
}

func (r connectors) OrganisationSecrets(ctx context.Context) ([]storage.ConnectorSecret, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.OrganisationConnectorSecrets(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ConnectorSecret, 0, len(rows))
	for _, x := range rows {
		out = append(out, toConnectorSecret(sqlcdb.FyniqInstallationConnectorSecret(x)))
	}
	return out, nil
}

func (r connectors) InstallOrganisation(ctx context.Context, n storage.NewConnector) (storage.InstalledConnector, error) {
	if err := r.u.check(); err != nil {
		return storage.InstalledConnector{}, err
	}
	config, err := configurationJSON(n.Configuration)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	consent, err := consentJSON(n.Consent)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	id, err := r.u.q.InsertOrganisationConnector(ctx, sqlcdb.InsertOrganisationConnectorParams{Connector: n.Connector,
		ManifestVersion: n.ManifestVersion, ManifestHash: n.ManifestHash, SecretNames: secretNames(n.SecretNames), Consent: consent, Configuration: config,
		Position: small(n.Position), By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.InstalledConnector{}, mapError(err)
	}
	out, err := r.organisationOne(ctx, id)
	if err != nil {
		return out, err
	}
	changes := connectorChanges(storage.InstalledConnector{}, out)
	changes["connector"] = [2]any{nil, out.Connector}
	return out, r.u.event(ctx, "organisation_connector.installed", "organisation_connector", id, &out.Version, changes)
}

func (r connectors) ChangeOrganisation(ctx context.Context, id uuid.UUID, version int, c storage.ConnectorChange) (storage.InstalledConnector, error) {
	before, err := r.organisationOne(ctx, id)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	config, err := configurationJSON(c.Configuration)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	consent, err := consentJSON(c.Consent)
	if err != nil {
		return storage.InstalledConnector{}, err
	}
	passed, reason, tested := testArgs(c.Test)
	n, err := r.u.q.UpdateOrganisationConnector(ctx, sqlcdb.UpdateOrganisationConnectorParams{State: c.State, Configuration: config, Consent: consent,
		Position: small(c.Position), TestPassed: passed, TestReason: reason, TestedAt: tested, ManifestVersion: c.ManifestVersion,
		ManifestHash: c.ManifestHash, SecretNames: secretNames(c.SecretNames), By: r.u.actor(ctx), At: c.At, ID: id, Version: small(version)})
	if err != nil {
		return storage.InstalledConnector{}, mapError(err)
	}
	if n == 0 {
		return storage.InstalledConnector{}, &storage.Error{Code: storage.CodeVersion, Message: "the connector changed since it was read"}
	}
	after, err := r.organisationOne(ctx, id)
	if err != nil {
		return after, err
	}
	return after, r.u.event(ctx, "organisation_connector."+stepEvents[c.Step], "organisation_connector", id, &after.Version, connectorChanges(before, after))
}

func (r connectors) SetOrganisationSecret(ctx context.Context, id uuid.UUID, name string, ref ports.SecretRef, validUntil, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	previous := r.currentSecret(ctx, id, name, false)
	if err := r.u.q.ReplaceOrganisationConnectorSecret(ctx, sqlcdb.ReplaceOrganisationConnectorSecretParams{ValidUntil: &validUntil,
		ConnectorID: id, Name: name}); err != nil {
		return mapError(err)
	}
	if err := r.u.q.InsertOrganisationConnectorSecret(ctx, sqlcdb.InsertOrganisationConnectorSecretParams{ConnectorID: id, Name: name,
		Ref: string(ref), By: r.u.actor(ctx), At: at}); err != nil {
		return mapError(err)
	}
	changes := map[string][2]any{"secret." + name: {previous, string(ref)}}
	if previous != nil {
		changes["valid_until"] = [2]any{nil, validUntil.UTC().Format(time.RFC3339)}
	}
	return r.u.event(ctx, "organisation_connector.secret_replaced", "organisation_connector", id, nil, changes)
}

func (r connectors) DestroyedOrganisationSecret(ctx context.Context, ref ports.SecretRef, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	n, err := r.u.q.DestroyedOrganisationConnectorSecret(ctx, sqlcdb.DestroyedOrganisationConnectorSecretParams{Ref: string(ref), At: &at})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return &storage.Error{Code: storage.CodeConnector, Message: "that secret version is not one to destroy"}
	}
	return nil
}
