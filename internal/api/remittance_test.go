//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/remitmail"
	"github.com/cognizhi/fyniq/internal/render"
)

// remittances is the worker's remittance sender over e, on the worker's own database role as cmd/worker runs it, so
// the fences on what the worker may write hold here too.
func (e *env) remittances() remitmail.Sender {
	r := testRenderer(e.t)
	w := workerStore(e.t)
	return remitmail.Sender{Store: w, Staging: e.blobs, Outbox: outbox.Dispatcher{Store: w, Queue: e.queue, Clock: e.clock},
		Render: func(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, created time.Time) (render.Response, error) {
			return r.Render(ctx, render.Request{DocumentType: documentType, Content: c, Data: d, Created: created, Check: true})
		}}
}

// runRemittances runs every queued job, the remittance advice through the worker's sender; it returns how many advices
// ran.
func (e *env) runRemittances() int {
	e.t.Helper()
	n := 0
	sender := e.remittances()
	for {
		kind, row := nextWorkRow(e.t, e)
		if kind == "" {
			return n
		}
		if kind == remitmail.Kind {
			n++
			if err := sender.Work()(context.Background(), e.books.Org, row); err != nil {
				e.t.Fatalf("the remittance job: %v", err)
			}
		}
	}
}

// releasedBatch prepares, approves and releases a batch from a new bank account of the items, paid on 20 October, and
// returns it and its bank account.
func (e *env) releasedBatch(items ...string) (response, storage.MoneyAccount) {
	e.t.Helper()
	e.bankBooks()
	m := e.newMoneyAccount()
	body := batchBody(m, items...)
	body["payment_date"] = "2026-10-20"
	r := e.call(http.MethodPost, "/v1/payment-batches", e.as(authz.Accountant), body)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("preparing the batch = %d: %s", r.Status, r.Raw)
	}
	e.approveBatch(r.str("id"))
	g := e.call(http.MethodGet, "/v1/payment-batches/"+r.str("id"), e.as(authz.Accountant), nil)
	rel := e.call(http.MethodPost, "/v1/payment-batches/"+r.str("id")+"/release", steppedUp(e, e.as(authz.PaymentReleaser)), nil, "If-Match", g.Header.Get("ETag"))
	if rel.Status != http.StatusOK {
		e.t.Fatalf("releasing the batch = %d: %s", rel.Status, rel.Raw)
	}
	return rel, m
}

// paymentOf is a released batch's payment to a payee, by name.
func paymentOf(t *testing.T, b response, payee string) map[string]any {
	t.Helper()
	for _, x := range b.Body["payments"].([]any) {
		p := x.(map[string]any)
		if p["payee"].(map[string]any)["name"] == payee {
			return p
		}
	}
	t.Fatalf("no payment to %s in %s", payee, b.Raw)
	return nil
}

