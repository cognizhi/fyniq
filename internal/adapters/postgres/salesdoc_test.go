//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// FR-AR-05 (p1-sales task 5.1): what an invoice renders with, worked by hand. GST-registered from 1 Oct (202612345K),
// INV-0001 to Acme on 14 Oct is 2 × S$500.00 SR and 1 × S$200.00 ZR: S$1,200.00 before GST, S$90.00 GST at 9% on
// S$1,000.00 and none at 0% on S$200.00, S$1,290.00; due on Acme's 14 days; after RC-0001's S$700.00 it shows S$700.00
// paid and S$590.00 due; its lines show 9% and 0%. A USD 1,000.00 draft to Globex at the cascade's 1.3456 shows S$1,345.60,
// S$121.10 and S$1,466.70 in SGD, and no number. With simplified tax invoices on from 15 Oct, an S$100.00 SR invoice
// that day (S$109.00) is simplified, one of S$1,000.00 (S$1,090.00) is not, nor one on 14 Oct; a credit note is not
// laid out as an invoice.
// req: FR-AR-05
func TestInvoicesFillTheirDocument(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Documents Pte Ltd")
	invoices, receipts := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var dbs storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 1)}); err != nil {
			return err
		}
		if dbs, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 14), Rate: "1.3456", Quote: "1.3456",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	data := func(doc storage.Document) (salesdoc.Invoice, error) {
		t.Helper()
		var out salesdoc.Invoice
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, doc.ID)
			if err != nil {
				return err
			}
			settings, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return err
			}
			out, err = salesdoc.InvoiceData(ctx, u, doc, settings)
			return err
		})
		return out, err
	}
	field := func(d salesdoc.Invoice, path string) string {
		v, ok := d.Data.Fields[path]
		switch {
		case !ok:
			return "-"
		case v.Amount != nil:
			return v.Amount.Amount + " " + v.Amount.Currency
		case v.Date != "":
			return v.Date
		}
		return v.Text
	}
	fields := func(d salesdoc.Invoice, paths ...string) string {
		out := ""
		for _, p := range paths {
			out += field(d, p) + "; "
		}
		return out
	}
	create := func(customer storage.Contact, day int, cur money.Currency, lines ...invoice.Line) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: customer.ID, Date: porttest.Day(10, day), Currency: cur, Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}

	inv := create(b.Customer, 14, sgd, invoiceLine(t, b, "2", "500.00", "", "SR", "SGD"), invoiceLine(t, b, "1", "200.00", "", "ZR", "SGD"))
	if _, err := invoices.Post(ctx, b.Org, inv.ID); err != nil {
		t.Fatal(err)
	}
	part, _ := money.Parse("700.00", sgd)
	if _, err := receipts.Record(ctx, b.Org, receipt.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 17), AccountID: dbs.ID, Currency: sgd,
		Invoices: []receipt.Part{{InvoiceID: inv.ID, Amount: part}}, Unapplied: money.Zero(sgd)}); err != nil {
		t.Fatal(err)
	}
	d, err := data(inv)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fields(d, "organisation.gst_number", "customer.name", "document.number", "document.date", "document.due_date", "document.payment_terms",
		"document.subtotal", "document.gst_total", "document.total", "document.amount_paid", "document.amount_due", "document.total_sgd"),
		"202612345K; Acme Analytics Pte Ltd; INV-0001; 2026-10-14; 2026-10-28; 14 days; 1200.00 SGD; 90.00 SGD; 1290.00 SGD; 700.00 SGD; 590.00 SGD; -; "; got != want {
		t.Errorf("INV-0001's fields:\n got %s\nwant %s", got, want)
	}
	lines := ""
	for _, l := range d.Data.Lines {
		lines += fmt.Sprintf("%s × %s %s %s; ", l.Quantity, l.UnitPrice, l.GSTRate, l.Amount.Amount)
	}
	if want := "2 × 500.00 9% 1000.00; 1 × 200.00 0% 200.00; "; lines != want || !d.Data.GSTRegistered || d.Data.Simplified ||
		len(d.Data.GSTSummary) != 2 || d.Data.GSTSummary[0] != (render.GSTRow{Rate: "9%", Net: render.Amount{Amount: "1000.00", Currency: "SGD"},
		GST: render.Amount{Amount: "90.00", Currency: "SGD"}}) || d.Data.GSTSummary[1] != (render.GSTRow{Rate: "0%", Net: render.Amount{Amount: "200.00", Currency: "SGD"},
		GST: render.Amount{Amount: "0.00", Currency: "SGD"}}) {
		t.Errorf("INV-0001's lines and GST: %s %+v", lines, d.Data)
	}

	foreign := create(b.Globex, 14, usd, invoiceLine(t, b, "1", "1000.00", "", "SR", "USD"))
	if d, err := data(foreign); err != nil || fields(d, "document.number", "document.total", "document.subtotal_sgd", "document.gst_total_sgd", "document.total_sgd") !=
		"-; 1090.00 USD; 1345.60 SGD; 121.10 SGD; 1466.70 SGD; " || !d.Created.Equal(porttest.Day(10, 14)) {
		t.Errorf("the USD draft: %s %v %v", fields(d, "document.number", "document.subtotal_sgd", "document.gst_total_sgd", "document.total_sgd"), d.Created, err)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, salesdoc.SimplifiedSetting, "false", "true", porttest.Day(10, 15), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		day   int
		price string
		want  bool
	}{{"S$109.00 on 15 Oct", 15, "100.00", true}, {"S$1,090.00 on 15 Oct", 15, "1000.00", false}, {"S$109.00 on 14 Oct", 14, "100.00", false}} {
		if d, err := data(create(b.Customer, c.day, sgd, invoiceLine(t, b, "1", c.price, "", "SR", "SGD"))); err != nil || d.Data.Simplified != c.want {
			t.Errorf("%s: simplified %v, %v", c.name, d.Data.Simplified, err)
		}
	}
	credit, err := invoices.CreateCredit(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 15), Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "10.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	var se *salesdoc.Error
	if _, err := data(credit); !errors.As(err, &se) || se.Code != salesdoc.CodeNotInvoice {
		t.Errorf("a credit note: %v", err)
	}
}

