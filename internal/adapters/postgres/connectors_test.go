//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

const zeptoHash = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"

// makeSuperAdmin makes user an active Super admin, as the setup wizard or an accepted invitation would, but without
// the installation's trail, which this package's database has none of: as the superuser, with triggers off for that
// one transaction.
func makeSuperAdmin(t *testing.T, user uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn(t, "fyniq").Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO fyniq.super_admin (user_id) VALUES ($1)", user); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// requestChange asks for a change to an installation connector as by, and returns it.
func requestChange(t *testing.T, org, by uuid.UUID, id *uuid.UUID, connector, step string, now time.Time) storage.InstallationChange {
	t.Helper()
	envelope := `{"connector":"` + connector + `","step":"` + step + `","at":"` + now.Format(time.RFC3339Nano) + `"}`
	sum := sha256.Sum256([]byte(envelope))
	var out storage.InstallationChange
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = u.Connectors().RequestInstallationChange(ctx, storage.NewInstallationChange{ConnectorID: id, Connector: connector, Step: step,
			Summary: step + " " + connector, Change: []byte(`{"step":"` + step + `"}`), Envelope: envelope, EnvelopeHash: sum[:], RequestedBy: by,
			RequesterMFAAt: now, At: now, ExpiresAt: now.Add(7 * 24 * time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func decide(t *testing.T, org, by uuid.UUID, c storage.InstallationChange, hash []byte, approve bool, now time.Time) error {
	t.Helper()
	return store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Connectors().DecideInstallationChange(ctx, by, c.ID, hash, approve, now, "", now)
		return err
	})
}

func installation(t *testing.T, org uuid.UUID, connector string) storage.InstalledConnector {
	t.Helper()
	var out storage.InstalledConnector
	if err := store(t, "fyniq_worker").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Connectors().Installation(ctx)
		for _, c := range all {
			if c.Connector == connector && c.State != storage.ConnectorUninstalled {
				out = c
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// step applies a step to installation connector c as by, under change (uuid.Nil for none).
func step(t *testing.T, org, by, change uuid.UUID, c storage.InstalledConnector, s, state string, now time.Time) (storage.InstalledConnector, error) {
	t.Helper()
	var out storage.InstalledConnector
	err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = u.Connectors().ChangeInstallation(ctx, by, change, c.ID, c.Version, storage.ConnectorChange{Step: s, State: state,
			Configuration: c.Configuration, Position: c.Position, Test: c.Test, ManifestVersion: c.ManifestVersion, ManifestHash: c.ManifestHash,
			SecretNames: c.SecretNames, At: now})
		return err
	})
	return out, err
}

// The installation's connectors live in the database as the lifecycle allows (migration 000141): the first-start
// bootstrap installs the deployment's email connectors once, into an empty catalogue, enabled, with their secrets as
// references; with one Super admin a step applies at once; once there are two, every step but pause needs the other
// one's approved change, decided once, by another person, signing the envelope's hash, before it expires, and applied
// once. A replaced secret stays valid for its overlap and is destroyed only after. Every step is audited in the
// unit of work's organisation, and the database refuses what the lifecycle does not allow.
// req: FR-INT-20
// req: FR-INT-26
// req: FR-ADM-07
// req: FR-INT-21
// req: QA-ENG-05
// asvs: V2.3.3, V2.3.5, V16.3.3
func TestInstallationConnectorsKeepTheirLifecycle(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Connectors installation Pte Ltd")
	org := b.Org
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	first, second := founder(t, st, org, 31, now), founder(t, st, org, 32, now)
	makeSuperAdmin(t, first)

	boot := []storage.BootstrapConnector{
		{NewConnector: storage.NewConnector{Connector: "zeptomail", ManifestVersion: "1.0.0", ManifestHash: zeptoHash,
			SecretNames: []string{"send_token", "webhook_key"}, Configuration: map[string]any{"url": "https://cpaas.zoho.com/v1.1/email"}, Position: 1},
			Secrets: map[string]ports.SecretRef{"send_token": "projects/1/secrets/zeptomail-key/versions/3"}},
		{NewConnector: storage.NewConnector{Connector: "smtp", ManifestVersion: "1.0.0", ManifestHash: zeptoHash,
			Configuration: map[string]any{"address": "smtp.example.com:587"}, Position: 2}},
	}
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		n, err := u.Connectors().BootstrapInstallation(ctx, boot, now)
		if err == nil && n != 2 {
			t.Errorf("bootstrapped %d", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Connectors().BootstrapInstallation(ctx, boot, now)
		return err
	}); storage.CodeOf(err) != storage.CodeBootstrap {
		t.Fatalf("a second bootstrap: %v", err)
	}
	zepto := installation(t, org, "zeptomail")
	if zepto.State != storage.ConnectorEnabled || zepto.Position != 1 || zepto.InstalledBy != "service:api" || zepto.Configuration["url"] == nil {
		t.Fatalf("bootstrapped: %+v", zepto)
	}

	// One Super admin: a step applies at once.
	paused, err := step(t, org, first, uuid.Nil, zepto, storage.StepPause, storage.ConnectorPaused, now)
	if err != nil || paused.State != storage.ConnectorPaused || paused.Version != 2 {
		t.Fatalf("pause: %+v, %v", paused, err)
	}
	if _, err := step(t, org, first, uuid.Nil, zepto, storage.StepResume, storage.ConnectorEnabled, now); storage.CodeOf(err) != storage.CodeVersion {
		t.Fatalf("a step on a stale version: %v", err)
	}
	zepto, err = step(t, org, first, uuid.Nil, paused, storage.StepResume, storage.ConnectorEnabled, now)
	if err != nil || zepto.State != storage.ConnectorEnabled {
		t.Fatalf("resume with one Super admin: %+v, %v", zepto, err)
	}

	// Two Super admins: pause alone, everything else through the other's approved change.
	makeSuperAdmin(t, second)
	if paused, err = step(t, org, second, uuid.Nil, zepto, storage.StepPause, storage.ConnectorPaused, now); err != nil {
		t.Fatalf("pause by one of two Super admins: %v", err)
	}
	if _, err := step(t, org, second, uuid.Nil, paused, storage.StepResume, storage.ConnectorEnabled, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("resume without the other's approval: %v", err)
	}
	resume := requestChange(t, org, second, &paused.ID, "zeptomail", storage.StepResume, now)
	if err := decide(t, org, second, resume, resume.EnvelopeHash, true, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("deciding one's own change: %v", err)
	}
	other := sha256.Sum256([]byte("another change"))
	if err := decide(t, org, first, resume, other[:], true, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("approving another envelope: %v", err)
	}
	if _, err := step(t, org, second, resume.ID, paused, storage.StepResume, storage.ConnectorEnabled, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("applying a pending change: %v", err)
	}
	if err := decide(t, org, first, resume, resume.EnvelopeHash, true, now); err != nil {
		t.Fatalf("approval by the other Super admin: %v", err)
	}
	if err := decide(t, org, first, resume, resume.EnvelopeHash, false, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("deciding twice: %v", err)
	}
	if zepto, err = step(t, org, second, resume.ID, paused, storage.StepResume, storage.ConnectorEnabled, now); err != nil || zepto.State != storage.ConnectorEnabled {
		t.Fatalf("resume under the approved change: %+v, %v", zepto, err)
	}
	if paused, err = step(t, org, first, uuid.Nil, zepto, storage.StepPause, storage.ConnectorPaused, now); err != nil {
		t.Fatal(err)
	}
	if _, err := step(t, org, second, resume.ID, paused, storage.StepResume, storage.ConnectorEnabled, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("applying a change twice: %v", err)
	}

	// A secret replaced under an approved change: the old version stays valid for its overlap, then is destroyed.
	secret := requestChange(t, org, first, &paused.ID, "zeptomail", storage.StepSecret, now)
	if err := decide(t, org, second, secret, secret.EnvelopeHash, true, now); err != nil {
		t.Fatal(err)
	}
	overlap := now.Add(time.Hour)
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Connectors().SetInstallationSecret(ctx, first, secret.ID, paused.ID, "send_token", "projects/1/secrets/zeptomail-key/versions/4", overlap, now)
	}); err != nil {
		t.Fatalf("replacing the send token: %v", err)
	}
	var versions []storage.ConnectorSecret
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		versions, err = u.Connectors().InstallationSecrets(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].ValidUntil == nil || !versions[0].ValidUntil.Equal(overlap) || versions[1].ValidUntil != nil {
		t.Fatalf("the versions: %+v", versions)
	}
	worker := store(t, "fyniq_worker")
	if err := worker.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Connectors().DestroyedInstallationSecret(ctx, versions[0].Ref, now)
	}); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("destroying a version still in its overlap: %v", err)
	}
	if err := worker.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Connectors().DestroyedInstallationSecret(ctx, versions[0].Ref, overlap)
	}); err != nil {
		t.Fatalf("destroying a version past its overlap: %v", err)
	}

	// An install and an illegal step, each under its approved change; an uninstalled connector is final.
	install := requestChange(t, org, first, nil, "resend", storage.StepInstall, now)
	if err := decide(t, org, second, install, install.EnvelopeHash, true, now); err != nil {
		t.Fatal(err)
	}
	var resend storage.InstalledConnector
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		resend, err = u.Connectors().InstallInstallation(ctx, first, install.ID, storage.NewConnector{Connector: "resend", ManifestVersion: "1.0.0",
			ManifestHash: zeptoHash, SecretNames: []string{"api_key", "webhook_secret"}, Position: 3, At: now})
		return err
	}); err != nil || resend.State != storage.ConnectorInstalled {
		t.Fatalf("install: %+v, %v", resend, err)
	}
	enable := requestChange(t, org, first, &resend.ID, "resend", storage.StepEnable, now)
	if err := decide(t, org, second, enable, enable.EnvelopeHash, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := step(t, org, first, enable.ID, resend, storage.StepEnable, storage.ConnectorEnabled, now); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("enabling a connector never configured or tested: %v", err)
	}
	configure := requestChange(t, org, first, &resend.ID, "resend", storage.StepConfigure, now)
	if err := decide(t, org, second, configure, configure.EnvelopeHash, true, now); err != nil {
		t.Fatal(err)
	}
	withKey := resend
	withKey.Configuration = map[string]any{"api_key": "re_live_value"}
	if _, err := step(t, org, first, configure.ID, withKey, storage.StepConfigure, storage.ConnectorConfigured, now); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("a secret in the configuration: %v", err)
	}
	uninstall := requestChange(t, org, first, &resend.ID, "resend", storage.StepUninstall, now)
	if err := decide(t, org, second, uninstall, uninstall.EnvelopeHash, true, now); err != nil {
		t.Fatal(err)
	}
	gone, err := step(t, org, first, uninstall.ID, resend, storage.StepUninstall, storage.ConnectorUninstalled, now)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := step(t, org, first, uuid.Nil, gone, storage.StepPause, storage.ConnectorPaused, now); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("a step on an uninstalled connector: %v", err)
	}

	// A change not decided in time expires and cannot be decided after.
	late := requestChange(t, org, first, &paused.ID, "zeptomail", storage.StepDisable, now)
	if err := worker.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		expired, err := u.Connectors().ExpireInstallationChanges(ctx, now.Add(8*24*time.Hour))
		if err == nil && !slices.ContainsFunc(expired, func(c storage.InstallationChange) bool { return c.ID == late.ID }) {
			t.Errorf("expired: %+v", expired)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := decide(t, org, second, late, late.EnvelopeHash, true, now); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("deciding an expired change: %v", err)
	}

	got := actions(t, org, zepto.ID)
	for _, want := range []string{"installation_connector.installed", "installation_connector.paused", "installation_connector.resumed",
		"installation_connector.secret_replaced"} {
		if !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}
	if a := actions(t, org, resume.ID); !slices.Contains(a, "installation_change.requested") || !slices.Contains(a, "installation_change.approved") {
		t.Errorf("the change's events: %v", a)
	}

	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "write the installation's connectors directly", "UPDATE fyniq.installation_connector SET state = 'enabled' WHERE id = $1", "42501"},
		{"fyniq_api", "change a connector without being a Super admin", "SELECT fyniq.change_installation_connector(gen_random_uuid(), NULL, 'pause', $1, 1, 'paused', '{}', 1, NULL, NULL, NULL, '1.0.0', '" + zeptoHash + "', '{}', '{}', now())", "FY085"},
		{"fyniq_worker", "change a connector as the worker", "SELECT fyniq.change_installation_connector(gen_random_uuid(), NULL, 'pause', $1, 1, 'paused', '{}', 1, NULL, NULL, NULL, '1.0.0', '" + zeptoHash + "', '{}', '{}', now())", "42501"},
		{"fyniq_worker", "approve a change as the worker", "SELECT fyniq.decide_installation_change(gen_random_uuid(), $1, '\\x00', true, now(), NULL, now())", "42501"},
		{"fyniq_migrator", "delete a connector", "DELETE FROM fyniq.installation_connector WHERE id = $1", "FY205"},
		{"fyniq_migrator", "rename a connector", "UPDATE fyniq.installation_connector SET connector = 'other', version = version + 1 WHERE id = $1", "FY205"},
		{"fyniq_migrator", "change a secret version's reference", "UPDATE fyniq.installation_connector_secret SET ref = 'projects/1/secrets/x/versions/9' WHERE connector_id = $1", "FY208"},
		{"fyniq_migrator", "delete a secret version", "DELETE FROM fyniq.installation_connector_secret WHERE connector_id = $1", "FY208"},
		{"fyniq_migrator", "reopen a decided change", "UPDATE fyniq.installation_change SET status = 'pending', decided_by = NULL, decided_at = NULL, decider_mfa_at = NULL WHERE connector_id = $1 AND status = 'approved'", "FY209"},
		{"fyniq_migrator", "delete a change", "DELETE FROM fyniq.installation_change WHERE connector_id = $1", "FY209"},
	} {
		err := asOrg(ctx, conn(t, x.role), org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, zepto.ID); return err })
		wantState(t, x.what, err, x.state)
	}
}

