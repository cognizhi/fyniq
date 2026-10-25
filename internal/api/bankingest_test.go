//go:build local

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/outbox"
)

// matrixBankIngest are the matrix's rows of the bank-ingest address (QA-SEC-03).
var matrixBankIngest = map[string]func(e *env, tok string) (response, func() bool){
	"setBankCaptureAddress": func(e *env, tok string) (response, func() bool) {
		tag := func() any {
			return e.call(http.MethodGet, "/v1/organisation/email/capture-addresses", e.as(authz.Auditor), nil).Body["bank_etag"]
		}
		before := tag()
		r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/bank", tok, map[string]any{"name": "b-" + uuid.NewString()[:8]}, "If-Match", "*")
		return r, func() bool { return tag() == before }
	},
	"decideBankSender": func(e *env, tok string) (response, func() bool) {
		// A decision shows only in what it does to mail that arrives; nothing is quarantined here to settle.
		return e.call(http.MethodPost, "/v1/organisation/email/capture-senders/bank", tok, map[string]any{"sender": "m-" + uuid.NewString()[:8] + "@bank.example",
			"decision": "rejected"}), nil
	},
}

// The bank-ingest address through the API (FR-BNK-17, FR-INT-22; p1-bank-import-reconcile task 7.1, design D10): an
// Administrator names it beside the documents address, a Bookkeeper may not; mail to it is routed by its name, and the
// worker quarantines a sender no money account registers; accepting that sender settles the mail waiting and does not
// make them known, so their next report waits too; the list says which kind of address each message came to.
// req: FR-BNK-17
// req: FR-INT-22
func TestTheBankIngestAddressThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	admin := e.as(authz.Administrator)
	name := "bank-" + uuid.NewString()[:6]
	e.problemIs(e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/bank", e.as(authz.Bookkeeper), map[string]any{"name": name}, "If-Match", `"none"`),
		"setBankCaptureAddress", 403, CodeForbidden)
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/organisation", admin, map[string]any{"name": "bills-" + name[5:]}, "If-Match", `"none"`); r.Status != 200 {
		t.Fatalf("naming the documents address: %d %s", r.Status, r.Raw)
	}
	r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/bank", admin, map[string]any{"name": name}, "If-Match", `"none"`)
	if r.Status != 200 || r.str("bank", "address") != name+"@in.example.com" || r.str("organisation", "name") != "bills-"+name[5:] || r.str("bank_etag") == `"none"` {
		t.Fatalf("naming the bank-ingest address: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/bank", admin, map[string]any{"name": "bank2-" + name[5:]}, "If-Match", `"none"`),
		"setBankCaptureAddress", 412, CodeStale)

	mux := http.NewServeMux()
	(&mailhooks.Receiver{Store: e.store, Clock: e.clock, ResendSecret: hookSecret,
		Inbound: &mailhooks.Inbound{Domain: "in.example.com", Routes: e.store, Dispatcher: outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}}}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
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
	drainWork(t, e)
	report := func() {
		t.Helper()
		received(t, e, srv, name+"@in.example.com", "alerts@bank.example")
		k, row := nextWorkRow(t, e)
		if k != email.FetchKind {
			t.Fatalf("queued %q; want %s", k, email.FetchKind)
		}
		fetcher := email.Fetcher{Mail: strangerMail{from: "alerts@bank.example"}, Store: worker, Files: staging, Scanner: local.EICARScanner{}, Clock: e.clock}
		if err := fetcher.Work()(context.Background(), e.books.Org, row); err != nil {
			t.Fatal(err)
		}
	}
	report()
	q := e.call(http.MethodGet, "/v1/organisation/email/inbound-messages?status=quarantined", e.as(authz.Auditor), nil)
	if items, _ := q.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["kind"] != "bank" || items[0].(map[string]any)["for"] != "organisation" {
		t.Fatalf("the quarantine: %s", q.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/email/capture-senders/bank", e.as(authz.Bookkeeper),
		map[string]any{"sender": "alerts@bank.example", "decision": "accepted"}), "decideBankSender", 403, CodeForbidden)
	acc := e.call(http.MethodPost, "/v1/organisation/email/capture-senders/bank", admin, map[string]any{"sender": "alerts@bank.example", "decision": "accepted"})
	if acc.Status != 200 || acc.Body["settled"] != float64(1) {
		t.Fatalf("accepting the sender's mail: %d %s", acc.Status, acc.Raw)
	}
	report()
	q = e.call(http.MethodGet, "/v1/organisation/email/inbound-messages?status=quarantined", e.as(authz.Auditor), nil)
	if items, _ := q.Body["items"].([]any); len(items) != 1 {
		t.Fatalf("the sender's next report was not quarantined: %s", q.Raw)
	}
}
