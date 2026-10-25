package connector_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type memSecrets struct{ values map[ports.SecretRef]string }

func (m memSecrets) Put(context.Context, string, string) (ports.SecretRef, error) { return "", nil }
func (m memSecrets) Get(_ context.Context, ref ports.SecretRef) (string, error) {
	if v, ok := m.values[ref]; ok {
		return v, nil
	}
	return "", ports.ErrNotFound
}
func (m memSecrets) Latest(context.Context, string) (ports.SecretRef, error) {
	return "", ports.ErrNotFound
}
func (m memSecrets) Disable(context.Context, ports.SecretRef) error { return nil }
func (m memSecrets) Destroy(context.Context, ports.SecretRef) error { return nil }

// scriptedEmail answers each send with the next error of its script, nil for an acceptance.
type scriptedEmail struct {
	name   string
	script []error
	sent   *int
}

func (s scriptedEmail) Name() string { return s.name }
func (s scriptedEmail) Send(context.Context, ports.OutboundEmail) (ports.EmailReceipt, error) {
	i := *s.sent
	*s.sent++
	if i < len(s.script) {
		return ports.EmailReceipt{}, s.script[i]
	}
	return ports.EmailReceipt{ProviderID: "ok"}, nil
}
func (s scriptedEmail) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, ports.ErrEmailUnknown
}