// When bank import matches a batch's supplier payment to its statement line, the supplier is sent its remittance advice
// (FR-BNK-12, design D8): the rendered PDF kept as the payment's evidence and emailed to its remittance address, once,
// however often the match is undone and done again. A supplier with no remittance address is warned of before release
// and sent nothing; a claimant's reimbursement sends nothing. The batch is paid when every payment is matched. The job
// runs on the worker's own database role, which writes advice for no other payment.
// req: FR-BNK-12
// req: FR-AP-13
// req: banking/bank-transactions Remittance advice on the match (rev 0.154)
// req: QA-ENG-05
// asvs: V8.2.2
func TestRemittanceAdviceIsSentWhenTheBatchPaymentMatches(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	northwind, quiet := e.vendor("Northwind Analytics Pte Ltd"), e.vendor("Quiet Supplier Pte Ltd")
	if r := e.call(http.MethodPatch, "/v1/contacts/"+northwind, accountant, map[string]any{"remittance_email": "payments@northwind.example"}, "If-Match", "*"); r.Status != http.StatusOK {
		t.Fatalf("setting the remittance email = %d: %s", r.Status, r.Raw)
	}
	e.vendorAccount(northwind, "0123456789")
	e.vendorAccount(quiet, "1112223334")
	person := e.personAccount("5550001234")
	b1, b2, claim := e.billOf(northwind, "100.00", "9.00"), e.billOf(quiet, "50.00", "4.50"), e.postedClaim(person)

	draft := e.call(http.MethodPost, "/v1/payment-batches", accountant, batchBody(e.batchAccount(), "bill:"+b1, "bill:"+b2))
	if !strings.Contains(draft.Raw, CodeBatchNoRemittance) || !strings.Contains(draft.Raw, "Quiet Supplier Pte Ltd has no remittance email") {
		t.Errorf("no warning of the supplier with no remittance email: %s", draft.Raw)
	}

	rel, m := e.releasedBatch("bill:"+b1, "bill:"+b2, "claim:"+claim)
	id := rel.str("id")
	if p := paymentOf(t, rel, "Northwind Analytics Pte Ltd"); p["remittance"] != "waiting" || p["paid"] != false || p["amount"].(map[string]any)["amount"] != "109.00" {
		t.Fatalf("Northwind's payment before the match: %v", p)
	}
	if p := paymentOf(t, rel, "Quiet Supplier Pte Ltd"); p["remittance"] != "no_email" {
		t.Errorf("the quiet supplier's payment: %v", p)
	}
	if e.runRemittances() != 0 {
		t.Fatal("an advice was sent before any match")
	}

	match := func(amount, payment string) string {
		l := e.lineOn(m, amount, nil)
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/match-recorded", accountant, map[string]any{"document_id": payment}, "If-Match", etag(l.Version))
		if r.Status != http.StatusOK {
			t.Fatalf("matching %s = %d: %s", amount, r.Status, r.Raw)
		}
		return l.ID.String()
	}
	nw := paymentOf(t, rel, "Northwind Analytics Pte Ltd")["payment_id"].(string)
	line := match("-109.00", nw)
	if n := e.runRemittances(); n != 1 {
		t.Fatalf("%d remittance jobs after the match, want 1", n)
	}
	g := e.call(http.MethodGet, "/v1/payment-batches/"+id, accountant, nil)
	if p := paymentOf(t, g, "Northwind Analytics Pte Ltd"); p["remittance"] != "sent" || p["paid"] != true {
		t.Fatalf("Northwind's payment after the match: %v", p)
	}
	if g.str("status") != "released" {
		t.Errorf("a batch with payments unmatched is %s", g.str("status"))
	}
	var sent []storage.DocumentSend
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		sent, err = u.DocumentSends().OfDocument(ctx, uuid.MustParse(nw))
		return err
	})
	if len(sent) != 1 || sent[0].Recipient != "payments@northwind.example" {
		t.Fatalf("the sends of Northwind's payment: %+v", sent)
	}

	// The worker's fence (FY201, FY202; migration 000189): it keeps advice and records a send only for a batch payment
	// matched to the bank, not the quiet supplier's (released, unmatched) nor a payment recorded by hand, and queues
	// remittance email only with such a send.
	hand := e.call(http.MethodPost, "/v1/bill-payments", accountant, e.payment(e.billOf(northwind, "1.00", "0.09"), "1.09"))
	if hand.Status != http.StatusCreated {
		t.Fatalf("paying a bill by hand = %d: %s", hand.Status, hand.Raw)
	}
	asWorker := func(stmt string) error {
		ctx := context.Background()
		c, err := pgx.Connect(ctx, db.URL("fyniq_worker"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(ctx)
		tx, err := c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", e.books.Org.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for _, p := range []string{paymentOf(t, g, "Quiet Supplier Pte Ltd")["payment_id"].(string), hand.str("payment", "id")} {
		for _, stmt := range []string{
			"INSERT INTO fyniq.evidence (document_id, evidence_type, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class, staging_key, uploaded_by) " +
				"VALUES ('" + p + "', 'Remittance advice as sent', 'x.pdf', 'application/pdf', 1, repeat('ab', 32), repeat('ab', 32), 'generated', 'general', 'staging/x', '" + remitmail.Actor.String() + "')",
			"INSERT INTO fyniq.document_send (document_id, document_version, evidence_id, message_id, recipient, sent_by, template_source, template_hash, pdf_sha256) " +
				"VALUES ('" + p + "', 1, '" + uuid.NewString() + "', '" + uuid.NewString() + "', 'victim@example.com', '" + remitmail.Actor.String() + "', 'default', repeat('ab', 32), repeat('ab', 32))",
		} {
			if err := asWorker(stmt); err == nil || !strings.Contains(err.Error(), "FY201") {
				t.Errorf("the worker: %s: %v", stmt, err)
			}
		}
	}
	if err := asWorker("INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES ('remittance', 'victim@example.com', 'Paid')"); err == nil || !strings.Contains(err.Error(), "FY202") {
		t.Errorf("the worker queued remittance email no send carries: %v", err)
	}

	// Undone and done again: no second advice.
	var l storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		l, err = u.Banking().Line(ctx, uuid.MustParse(line))
		return err
	})
	if u := e.call(http.MethodPost, "/v1/bank-lines/"+line+"/unreconcile", accountant, map[string]any{"reason": "Matched to the wrong line"}, "If-Match", etag(l.Version)); u.Status != http.StatusOK {
		t.Fatalf("undoing the match = %d: %s", u.Status, u.Raw)
	}
	match("-109.00", nw)
	if n := e.runRemittances(); n != 0 {
		t.Errorf("%d remittance jobs after the match was done again, want none", n)
	}

	// The quiet supplier's and the claimant's payments matched: no advice, and the batch is paid.
	match("-54.50", paymentOf(t, g, "Quiet Supplier Pte Ltd")["payment_id"].(string))
	for _, x := range g.Body["payments"].([]any) {
		if p := x.(map[string]any); p["payee"].(map[string]any)["kind"] == "person" {
			if _, ok := p["remittance"]; ok {
				t.Errorf("a claimant's payment has a remittance: %v", p)
			}
			match("-27.30", p["payment_id"].(string))
		}
	}
	e.runRemittances()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		msgs, err := u.EmailMessages().List(ctx, storage.EmailMessageFilter{Purpose: "remittance", Limit: 100})
		n := 0
		for _, msg := range msgs {
			if msg.Purpose == "remittance" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d remittance emails, want 1", n)
		}
		return err
	})
	if g := e.call(http.MethodGet, "/v1/payment-batches/"+id, accountant, nil); g.str("status") != "paid" {
		t.Errorf("every payment matched, and the batch is %s: %s", g.str("status"), g.Raw)
	}
}

