//go:build local

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/webhooks"
)

// receiver is a webhook endpoint under test: it keeps what it was sent and answers as its mode says.
type receiver struct {
	srv  *httptest.Server
	mu   sync.Mutex
	mode string // "ok", "fail", "gone" or "silent" (2xx without the challenge)
	got  []receivedHook
}

type receivedHook struct {
	Header http.Header
	Body   []byte
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{mode: "ok"}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, receivedHook{Header: req.Header.Clone(), Body: b})
		mode := r.mode
		r.mu.Unlock()
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Challenge string `json:"challenge"`
			} `json:"data"`
		}
		_ = json.Unmarshal(b, &ev)
		switch {
		case mode == "fail":
			http.Error(w, "down", http.StatusServiceUnavailable)
		case mode == "gone":
			http.Error(w, "gone", http.StatusGone)
		case ev.Type == webhooks.VerifyType && mode == "ok":
			_, _ = w.Write([]byte(`{"challenge":"` + ev.Data.Challenge + `"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) set(mode string) { r.mu.Lock(); r.mode = mode; r.mu.Unlock() }
func (r *receiver) count() int      { r.mu.Lock(); defer r.mu.Unlock(); return len(r.got) }
func (r *receiver) last() receivedHook {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got[len(r.got)-1]
}

// webhookEnv is an organisation with webhooks switched on (the Owner's second approver is the setting's own test), an Administrator stepped up, and the worker's pieces.
type webhookEnv struct {
	e       *env
	admin   string
	worker  storage.Store
	version int
}

func newWebhookEnv(t *testing.T) *webhookEnv {
	e := newEnv(t)
	return &webhookEnv{e: e, admin: steppedUp(e, e.as(authz.Administrator)), worker: workerStore(t)}
}

func (w *webhookEnv) switchOn(on string) {
	w.e.t.Helper()
	y, m, d := w.e.clock.Now().In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	was := map[string]string{"true": "false", "false": "true"}[on]
	w.version++
	w.e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "features.webhooks", was, on, from, w.version)
		return err
	})
}

// emit records one event and runs the fan-out the worker runs for it.
func (w *webhookEnv) emit(title string) uuid.UUID {
	w.e.t.Helper()
	rec := uuid.New()
	w.e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := notify.Emit(ctx, outbox.Dispatcher{Store: w.e.store, Queue: w.e.queue, Clock: w.e.clock}, u, w.e.books.Org,
			notify.Event{Type: "ledger.posted", RecordType: "document", RecordID: &rec, Title: title, At: w.e.clock.Now(), Audience: notify.Audience{Capability: string(authz.Journals), Need: "V"}})
		return err
	})
	fan := notify.Fanout{Store: w.worker, Dispatcher: outbox.Dispatcher{Store: w.worker, Queue: w.e.queue, Clock: w.e.clock}, Clock: w.e.clock, Origin: testOrigin}
	for {
		kind, row := nextWorkRow(w.e.t, w.e)
		if kind == "" {
			return rec
		}
		if kind == notify.FanoutKind {
			if err := fan.Work()(context.Background(), w.e.books.Org, row); err != nil {
				w.e.t.Fatalf("the fan-out: %v", err)
			}
		}
	}
}

func (w *webhookEnv) deliver() int {
	w.e.t.Helper()
	n, err := webhooks.Deliverer{Store: w.worker, Cipher: testFields(w.e.t), Egress: &egress.Guard{Local: true}, Clock: w.e.clock, Source: testOrigin}.Run(context.Background(), w.e.books.Org)
	if err != nil {
		w.e.t.Fatalf("the delivery job: %v", err)
	}
	return n
}

func (w *webhookEnv) create(url string, types ...string) response {
	return w.e.call(http.MethodPost, "/v1/webhook-endpoints", w.admin, map[string]any{"url": url, "label": "Accounting sync", "event_types": types})
}

// FR-INT-12 (p2-api-third-parties, rev 0.602): an endpoint is off until an Owner switches webhooks on; an Administrator signed in and stepped up adds one for event types on the
// allow-list, at a web address, and gets its secret once; it is pending until it echoes the challenge of a signed verification event; then an event the fan-out records reaches it
// as a CloudEvent signed as Standard Webhooks, carrying IDs and times and never the title; the delivery is logged; a person who is not a signed-in person with step-up cannot make one.
// req: FR-INT-12
// req: FR-INT-01
// asvs: V4.1.1, V13.2.1
func TestAWebhookEndpointIsVerifiedBeforeItReceivesASignedEvent(t *testing.T) {
	w := newWebhookEnv(t)
	e := w.e
	rx := newReceiver(t)
	url := rx.srv.URL + "/fyniq"
	// Off until an Owner turns it on.
	if r := w.create(url, "fyniq.ledger.posted.v1"); r.Status != http.StatusForbidden || r.str("code") != CodeWebhooksOff {
		t.Fatalf("with webhooks off: %d %s", r.Status, r.Raw)
	}
	w.switchOn("true")
	// What may not be asked for or sent to.
	for name, c := range map[string]struct {
		body map[string]any
		code string
	}{
		"a security event":     {map[string]any{"url": url, "label": "x", "event_types": []string{"fyniq.security.alert.v1"}}, CodeWebhookEvents},
		"a made-up event":      {map[string]any{"url": url, "label": "x", "event_types": []string{"fyniq.made.up.v1"}}, CodeWebhookEvents},
		"credentials in a url": {map[string]any{"url": "http://user:pw@" + strings.TrimPrefix(rx.srv.URL, "http://"), "label": "x", "event_types": []string{"fyniq.ledger.posted.v1"}}, CodeWebhookURL},
		"a fragment":           {map[string]any{"url": url + "#x", "label": "x", "event_types": []string{"fyniq.ledger.posted.v1"}}, CodeWebhookURL},
		"ftp":                  {map[string]any{"url": "ftp://example.com/x", "label": "x", "event_types": []string{"fyniq.ledger.posted.v1"}}, CodeWebhookURL},
	} {
		if r := e.call(http.MethodPost, "/v1/webhook-endpoints", w.admin, c.body); r.Status != 422 || r.str("code") != c.code {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
	// Only a person signed in and stepped up: no step-up, an integration token and the Accountant's right are each refused.
	if r := e.call(http.MethodPost, "/v1/webhook-endpoints", e.as(authz.Administrator), map[string]any{"url": url, "label": "x", "event_types": []string{"fyniq.ledger.posted.v1"}}); r.Status != http.StatusForbidden && r.Status != 401 && r.Status != 422 {
		t.Errorf("without step-up: %d %s", r.Status, r.Raw)
	}
	machine := e.principal(authn.Principal{Via: authn.ViaToken, Subject: "client", Client: "integration", Class: authn.ClassIntegration, Organisation: e.books.Org, Roles: []authz.Role{authz.Administrator},
		Scopes: []string{"fyniq.write.sales"}})
	if r := e.call(http.MethodPost, "/v1/webhook-endpoints", machine, map[string]any{"url": url, "label": "x", "event_types": []string{"fyniq.ledger.posted.v1"}}); r.Status < 400 {
		t.Errorf("an integration client made an endpoint: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/webhook-endpoints", e.as(authz.Accountant), map[string]any{"url": url, "label": "x", "event_types": []string{"fyniq.ledger.posted.v1"}}); r.Status != http.StatusForbidden {
		t.Errorf("an Accountant made an endpoint: %d", r.Status)
	}

	made := w.create(url, "fyniq.ledger.posted.v1", "fyniq.ledger.posted.v1") // a repeated type is one
	secret := made.str("secret")
	if made.Status != http.StatusCreated || made.str("state") != "pending" || !strings.HasPrefix(secret, webhooks.SecretPrefix) || made.str("secret_hint") != webhooks.Hint(secret) ||
		made.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("the endpoint: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")
	if got := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id, w.admin, nil); strings.Contains(got.Raw, secret) || got.str("secret") != "" {
		t.Errorf("a read shows the secret: %s", got.Raw)
	}
	// A pending endpoint receives nothing: the event is recorded and no delivery waits.
	w.emit("INV-1 was posted by Wei Ling for S$1,090.00")
	if n := w.deliver(); n != 0 || rx.count() != 0 {
		t.Fatalf("a pending endpoint was sent %d events (%d received)", n, rx.count())
	}
	// Verify: an endpoint that does not echo stays pending, one that does is active.
	rx.set("silent")
	if r := e.call(http.MethodPost, "/v1/webhook-endpoints/"+id+"/verify", w.admin, nil, "If-Match", made.Header.Get("ETag")); r.Status != 422 || r.str("code") != CodeWebhookVerify {
		t.Fatalf("an endpoint that does not echo: %d %s", r.Status, r.Raw)
	}
	rx.set("ok")
	tag := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id, w.admin, nil).Header.Get("ETag")
	v := e.call(http.MethodPost, "/v1/webhook-endpoints/"+id+"/verify", w.admin, nil, "If-Match", tag)
	if v.Status != 200 || v.str("state") != "active" || v.str("verified_at") == "" {
		t.Fatalf("verify: %d %s", v.Status, v.Raw)
	}
	vh := rx.last()
	if err := webhooks.Verify(secret, vh.Header.Get("webhook-id"), vh.Header.Get("webhook-timestamp"), vh.Header.Get("webhook-signature"), vh.Body, e.clock.Now()); err != nil {
		t.Errorf("the verification event's signature: %v", err)
	}
	// An event now reaches it, signed, as a CloudEvent with IDs and times only.
	before := rx.count()
	rec := w.emit("INV-2 was posted by Wei Ling for S$2,180.00")
	if n := w.deliver(); n != 1 || rx.count() != before+1 {
		t.Fatalf("delivered %d, received %d more", n, rx.count()-before)
	}
	h := rx.last()
	if err := webhooks.Verify(secret, h.Header.Get("webhook-id"), h.Header.Get("webhook-timestamp"), h.Header.Get("webhook-signature"), h.Body, e.clock.Now()); err != nil {
		t.Errorf("the delivery's signature: %v", err)
	}
	if err := webhooks.Verify(secret, h.Header.Get("webhook-id"), h.Header.Get("webhook-timestamp"), h.Header.Get("webhook-signature"), append([]byte{' '}, h.Body...), e.clock.Now()); err == nil {
		t.Error("a changed body verified")
	}
	var ev struct {
		SpecVersion string `json:"specversion"`
		ID          string `json:"id"`
		Type        string `json:"type"`
		Subject     string `json:"subject"`
		Data        struct {
			OrganisationID string `json:"organisation_id"`
			RecordID       string `json:"record_id"`
			RecordType     string `json:"record_type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(h.Body, &ev); err != nil || ev.SpecVersion != "1.0" || ev.Type != "fyniq.ledger.posted.v1" || ev.ID != h.Header.Get("webhook-id") ||
		ev.Data.OrganisationID != e.books.Org.String() || ev.Data.RecordID != rec.String() || ev.Subject != rec.String() {
		t.Errorf("the event: %s (%v)", h.Body, err)
	}
	for _, leak := range []string{"INV-2", "Wei Ling", "2,180"} {
		if strings.Contains(string(h.Body), leak) {
			t.Errorf("the delivery carries %q: %s", leak, h.Body)
		}
	}
	list := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id+"/deliveries", w.admin, nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 1 || items[0].(map[string]any)["state"] != "delivered" || items[0].(map[string]any)["attempts"] != float64(1) {
		t.Errorf("the delivery log: %d %s", list.Status, list.Raw)
	}
	// Switching webhooks off stops everything at once.
	w.switchOn("false")
	w.emit("INV-3 was posted")
	if n := w.deliver(); n != 0 {
		t.Errorf("with webhooks off, %d were sent", n)
	}
}

// FR-INT-12: a delivery that fails is retried with the same webhook-id and a fresh signature, kept for three days, then dead for a person to replay; ten failures in a row pause the
// endpoint; a 410 disables it; a rolled secret signs alongside the old for a day; and every attempt is logged.
// req: FR-INT-12
// asvs: V13.2.1
func TestAFailedWebhookDeliveryIsRetriedThenDeadThenReplayed(t *testing.T) {
	w := newWebhookEnv(t)
	e := w.e
	rx := newReceiver(t)
	w.switchOn("true")
	made := w.create(rx.srv.URL+"/hook", "fyniq.ledger.posted.v1")
	id, secret := made.str("id"), made.str("secret")
	if v := e.call(http.MethodPost, "/v1/webhook-endpoints/"+id+"/verify", w.admin, nil, "If-Match", made.Header.Get("ETag")); v.Status != 200 {
		t.Fatalf("verify: %d %s", v.Status, v.Raw)
	}
	rx.set("fail")
	w.emit("INV-1")
	first := rx.count()
	if w.deliver() != 1 || rx.count() != first+1 {
		t.Fatal("the first attempt was not made")
	}
	one := rx.last()
	// Not due yet: nothing is sent until the back-off ends.
	if w.deliver() != 0 {
		t.Error("a retry came before its time")
	}
	e.clock.Advance(10 * time.Second)
	if w.deliver() != 1 {
		t.Fatal("the retry did not come after the back-off")
	}
	two := rx.last()
	if one.Header.Get("webhook-id") != two.Header.Get("webhook-id") || one.Header.Get("webhook-timestamp") == two.Header.Get("webhook-timestamp") ||
		one.Header.Get("webhook-signature") == two.Header.Get("webhook-signature") {
		t.Errorf("a retry keeps its webhook-id and signs afresh: %v / %v", one.Header, two.Header)
	}
	if err := webhooks.Verify(secret, two.Header.Get("webhook-id"), two.Header.Get("webhook-timestamp"), two.Header.Get("webhook-signature"), two.Body, e.clock.Now()); err != nil {
		t.Errorf("the retry is outside the replay window or badly signed: %v", err)
	}
	// Three days of failures end dead.
	for i := 0; i < 20 && w.deadCount(id) == 0; i++ {
		e.clock.Advance(13 * time.Hour)
		w.deliver()
	}
	if w.deadCount(id) != 1 {
		t.Fatalf("the delivery did not go dead: %s", e.call(http.MethodGet, "/v1/webhook-endpoints/"+id+"/deliveries", w.admin, nil).Raw)
	}
	dead := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id+"/deliveries?state=dead", w.admin, nil).Body["items"].([]any)[0].(map[string]any)
	if dead["last_status"] != float64(503) || dead["attempts"].(float64) < 7 {
		t.Errorf("the dead delivery: %v", dead)
	}
	// A dead delivery is not sent again by itself; a person replays it, once it is dead, and it arrives with the same webhook-id.
	at := rx.count()
	e.clock.Advance(24 * time.Hour)
	if w.deliver() != 0 || rx.count() != at {
		t.Error("a dead delivery was sent again by itself")
	}
	rx.set("ok")
	replay := e.call(http.MethodPost, "/v1/webhook-deliveries/"+dead["id"].(string)+"/replay", w.admin, nil, "Idempotency-Key", uuid.NewString())
	if replay.Status != 200 || replay.str("state") != "pending" || replay.Body["replays"] != float64(1) {
		t.Fatalf("replay: %d %s", replay.Status, replay.Raw)
	}
	if w.deliver() != 1 || rx.last().Header.Get("webhook-id") != dead["message_id"] {
		t.Error("the replay did not arrive with the same webhook-id")
	}
	if again := e.call(http.MethodPost, "/v1/webhook-deliveries/"+dead["id"].(string)+"/replay", w.admin, nil, "Idempotency-Key", uuid.NewString()); again.Status != 422 {
		t.Errorf("replaying a delivered one: %d %s", again.Status, again.Raw)
	}
	// Every attempt is logged.
	var attempts []storage.WebhookAttempt
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		attempts, err = u.Webhooks().Attempts(ctx, uuid.MustParse(dead["id"].(string)))
		return err
	})
	if len(attempts) < 8 || attempts[len(attempts)-1].Status == nil || *attempts[len(attempts)-1].Status != 204 {
		t.Errorf("%d attempts logged, the last %+v", len(attempts), attempts[len(attempts)-1])
	}
	// A rolled secret signs alongside the old one for a day.
	w.admin = steppedUp(e, e.as(authz.Administrator)) // the clock has moved on: step up again
	tag := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id, w.admin, nil).Header.Get("ETag")
	rolled := e.call(http.MethodPost, "/v1/webhook-endpoints/"+id+"/roll-secret", w.admin, nil, "If-Match", tag)
	next := rolled.str("secret")
	if rolled.Status != 200 || next == "" || next == secret {
		t.Fatalf("roll: %d %s", rolled.Status, rolled.Raw)
	}
	w.emit("INV-2")
	w.deliver()
	h := rx.last()
	if len(strings.Fields(h.Header.Get("webhook-signature"))) != 2 {
		t.Errorf("signatures while rolling: %q", h.Header.Get("webhook-signature"))
	}
	for _, s := range []string{secret, next} {
		if err := webhooks.Verify(s, h.Header.Get("webhook-id"), h.Header.Get("webhook-timestamp"), h.Header.Get("webhook-signature"), h.Body, e.clock.Now()); err != nil {
			t.Errorf("a secret in force does not verify: %v", err)
		}
	}
	e.clock.Advance(25 * time.Hour)
	w.emit("INV-3")
	w.deliver()
	if h := rx.last(); len(strings.Fields(h.Header.Get("webhook-signature"))) != 1 {
		t.Errorf("the old secret still signs after its day: %q", h.Header.Get("webhook-signature"))
	}
	// A 410 disables the endpoint; ten failures in a row pause one.
	rx.set("gone")
	w.emit("INV-4")
	w.deliver()
	if got := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id, w.admin, nil); got.str("state") != "disabled" {
		t.Errorf("after 410: %s", got.Raw)
	}
	rx.set("ok")
	w.admin = steppedUp(e, e.as(authz.Administrator))
	tag = e.call(http.MethodGet, "/v1/webhook-endpoints/"+id, w.admin, nil).Header.Get("ETag")
	pending := e.call(http.MethodPatch, "/v1/webhook-endpoints/"+id, w.admin, map[string]any{"state": "pending"}, "If-Match", tag)
	if pending.Status != 200 || pending.str("state") != "pending" {
		t.Fatalf("pending again: %d %s", pending.Status, pending.Raw)
	}
	if v := e.call(http.MethodPost, "/v1/webhook-endpoints/"+id+"/verify", w.admin, nil, "If-Match", pending.Header.Get("ETag")); v.Status != 200 || v.str("state") != "active" {
		t.Fatalf("verify again: %d %s", v.Status, v.Raw)
	}
	rx.set("fail")
	for i := 0; i < 10; i++ {
		w.emit("INV-" + string(rune('a'+i)))
		e.clock.Advance(time.Second)
		w.deliver()
	}
	if got := e.call(http.MethodGet, "/v1/webhook-endpoints/"+id, w.admin, nil); got.str("state") != "paused" || !strings.Contains(got.str("paused_reason"), "Ten deliveries") {
		t.Errorf("after ten failures: %s", got.Raw)
	}
}

