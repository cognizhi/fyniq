//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// strangerMail is a receiving provider that always has the same message from a stranger, with one PDF attached.
type strangerMail struct{ from string }

func (strangerMail) Name() string { return "resend" }

func (s strangerMail) Fetch(context.Context, string) (ports.InboundEmail, error) {
	return ports.InboundEmail{From: s.from, Text: "Our invoice is attached.", DMARC: "pass",
		Attachments: []ports.InboundAttachment{{ID: "a-1", FileName: "INV-0042.pdf", Size: 22}}}, nil
}

func (strangerMail) Attachment(context.Context, string, ports.InboundAttachment, int64) ([]byte, error) {
	return []byte("%PDF-1.7\n% an invoice\n"), nil
}

// received posts Resend's email.received for a message from sender to address, signed.
func received(t *testing.T, e *env, srv *httptest.Server, address, sender string) {
	t.Helper()
	body := `{"type":"email.received","created_at":"` + e.clock.Now().UTC().Format("2006-01-02T15:04:05Z") + `","data":{"email_id":"` + uuid.NewString() +
		`","from":"` + sender + `","to":["` + address + `"],"received_for":["` + address + `"],"subject":"Invoice INV-0042"}}`
	if got := resendHook(t, e, srv, "msg_"+uuid.NewString(), body, true); got != http.StatusNoContent {
		t.Fatalf("the received webhook: %d", got)
	}
}

