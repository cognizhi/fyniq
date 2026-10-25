//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankingest"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// alertMail is a receiving provider whose message is an alert: its text and no attachment, with DMARC passing.
type alertMail struct{ from, text string }

func (alertMail) Name() string { return "resend" }

func (a alertMail) Fetch(context.Context, string) (ports.InboundEmail, error) {
	return ports.InboundEmail{From: a.from, Text: a.text, SPF: "pass", DKIM: "pass", DMARC: "pass"}, nil
}

func (alertMail) Attachment(context.Context, string, ports.InboundAttachment, int64) ([]byte, error) {
	return nil, errors.New("the alert has no attachment")
}

// worker is the worker's store, for what only the worker records, such as a signal.
func (e *env) worker() storage.Store {
	e.t.Helper()
	w, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(w.Close)
	return w
}

// signal is a pending signal of S$12.34 on a new money account, pointing to no invoice, as the worker records an alert.
func (e *env) signal() storage.BankSignal {
	e.t.Helper()
	m := e.newMoneyAccount()
	var s storage.BankSignal
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &bankingest.Actor})
	if err := e.worker().Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		s, err = u.BankMatching().RecordSignal(ctx, storage.NewBankSignal{MoneyAccountID: m.ID, Amount: "12.34", Currency: "SGD", Payer: "MATRIX PTE LTD",
			ReceivedAt: e.clock.Now()})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) signalNow(id uuid.UUID) storage.BankSignal {
	e.t.Helper()
	var s storage.BankSignal
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		s, err = u.BankMatching().Signal(ctx, id)
		return err
	})
	return s
}

// matrixBankSignals are the matrix's rows of the incoming-funds signals (QA-SEC-03).
var matrixBankSignals = map[string]func(e *env, tok string) (response, func() bool){
	"listBankSignals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bank-signals?status=pending", tok, nil), nil
	},
	"dismissBankSignal": func(e *env, tok string) (response, func() bool) {
		s := e.signal()
		r := e.call(http.MethodPost, "/v1/bank-signals/"+s.ID.String()+"/dismiss", tok, map[string]any{"reason": "Not a payment of ours"})
		return r, func() bool { return e.signalNow(s.ID).Status == storage.SignalPending }
	},
}