func exampleBinding(t *testing.T, id string, change func(m map[string]any), build func(connector.Config) (ports.Email, error)) connector.Binding {
	t.Helper()
	m, err := connector.Parse(edit(t, func(m map[string]any) {
		m["id"] = id
		if change != nil {
			change(m)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	return connector.Binding{Manifest: m, OutboundEmail: build}
}

func runtimeFor(t *testing.T, c *clock.Frozen, bs ...connector.Binding) *connector.Runtime {
	t.Helper()
	cat, err := connector.NewCatalogue(bs...)
	if err != nil {
		t.Fatal(err)
	}
	return &connector.Runtime{Catalogue: cat, Secrets: connector.NewSecretCache(memSecrets{values: map[ports.SecretRef]string{"ref-1": "k"}}, c),
		Egress: &egress.Guard{Local: true}, SenderDomain: "mail.example.com", Clock: c}
}

func installedOf(b connector.Binding, state string, position int) storage.InstalledConnector {
	return storage.InstalledConnector{ID: uuid.New(), Installation: true, Connector: b.Manifest.ID, State: state, Position: position,
		ManifestVersion: b.Manifest.Version, ManifestHash: b.Manifest.Hash(), Consent: b.Manifest.Consent(),
		Configuration: map[string]any{"address": "smtp.example.com:587"}, SecretNames: []string{"key"}, Version: 1}
}

// The outbound email of a piece of work is the enabled connectors in order, the primary first; a paused or disabled
// one is never sent through but stays available to reconcile an earlier attempt; an uninstalled one is gone; a
// connector whose secret cannot be read is skipped with the reason, so core work goes on through the next.
// req: FR-INT-20
// req: FR-INT-26
// req: FR-INT-27
func TestTheMailOfAPieceOfWorkIsTheEnabledConnectorsInOrder(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))
	mk := func(id string) connector.Binding {
		return exampleBinding(t, id, nil, func(connector.Config) (ports.Email, error) { return scriptedEmail{name: id, sent: new(int)}, nil })
	}
	a, b, p, d, gone, unreadable := mk("alpha"), mk("beta"), mk("paused"), mk("disabled"), mk("gone"), mk("unreadable")
	r := runtimeFor(t, c, a, b, p, d, gone, unreadable)
	ia, ib := installedOf(a, storage.ConnectorEnabled, 2), installedOf(b, storage.ConnectorEnabled, 1)
	ip, id := installedOf(p, storage.ConnectorPaused, 3), installedOf(d, storage.ConnectorDisabled, 4)
	ig, iu := installedOf(gone, storage.ConnectorUninstalled, 5), installedOf(unreadable, storage.ConnectorEnabled, 6)
	secrets := []storage.ConnectorSecret{}
	for _, x := range []storage.InstalledConnector{ia, ib, ip, id, ig} {
		secrets = append(secrets, storage.ConnectorSecret{ConnectorID: x.ID, Name: "key", Ref: "ref-1"})
	}
	secrets = append(secrets, storage.ConnectorSecret{ConnectorID: iu.ID, Name: "key", Ref: "ref-missing"})
	mail, err := r.Mail(context.Background(), []storage.InstalledConnector{ia, ib, ip, id, ig, iu}, secrets, connector.OutboxEmail)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range mail.Enabled {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "beta,alpha" {
		t.Fatalf("enabled, in order: %v", names)
	}
	for _, want := range []string{"alpha", "beta", "paused", "disabled"} {
		if mail.Reconcile[want] == nil {
			t.Errorf("%s cannot reconcile an earlier attempt", want)
		}
	}
	if mail.Reconcile["gone"] != nil || len(mail.Skipped) != 1 || !strings.HasPrefix(mail.Skipped[0], "unreadable") {
		t.Fatalf("reconcile %v, skipped %v", mail.Reconcile, mail.Skipped)
	}
}

// Work acts only on the authority its capability's manifest names: a system rule where the manifest says so, a
// completed human authority with its approval, never a capability nobody may trigger, and never on an AI proposal
// alone.
// req: FR-INT-27
// req: AI-66
func TestTriggerMustMatchTheManifest(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))
	build := func(connector.Config) (ports.Email, error) { return scriptedEmail{name: "x", sent: new(int)}, nil }
	system := exampleBinding(t, "system", nil, build)
	human := exampleBinding(t, "human", func(m map[string]any) { capability(m)["trigger"] = "human-authority" }, build)
	nobody := exampleBinding(t, "nobody", func(m map[string]any) { capability(m)["trigger"] = "none" }, build)
	for _, x := range []struct {
		name string
		b    connector.Binding
		a    connector.Authority
		ok   bool
	}{
		{"a system rule", system, connector.OutboxEmail, true},
		{"a system capability from an AI proposal", system, connector.Authority{Trigger: connector.TriggerSystem, Rule: "x", FromAI: true}, false},
		{"a human capability on a system rule", human, connector.OutboxEmail, false},
		{"a human authority without its approval", human, connector.Authority{Trigger: connector.TriggerHuman}, false},
		{"a human authority with its approval", human, connector.Authority{Trigger: connector.TriggerHuman, Approval: uuid.New()}, true},
		{"an AI proposal a person approved", human, connector.Authority{Trigger: connector.TriggerHuman, Approval: uuid.New(), FromAI: true}, true},
		{"a capability nobody triggers", nobody, connector.OutboxEmail, false},
	} {
		r := runtimeFor(t, c, x.b)
		i := installedOf(x.b, storage.ConnectorEnabled, 1)
		_, err := r.Mail(context.Background(), []storage.InstalledConnector{i}, []storage.ConnectorSecret{{ConnectorID: i.ID, Name: "key", Ref: "ref-1"}}, x.a)
		if (err == nil) != x.ok || (err != nil && !errors.Is(err, connector.ErrTrigger)) {
			t.Errorf("%s: %v", x.name, err)
		}
	}
}

// A connector reaches only the destinations it consented to: a release whose manifest declares a new host runs under
// the old consent, and the new host is refused until someone consents; the diff shows what the release asks.
// req: FR-INT-26
// req: SEC-36
// asvs: V13.2.4
func TestWideningManifestRunsUnderTheOldConsent(t *testing.T) {
	old := storage.ConnectorConsent{DataClasses: []string{"personal-data"}, Destinations: []storage.ConsentDestination{{Host: "api.example.com", Port: 443}}}
	release := exampleBinding(t, "widening", func(m map[string]any) {
		m["destinations"] = []any{map[string]any{"host": "api.example.com", "port": 443}, map[string]any{"host": "*.tracking.example.com"}}
		m["data_classes"] = []any{"personal-data", "business-figures"}
	}, func(connector.Config) (ports.Email, error) { return nil, nil })
	allow := connector.Allows(old, nil)
	if !allow("api.example.com", 443) || allow("api.example.com", 80) || allow("pixel.tracking.example.com", 443) {
		t.Fatal("the old consent's destinations")
	}
	if !connector.Allows(release.Manifest.Consent(), nil)("pixel.tracking.example.com", 443) {
		t.Fatal("the new consent's subdomains")
	}
	d := connector.Diff(old, release.Manifest)
	if !d.Widens() || len(d.Added.Destinations) != 1 || d.Added.Destinations[0].Host != "*.tracking.example.com" ||
		len(d.Added.DataClasses) != 1 || d.Added.DataClasses[0] != "business-figures" || len(d.Removed.Destinations) != 0 {
		t.Fatalf("the diff: %+v", d)
	}
	if same := connector.Diff(release.Manifest.Consent(), release.Manifest); same.Widens() {
		t.Fatalf("a manifest compared with its own consent: %+v", same)
	}
	configured := connector.Allows(storage.ConnectorConsent{Destinations: []storage.ConsentDestination{{Configured: "address"}}},
		map[string]any{"address": "smtp.example.com:587"})
	if !configured("smtp.example.com", 587) || configured("other.example.com", 587) {
		t.Fatal("a configured destination")
	}
}

// The breaker opens after the manifest's consecutive transport failures and holds sends until its cool-down, then lets
// one trial through; a held send is a refusal, never handed over, so the failover may send it. A provider's own refusal
// is an answer and never opens the breaker. The rate limit holds sends beyond the manifest's requests a minute.
// req: FR-INT-27
func TestBreakerFailsOverOnlyWhatWasNeverHandedOver(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))
	sent := new(int)
	lost := errors.New("connection reset after the data")
	script := []error{ports.ErrEmailRefused, ports.ErrEmailRefused, lost, lost, lost, lost, lost}
	b := exampleBinding(t, "flaky", func(m map[string]any) {
		m["limits"] = map[string]any{"requests_per_minute": 100, "breaker_failures": 5, "breaker_cooldown_seconds": 60}
	}, func(connector.Config) (ports.Email, error) {
		return scriptedEmail{name: "flaky", script: script, sent: sent}, nil
	})
	r := runtimeFor(t, c, b)
	i := installedOf(b, storage.ConnectorEnabled, 1)
	mail := func() ports.Email {
		m, err := r.Mail(context.Background(), []storage.InstalledConnector{i}, []storage.ConnectorSecret{{ConnectorID: i.ID, Name: "key", Ref: "ref-1"}}, connector.OutboxEmail)
		if err != nil || len(m.Enabled) != 1 {
			t.Fatal(m, err)
		}
		return m.Enabled[0]
	}
	ctx := context.Background()
	for range 2 { // refusals: answers, which never open the breaker
		if _, err := mail().Send(ctx, ports.OutboundEmail{}); !errors.Is(err, ports.ErrEmailRefused) {
			t.Fatal(err)
		}
	}
	for range 5 {
		if _, err := mail().Send(ctx, ports.OutboundEmail{}); !errors.Is(err, lost) {
			t.Fatalf("a transport failure: %v", err)
		}
	}
	before := *sent
	_, err := mail().Send(ctx, ports.OutboundEmail{})
	if !errors.Is(err, ports.ErrEmailRefused) || !errors.Is(err, connector.ErrHeld) || *sent != before {
		t.Fatalf("an open breaker: %v after %d sends", err, *sent-before)
	}
	c.Advance(time.Minute)
	if _, err := mail().Send(ctx, ports.OutboundEmail{}); err != nil || *sent != before+1 {
		t.Fatalf("the trial after the cool-down: %v", err)
	}
	if _, err := mail().Send(ctx, ports.OutboundEmail{}); err != nil {
		t.Fatalf("after a successful trial: %v", err)
	}

	slow := exampleBinding(t, "slow", func(m map[string]any) {
		m["limits"] = map[string]any{"requests_per_minute": 2, "breaker_failures": 5, "breaker_cooldown_seconds": 60}
	}, func(connector.Config) (ports.Email, error) { return scriptedEmail{name: "slow", sent: new(int)}, nil })
	rs := runtimeFor(t, c, slow)
	is := installedOf(slow, storage.ConnectorEnabled, 1)
	send := func() error {
		m, _ := rs.Mail(ctx, []storage.InstalledConnector{is}, []storage.ConnectorSecret{{ConnectorID: is.ID, Name: "key", Ref: "ref-1"}}, connector.OutboxEmail)
		_, err := m.Enabled[0].Send(ctx, ports.OutboundEmail{})
		return err
	}
	if send() != nil || send() != nil || !errors.Is(send(), connector.ErrHeld) {
		t.Fatal("the third send within a minute of a 2-a-minute connector")
	}
	c.Advance(30 * time.Second)
	if err := send(); err != nil {
		t.Fatalf("a token back after half a minute: %v", err)
	}
}

