//go:build local

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankingest"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// reportMail is a receiving provider whose message carries one report, a CSV file, with DMARC passing.
type reportMail struct {
	from string
	csv  []byte
}

func (reportMail) Name() string { return "resend" }

func (r reportMail) Fetch(context.Context, string) (ports.InboundEmail, error) {
	return ports.InboundEmail{From: r.from, Text: "Your scheduled report is attached.", SPF: "pass", DKIM: "pass", DMARC: "pass",
		Attachments: []ports.InboundAttachment{{ID: "r-1", FileName: "IDEAL-report.csv", ContentType: "text/csv", Size: int64(len(r.csv))}}}, nil
}

func (r reportMail) Attachment(context.Context, string, ports.InboundAttachment, int64) ([]byte, error) {
	return r.csv, nil
}

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
	if k, _ := nextWorkRow(t, e); k != bankingest.Kind { // the accepted report is imported (task 7.2)
		t.Fatalf("accepting it queued %q; want %s", k, bankingest.Kind)
	}
	report()
	q = e.call(http.MethodGet, "/v1/organisation/email/inbound-messages?status=quarantined", e.as(authz.Auditor), nil)
	if items, _ := q.Body["items"].([]any); len(items) != 1 {
		t.Fatalf("the sender's next report was not quarantined: %s", q.Raw)
	}
}

