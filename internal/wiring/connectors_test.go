//go:build local

package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type connectorWorld struct {
	db          *pgtest.DB
	api, worker *postgres.Store
	org         uuid.UUID
	secrets     *local.SecretFiles
	clock       *clock.Frozen
	superAdmin  uuid.UUID
}

// startConnectors starts a database whose installation organisation is set, as first-run setup leaves it, and one Super
// admin: as the superuser, with triggers off for that one transaction, since no setup code is involved.
func startConnectors(t *testing.T, withTrail bool) connectorWorld {
	t.Helper()
	ctx := context.Background()
	db, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("the connector wiring tests need Docker: %v", err)
	}
	t.Cleanup(db.Stop)
	api, err := postgres.Open(ctx, db.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	worker, err := postgres.Open(ctx, db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	org, err := api.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Installation Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := local.NewSecretFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := connectorWorld{db: db, api: api, worker: worker, org: org.ID, secrets: secrets, clock: clock.NewFrozen(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))}
	c, err := pgx.Connect(ctx, db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string, args ...any) {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("SET LOCAL session_replication_role = replica")
	if withTrail {
		exec(`INSERT INTO fyniq.installation (singleton, setup_completed_at, installation_org) VALUES (true, now(), $1)
		      ON CONFLICT (singleton) DO UPDATE SET setup_code_hash = NULL, setup_completed_at = now(), installation_org = $1`, org.ID)
	}
	if err := tx.QueryRow(ctx, "INSERT INTO fyniq.app_user (display_name) VALUES ('Super admin') RETURNING user_id").Scan(&w.superAdmin); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO fyniq.super_admin (user_id) VALUES ($1)", w.superAdmin)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return w
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// The first start installs the email connectors from the deployment's settings: the providers in their order, enabled,
// their endpoints and DNS records from the same variables, each key a reference to the newest version of the secret
// FYNIQ_CONNECTOR_SECRETS names, never its value; audited as the service. It runs once: a second start installs nothing,
// and nothing more reaches Secret Manager. Before first-run setup it waits.
// req: FR-INT-21
// req: FR-INT-20
// asvs: V13.3.1
func TestBootstrapFromTheDeployment(t *testing.T) {
	ctx := context.Background()
	w := startConnectors(t, true)
	cat, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := w.secrets.Put(ctx, "zeptomail-key", "the-deployments-send-token")
	if err != nil {
		t.Fatal(err)
	}
	k := &Kit{Store: w.api, Connectors: cat, ConnectorSecrets: w.secrets, ConnectorNames: map[string]string{"zeptomail.send_token": "zeptomail-key"},
		Egress: &egress.Guard{Local: true}}
	deployment := env(map[string]string{"FYNIQ_EMAIL_PROVIDERS": "zeptomail,smtp", "FYNIQ_SMTP_ADDR": "smtp.example.com:587",
		"FYNIQ_ZEPTOMAIL_URL": "https://cpaas.zoho.com/v1.1/email",
		"FYNIQ_MAIL_DNS":      `{"mail.example.com":[{"type":"TXT","name":"zm._domainkey.mail.example.com","value":"v=DKIM1"}]}`})
	// A key whose secret has no version: nothing is installed, so the deployment's providers keep sending.
	missing := &Kit{Store: w.api, Connectors: cat, ConnectorSecrets: w.secrets, ConnectorNames: map[string]string{"zeptomail.send_token": "connector-zeptomail-unset"},
		Egress: &egress.Guard{Local: true}}
	if n, err := BootstrapConnectors(ctx, missing, deployment, w.clock); err == nil || n != 0 || !strings.Contains(err.Error(), "connector-zeptomail-unset") {
		t.Fatalf("a bootstrap without the key: %d, %v", n, err)
	}
	n, err := BootstrapConnectors(ctx, k, deployment, w.clock)
	if err != nil || n != 2 {
		t.Fatalf("the bootstrap: %d, %v", n, err)
	}
	var all []storage.InstalledConnector
	var versions []storage.ConnectorSecret
	if err := w.api.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if all, err = u.Connectors().Installation(ctx); err != nil {
			return err
		}
		versions, err = u.Connectors().InstallationSecrets(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Connector != "zeptomail" || all[0].Position != 1 || all[1].Connector != "smtp" || all[0].State != storage.ConnectorEnabled ||
		all[0].InstalledBy != "service:api" || all[0].Configuration["dns_records"] == nil || all[1].Configuration["address"] != "smtp.example.com:587" ||
		len(all[0].Consent.Destinations) != 1 {
		t.Fatalf("installed: %+v", all)
	}
	if len(versions) != 1 || versions[0].Ref != ref || versions[0].Name != "send_token" {
		t.Fatalf("the key, by reference: %+v", versions)
	}
	raw, _ := json.Marshal(all)
	if strings.Contains(string(raw), "the-deployments-send-token") {
		t.Fatal("the key's value reached the database")
	}
	if n, err := BootstrapConnectors(ctx, k, deployment, w.clock); err != nil || n != 0 {
		t.Fatalf("a second start: %d, %v", n, err)
	}
	if drift, err := EmailDrift(ctx, k, deployment); err != nil || drift != "" {
		t.Fatalf("the variables as installed: %q, %v", drift, err)
	}
	if drift, _ := EmailDrift(ctx, k, env(map[string]string{"FYNIQ_EMAIL_PROVIDERS": "resend"})); !strings.Contains(drift, "no longer read") {
		t.Fatalf("variables that differ: %q", drift)
	}
	early := startConnectors(t, false)
	k2 := &Kit{Store: early.api, Connectors: cat, ConnectorSecrets: early.secrets, Egress: &egress.Guard{Local: true}}
	if n, err := BootstrapConnectors(ctx, k2, deployment, early.clock); err != nil || n != 0 {
		t.Fatalf("before first-run setup: %d, %v", n, err)
	}
}

// fake is an email provider that counts its sends and answers Status from its record of what it took.
type fake struct {
	name  string
	sends *int
	fail  error
	took  map[string]bool
}

func (f fake) Name() string { return f.name }
func (f fake) Send(_ context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	*f.sends++
	if f.fail != nil {
		return ports.EmailReceipt{}, f.fail
	}
	f.took[m.Key] = true
	return ports.EmailReceipt{ProviderID: f.name + "-1"}, nil
}
func (f fake) Status(_ context.Context, m ports.OutboundEmail, _ time.Time) (ports.EmailReceipt, error) {
	if f.took[m.Key] {
		return ports.EmailReceipt{ProviderID: f.name + "-1"}, nil
	}
	return ports.EmailReceipt{}, ports.ErrEmailRefused
}

func fakeManifest(t *testing.T, id string) connector.Manifest {
	t.Helper()
	m, err := connector.Parse([]byte(`{"id":"` + id + `","name":"` + id + `","publisher":"Fyniq","version":"1.0.0","min_platform":"0.1.0",
	  "summary":"A fake.","scope":"installation","capabilities":[{"kind":"outbound-email","trigger":"system-rule",
	  "trigger_rule":"A message the outbox holds.","on_pause":"hold","on_disable":"hold","in_flight":"reconcile"}],
	  "configuration":{"type":"object","additionalProperties":false,"properties":{}},"secrets":[],"provider_scopes":[],"fyniq_scopes":[],
	  "data_classes":["personal-data"],"regions":["SG"],"destinations":[{"host":"` + id + `.example.com"}],"webhooks":[],
	  "health_check":"Counts.","test_action":"Sends.","local_substitute":"A fake.",
	  "limits":{"requests_per_minute":600,"breaker_failures":5,"breaker_cooldown_seconds":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Email goes through the connectors enabled when each send starts (G4-096): a paused primary is skipped and the
// failover sends; with every connector paused or disabled a queued message waits, held, and nothing new is sent; a
// message an earlier attempt may have handed to a connector disabled since is reconciled with it, never dropped or sent
// again. Until the bootstrap has installed connectors, the deployment's providers send.
// req: FR-INT-20
// req: FR-INT-26
// req: QA-SEC-04
// asvs: V2.3.2
func TestEmailGoesThroughTheConnectorsEnabledAtEachSend(t *testing.T) {
	ctx := context.Background()
	w := startConnectors(t, true)
	alphaSends, betaSends, fallbackSends := new(int), new(int), new(int)
	lost := errors.New("the connection broke after the data")
	alphaTook := map[string]bool{}
	alphaFail := lost
	alpha := connector.Binding{Manifest: fakeManifest(t, "alpha"), OutboundEmail: func(connector.Config) (ports.Email, error) {
		return fake{name: "alpha", sends: alphaSends, fail: alphaFail, took: alphaTook}, nil
	}}
	beta := connector.Binding{Manifest: fakeManifest(t, "beta"), OutboundEmail: func(connector.Config) (ports.Email, error) {
		return fake{name: "beta", sends: betaSends, took: map[string]bool{}}, nil
	}}
	cat, err := connector.NewCatalogue(alpha, beta)
	if err != nil {
		t.Fatal(err)
	}
	k := &Kit{Store: w.worker, Connectors: cat, ConnectorSecrets: w.secrets, Egress: &egress.Guard{Local: true},
		Mail: MailConfig{From: "Fyniq <no-reply@mail.example.com>", Providers: []ports.Email{fake{name: "smtp", sends: fallbackSends, took: map[string]bool{}}}}}
	work := email.WorkFrom(k.EmailProviders(k.Runtime(w.clock)), w.worker, k.Mail.From, w.clock, nil)
	d := outbox.Dispatcher{Store: w.api, Queue: local.NewMemory(), Clock: w.clock}
	send := func(to string) (uuid.UUID, error) {
		var m storage.EmailMessage
		if err := w.api.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			m, err = email.Queue(ctx, d, u, w.org, "notification", to, "Hello", "Hello.\n")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return m.ID, work(ctx, w.org, storage.OutboxRow{ID: uuid.New(), Payload: payload(t, m.ID, to), MaxAttempts: 5, Attempts: 1})
	}
	status := func(id uuid.UUID) storage.EmailMessage {
		var m storage.EmailMessage
		_ = w.api.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			m, err = u.EmailMessages().Get(ctx, id)
			return err
		})
		return m
	}

	// No connector installed yet: the deployment's providers send.
	if _, err := send("a@example.com"); err != nil || *fallbackSends != 1 {
		t.Fatalf("before the bootstrap: %v, %d sends", err, *fallbackSends)
	}

	s := connector.Installations{Catalogue: cat, Secrets: w.secrets, Admins: w.api, Clock: w.clock}
	ids := map[string]storage.InstalledConnector{}
	for _, id := range []string{"alpha", "beta"} {
		var c storage.InstalledConnector
		if err := w.api.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := s.Install(ctx, u, w.superAdmin, w.clock.Now(), id)
			c = r.Connector
			for _, x := range []struct {
				step string
				in   connector.Input
			}{
				{storage.StepConfigure, connector.Input{Configuration: map[string]any{}}},
				{storage.StepTest, connector.Input{Test: &storage.ConnectorTest{Passed: true, Reason: "Accepted.", At: w.clock.Now()}}},
				{storage.StepEnable, connector.Input{}},
			} {
				if err != nil {
					return err
				}
				r, err = s.Step(ctx, u, w.superAdmin, w.clock.Now(), c.ID, c.Version, x.step, x.in, "")
				c = r.Connector
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		ids[id] = c
	}
	step := func(id, st string) {
		c := ids[id]
		if err := w.api.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := s.Step(ctx, u, w.superAdmin, w.clock.Now(), c.ID, c.Version, st, connector.Input{}, "")
			ids[id] = r.Connector
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	// alpha, the primary, loses a send: the attempt stays marked with alpha, in flight.
	inFlight, err := send("b@example.com")
	if !errors.Is(err, lost) || *alphaSends != 1 || status(inFlight).Provider != "alpha" || status(inFlight).AttemptStartedAt == nil {
		t.Fatalf("a lost send: %v, %d sends, %+v", err, *alphaSends, status(inFlight))
	}
	alphaFail = nil

	// alpha paused: the failover sends; disabled with beta paused: a queued message waits and nothing is sent.
	step("alpha", storage.StepPause)
	if id, err := send("c@example.com"); err != nil || *betaSends != 1 || *alphaSends != 1 || status(id).Provider != "beta" {
		t.Fatalf("a paused primary: %v, alpha %d, beta %d", err, *alphaSends, *betaSends)
	}
	step("alpha", storage.StepDisable)
	step("beta", storage.StepPause)
	held, err := send("d@example.com")
	if !errors.Is(err, outbox.ErrHeld) || *alphaSends != 1 || *betaSends != 1 || status(held).Status != storage.EmailQueued || status(held).AttemptStartedAt != nil {
		t.Fatalf("every connector stopped: %v, alpha %d, beta %d, %+v", err, *alphaSends, *betaSends, status(held))
	}

	// The in-flight message is reconciled with alpha, disabled since: alpha says it took it.
	alphaTook[inFlight.String()] = true
	if err := work(ctx, w.org, storage.OutboxRow{ID: uuid.New(), Payload: payload(t, inFlight, "b@example.com"), MaxAttempts: 5, Attempts: 2}); err != nil {
		t.Fatal(err)
	}
	if m := status(inFlight); m.Status != storage.EmailAccepted || m.Provider != "alpha" || *alphaSends != 1 {
		t.Fatalf("reconciled with a disabled connector: %+v, %d sends", m, *alphaSends)
	}

	// Resumed: the held message goes.
	step("beta", storage.StepResume)
	if err := work(ctx, w.org, storage.OutboxRow{ID: uuid.New(), Payload: payload(t, held, "d@example.com"), MaxAttempts: 5, Attempts: 2}); err != nil || status(held).Status != storage.EmailAccepted {
		t.Fatalf("after resuming: %v, %+v", err, status(held))
	}
}

func payload(t *testing.T, id uuid.UUID, to string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"message_id": id, "to": to, "subject": "Hello", "text": "Hello.\n"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