// An organisation's connectors are its own rows (migration 000141): they move only along the lifecycle, keep no
// secret in their configuration, replace a secret by a new version, are audited in the same transaction, and are
// invisible to every other organisation.
// req: FR-INT-26
// req: INV-06
// req: QA-ENG-05
// asvs: V8.4.1, V16.3.3
func TestOrganisationConnectorsKeepTheirLifecycle(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Connectors organisation A Pte Ltd"), books(t, "Connectors organisation B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var c storage.InstalledConnector
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: "wise", ManifestVersion: "1.0.0", ManifestHash: zeptoHash,
			SecretNames: []string{"api_token"}, Position: 1, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	change := func(c storage.InstalledConnector, s, state string, config map[string]any) (storage.InstalledConnector, error) {
		var out storage.InstalledConnector
		err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: s, State: state, Configuration: config,
				Position: c.Position, Test: c.Test, ManifestVersion: c.ManifestVersion, ManifestHash: c.ManifestHash, SecretNames: c.SecretNames, At: now})
			return err
		})
		return out, err
	}
	if _, err := change(c, storage.StepEnable, storage.ConnectorEnabled, nil); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("enable before configuring: %v", err)
	}
	if _, err := change(c, storage.StepConfigure, storage.ConnectorConfigured, map[string]any{"api_token": "x"}); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("a secret in the configuration: %v", err)
	}
	configured, err := change(c, storage.StepConfigure, storage.ConnectorConfigured, map[string]any{"profile": "business"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := change(c, storage.StepTest, storage.ConnectorTested, configured.Configuration); storage.CodeOf(err) != storage.CodeVersion {
		t.Fatalf("a stale version: %v", err)
	}
	configured.Test = &storage.ConnectorTest{Passed: true, Reason: "The token was accepted.", At: now}
	tested, err := change(configured, storage.StepTest, storage.ConnectorTested, configured.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := change(tested, storage.StepEnable, storage.ConnectorEnabled, tested.Configuration)
	if err != nil || enabled.Test == nil || !enabled.Test.Passed {
		t.Fatalf("enable: %+v, %v", enabled, err)
	}
	for _, ref := range []ports.SecretRef{"local:org-wise-api_token/1", "local:org-wise-api_token/2"} {
		if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Connectors().SetOrganisationSecret(ctx, c.ID, "api_token", ref, now.Add(time.Hour), now)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Connectors().SetOrganisationSecret(ctx, c.ID, "webhook_key", "local:org-wise-webhook_key/1", now, now)
	}); storage.CodeOf(err) != storage.CodeConnector {
		t.Fatalf("a secret the manifest does not name: %v", err)
	}
	if got := actions(t, a.Org, c.ID); !slices.Contains(got, "organisation_connector.enabled") || !slices.Contains(got, "organisation_connector.secret_replaced") {
		t.Errorf("events: %v", got)
	}
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		mine, err := u.Connectors().Organisation(ctx)
		if err == nil && len(mine) != 0 {
			t.Errorf("organisation B sees %d connectors", len(mine))
		}
		secrets, err2 := u.Connectors().OrganisationSecrets(ctx)
		if err2 == nil && len(secrets) != 0 {
			t.Errorf("organisation B sees %d secrets", len(secrets))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "install an enabled connector", "INSERT INTO fyniq.organisation_connector (connector, manifest_version, manifest_hash, secret_names, state, position, installed_by, installed_at, changed_by, changed_at) SELECT 'other', '1.0.0', manifest_hash, '{}', 'enabled', 1, 'user:x', now(), 'user:x', now() FROM fyniq.organisation_connector WHERE id = $1", "FY207"},
		{"fyniq_api", "delete a connector", "DELETE FROM fyniq.organisation_connector WHERE id = $1", "42501"},
		{"fyniq_migrator", "delete a connector as the migrator", "DELETE FROM fyniq.organisation_connector WHERE id = $1", "FY205"},
		{"fyniq_api", "change a secret's reference", "UPDATE fyniq.organisation_connector_secret SET ref = 'local:x/1' WHERE connector_id = $1", "42501"},
		{"fyniq_api", "extend a replaced version", "UPDATE fyniq.organisation_connector_secret SET valid_until = now() + interval '1 year' WHERE connector_id = $1 AND valid_until IS NOT NULL", "FY208"},
		{"fyniq_worker", "change a connector as the worker", "UPDATE fyniq.organisation_connector SET state = 'paused', version = version + 1 WHERE id = $1", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, c.ID); return err })
		wantState(t, x.what, err, x.state)
	}
	if n := count(t, b.Org, "organisation_connector"); n != 0 {
		t.Fatalf("organisation B counts %d connectors", n)
	}
}

// AfterRollback runs when a unit of work does not commit (an error or a preview), never after a commit; AfterCommit the
// other way round. A secret version put before the transaction that records it is destroyed this way.
// req: FR-INT-26
func TestAfterRollbackRunsOnlyWhenNothingCommits(t *testing.T) {
	ctx := context.Background()
	b := books(t, "After rollback Pte Ltd")
	st := store(t, "fyniq_api")
	run := func(ctx context.Context, fail error) (committed, rolledBack bool, err error) {
		err = st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			u.AfterCommit(func(context.Context) { committed = true })
			u.AfterRollback(func(context.Context) { rolledBack = true })
			return fail
		})
		return committed, rolledBack, err
	}
	if c, r, err := run(ctx, nil); err != nil || !c || r {
		t.Fatalf("a commit: committed %v, rolled back %v, %v", c, r, err)
	}
	if c, r, _ := run(ctx, ports.ErrNotFound); c || !r {
		t.Fatalf("an error: committed %v, rolled back %v", c, r)
	}
	if c, r, err := run(storage.WithPreview(ctx), nil); !errors.Is(err, storage.ErrPreviewed) || c || !r {
		t.Fatalf("a preview: committed %v, rolled back %v, %v", c, r, err)
	}
}
