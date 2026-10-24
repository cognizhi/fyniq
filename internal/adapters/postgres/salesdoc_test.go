//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
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