// UC-BNK-02 through the API (FR-BNK-16; p1-bank-import-reconcile task 7.3, design D10): a DBS alert that S$10,900
// arrived from "ACME ROBOTICS", mailed to the bank-ingest address, becomes a pending signal that points to INV for that
// amount and customer, not to another customer's invoice of the same amount; the invoice shows "likely paid" while its
// status, settlement and the ledger stay as they were; the same alert again records nothing more. Next morning's report
// brings the statement line, which waits for Reconcile now, is over the line limit, and is matched by a person: the
// signal closes by that line, and the invoice is paid. Another customer's alert is dismissed with a reason, once; text
// that is not an alert records nothing; an alert no line closes expires after the setting's 7 days, not before.
// req: FR-BNK-16
// req: FR-BNK-17
// req: banking/bank-transactions Incoming-funds alert speeds up matching
// req: banking/bank-transactions Alert shows "likely paid"
func TestAnIncomingFundsAlertSpeedsUpMatching(t *testing.T) {
	e := newEnv(t)
	admin, bk, accountant := e.as(authz.Administrator), e.as(authz.Bookkeeper), e.as(authz.Accountant)
	name := "bank-" + uuid.NewString()[:6]
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/bank", admin, map[string]any{"name": name}, "If-Match", `"none"`); r.Status != 200 {
		t.Fatalf("naming the bank-ingest address: %d %s", r.Status, r.Raw)
	}
	e.bankBooks()
	m := e.newMoneyAccount()
	var mp storage.StatementMapping
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		mp, err = u.Banking().SaveMapping(ctx, storage.NewStatementMapping{Name: "IDEAL " + uuid.NewString()[:8], Format: "csv", At: e.clock.Now(),
			Spec: []byte(`{"date_formats":["02/01/2006"],"date":"Date","amount":"Amount","description":["Description"],"balance":"Balance"}`)})
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: m.ID, Kind: storage.MoneySourceEmail,
			Senders: []string{"ideal@dbs.example"}, ReportFormat: "csv", MappingID: &mp.ID, FromDate: e.clock.Now(), At: e.clock.Now()})
		return err
	})

	// Two open invoices of S$10,900.00: another customer's, and ACME ROBOTICS's, issued by one person under a raised
	// self-approval threshold (the approval of larger invoices has its own tests).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "approvals.self_approval_threshold", "2000.00", "20000.00", porttest.Day(10, 1), 1)
		return err
	})
	other, _ := e.issuedInvoice("1", "10900.00")
	var acme storage.Contact
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		acme, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Acme Robotics Pte. Ltd.", Customer: true})
		return err
	})
	draft := e.invoiceDraft("1", "10900.00")
	draft["customer_id"] = acme.ID.String()
	c := e.call(http.MethodPost, "/v1/invoices", accountant, draft)
	if c.Status != http.StatusCreated {
		t.Fatalf("the invoice to ACME ROBOTICS: %d %s", c.Status, c.Raw)
	}
	inv := c.str("id")
	if r := e.call(http.MethodPost, "/v1/invoices/"+inv+"/issue", accountant, nil, "If-Match", c.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("issuing it: %d %s", r.Status, r.Raw)
	}
	invoice := func(id string) response { return e.call(http.MethodGet, "/v1/invoices/"+id, accountant, nil) }
	before := invoice(inv)
	if before.str("total", "amount") != "10900.00" || before.str("states", "settlement") != "open" {
		t.Fatalf("the invoice: %s", before.Raw)
	}
	revision := func() int64 {
		var rev int64
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			rev, err = u.Ledger().Revision(ctx)
			return err
		})
		return rev
	}
	rev := revision()

	// Mail arrives through Mailpit's webhook, is fetched by the worker and accepted from the account's sender, and the
	// import it queues runs.
	mux := http.NewServeMux()
	(&mailhooks.Receiver{Store: e.store, Clock: e.clock, MailpitSecret: "fyniq:mailpit",
		Inbound: &mailhooks.Inbound{Domain: "in.example.com", Routes: e.store, Dispatcher: outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}}}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	worker := e.worker()
	staging, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.api.deps.Staging = staging
	importer := bankingest.Importer{Store: worker, Files: staging, Clock: e.clock}
	drainWork(t, e)
	deliver := func(mail ports.InboundMail) storage.OutboxRow {
		t.Helper()
		body := `{"ID":"` + uuid.NewString() + `","From":{"Address":"ideal@dbs.example"},"To":[{"Address":"` + name + `@in.example.com"}],` +
			`"Subject":"DBS IDEAL","Created":"` + e.clock.Now().UTC().Format("2006-01-02T15:04:05Z") + `"}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/email/mailpit", strings.NewReader(body))
		req.SetBasicAuth("fyniq", "mailpit")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		k, row := nextWorkRow(t, e)
		if k != email.FetchKind {
			t.Fatalf("queued %q; want %s", k, email.FetchKind)
		}
		fetcher := email.Fetcher{Mail: mail, Store: worker, Files: staging, Scanner: local.EICARScanner{}, Clock: e.clock,
			Accepted: bankingest.Accepted(outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock})}
		if err := fetcher.Work()(context.Background(), e.books.Org, row); err != nil {
			t.Fatal(err)
		}
		k, row = nextWorkRow(t, e)
		if k != bankingest.Kind {
			t.Fatalf("queued %q; want %s", k, bankingest.Kind)
		}
		if err := importer.Work()(context.Background(), e.books.Org, row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	signals := func(status string) []map[string]any {
		r := e.call(http.MethodGet, "/v1/bank-signals?status="+status, bk, nil)
		if r.Status != 200 {
			t.Fatalf("the signals: %d %s", r.Status, r.Raw)
		}
		var out []map[string]any
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}

	// The alert: a pending signal pointing to ACME ROBOTICS's invoice, "likely paid" on it, and nothing else changed.
	row := deliver(alertMail{from: "ideal@dbs.example",
		text: "DBS IDEAL Alert: SGD 10,900.00 has been received in your account from ACME ROBOTICS on 26 Oct 2026. Ref: 2610-0001."})
	pending := signals("pending")
	if len(pending) != 1 || pending[0]["payer"] != "ACME ROBOTICS" || pending[0]["reference"] != "2610-0001" ||
		pending[0]["amount"].(map[string]any)["amount"] != "10900.00" || pending[0]["invoice"] == nil ||
		pending[0]["invoice"].(map[string]any)["id"] != inv || pending[0]["invoice"].(map[string]any)["number"] != before.str("number") {
		t.Fatalf("the signal: %v", pending)
	}
	after := invoice(inv)
	if after.str("likely_paid", "signal_id") != pending[0]["id"] || after.str("likely_paid", "amount", "amount") != "10900.00" ||
		after.str("status") != "posted" || after.str("states", "settlement") != "open" || after.str("open", "amount") != "10900.00" ||
		after.Header.Get("ETag") != before.Header.Get("ETag") {
		t.Fatalf("the invoice after the alert: %s", after.Raw)
	}
	if o := invoice(other); o.Body["likely_paid"] != nil {
		t.Errorf("another customer's invoice of the same amount is likely paid: %s", o.Raw)
	}
	if got := revision(); got != rev {
		t.Errorf("the alert changed the ledger: revision %d, was %d", got, rev)
	}
	if err := importer.Work()(context.Background(), e.books.Org, row); err != nil || len(signals("")) != 1 {
		t.Fatalf("the same alert again: %v, %v", err, signals(""))
	}

	// Another customer's alert, dismissed once with a reason; text that is not an alert records nothing.
	deliver(alertMail{from: "ideal@dbs.example", text: "S$10,900.00 received from ACME ANALYTICS via FAST."})
	var second map[string]any
	for _, s := range signals("pending") {
		if s["payer"] == "ACME ANALYTICS" {
			second = s
		}
	}
	if second == nil || second["invoice"].(map[string]any)["id"] != other {
		t.Fatalf("the second alert: %v", signals("pending"))
	}
	dismiss := func(id any) response {
		return e.call(http.MethodPost, "/v1/bank-signals/"+id.(string)+"/dismiss", bk, map[string]any{"reason": "A refund the customer sent back"})
	}
	if d := dismiss(second["id"]); d.Status != 200 || d.str("status") != storage.SignalDismissed || d.str("closed_at") == "" {
		t.Fatalf("dismissing it: %d %s", d.Status, d.Raw)
	}
	if o := invoice(other); o.Body["likely_paid"] != nil {
		t.Errorf("a dismissed signal still shows: %s", o.Raw)
	}
	e.problemIs(dismiss(second["id"]), "dismissBankSignal", 409, storage.CodeBanking)
	deliver(alertMail{from: "ideal@dbs.example", text: "Your IDEAL statement for October is ready to download."})
	if n := len(signals("")); n != 2 {
		t.Errorf("text that is not an alert recorded a signal: %d signals", n)
	}
	deliver(alertMail{from: "ideal@dbs.example", text: "SGD 55.00 has been credited from SOMEONE NEW to your account."})
	var orphan map[string]any
	for _, s := range signals("pending") {
		if s["payer"] == "SOMEONE NEW" {
			orphan = s
		}
	}
	if orphan == nil || orphan["invoice"] != nil {
		t.Fatalf("an alert for no invoice: %v", signals("pending"))
	}

	// Next morning's report: the line waits for Reconcile now, is over the line limit, and a person matches it.
	report := reportMail{from: "ideal@dbs.example",
		csv: []byte("Date,Description,Amount,Balance\r\n" + e.clock.Now().Format("02/01/2006") + ",GIRO ACME ROBOTICS " + before.str("number") + ",10900.00,10900.00\r\n")}
	deliver(report)
	lines := e.call(http.MethodGet, "/v1/bank-lines?money_account_id="+m.ID.String(), bk, nil)
	items, _ := lines.Body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the report's line: %s", lines.Raw)
	}
	lineID := items[0].(map[string]any)["id"].(string)
	if r := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", bk, map[string]any{"money_account_id": m.ID.String()}); r.Status != 200 || r.Body["reconciled"] != 0.0 {
		t.Fatalf("Reconcile now over the line limit: %d %s", r.Status, r.Raw)
	}
	l := e.call(http.MethodGet, "/v1/bank-lines/"+lineID, bk, nil)
	mt := e.call(http.MethodPost, "/v1/bank-lines/"+lineID+"/match-invoices", bk, map[string]any{"customer_id": acme.ID.String(),
		"invoices": []map[string]any{{"invoice_id": inv, "amount": sgd("10900.00")}}}, "If-Match", l.Header.Get("ETag"))
	if mt.Status != 200 || mt.str("line", "status") != storage.LineMatched {
		t.Fatalf("matching the line: %d %s", mt.Status, mt.Raw)
	}
	closed := signals("closed")
	if len(closed) != 1 || closed[0]["id"] != pending[0]["id"] || closed[0]["closed_line_id"] != lineID {
		t.Fatalf("the signal after the match: %v", closed)
	}
	if paid := invoice(inv); paid.str("states", "settlement") != "paid" || paid.Body["likely_paid"] != nil {
		t.Errorf("the invoice after the match: %s", paid.Raw)
	}

	// The alert no line closed expires after the setting's 7 days, not before, as the scheduler.
	if n, err := banking.ExpireSignals(context.Background(), worker, e.books.Org, e.clock.Now().AddDate(0, 0, 6)); err != nil || n != 0 {
		t.Fatalf("after 6 days: %d expired, %v", n, err)
	}
	if n, err := banking.ExpireSignals(context.Background(), worker, e.books.Org, e.clock.Now().AddDate(0, 0, 8)); err != nil || n != 1 {
		t.Fatalf("after 8 days: %d expired, %v", n, err)
	}
	if ex := signals("expired"); len(ex) != 1 || ex[0]["id"] != orphan["id"] || len(signals("pending")) != 0 {
		t.Errorf("the expired signal: %v", ex)
	}
	e.problemIs(dismiss(orphan["id"]), "dismissBankSignal", 409, storage.CodeBanking)
	e.problemIs(e.call(http.MethodPost, "/v1/bank-signals/"+uuid.NewString()+"/dismiss", bk, map[string]any{"reason": "None"}),
		"dismissBankSignal", 404, CodeNotFound)
}