// A remittance job that gave up after its attempts shows as failed on the batch, so the supplier can be told of the
// payment another way; nothing queues it again (design D8).
// req: FR-BNK-12
func TestARemittanceThatGaveUpShowsAsFailed(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	northwind := e.vendor("Northwind Analytics Pte Ltd")
	if r := e.call(http.MethodPatch, "/v1/contacts/"+northwind, accountant, map[string]any{"remittance_email": "payments@northwind.example"}, "If-Match", "*"); r.Status != http.StatusOK {
		t.Fatalf("setting the remittance email = %d: %s", r.Status, r.Raw)
	}
	e.vendorAccount(northwind, "0123456789")
	rel, m := e.releasedBatch("bill:" + e.billOf(northwind, "100.00", "9.00"))
	nw := paymentOf(t, rel, "Northwind Analytics Pte Ltd")["payment_id"].(string)
	l := e.lineOn(m, "-109.00", nil)
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/match-recorded", accountant, map[string]any{"document_id": nw}, "If-Match", etag(l.Version)); r.Status != http.StatusOK {
		t.Fatalf("matching = %d: %s", r.Status, r.Raw)
	}
	if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.Outbox().ByKey(ctx, remitmail.Kind, remitmail.Key(uuid.MustParse(nw)))
		if err != nil || row == nil {
			t.Fatalf("the remittance job: %+v, %v", row, err)
		}
		return u.Sweeping().MarkDead(ctx, row.ID, "the render service did not answer")
	}); err != nil {
		t.Fatal(err)
	}
	g := e.call(http.MethodGet, "/v1/payment-batches/"+rel.str("id"), accountant, nil)
	if p := paymentOf(t, g, "Northwind Analytics Pte Ltd"); p["remittance"] != "failed" || p["paid"] != true {
		t.Fatalf("Northwind's payment after its remittance gave up: %v", p)
	}
}