// FR-AR-06 (p1-sales task 5.2): the record of an invoice sent by email. Only an issued invoice's current version is
// sent: a draft, or an issued one at a stale version, is refused (FY196). A send names its message and the PDF in the
// invoice's evidence, is audited, and its delivery follows the provider's events: sent, then delivered.
// req: FR-AR-06
// req: FR-INT-21
func TestASendIsOfAnIssuedInvoicesCurrentVersion(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Sends Pte Ltd")
	invoices := journal.Invoices{Store: s}
	sgd := money.MustLookup("SGD")
	inv, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	send := func(version int) (storage.DocumentSend, error) {
		var out storage.DocumentSend
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			m, err := u.EmailMessages().Queue(ctx, "invoice", "ap@acme.example", "Invoice INV-0001")
			if err != nil {
				return err
			}
			e, err := u.Evidence().Attach(ctx, newEvidence(b.Org, inv.ID, "Invoice as sent"))
			if err != nil {
				return err
			}
			out, err = u.DocumentSends().Record(ctx, storage.NewDocumentSend{DocumentID: inv.ID, DocumentVersion: version, MessageID: m.ID, EvidenceID: e.ID,
				Recipient: "ap@acme.example", TemplateSource: "platform", TemplateHash: strings.Repeat("cd", 32), PDFSHA256: strings.Repeat("ab", 32)})
			return err
		})
		return out, err
	}
	var se *storage.Error
	if _, err := send(1); !errors.As(err, &se) || se.Code != storage.CodeSend {
		t.Fatalf("a draft's send: %v", err)
	}
	if _, err := invoices.Post(ctx, b.Org, inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := send(2); !errors.As(err, &se) || se.Code != storage.CodeSend {
		t.Fatalf("a stale version's send: %v", err)
	}
	sent, err := send(1)
	if err != nil || sent.SentBy != "user:ar-specialist" || sent.MessageStatus != storage.EmailQueued {
		t.Fatalf("the issued invoice's send: %+v %v", sent, err)
	}
	latest := func() *storage.DocumentSend {
		t.Helper()
		var out map[uuid.UUID]storage.DocumentSend
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.DocumentSends().Latest(ctx, []uuid.UUID{inv.ID})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		x, ok := out[inv.ID]
		if !ok {
			return nil
		}
		return &x
	}
	if l := latest(); l == nil || l.ID != sent.ID || salesdoc.Delivery(l, false) != salesdoc.Sent {
		t.Fatalf("its delivery: %+v", l)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { // the provider's webhook, as the api receives it
		_, _, err := u.EmailMessages().RecordEvent(ctx, storage.EmailEvent{MessageID: sent.MessageID, Provider: "resend", DeliveryID: "evt-1",
			Kind: storage.EventDelivered, Recipient: "ap@acme.example", OccurredAt: porttest.Day(10, 15)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if l := latest(); l == nil || l.LastEvent != storage.EventDelivered || salesdoc.Delivery(l, false) != salesdoc.Delivered {
		t.Fatalf("after the provider's delivered: %+v", l)
	}
	if got := actions(t, b.Org, sent.ID); len(got) != 1 || got[0] != "document_send.recorded" {
		t.Errorf("the send's audit trail: %v", got)
	}
}

// FR-AR-06 (p1-sales task 5.3): an online link. Only of an issued invoice's current version (FY198); kept as its
// token's hash, which the registry routes to the organisation and link before any tenant is set; viewed once and
// revoked once, each audited, and otherwise never changed, deleted or truncated, whoever asks (FY197, the threat
// model's T7). The audit trail names the link, never its hash.
// req: FR-AR-06
// req: CTL-AUD-06
func TestALinkChangesOnlyInItsViewAndRevocation(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Links Pte Ltd")
	invoices := journal.Invoices{Store: s}
	inv, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"),
		Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	token, hash, err := salesdoc.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	link := func() (storage.DocumentLink, error) {
		var out storage.DocumentLink
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			m, err := u.EmailMessages().Queue(ctx, "invoice", "ap@acme.example", "Invoice INV-0001")
			if err != nil {
				return err
			}
			out, err = u.DocumentLinks().Create(ctx, storage.NewDocumentLink{DocumentID: inv.ID, DocumentVersion: 1, MessageID: m.ID, TokenHash: hash,
				ExpiresAt: porttest.Day(10, 15).AddDate(0, 0, salesdoc.LinkDays)})
			return err
		})
		return out, err
	}
	var se *storage.Error
	if _, err := link(); !errors.As(err, &se) || se.Code != storage.CodeLink {
		t.Fatalf("a draft's link: %v", err)
	}
	if _, err := invoices.Post(ctx, b.Org, inv.ID); err != nil {
		t.Fatal(err)
	}
	l, err := link()
	if err != nil || l.CreatedBy != "user:ar-specialist" || l.FirstViewedAt != nil || l.RevokedAt != nil {
		t.Fatalf("the issued invoice's link: %+v %v", l, err)
	}
	var routes storage.LinkRoutes = s
	if org, id, err := routes.LinkRoute(context.Background(), hash); err != nil || org != b.Org || id != l.ID {
		t.Fatalf("the route: %s %s %v", org, id, err)
	}
	if _, _, err := routes.LinkRoute(context.Background(), strings.Repeat("0", 64)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("an unknown hash: %v", err)
	}
	if _, err := link(); !errors.As(err, &se) || se.Code != storage.CodeLink {
		t.Errorf("the same token twice: %v", err)
	}
	do := func(fn func(u storage.UnitOfWork) error) error {
		return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) })
	}
	var first, again bool
	if err := do(func(u storage.UnitOfWork) (err error) {
		if _, first, err = u.DocumentLinks().Viewed(ctx, l.ID, porttest.Day(10, 16)); err != nil {
			return err
		}
		_, again, err = u.DocumentLinks().Viewed(ctx, l.ID, porttest.Day(10, 17))
		return err
	}); err != nil || !first || again {
		t.Fatalf("viewed: %v %v %v", first, again, err)
	}
	if err := do(func(u storage.UnitOfWork) error { _, err := u.DocumentLinks().Revoke(ctx, l.ID); return err }); err != nil {
		t.Fatal(err)
	}
	if err := do(func(u storage.UnitOfWork) error { _, err := u.DocumentLinks().Revoke(ctx, l.ID); return err }); !errors.As(err, &se) || se.Code != storage.CodeLink {
		t.Errorf("revoking twice: %v", err)
	}
	if got := actions(t, b.Org, l.ID); strings.Join(got, ",") != "document_link.created,document_link.viewed,document_link.revoked" {
		t.Errorf("the link's audit trail: %v", got)
	}
	for _, e := range events(t, b.Org) {
		if strings.Contains(e.Body, hash) || strings.Contains(e.Body, token) {
			t.Fatalf("an audit event holds the token or its hash: %s", e.Body)
		}
	}
	for _, role := range allRoles {
		c := conn(t, role)
		for _, stmt := range []string{
			"UPDATE fyniq.document_link SET revoked_at = NULL, revoked_by = NULL",
			"UPDATE fyniq.document_link SET first_viewed_at = now()",
			"UPDATE fyniq.document_link SET expires_at = expires_at + interval '1 year'",
			"UPDATE fyniq.document_link SET token_hash = repeat('ab', 32)",
			"DELETE FROM fyniq.document_link",
			"TRUNCATE fyniq.document_link",
			"UPDATE fyniq.document_link_token SET owner = gen_random_uuid()",
			"DELETE FROM fyniq.document_link_token",
		} {
			err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			wantState(t, role+": "+stmt, err, "42501", "FY197", "FY002")
		}
	}
}