// The daily report (FR-BNK-17; p1-bank-import-reconcile task 7.2, design D10): a report from the sender a money account
// registers, accepted at the bank-ingest address, is imported by the worker through that account's format and mapping,
// as an emailed statement keeping its file; its lines wait for Reconcile now (rev 0.143); a missing day shows as a gap;
// the same report again adds nothing; a file that does not read is recorded and imports nothing; a sender's report a
// person accepts from quarantine is imported too.
// req: FR-BNK-17
// req: FR-BNK-16
// req: banking/bank-transactions Daily DBS sync without a bank API
// req: banking/bank-transactions Lines that arrive by feed wait for Reconcile now
func TestEmailedBankReportsAreImportedByTheWorker(t *testing.T) {
	e := newEnv(t)
	admin := e.as(authz.Administrator)
	name := "bank-" + uuid.NewString()[:6]
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/bank", admin, map[string]any{"name": name}, "If-Match", `"none"`); r.Status != 200 {
		t.Fatalf("naming the bank-ingest address: %d %s", r.Status, r.Raw)
	}
	m := e.newMoneyAccount()
	var mp storage.StatementMapping
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		mp, err = u.Banking().SaveMapping(ctx, storage.NewStatementMapping{Name: "IDEAL " + uuid.NewString()[:8], Format: "csv", At: e.clock.Now(),
			Spec: []byte(`{"date_formats":["02/01/2006"],"date":"Date","amount":"Amount","description":["Description"],"balance":"Balance"}`)})
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: m.ID, Kind: storage.MoneySourceEmail,
			Senders: []string{"ideal.reports@dbs.example"}, ReportFormat: "csv", MappingID: &mp.ID, FromDate: e.clock.Now(), At: e.clock.Now()})
		return err
	})

	// Mail arrives as on the local stack: Mailpit's webhook announces it (local mode's inbound mail).
	mux := http.NewServeMux()
	(&mailhooks.Receiver{Store: e.store, Clock: e.clock, MailpitSecret: "fyniq:mailpit",
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
	importer := bankingest.Importer{Store: worker, Files: staging, Clock: e.clock}
	drainWork(t, e)
	mailpit := func(sender string) {
		t.Helper()
		body := `{"ID":"` + uuid.NewString() + `","From":{"Address":"` + sender + `"},"To":[{"Address":"` + name + `@in.example.com"}],` +
			`"Subject":"IDEAL scheduled report","Created":"` + e.clock.Now().UTC().Format("2006-01-02T15:04:05Z") + `"}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/email/mailpit", strings.NewReader(body))
		req.SetBasicAuth("fyniq", "mailpit")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("Mailpit's webhook: %d", res.StatusCode)
		}
	}
	// report sends a report from sender, fetches it and runs whatever import it queued.
	report := func(sender string, csv string) {
		t.Helper()
		mailpit(sender)
		k, row := nextWorkRow(t, e)
		if k != email.FetchKind {
			t.Fatalf("queued %q; want %s", k, email.FetchKind)
		}
		fetcher := email.Fetcher{Mail: reportMail{from: sender, csv: []byte(csv)}, Store: worker, Files: staging, Scanner: local.EICARScanner{}, Clock: e.clock,
			Accepted: bankingest.Accepted(outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock})}
		if err := fetcher.Work()(context.Background(), e.books.Org, row); err != nil {
			t.Fatal(err)
		}
		for k, row := nextWorkRow(t, e); k != ""; k, row = nextWorkRow(t, e) {
			if k != bankingest.Kind {
				t.Fatalf("queued %q; want %s", k, bankingest.Kind)
			}
			if err := importer.Work()(context.Background(), e.books.Org, row); err != nil {
				t.Fatal(err)
			}
		}
	}
	statements := func() []any {
		items, _ := e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String()+"/statements", e.as(authz.Bookkeeper), nil).Body["items"].([]any)
		return items
	}

	report("ideal.reports@dbs.example", "Date,Description,Amount,Balance\r\n20/10/2026,GIRO ACME,100.00,1100.00\r\n")
	if st := statements(); len(st) != 1 || st[0].(map[string]any)["source"] != "email" || st[0].(map[string]any)["has_original"] != true {
		t.Fatalf("the first report: %v", st)
	}
	lines := e.call(http.MethodGet, "/v1/bank-lines?money_account_id="+m.ID.String(), e.as(authz.Bookkeeper), nil)
	if items, _ := lines.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["status"] != storage.LineUnreconciled {
		t.Fatalf("its line waits for Reconcile now: %s", lines.Raw)
	}
	report("ideal.reports@dbs.example", "Date,Description,Amount,Balance\r\n22/10/2026,AWS EMEA,-50.00,1000.00\r\n")
	if a := e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String(), e.as(authz.Bookkeeper), nil); len(statements()) != 2 || len(a.Body["gaps"].([]any)) != 1 {
		t.Fatalf("the missing day is not a gap: %s", a.Raw)
	}
	report("ideal.reports@dbs.example", "Date,Description,Amount,Balance\r\n22/10/2026,AWS EMEA,-50.00,1000.00\r\n")
	if n := len(statements()); n != 2 {
		t.Errorf("the same report again added a statement: %d", n)
	}
	report("ideal.reports@dbs.example", "not a statement at all")
	if n := len(statements()); n != 2 {
		t.Errorf("a file that does not read added a statement: %d", n)
	}

	// The month's export ties out: over the daily reports it adds nothing while their balances agree, and a line whose
	// balance differs is kept for a person as a possible duplicate.
	upload := func(csv string) response {
		return e.upload("/v1/bank-imports", e.as(authz.Bookkeeper), map[string]string{"format": "csv", "money_account_id": m.ID.String(),
			"mapping_id": mp.ID.String()}, "october.csv", []byte(csv))
	}
	same := upload("Date,Description,Amount,Balance\r\n20/10/2026,GIRO ACME,100.00,1100.00\r\n22/10/2026,AWS EMEA,-50.00,1000.00\r\n")
	if !strings.Contains(same.Raw, `"dropped":2`) || !strings.Contains(same.Raw, `"added":0`) || len(statements()) != 2 {
		t.Fatalf("the month's export over agreeing reports: %d %s", same.Status, same.Raw)
	}
	differs := upload("Date,Description,Amount,Balance\r\n20/10/2026,GIRO ACME,100.00,1100.00\r\n22/10/2026,AWS EMEA,-50.00,1010.00\r\n")
	if !strings.Contains(differs.Raw, `"flagged":1`) {
		t.Fatalf("the month's export that does not tie out: %d %s", differs.Status, differs.Raw)
	}

	// From quarantine: a person accepts a report that waited, and it is imported.
	report("alerts@dbs.example", "Date,Description,Amount,Balance\r\n23/10/2026,FAST IN,25.00,1025.00\r\n")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: m.ID, Kind: storage.MoneySourceEmail,
			Senders: []string{"ideal.reports@dbs.example", "alerts@dbs.example"}, ReportFormat: "csv", MappingID: &mp.ID,
			FromDate: e.clock.Now().AddDate(0, 0, 1), At: e.clock.Now()})
		return err
	})
	if r := e.call(http.MethodPost, "/v1/organisation/email/capture-senders/bank", admin, map[string]any{"sender": "alerts@dbs.example", "decision": "accepted"}); r.Status != 200 ||
		r.Body["settled"] != 1.0 {
		t.Fatalf("accepting the waiting report: %d %s", r.Status, r.Raw)
	}
	k, row := nextWorkRow(t, e)
	if k != bankingest.Kind {
		t.Fatalf("queued %q; want %s", k, bankingest.Kind)
	}
	if err := importer.Work()(context.Background(), e.books.Org, row); err != nil {
		t.Fatal(err)
	}
	if n := len(statements()); n != 4 {
		t.Errorf("the accepted report was not imported: %d statements", n)
	}
}