// The test action answers pass or fail with Fyniq's own reason, never what the destination sent back, and is refused
// before connecting to an address the connector may not reach.
// req: SEC-36
// req: FR-INT-20
// asvs: V15.3.1
func TestTestActionNeverReturnsTheBody(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"internal-account-4242 belongs to acme; your key sk_xyz is invalid"}`))
	}))
	defer srv.Close()
	var b connector.Binding
	b = exampleBinding(t, "probe", func(m map[string]any) {
		m["destinations"] = []any{map[string]any{"configured": "address"}}
	}, func(cfg connector.Config) (ports.Email, error) {
		return httpEmail{url: cfg.String("address", ""), client: cfg.HTTP}, nil
	})
	r := runtimeFor(t, c, b)
	i := installedOf(b, storage.ConnectorConfigured, 1)
	i.Configuration = map[string]any{"address": srv.URL}
	got := r.TestEmail(context.Background(), i, nil, "founder@example.com", "Fyniq <no-reply@mail.example.com>")
	if got.Passed || strings.Contains(got.Reason, "acme") || strings.Contains(got.Reason, "sk_xyz") || !strings.Contains(got.Reason, "refused") {
		t.Fatalf("a refused test: %+v", got)
	}
	i.Configuration = map[string]any{"address": "http://169.254.169.254/computeMetadata/v1/"}
	if got := r.TestEmail(context.Background(), i, nil, "founder@example.com", "Fyniq <no-reply@mail.example.com>"); got.Passed ||
		!strings.Contains(got.Reason, "Fyniq refused the connection") {
		t.Fatalf("a test aimed at the metadata service: %+v", got)
	}
}

// httpEmail posts a message to url, and refuses on a 4xx, as a provider's adapter does.
type httpEmail struct {
	url    string
	client *http.Client
}

func (h httpEmail) Name() string { return "probe" }
func (h httpEmail) Send(ctx context.Context, _ ports.OutboundEmail) (ports.EmailReceipt, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, strings.NewReader("{}"))
	if err != nil {
		return ports.EmailReceipt{}, err
	}
	res, err := h.client.Do(req)
	if err != nil {
		return ports.EmailReceipt{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 400 && res.StatusCode < 500 {
		return ports.EmailReceipt{}, errors.Join(ports.ErrEmailRefused, errors.New(res.Status))
	}
	return ports.EmailReceipt{ProviderID: "x"}, nil
}
func (h httpEmail) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, ports.ErrEmailUnknown
}

// A connector's work is audited as the connector itself, on behalf of whoever made it act.
// req: FR-INT-27
// req: AUD-01
func TestConnectorWorkIsAuditedAsItsOwnPrincipal(t *testing.T) {
	c := storage.InstalledConnector{ID: uuid.MustParse("01928f6e-0000-7000-8000-000000000001")}
	ctx := connector.Principal(context.Background(), c, &storage.Actor{Type: "user", ID: "u-1"})
	a := storage.AuditFrom(ctx)
	if a.Actor == nil || a.Actor.String() != "integration:connector/01928f6e-0000-7000-8000-000000000001" || a.OnBehalfOf == nil || a.OnBehalfOf.ID != "u-1" {
		t.Fatalf("%+v", a)
	}
}
