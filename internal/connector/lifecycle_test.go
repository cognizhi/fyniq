//go:build local

package connector_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type world struct {
	db      *pgtest.DB
	store   *postgres.Store
	org     uuid.UUID
	secrets *local.SecretFiles
	clock   *clock.Frozen
}

func start(t *testing.T) world {
	t.Helper()
	ctx := context.Background()
	db, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("the lifecycle tests need Docker: %v", err)
	}
	t.Cleanup(db.Stop)
	st, err := postgres.Open(ctx, db.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	org, err := st.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Installation trail Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := local.NewSecretFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return world{db: db, store: st, org: org.ID, secrets: secrets, clock: clock.NewFrozen(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))}
}

// superAdmin adds an active Super admin, without the installation's trail this database has none of: as the superuser,
// with triggers off for that one transaction.
func (w world) superAdmin(t *testing.T, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, w.db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id uuid.UUID
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, "INSERT INTO fyniq.app_user (display_name) VALUES ($1) RETURNING user_id", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO fyniq.super_admin (user_id) VALUES ($1)", id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func (w world) do(t *testing.T, fn func(ctx context.Context, u storage.UnitOfWork) error) error {
	t.Helper()
	return w.store.Do(context.Background(), w.org, fn)
}

// The installation's connectors go through the lifecycle for the Super admins: with one, each step applies at once;
// with two, every step but pause and test becomes a change the other approves, applied with the approval; a change
// asked of a connector that has changed since is refused; a rejected change's new secret version is destroyed; a key
// replaced stays valid for its overlap and the clean-up destroys it after; an uninstall lists the webhooks a person
// removes by hand.
// req: FR-INT-20
// req: FR-INT-26
// req: FR-ADM-07
// asvs: V2.3.5, V13.3.4
func TestInstallationConnectorsGoThroughTheLifecycle(t *testing.T) {
	w := start(t)
	ctx := context.Background()
	cat, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	s := connector.Installations{Catalogue: cat, Secrets: w.secrets, Names: map[string]string{"zeptomail.send_token": "zeptomail-key"},
		Admins: w.store, Clock: w.clock}
	first := w.superAdmin(t, "First")
	now := w.clock.Now()

	var zepto storage.InstalledConnector
	step := func(by uuid.UUID, st string, in connector.Input, value string) (connector.Result, error) {
		var r connector.Result
		err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			r, err = s.Step(ctx, u, by, now, zepto.ID, zepto.Version, st, in, value)
			return err
		})
		if err == nil && r.Pending == nil {
			zepto = r.Connector
		}
		return r, err
	}
	if err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := s.Install(ctx, u, first, now, "zeptomail")
		zepto = r.Connector
		return err
	}); err != nil || zepto.State != storage.ConnectorInstalled || zepto.Position != 1 {
		t.Fatalf("install: %+v, %v", zepto, err)
	}
	if _, err := step(first, storage.StepConfigure, connector.Input{Configuration: map[string]any{"url": "ftp://nowhere"}}, ""); code(err) != connector.CodeConfiguration {
		t.Fatalf("a configuration the manifest refuses: %v", err)
	}
	if _, err := step(first, storage.StepConfigure, connector.Input{Configuration: map[string]any{"url": "https://cpaas.zoho.com/v1.1/email"}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := step(first, storage.StepTest, connector.Input{Test: &storage.ConnectorTest{Passed: true, Reason: "The send token was accepted.", At: now}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := step(first, storage.StepEnable, connector.Input{}, ""); code(err) != connector.CodeSecretMissing {
		t.Fatalf("enable before its secrets are set: %v", err)
	}
	if _, err := step(first, storage.StepSecret, connector.Input{Secret: "send_token"}, "zepto-token-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := step(first, storage.StepSecret, connector.Input{Secret: "webhook_key"}, "zepto-webhook-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := step(first, storage.StepSecret, connector.Input{Secret: "password"}, "x"); code(err) != connector.CodeSecretUnknown {
		t.Fatalf("a secret the manifest does not have: %v", err)
	}
	if r, err := step(first, storage.StepEnable, connector.Input{}, ""); err != nil || r.Connector.State != storage.ConnectorEnabled {
		t.Fatalf("enable with one Super admin: %+v, %v", r, err)
	}
	var versions []storage.ConnectorSecret
	_ = w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		versions, err = u.Connectors().InstallationSecrets(ctx)
		return err
	})
	if len(versions) != 2 || versions[0].Ref == "" || versions[0].Ref[:len("local:zeptomail-key/")] != "local:zeptomail-key/" {
		t.Fatalf("the secret versions, held where FYNIQ_CONNECTOR_SECRETS names: %+v", versions)
	}
	if v, err := w.secrets.Get(ctx, versions[0].Ref); err != nil || v != "zepto-token-one" {
		t.Fatalf("the send token in the store: %q, %v", v, err)
	}

	// A second Super admin: pause alone, the rest through the other's approval.
	second := w.superAdmin(t, "Second")
	if r, err := step(second, storage.StepPause, connector.Input{}, ""); err != nil || r.Pending != nil || r.Connector.State != storage.ConnectorPaused {
		t.Fatalf("pause by one of two: %+v, %v", r, err)
	}
	r, err := step(second, storage.StepResume, connector.Input{}, "")
	if err != nil || r.Pending == nil || r.Connector.State != storage.ConnectorPaused {
		t.Fatalf("resume by one of two asks the other: %+v, %v", r, err)
	}
	resume := *r.Pending
	approve := func(by uuid.UUID, c storage.InstallationChange) (connector.Result, error) {
		var out connector.Result
		err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out, err = s.Approve(ctx, u, by, now, c.ID, c.EnvelopeHash)
			return err
		})
		if err == nil && out.Connector.ID == zepto.ID {
			zepto = out.Connector
		}
		return out, err
	}
	if _, err := approve(second, resume); storage.CodeOf(err) != storage.CodeConnectorChange {
		t.Fatalf("approving one's own change: %v", err)
	}
	if r, err := approve(first, resume); err != nil || r.Connector.State != storage.ConnectorEnabled {
		t.Fatalf("approved by the other, and applied: %+v, %v", r, err)
	}

	// A change asked of an older version never applies.
	stale, err := step(first, storage.StepOrder, connector.Input{Position: 2}, "")
	if err != nil || stale.Pending == nil {
		t.Fatalf("order asks the other: %+v, %v", stale, err)
	}
	if _, err := step(second, storage.StepPause, connector.Input{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := approve(second, *stale.Pending); code(err) != connector.CodeChange {
		t.Fatalf("approving a change of an older version: %v", err)
	}

	// A rejected key change: its new version is destroyed once the rejection commits.
	key, err := step(first, storage.StepSecret, connector.Input{Secret: "send_token"}, "zepto-token-rejected")
	if err != nil || key.Pending == nil {
		t.Fatalf("a key change asks the other: %+v, %v", key, err)
	}
	var rejected ports.SecretRef
	_ = w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		all, _ := u.Connectors().InstallationChanges(ctx)
		for _, c := range all {
			if c.ID == key.Pending.ID {
				rejected = ports.SecretRef(extractRef(t, c.Change))
			}
		}
		return nil
	})
	if v, err := w.secrets.Get(ctx, rejected); err != nil || v != "zepto-token-rejected" {
		t.Fatalf("the asked-for version waits in the store: %q, %v", v, err)
	}
	if err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := s.Reject(ctx, u, second, now, key.Pending.ID, key.Pending.EnvelopeHash, "Not this key.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.secrets.Get(ctx, rejected); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("the rejected version: %v; want it destroyed", err)
	}

	// A key change approved: the old version stays valid for the manifest's overlap (1 hour), then the clean-up
	// destroys it.
	key, err = step(first, storage.StepSecret, connector.Input{Secret: "send_token"}, "zepto-token-two")
	if err != nil || key.Pending == nil {
		t.Fatal(key, err)
	}
	if _, err := approve(second, *key.Pending); err != nil {
		t.Fatalf("the key change approved: %v", err)
	}
	old := versions[0].Ref
	worker, err := postgres.Open(ctx, w.db.URL("fyniq_worker")) // the clean-up runs in the jobs, as the worker
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	cleanup := connector.Cleanup{Store: worker, Installation: installationOrg(w.org), Secrets: w.secrets, Clock: w.clock}
	if n, err := cleanup.Run(ctx); err != nil || n != 0 {
		t.Fatalf("within the overlap: %d destroyed, %v", n, err)
	}
	if v, _ := w.secrets.Get(ctx, old); v != "zepto-token-one" {
		t.Fatal("the old key is gone within its overlap")
	}
	w.clock.Advance(time.Hour)
	if n, err := cleanup.Run(ctx); err != nil || n != 1 {
		t.Fatalf("after the overlap: %d destroyed, %v", n, err)
	}
	if _, err := w.secrets.Get(ctx, old); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("the old key after its overlap: %v", err)
	}

	// A version put for a unit of work that rolls back is destroyed with it.
	boom := errors.New("boom")
	var orphan ports.SecretRef
	err = w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := s.Step(ctx, u, first, now, zepto.ID, zepto.Version, storage.StepSecret, connector.Input{Secret: "webhook_key"}, "never-kept")
		if err != nil {
			return err
		}
		all, _ := u.Connectors().InstallationChanges(ctx)
		for _, c := range all {
			if c.ID == r.Pending.ID {
				orphan = ports.SecretRef(extractRef(t, c.Change))
			}
		}
		return boom
	})
	if !errors.Is(err, boom) || orphan == "" {
		t.Fatalf("%v, %q", err, orphan)
	}
	if _, err := w.secrets.Get(ctx, orphan); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a version put for a rolled-back change: %v; want it destroyed", err)
	}

	// Uninstall: approved, and the webhooks set by hand are listed to remove.
	gone, err := step(first, storage.StepUninstall, connector.Input{}, "")
	if err != nil || gone.Pending == nil {
		t.Fatal(gone, err)
	}
	done, err := approve(second, *gone.Pending)
	if err != nil || done.Connector.State != storage.ConnectorUninstalled || len(done.Webhooks) != 1 || done.Webhooks[0].Path != "/api/webhooks/email/zeptomail" {
		t.Fatalf("uninstall: %+v, %v", done, err)
	}
	if n, err := cleanup.Run(ctx); err != nil || n != 2 {
		t.Fatalf("the uninstalled connector's versions: %d destroyed, %v", n, err)
	}
}