// A batch payment voided after its match queued the remittance advice, and before the worker ran it, sends nothing:
// no advice kept, no send recorded, no email; the job runs once and is done (FR-BNK-12, design D8). The void is a
// person's direct one, so the organisation is in Standard mode (FM-1 asks for it in Founder mode).
// req: FR-BNK-12
// req: FR-AP-13
func TestAPaymentVoidedBeforeItsRemittanceSendsNothing(t *testing.T) {
	e := inStandardMode(newEnv(t))
	accountant := e.as(authz.Accountant)
	northwind := e.vendor("Northwind Analytics Pte Ltd")
	if r := e.call(http.MethodPatch, "/v1/contacts/"+northwind, accountant, map[string]any{"remittance_email": "payments@northwind.example"}, "If-Match", "*"); r.Status != http.StatusOK {
		t.Fatalf("setting the remittance email = %d: %s", r.Status, r.Raw)
	}
	e.vendorAccount(northwind, "0123456789")
	rel, m := e.releasedBatch("bill:" + e.billOf(northwind, "100.00", "9.00"))
	nw := paymentOf(t, rel, "Northwind Analytics Pte Ltd")["payment_id"].(string)
	l := e.lineOn(m, "-109.00", nil)
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/match-recorded", accountant, map[string]any{"document_id": nw}, "If-Match", etag(l.Version)); r.Status != http.StatusOK {
		t.Fatalf("matching = %d: %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		l, err = u.Banking().Line(ctx, l.ID)
		return err
	})
	if u := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/unreconcile", accountant, map[string]any{"reason": "Not this payment"}, "If-Match", etag(l.Version)); u.Status != http.StatusOK {
		t.Fatalf("undoing the match = %d: %s", u.Status, u.Raw)
	}
	g := e.call(http.MethodGet, "/v1/bill-payments/"+nw, accountant, nil)
	if v := e.call(http.MethodPost, "/v1/bill-payments/"+nw+"/void", accountant, map[string]any{"date": "2026-10-21", "reason": "Returned by the bank"}, "If-Match", g.Header.Get("ETag")); v.Status != http.StatusOK {
		t.Fatalf("voiding the payment = %d: %s", v.Status, v.Raw)
	}
	if n := e.runRemittances(); n != 1 {
		t.Fatalf("%d remittance jobs ran, want the one the match queued", n)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		sent, err := u.DocumentSends().OfDocument(ctx, uuid.MustParse(nw))
		if err != nil {
			return err
		}
		files, err := u.Evidence().OfDocument(ctx, uuid.MustParse(nw), uuid.Nil, 10)
		if err != nil {
			return err
		}
		msgs, err := u.EmailMessages().List(ctx, storage.EmailMessageFilter{Purpose: "remittance", Limit: 10})
		if len(sent) != 0 || len(files) != 0 || len(msgs) != 0 {
			t.Errorf("a voided payment's remittance: sends %+v, evidence %+v, email %+v", sent, files, msgs)
		}
		return err
	})
	if n := e.runRemittances(); n != 0 {
		t.Errorf("%d remittance jobs ran again", n)
	}
}