func (w *webhookEnv) deadCount(id string) int {
	r := w.e.call(http.MethodGet, "/v1/webhook-endpoints/"+id+"/deliveries?state=dead", w.admin, nil)
	items, _ := r.Body["items"].([]any)
	return len(items)
}

func init() {
	// The matrix's organisation never switches webhooks on and has no endpoint, so a role the policy allows is answered by its state, past the policy.
	pastPolicy["createWebhookEndpoint"] = CodeWebhooksOff
	pastPolicy["verifyWebhookEndpoint"] = CodeWebhooksOff // the switch is read first
	for _, op := range []string{"getWebhookEndpoint", "changeWebhookEndpoint", "rollWebhookSecret", "listWebhookDeliveries", "replayWebhookDelivery"} {
		pastPolicy[op] = CodeWebhookNotFound
	}
}

var matrixWebhooks = map[string]func(e *env, tok string) (response, func() bool){
	"listWebhookEndpoints": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/webhook-endpoints", tok, nil), nil
	},
	"getWebhookEndpoint": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/webhook-endpoints/"+uuid.NewString(), tok, nil), nil
	},
	"createWebhookEndpoint": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/webhook-endpoints", steppedUp(e, tok), map[string]any{"url": "https://hooks.example.com/x", "label": "Matrix", "event_types": []string{"fyniq.ledger.posted.v1"}}), nil
	},
	"changeWebhookEndpoint": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPatch, "/v1/webhook-endpoints/"+uuid.NewString(), steppedUp(e, tok), map[string]any{"label": "Matrix"}, "If-Match", `"1"`), nil
	},
	"rollWebhookSecret": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/webhook-endpoints/"+uuid.NewString()+"/roll-secret", steppedUp(e, tok), nil, "If-Match", `"1"`), nil
	},
	"verifyWebhookEndpoint": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/webhook-endpoints/"+uuid.NewString()+"/verify", steppedUp(e, tok), nil, "If-Match", `"1"`), nil
	},
	"listWebhookDeliveries": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/webhook-endpoints/"+uuid.NewString()+"/deliveries", tok, nil), nil
	},
	"replayWebhookDelivery": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/webhook-deliveries/"+uuid.NewString()+"/replay", steppedUp(e, tok), nil, "Idempotency-Key", uuid.NewString()), nil
	},
}