// Mail to the organisation's email-in address is recorded from Resend's signed webhook and its fetch queued; the worker
// quarantines a stranger's; the Administrator lists it and accepts the sender, which accepts their quarantined mail,
// or rejects another, which deletes theirs. A member lists and decides at their own address, and is told of the mail
// waiting there. Mail to a name that routes nowhere is ignored.
// req: FR-INT-22
// req: FR-NTF-01
// req: FR-DOC-12
func TestReceivedMailIsQuarantinedUntilSomeoneDecides(t *testing.T) {
	f := newFounders(t)
	e := f.e
	admin := e.as(authz.Administrator)
	name := "bills-" + uuid.NewString()[:6]
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/organisation", admin, map[string]any{"name": name}, "If-Match", `"none"`); r.Status != 200 {
		t.Fatalf("naming the address: %d %s", r.Status, r.Raw)
	}
	mux := http.NewServeMux()
	(&mailhooks.Receiver{Store: e.store, Clock: e.clock, ResendSecret: hookSecret,
		Inbound: &mailhooks.Inbound{Domain: "in.example.com", Routes: e.store, Dispatcher: outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}}}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	drainWork(t, e)
	received(t, e, srv, "nobody-"+name+"@in.example.com", "x@stranger.example")
	if k := nextWork(t, e); k != "" {
		t.Fatalf("mail to a name that routes nowhere queued %s", k)
	}
	received(t, e, srv, strings.ToUpper(name)+"@in.example.com", "Ann <ann@stranger.example>")
	received(t, e, srv, name+"@in.example.com", "spam@junk.example")
	list := e.call(http.MethodGet, "/v1/organisation/email/inbound-messages", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 2 || items[1].(map[string]any)["status"] != "received" || items[1].(map[string]any)["sender"] != "ann@stranger.example" ||
		items[1].(map[string]any)["address"] != name+"@in.example.com" {
		t.Fatalf("the received mail: %d %s", list.Status, list.Raw)
	}

	// The worker fetches each: strangers are quarantined, their files kept.
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	staging, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.api.deps.Staging = staging
	for _, sender := range []string{"ann@stranger.example", "spam@junk.example"} {
		k, row := nextWorkRow(t, e)
		if k != email.FetchKind {
			t.Fatalf("queued %q; want %s", k, email.FetchKind)
		}
		fetcher := email.Fetcher{Mail: strangerMail{from: sender}, Store: worker, Files: staging, Scanner: local.EICARScanner{}, Clock: e.clock,
			Quarantined: notify.Quarantined(outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock})}
		if err := fetcher.Work()(context.Background(), e.books.Org, row); err != nil {
			t.Fatal(err)
		}
	}
	fanOutAll(t, e)
	q := e.call(http.MethodGet, "/v1/organisation/email/inbound-messages?status=quarantined", e.as(authz.Auditor), nil)
	if items, _ := q.Body["items"].([]any); len(items) != 2 || len(items[0].(map[string]any)["files"].([]any)) != 2 || items[0].(map[string]any)["expires_at"] == nil {
		t.Fatalf("the quarantine: %s", q.Raw)
	}

	acc := e.call(http.MethodPost, "/v1/organisation/email/capture-senders", admin, map[string]any{"sender": "ANN@stranger.example", "decision": "accepted"},
		"Idempotency-Key", uuid.NewString())
	if acc.Status != 200 || acc.Body["settled"] != float64(1) || acc.str("sender") != "ann@stranger.example" {
		t.Fatalf("accepting Ann: %d %s", acc.Status, acc.Raw)
	}
	rej := e.call(http.MethodPost, "/v1/organisation/email/capture-senders", admin, map[string]any{"sender": "spam@junk.example", "decision": "rejected"})
	if rej.Status != 200 || rej.Body["settled"] != float64(1) {
		t.Fatalf("rejecting the spammer: %d %s", rej.Status, rej.Raw)
	}
	all := e.call(http.MethodGet, "/v1/organisation/email/inbound-messages", e.as(authz.Auditor), nil).Body["items"].([]any)
	for _, x := range all {
		m := x.(map[string]any)
		switch m["sender"] {
		case "ann@stranger.example":
			if m["status"] != "accepted" {
				t.Errorf("Ann's mail: %v", m["status"])
			}
		case "spam@junk.example":
			if m["status"] != "rejected" || m["files"].([]any)[0].(map[string]any)["removed"] != true {
				t.Errorf("the spammer's mail: %v", m)
			}
		}
	}
	if r := e.call(http.MethodPost, "/v1/organisation/email/capture-senders", admin, map[string]any{"sender": "Ann <a@b.example>", "decision": "accepted"}); r.problemCode() != CodeSenderNotAddress {
		t.Fatalf("a sender with a name: %d %s", r.Status, r.Raw)
	}

	// A member's own address: their mail lists there, and they decide there.
	me := f.as(f.ahkow, authz.Employee)
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/mine", me, map[string]any{"name": "ahkow-" + name}, "If-Match", `"none"`); r.Status != 200 {
		t.Fatalf("naming Ah Kow's: %d %s", r.Status, r.Raw)
	}
	received(t, e, srv, "ahkow-"+name+"@in.example.com", "friend@example.net")
	if k, row := nextWorkRow(t, e); k != email.FetchKind {
		t.Fatalf("queued %q; want %s", k, email.FetchKind)
	} else if err := (email.Fetcher{Mail: strangerMail{from: "friend@example.net"}, Store: worker, Files: staging, Scanner: local.EICARScanner{},
		Clock: e.clock, Quarantined: notify.Quarantined(outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock})}).Work()(context.Background(), e.books.Org, row); err != nil {
		t.Fatal(err)
	}
	fanOutAll(t, e)
	told := e.call(http.MethodGet, "/v1/notifications", me, nil).Body["items"].([]any)
	if len(told) != 1 || told[0].(map[string]any)["type"] != "email.quarantined" || told[0].(map[string]any)["actionable"] != true ||
		!strings.Contains(told[0].(map[string]any)["title"].(string), "friend@example.net") {
		t.Fatalf("Ah Kow was not told of the mail waiting at his address: %v", told)
	}
	if r := e.call(http.MethodGet, "/v1/notifications", f.as(f.weiling, authz.Employee), nil); strings.Contains(r.Raw, "friend@example.net") {
		t.Fatalf("Wei Ling was told of Ah Kow's mail: %s", r.Raw)
	}
	mine := e.call(http.MethodGet, "/v1/organisation/email/inbound-messages/mine", me, nil)
	if items, _ := mine.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["for"] != "member" {
		t.Fatalf("Ah Kow's mail: %s", mine.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/organisation/email/inbound-messages/mine", f.as(f.weiling, authz.Employee), nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("Wei Ling sees Ah Kow's mail: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/organisation/email/capture-senders/mine", me, map[string]any{"sender": "friend@example.net", "decision": "accepted"}); r.Status != 200 {
		t.Fatalf("Ah Kow accepting a friend: %d %s", r.Status, r.Raw)
	}
	trail := auditTrail(t, e)
	for _, action := range []string{"inbound_message.received", "inbound_message.quarantined", "inbound_message.accepted", "inbound_message.rejected",
		"inbound_message.file_removed", "capture_sender.accepted", "capture_sender.rejected", `"email.quarantined"`} {
		if !strings.Contains(trail, action) {
			t.Errorf("the audit trail lacks %s", action)
		}
	}
}

// nextWorkRow is the next worker task's kind and its outbox row.
func nextWorkRow(t *testing.T, e *env) (string, storage.OutboxRow) {
	t.Helper()
	task, err := e.queue.Next(context.Background(), "worker")
	if err != nil {
		return "", storage.OutboxRow{}
	}
	var m outbox.Message
	if err := json.Unmarshal(task.Payload, &m); err != nil {
		t.Fatal(err)
	}
	var row storage.OutboxRow
	if err := e.store.Do(context.Background(), m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) error {
		row, err = u.Outbox().Get(ctx, m.OutboxID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row.Kind, row
}