type installationOrg uuid.UUID

func (o installationOrg) InstallationOrganisation(context.Context) (uuid.UUID, error) {
	return uuid.UUID(o), nil
}

func extractRef(t *testing.T, change []byte) string {
	t.Helper()
	var c struct {
		Ref string `json:"ref"`
	}
	if err := jsonUnmarshal(change, &c); err != nil || c.Ref == "" {
		t.Fatalf("the change carries no reference: %s", change)
	}
	return c.Ref
}

// An organisation's connectors take each step through Apply, which the approval engine calls once a step is approved
// (FM-1): a connector installed installation-wide is refused here; a secret is put before the change and set by Apply;
// disabling cancels the queued work its manifest says disable cancels.
// req: FR-INT-26
// req: FR-INT-20
func TestOrganisationConnectorsGoThroughTheLifecycle(t *testing.T) {
	w := start(t)
	ctx := context.Background()
	raw := edit(t, func(m map[string]any) {
		m["id"], m["scope"] = "example-org", "organisation"
		capability(m)["on_disable"] = "cancel"
	})
	m, err := connector.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := connector.NewCatalogue(connector.Binding{Manifest: m, OutboundEmail: func(connector.Config) (ports.Email, error) { return nopEmail{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	var cancelled []uuid.UUID
	s := connector.Organisations{Catalogue: cat, Secrets: w.secrets, Clock: w.clock, Cancellers: map[connector.Kind]connector.Canceller{
		connector.OutboundEmail: func(_ context.Context, _ storage.UnitOfWork, c storage.InstalledConnector) error {
			cancelled = append(cancelled, c.ID)
			return nil
		}}}
	real, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := connector.Organisations{Catalogue: real, Secrets: w.secrets, Clock: w.clock}.Install(ctx, u, "zeptomail")
		return err
	}); code(err) != connector.CodeScope {
		t.Fatalf("installing an installation-wide connector for an organisation: %v", err)
	}
	var c storage.InstalledConnector
	apply := func(st string, in connector.Input) error {
		return w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := s.Apply(ctx, u, c.ID, c.Version, st, in)
			if err == nil && st != storage.StepSecret {
				c = r.Connector
			}
			return err
		})
	}
	if err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		c, err = s.Install(ctx, u, "example-org")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		step string
		in   connector.Input
	}{
		{storage.StepConfigure, connector.Input{Configuration: map[string]any{"address": "smtp.example.com:587"}}},
		{storage.StepTest, connector.Input{Test: &storage.ConnectorTest{Passed: true, Reason: "Accepted.", At: w.clock.Now()}}},
	} {
		if err := apply(x.step, x.in); err != nil {
			t.Fatalf("%s: %v", x.step, err)
		}
	}
	var ref ports.SecretRef
	if err := w.do(t, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		ref, err = s.PutSecret(ctx, u, w.org, c, "key", "org-key-one")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := apply(storage.StepSecret, connector.Input{Secret: "key", Ref: ref}); err != nil {
		t.Fatal(err)
	}
	if err := apply(storage.StepEnable, connector.Input{}); err != nil || c.State != storage.ConnectorEnabled {
		t.Fatalf("enable: %+v, %v", c, err)
	}
	if err := apply(storage.StepDisable, connector.Input{}); err != nil || c.State != storage.ConnectorDisabled || !slices.Equal(cancelled, []uuid.UUID{c.ID}) {
		t.Fatalf("disable: %+v, %v, cancelled %v", c, err, cancelled)
	}
	if v, err := w.secrets.Get(ctx, ref); err != nil || v != "org-key-one" || string(ref)[:len("local:org-")] != "local:org-" {
		t.Fatalf("the organisation's key: %q %q %v", ref, v, err)
	}
}
