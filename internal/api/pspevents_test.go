//go:build local

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/pspevents"
)

// fakeProvider is the provider behind the example-psp connector: the objects it has, and how often each was fetched. It
// has no method that writes.
type fakeProvider struct {
	mu      sync.Mutex
	objects map[string]ports.ProviderObject
	fetched map[string]int
}

func (f *fakeProvider) Name() string { return "example-psp" }
func (f *fakeProvider) Object(_ context.Context, kind ports.ProviderKind, id string) (ports.ProviderObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched[id]++
	o, ok := f.objects[id]
	if !ok {
		return ports.ProviderObject{}, ports.ErrProviderNotFound
	}
	return o, nil
}
func (f *fakeProvider) List(context.Context, ports.ProviderKind, time.Time, time.Time, string) (ports.ProviderPage, error) {
	return ports.ProviderPage{}, nil
}
func (f *fakeProvider) Balance(context.Context) ([]ports.ProviderBalance, error) { return nil, nil }

// fakePSPConnector is a payment-provider connector over fp, whose webhooks carry X-Signature: good and whose events are
// {"id","type","object":"<id>"}: invoice.* names an invoice, balance.available is informational, anything else unknown.
func fakePSPConnector(t testing.TB, fp *fakeProvider) connector.Binding {
	t.Helper()
	m, err := connector.Parse([]byte(`{"id":"example-psp","name":"Example PSP","publisher":"Fyniq","version":"1.0.0","min_platform":"0.1.0",
	  "summary":"A fake payment provider.","scope":"organisation","capabilities":[{"kind":"payment-provider","trigger":"system-rule",
	  "trigger_rule":"A verified webhook.","on_pause":"hold","on_disable":"hold","in_flight":"finish","parser_version":"example@1"}],
	  "configuration":{"type":"object","additionalProperties":false,"properties":{}},
	  "secrets":[{"name":"token","title":"Token","description":"Its read-only key.","used_by":["worker"],"rotation_overlap_hours":1}],
	  "provider_scopes":[],"fyniq_scopes":[],"data_classes":[],"regions":["SG"],
	  "destinations":[{"host":"psp.example.com"}],"webhooks":[],"health_check":"Counts.","test_action":"Reads.","local_substitute":"A fake.",
	  "limits":{"requests_per_minute":600,"breaker_failures":5,"breaker_cooldown_seconds":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	return connector.Binding{Manifest: m, PaymentProvider: func(connector.Config) (ports.PaymentProvider, error) { return fp, nil },
		VerifyWebhook: func(_ connector.Config, h http.Header, _ []byte, _ time.Time) error {
			if h.Get("X-Signature") != "good" {
				return errors.New("bad signature")
			}
			return nil
		},
		ParseEvent: func(_ http.Header, body []byte) (ports.ProviderEvent, error) {
			var d struct{ ID, Type, Object string }
			if err := json.Unmarshal(body, &d); err != nil || d.ID == "" {
				return ports.ProviderEvent{}, errors.New("not an event")
			}
			e := ports.ProviderEvent{ID: d.ID, Type: d.Type, ObjectID: d.Object, APIVersion: "2026-01"}
			switch {
			case strings.HasPrefix(d.Type, "invoice."):
				e.Known, e.Handled, e.Kind = true, true, ports.ProviderInvoice
			case d.Type == "balance.available":
				e.Known = true
			}
			return e, nil
		}}
}

type recordingHandler struct {
	mu    sync.Mutex
	calls []string
	park  string
}

func (h *recordingHandler) Handle(_ context.Context, _ storage.UnitOfWork, _ storage.InstalledConnector, ev storage.PSPEvent, obj ports.ProviderObject) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, ev.EventID+"->"+obj.ID+":"+obj.Amount)
	return h.park, nil
}

// FR-PSP-01, UC-PSP-01, G4-040, G4-107: a verified provider webhook is stored once per event ID, as received (its raw
// payload sealed, with the provider's API version and the parser's version) and queued, and a redelivery answers 2xx and
// changes nothing; an event whose type the parser does not know is parked and posts nothing; an informational one is
// processed; a bad signature, an unknown connector, another connector's name, an oversized body and a disabled
// connector are refused and store nothing. The worker fetches the object the event names (never trusting the payload),
// hands it to the rule of its kind once, and a second run of the same row does nothing; an object the provider does not
// have fails the event for a retry and parks it when the retries run out.
// req: FR-PSP-01
// req: FR-INT-27
// asvs: V4.1.5
func TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{objects: map[string]ports.ProviderObject{
		"in_1": {Kind: ports.ProviderInvoice, ID: "in_1", Amount: "49.00", Currency: "SGD", Links: map[ports.ProviderKind][]string{}},
	}, fetched: map[string]int{}}
	b := fakePSPConnector(t, fp)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	var c storage.InstalledConnector
	m := b.Manifest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested},
			{storage.StepEnable, storage.ConnectorEnabled}} {
			if step.s == storage.StepTest {
				c.Test = &storage.ConnectorTest{Passed: true, Reason: "Read.", At: e.clock.Now()}
			}
			if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state,
				Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(), SecretNames: []string{"token"},
				Consent: m.Consent(), At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	disp := outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}
	mux := http.NewServeMux()
	(&pspevents.Hooks{Store: e.store, Routes: e.store, Runtime: rt, Dispatcher: disp, Clock: e.clock, Fields: e.api.deps.Fields}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	post := func(name string, id uuid.UUID, signature, body string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/psp/"+name+"/"+id.String(), strings.NewReader(body))
		req.Header.Set("X-Signature", signature)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	events := func() []storage.PSPEvent {
		var out []storage.PSPEvent
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.PSPEvents().List(ctx, storage.PSPEventFilter{Connector: c.ID})
			return err
		})
		return out
	}
	const secretNote = "Ada Lovelace, 1 Analytical Way"
	body := func(id, typ, obj string) string {
		return `{"id":"` + id + `","type":"` + typ + `","object":"` + obj + `","customer":"` + secretNote + `"}`
	}

	// A verified, handled event is stored as received and queued; a redelivery is 2xx and changes nothing.
	drainWork(t, e)
	if code := post("example-psp", c.ID, "good", body("evt_1", "invoice.paid", "in_1")); code != http.StatusOK {
		t.Fatalf("a verified event: %d", code)
	}
	if code := post("example-psp", c.ID, "good", body("evt_1", "invoice.paid", "in_1")); code != http.StatusOK {
		t.Fatalf("a redelivery: %d", code)
	}
	evs := events()
	if len(evs) != 1 || evs[0].Status != storage.PSPReceived || evs[0].EventID != "evt_1" || evs[0].ParserVersion != "example@1" ||
		evs[0].APIVersion != "2026-01" || evs[0].ObjectKind != "invoice" || evs[0].ObjectID != "in_1" || len(evs[0].PayloadSHA256) != 64 {
		t.Fatalf("stored: %+v", evs)
	}
	var sealed []byte
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		sealed, err = u.PSPEvents().Payload(ctx, evs[0].ID)
		return err
	})
	if strings.Contains(string(sealed), secretNote) {
		t.Fatal("the payload is stored in the clear")
	}
	plain, err := e.api.deps.Fields.Open(context.Background(), sealed, pspevents.Aad(e.books.Org, evs[0].ID))
	if err != nil || !strings.Contains(string(plain), secretNote) {
		t.Fatalf("the payload does not open as received: %v", err)
	}
	if _, err := e.api.deps.Fields.Open(context.Background(), sealed, pspevents.Aad(e.books.Org, uuid.New())); err == nil {
		t.Fatal("a payload opened against another event's row")
	}
	kind, row := nextWorkRow(t, e)
	if kind != pspevents.Kind || !strings.Contains(string(row.Payload), evs[0].ID.String()) {
		t.Fatalf("queued %q %s", kind, row.Payload)
	}
	if k := nextWork(t, e); k != "" {
		t.Fatalf("a redelivery queued %s", k)
	}

	// An unknown type is parked and queues nothing; an informational one is processed.
	if code := post("example-psp", c.ID, "good", body("evt_2", "radar.thing", "x_1")); code != http.StatusOK {
		t.Fatalf("an unknown type: %d", code)
	}
	if code := post("example-psp", c.ID, "good", body("evt_3", "balance.available", "")); code != http.StatusOK {
		t.Fatalf("an informational event: %d", code)
	}
	byID := map[string]storage.PSPEvent{}
	for _, x := range events() {
		byID[x.EventID] = x
	}
	if x := byID["evt_2"]; x.Status != storage.PSPParked || !strings.Contains(x.ParkedReason, "does not know this event type") {
		t.Errorf("unknown type: %+v", x)
	}
	if x := byID["evt_3"]; x.Status != storage.PSPProcessed || x.ProcessedAt == nil {
		t.Errorf("informational: %+v", x)
	}
	if k := nextWork(t, e); k != "" {
		t.Fatalf("a parked or informational event queued %s", k)
	}

	// What is refused stores nothing.
	before := len(events())
	for name, c := range map[string]struct {
		connector string
		id        uuid.UUID
		signature string
		body      string
		want      int
	}{
		"an unknown connector":   {"example-psp", uuid.New(), "good", body("evt_9", "invoice.paid", "in_1"), http.StatusNotFound},
		"another connector":      {"stripe", c.ID, "good", body("evt_9", "invoice.paid", "in_1"), http.StatusNotFound},
		"a signature that fails": {"example-psp", c.ID, "forged", body("evt_9", "invoice.paid", "in_1"), http.StatusUnauthorized},
		"a body too large":       {"example-psp", c.ID, "good", `{"id":"evt_9","type":"invoice.paid","pad":"` + strings.Repeat("x", pspevents.MaxBody) + `"}`, http.StatusRequestEntityTooLarge},
		"not an event":           {"example-psp", c.ID, "good", `[1,2]`, http.StatusBadRequest},
	} {
		if code := post(c.connector, c.id, c.signature, c.body); code != c.want {
			t.Errorf("%s: %d; want %d", name, code, c.want)
		}
	}
	if after := len(events()); after != before {
		t.Errorf("a refused webhook stored %d event(s)", after-before)
	}

	// The worker fetches the object the event names and hands it to the rule of its kind once.
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	h := &recordingHandler{}
	proc := pspevents.Processor{Store: worker, Runtime: rt, Clock: e.clock, Handlers: map[ports.ProviderKind]pspevents.Handler{ports.ProviderInvoice: h}}
	if err := proc.Work()(context.Background(), e.books.Org, row); err != nil {
		t.Fatal(err)
	}
	if err := proc.Work()(context.Background(), e.books.Org, row); err != nil { // the row delivered twice
		t.Fatal(err)
	}
	if len(h.calls) != 1 || h.calls[0] != "evt_1->in_1:49.00" || fp.fetched["in_1"] != 1 {
		t.Fatalf("the rule ran %v, the provider was asked %v", h.calls, fp.fetched)
	}
	for _, x := range events() {
		if x.EventID == "evt_1" && (x.Status != storage.PSPProcessed || x.ProcessedAt == nil) {
			t.Errorf("worked event: %+v", x)
		}
	}

	// An object the provider does not have fails the event for a retry, and parks it when the retries run out.
	if code := post("example-psp", c.ID, "good", body("evt_4", "invoice.paid", "in_missing")); code != http.StatusOK {
		t.Fatal(code)
	}
	k, row4 := nextWorkRow(t, e)
	if k != pspevents.Kind {
		t.Fatalf("queued %q", k)
	}
	if err := proc.Work()(context.Background(), e.books.Org, row4); !errors.Is(err, ports.ErrProviderNotFound) {
		t.Fatalf("a missing object: %v", err)
	}
	if x := eventByID(events(), "evt_4"); x.Status != storage.PSPFailed || x.LastError == "" {
		t.Fatalf("failed event: %+v", x)
	}
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return proc.Dead()(ctx, u, e.books.Org, row4, "dead after 5 attempts")
	}); err != nil {
		t.Fatal(err)
	}
	if x := eventByID(events(), "evt_4"); x.Status != storage.PSPParked || !strings.Contains(x.ParkedReason, "after its retries") {
		t.Fatalf("parked event: %+v", x)
	}

	// A rule that parks an object it cannot post parks its event with the reason.
	h.park = "the invoice has a usage price: the accountant decides"
	fp.objects["in_2"] = ports.ProviderObject{Kind: ports.ProviderInvoice, ID: "in_2", Amount: "10.00"}
	post("example-psp", c.ID, "good", body("evt_5", "invoice.finalized", "in_2"))
	_, row5 := nextWorkRow(t, e)
	if err := proc.Work()(context.Background(), e.books.Org, row5); err != nil {
		t.Fatal(err)
	}
	if x := eventByID(events(), "evt_5"); x.Status != storage.PSPParked || x.ParkedReason != h.park {
		t.Fatalf("parked by the rule: %+v", x)
	}

	// A disabled connector's webhook is refused.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: storage.StepDisable, State: storage.ConnectorDisabled,
			Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), At: e.clock.Now()})
		return err
	})
	if code := post("example-psp", c.ID, "good", body("evt_6", "invoice.paid", "in_1")); code != http.StatusNotFound {
		t.Errorf("a disabled connector's webhook: %d", code)
	}
}

func eventByID(all []storage.PSPEvent, id string) storage.PSPEvent {
	for _, x := range all {
		if x.EventID == id {
			return x
		}
	}
	return storage.PSPEvent{}
}
