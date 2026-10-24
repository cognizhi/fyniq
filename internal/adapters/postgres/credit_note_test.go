//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-04 and the posting rule "Credit documents and opening items" (p1-sales task 3.1, part): a credit note to a
// customer is drafted, issued with its own number (CN-), voided and edited through the invoice's posting service,
// posting an invoice's legs reversed (credit_note@1). Standalone, it adjusts GST at the rates of its own date; linked
// to an invoice, at the rates and the registration of that invoice's date, and it credits at most that invoice's total
// of that customer, in its currency, dated on or before it. The AR sub-ledger carries each issued credit note as a
// credit with the customer, and 1100 agrees; every entry replays (ENG-06).
// req: FR-AR-04
// req: ledger/posting-rules Credit documents and opening items
// req: INV-07
// req: FR-GL-12
func TestCreditNotesPostThroughThePostingService(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Credit Notes Pte Ltd")
	invoices := journal.Invoices{Store: s}
	sgd := money.MustLookup("SGD")
	oct14, oct20, oct21 := porttest.Day(10, 14), porttest.Day(10, 20), porttest.Day(10, 21)
	issue := func(customer uuid.UUID, on time.Time, price string) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: customer, Date: on, Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", "SGD")}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return reread(t, ctx, s, b.Org, doc.ID)
	}
	credit := func(d invoice.Draft) storage.Document {
		t.Helper()
		doc, err := invoices.CreateCredit(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}

	// INV-0001 on 14 October, before the GST registration from 16 October, charges none; INV-0002 on 20 October 9%.
	before := issue(b.Customer.ID, oct14, "1000.00")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 16), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: oct14})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after := issue(b.Customer.ID, oct20, "1000.00")
	other := issue(b.Globex.ID, oct14, "50.00")

	// A standalone credit note: S$100.00 at 9%, CN-0001.
	standalone := credit(invoice.Draft{CustomerID: b.Customer.ID, Date: oct21, Currency: sgd, Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}})
	c, err := invoices.Check(ctx, b.Org, standalone.ID, nil)
	if err != nil || len(c.Problems) > 0 || c.Receivable.String() != "109.00" || !c.Prepared.DueDate.IsZero() {
		t.Fatalf("the check of a standalone credit note: %+v %v", c, err)
	}
	e, err := invoices.Post(ctx, b.Org, standalone.ID)
	if err != nil || legsOf(t, s, b.Org, e) != "1100 -109 SGD = -109; 4000 100 SGD = 100; 2200 9 SGD = 9" || e.Memo != "Acme Analytics Pte Ltd CN-0001" {
		t.Fatalf("a standalone credit note: %s %q %v", legsOf(t, s, b.Org, e), e.Memo, err)
	}
	if standalone = reread(t, ctx, s, b.Org, standalone.ID); standalone.Number != "CN-0001" || standalone.Status != storage.DocumentPosted {
		t.Fatalf("the issued credit note: %+v", standalone)
	}

	// Linked to INV-0001, it adjusts the GST of a supply that bore none: none, though the company is registered by now.
	linked := credit(invoice.Draft{CustomerID: b.Customer.ID, Credits: before.ID, Date: oct21, Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "200.00", "", "SR", "SGD")}})
	if c, err = invoices.Check(ctx, b.Org, linked.ID, nil); err != nil || len(c.Problems) > 0 || c.Prepared.Registered {
		t.Fatalf("the check of a credit note of an invoice before registration: %+v %v", c, err)
	}
	if e, err = invoices.Post(ctx, b.Org, linked.ID); err != nil || legsOf(t, s, b.Org, e) != "1100 -200 SGD = -200; 4000 200 SGD = 200" {
		t.Fatalf("a credit note of INV-0001: %s %v", legsOf(t, s, b.Org, e), err)
	}
	// Linked to INV-0002, all of it: S$1,090.00 back.
	whole := credit(invoice.Draft{CustomerID: b.Customer.ID, Credits: after.ID, Date: oct21, Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "1000.00", "", "SR", "SGD")}})
	if e, err = invoices.Post(ctx, b.Org, whole.ID); err != nil || legsOf(t, s, b.Org, e) != "1100 -1090 SGD = -1090; 4000 1000 SGD = 1000; 2200 90 SGD = 90" {
		t.Fatalf("a credit note of all of INV-0002: %s %v", legsOf(t, s, b.Org, e), err)
	}

	// The refusals, each with its code, and no number taken.
	draft, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: oct20, Currency: sgd, Lines: []invoice.Line{invoiceLine(t, b, "1", "5.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	spread := invoiceLine(t, b, "1", "10.00", "", "SR", "SGD")
	spread.SpreadMonths = 3
	for _, x := range []struct {
		name  string
		d     invoice.Draft
		codes []string
		says  string
	}{
		{"of a draft invoice, with a due date and a spread", invoice.Draft{CustomerID: b.Customer.ID, Credits: draft.ID, Date: oct21, DueDate: oct21, Currency: sgd,
			Lines: []invoice.Line{spread}}, []string{invoice.CodeCredited, invoice.CodeDueDate, ledger.CodeSpread}, "is draft, not issued"},
		{"of another customer's invoice", invoice.Draft{CustomerID: b.Customer.ID, Credits: other.ID, Date: oct21, Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "1", "5.00", "", "SR", "SGD")}}, []string{invoice.CodeCredited}, "is to another customer"},
		{"more than the invoice", invoice.Draft{CustomerID: b.Customer.ID, Credits: before.ID, Date: oct21, Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "1", "1000.01", "", "SR", "SGD")}}, []string{invoice.CodeCredited}, "more than the 1000.00 SGD"},
		{"dated before the invoice", invoice.Draft{CustomerID: b.Customer.ID, Credits: after.ID, Date: oct14, Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "1", "5.00", "", "SR", "SGD")}}, []string{invoice.CodeCredited}, "after the credit note's"},
		{"of a document that is not an invoice", invoice.Draft{CustomerID: b.Customer.ID, Credits: standalone.ID, Date: oct21, Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "1", "5.00", "", "SR", "SGD")}}, []string{invoice.CodeCredited}, "is a credit_note, not an invoice"},
	} {
		doc := credit(x.d)
		var probs ledger.Problems
		if _, err := invoices.Post(ctx, b.Org, doc.ID); !asProblems(err, &probs) {
			t.Fatalf("%s: %v", x.name, err)
		}
		for _, code := range x.codes {
			if !probs.Has(code) {
				t.Errorf("%s: no %s among %v", x.name, code, probs)
			}
		}
		if !strings.Contains(probs.Error(), x.says) {
			t.Errorf("%s: %v does not say %q", x.name, probs, x.says)
		}
		if doc = reread(t, ctx, s, b.Org, doc.ID); doc.Number != "" || doc.Status != storage.DocumentDraft {
			t.Errorf("%s took a number: %+v", x.name, doc)
		}
	}
	naming, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Credits: before.ID, Date: oct21, Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "5.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, err := invoices.Post(ctx, b.Org, naming.ID); !asProblems(err, &probs) || !probs.Has(invoice.CodeCredited) {
		t.Errorf("an invoice naming an invoice: %v", err)
	}

	// A void reverses CN-0001; an edit of CN-0002 reverses its version and posts the next, keeping its number.
	if e, err = invoices.Void(ctx, b.Org, standalone.ID, oct21, "Raised in error"); err != nil || legsOf(t, s, b.Org, e) != "1100 109 SGD = 109; 4000 -100 SGD = -100; 2200 -9 SGD = -9" {
		t.Fatalf("voiding CN-0001: %s %v", legsOf(t, s, b.Org, e), err)
	}
	ed, err := invoices.Edit(ctx, b.Org, linked.ID, invoice.Draft{CustomerID: b.Customer.ID, Credits: before.ID, Date: oct21, Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "150.00", "", "SR", "SGD")}}, "S$150, not S$200", "")
	if err != nil || legsOf(t, s, b.Org, ed.Reversal) != "1100 200 SGD = 200; 4000 -200 SGD = -200" || legsOf(t, s, b.Org, ed.Entry) != "1100 -150 SGD = -150; 4000 150 SGD = 150" {
		t.Fatalf("editing CN-0002: %s then %s %v", legsOf(t, s, b.Org, ed.Reversal), legsOf(t, s, b.Org, ed.Entry), err)
	}
	if linked = reread(t, ctx, s, b.Org, linked.ID); linked.Number != "CN-0002" || linked.Version != 2 {
		t.Errorf("the edited credit note: %+v", linked)
	}

	// The AR sub-ledger: the three invoices open, CN-0002 and CN-0003 credits with Acme; 1100 agrees.
	var r receivables.Reconciliation
	var l receivables.Ledger
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, l, err = receivables.Reconcile(ctx, u, sgd, b.AR.ID, porttest.Day(10, 31))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r.Unexplained() || r.Base.SubLedger.String() != "900.00" || r.Base.Control.String() != "900.00" {
		t.Fatalf("the reconciliation: %+v", r)
	}
	credits := map[string]string{}
	for _, c := range l.Customers {
		for _, it := range c.Items {
			if it.Kind == receivables.KindCredit {
				credits[it.Number] = it.Open.String() + " of " + it.Total.String()
			}
		}
	}
	if len(credits) != 2 || credits["CN-0002"] != "-150.00 of 150.00" || credits["CN-0003"] != "-1090.00 of 1090.00" {
		t.Errorf("the credits: %v", credits)
	}

	// Every entry replays: a linked credit note reads its invoice's date again for its GST.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// reread is a document as it stands.
func reread(t *testing.T, ctx context.Context, s storage.Store, org, id uuid.UUID) storage.Document {
	t.Helper()
	var doc storage.Document
	if err := s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, err = u.Documents().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return doc
}
