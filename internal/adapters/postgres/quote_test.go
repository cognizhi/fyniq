//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func hasQuoteCode(err error, code string) bool {
	var probs ledger.Problems
	if errors.As(err, &probs) {
		return probs.Has(code)
	}
	var qe *quote.Error
	return errors.As(err, &qe) && qe.Code == code
}

// FR-AR-01 (p1-sales task 4.1, part): a quote of 2 × S$500.00 at 9% to Acme on 14 Oct, valid until 13 Nov, comes to
// S$1,090.00; sending it numbers it QT-0001 and posts no entry; a sent quote never changes; cancelling it reverses the
// document, with no entry. A quote with no validity date, or one before its date, and one to a supplier are refused and
// take no number; a sales order needs no validity date (SO-0001); a quote deducts no deposit.
// req: FR-AR-01
func TestQuotesAreSentAndCancelled(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Quotes Pte Ltd")
	quotes := journal.Quotes{Store: s}
	sgd := money.MustLookup("SGD")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 1)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	draft := func(customer uuid.UUID, validUntil time.Time) quote.Draft {
		return quote.Draft{Content: invoice.Draft{CustomerID: customer, Date: porttest.Day(10, 14), Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "2", "500.00", "", "SR", "SGD")}}, ValidUntil: validUntil}
	}
	entries := func() int {
		t.Helper()
		var n int
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			es, err := u.Ledger().Entries(ctx, 0, 1000)
			n = len(es)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}

	q, err := quotes.Create(ctx, b.Org, draft(b.Customer.ID, porttest.Day(11, 13)), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, probs, err := journal.CheckQuote(ctx, u, q)
		if err == nil && (len(probs) > 0 || p.Invoice.Totals.Total.String() != "1090.00" || p.Base.String() != "1090.00") {
			t.Errorf("the check: %+v %v", p, probs)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := entries()
	sent, err := quotes.Send(ctx, b.Org, q.ID)
	if err != nil || sent.Number != "QT-0001" || sent.Status != storage.DocumentPosted || quote.Status(sent, nil) != quote.StatusSent || entries() != before {
		t.Fatalf("sending it: %+v %v (%d entries, %d before)", sent, err, entries(), before)
	}
	if _, err := quotes.Revise(ctx, b.Org, q.ID, draft(b.Customer.ID, porttest.Day(11, 14))); !hasQuoteCode(err, quote.CodeNotDraft) {
		t.Errorf("revising a sent quote: %v", err)
	}
	if _, err := quotes.Send(ctx, b.Org, q.ID); !hasQuoteCode(err, quote.CodeNotDraft) {
		t.Errorf("sending twice: %v", err)
	}

	// The refusals: no validity date, one before its date, a supplier; none takes a number.
	for _, x := range []struct {
		name string
		d    quote.Draft
		code string
	}{
		{"no validity date", draft(b.Customer.ID, time.Time{}), quote.CodeValidity},
		{"valid before its date", draft(b.Customer.ID, porttest.Day(10, 13)), quote.CodeValidity},
		{"a supplier", draft(b.Supplier.ID, porttest.Day(11, 13)), invoice.CodeCustomer},
	} {
		doc, err := quotes.Create(ctx, b.Org, x.d, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := quotes.Send(ctx, b.Org, doc.ID); !hasQuoteCode(err, x.code) {
			t.Errorf("%s: %v", x.name, err)
		}
	}
	order, err := quotes.Create(ctx, b.Org, draft(b.Customer.ID, time.Time{}), true)
	if err != nil {
		t.Fatal(err)
	}
	if order, err = quotes.Send(ctx, b.Org, order.ID); err != nil || order.Number != "SO-0001" || order.Type != storage.DocumentSalesOrder {
		t.Fatalf("an order with no validity date: %+v %v", order, err)
	}
	next, err := quotes.Create(ctx, b.Org, draft(b.Customer.ID, porttest.Day(11, 13)), false)
	if err != nil {
		t.Fatal(err)
	}
	if next, err = quotes.Send(ctx, b.Org, next.ID); err != nil || next.Number != "QT-0002" {
		t.Errorf("the refusals took numbers: %+v %v", next, err)
	}
	withDeposit := draft(b.Customer.ID, porttest.Day(11, 13))
	withDeposit.Content.Deposits = []invoice.DepositUse{{DepositID: uuid.New(), Amount: money.FromMinor(100, sgd)}}
	if _, err := quotes.Create(ctx, b.Org, withDeposit, false); !hasQuoteCode(err, quote.CodeContent) {
		t.Errorf("a quote deducting a deposit: %v", err)
	}

	// Cancelling QT-0001 reverses it, with no entry; a draft is not cancelled.
	cancelled, err := quotes.Cancel(ctx, b.Org, q.ID, "The customer chose another supplier")
	if err != nil || cancelled.Status != storage.DocumentReversed || quote.Status(cancelled, nil) != quote.StatusCancelled || entries() != before {
		t.Fatalf("cancelling it: %+v %v", cancelled, err)
	}
	d, err := quotes.Create(ctx, b.Org, draft(b.Customer.ID, porttest.Day(11, 13)), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quotes.Cancel(ctx, b.Org, d.ID, "Never sent"); !hasQuoteCode(err, quote.CodeNotOpen) {
		t.Errorf("cancelling a draft: %v", err)
	}
}

// FR-AR-01 (p1-sales task 4.1, part): the customer's answer and expiry. QT-0001 (valid until 13 Nov) is accepted on 20
// Oct by Jane Tan; QT-0002 is declined; QT-0003 (valid until 31 Oct) expires when the scheduler runs on 1 Nov, while
// QT-0004 (valid until 30 Nov), SO-0001 with no validity date, the answered quotes and a draft do not. A quote has one
// answer, which never changes: answering again, answering an expired quote and cancelling a declined one are refused;
// an accepted quote is still cancelled. The database keeps an outcome once, and only of a sent quote.
// req: FR-AR-01
func TestQuotesAreAnsweredAndExpire(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Answered Quotes Pte Ltd")
	quotes := journal.Quotes{Store: s}
	sgd := money.MustLookup("SGD")
	send := func(validUntil time.Time, order bool) storage.Document {
		t.Helper()
		doc, err := quotes.Create(ctx, b.Org, quote.Draft{Content: invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: sgd,
			Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}}, ValidUntil: validUntil}, order)
		if err != nil {
			t.Fatal(err)
		}
		if doc, err = quotes.Send(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	status := func(doc storage.Document) string {
		t.Helper()
		var out string
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := u.Documents().Get(ctx, doc.ID)
			if err != nil {
				return err
			}
			os, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{doc.ID})
			if err != nil {
				return err
			}
			var o *storage.QuoteOutcome
			if x, ok := os[doc.ID]; ok {
				o = &x
			}
			out = quote.Status(d, o)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	accepted, declined, lapsing := send(porttest.Day(11, 13), false), send(porttest.Day(11, 13), false), send(porttest.Day(10, 31), false)
	later, order := send(porttest.Day(11, 30), false), send(time.Time{}, true)

	o, err := quotes.Accept(ctx, b.Org, accepted.ID, journal.QuoteAnswer{On: porttest.Day(10, 20), Name: "Jane Tan", Note: "Confirmed by email"})
	if err != nil || o.Outcome != storage.QuoteAccepted || o.Name != "Jane Tan" || o.RecordedBy != "user:ar-specialist" || status(accepted) != quote.StatusAccepted {
		t.Fatalf("accepting QT-0001: %+v %v", o, err)
	}
	if _, err := quotes.Decline(ctx, b.Org, accepted.ID, journal.QuoteAnswer{On: porttest.Day(10, 21)}); !hasQuoteCode(err, quote.CodeAnswered) {
		t.Errorf("answering twice: %v", err)
	}
	if _, err := quotes.Decline(ctx, b.Org, declined.ID, journal.QuoteAnswer{On: porttest.Day(10, 21), Note: "Too expensive"}); err != nil || status(declined) != quote.StatusDeclined {
		t.Fatalf("declining QT-0002: %v", err)
	}
	if _, err := quotes.Accept(ctx, b.Org, later.ID, journal.QuoteAnswer{}); !hasQuoteCode(err, quote.CodeAnswer) {
		t.Errorf("an answer with no date: %v", err)
	}

	// The scheduler on 1 November expires QT-0003 alone.
	draft, err := quotes.Create(ctx, b.Org, quote.Draft{Content: invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 1), Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "1.00", "", "SR", "SGD")}}, ValidUntil: porttest.Day(10, 2)}, false)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := quotes.ExpireDue(ctx, b.Org, porttest.Day(11, 1))
	if err != nil || len(expired) != 1 || expired[0] != lapsing.ID || status(lapsing) != quote.StatusExpired {
		t.Fatalf("the expiry: %v %v", expired, err)
	}
	for _, d := range []storage.Document{accepted, declined, later, order, draft} {
		if st := status(d); st == quote.StatusExpired {
			t.Errorf("%s expired", d.ID)
		}
	}
	if again, err := quotes.ExpireDue(ctx, b.Org, porttest.Day(11, 1)); err != nil || len(again) != 0 {
		t.Errorf("expiring twice: %v %v", again, err)
	}
	if _, err := quotes.Accept(ctx, b.Org, lapsing.ID, journal.QuoteAnswer{On: porttest.Day(11, 2)}); !hasQuoteCode(err, quote.CodeAnswered) {
		t.Errorf("accepting an expired quote: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		os, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{lapsing.ID})
		if err == nil && os[lapsing.ID].RecordedBy != "scheduler:quotes.expiry" {
			t.Errorf("the expiry's recorder: %+v", os[lapsing.ID])
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A declined quote is not cancelled; an accepted one is.
	if _, err := quotes.Cancel(ctx, b.Org, declined.ID, "Withdrawn"); !hasQuoteCode(err, quote.CodeAnswered) {
		t.Errorf("cancelling a declined quote: %v", err)
	}
	if _, err := quotes.Cancel(ctx, b.Org, accepted.ID, "The customer withdrew"); err != nil || status(accepted) != quote.StatusCancelled {
		t.Fatalf("cancelling an accepted quote: %v", err)
	}

	// The database: once per quote, and only of a sent one.
	for name, n := range map[string]storage.NewQuoteOutcome{
		"a second outcome":      {DocumentID: later.ID, Outcome: storage.QuoteAccepted, On: porttest.Day(10, 20)},
		"an outcome of a draft": {DocumentID: draft.ID, Outcome: storage.QuoteAccepted, On: porttest.Day(10, 20)},
	} {
		if name == "a second outcome" {
			if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.QuoteOutcomes().Record(ctx, n)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.QuoteOutcomes().Record(ctx, n)
			return err
		})
		var se *storage.Error
		if !errors.As(err, &se) || se.Code != storage.CodeQuoteOutcome {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// FR-AR-02 (p1-sales task 4.2): invoicing a quote. QT-0001 to Acme, 10 × S$20.00 and 1 × S$300.00, is invoiced only once
// accepted. INV-0001 takes 4 of line 1 and names it; asking for more than is left, or a line it lacks, is refused.
// Issued on 21 Oct, the quote cannot be cancelled, and what it has still to invoice is 6 × S$20.00 and the S$300.00.
// The rest makes INV-0002, changed to 7 delivered before issue: issued invoices then take every line, so it shows
// invoiced, and nothing is left. A credit note's line, an invoice to Globex and one naming a declined quote are
// refused (QUOTE-11). Voiding INV-0001 takes it back to accepted.
// req: FR-AR-02
func TestQuotesAreInvoiced(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Invoiced Quotes Pte Ltd")
	quotes, invoices := journal.Quotes{Store: s}, journal.Invoices{Store: s}
	sgd := money.MustLookup("SGD")
	send := func(lines ...invoice.Line) storage.Document {
		t.Helper()
		doc, err := quotes.Create(ctx, b.Org, quote.Draft{Content: invoice.Draft{CustomerID: b.Customer.ID, Reference: "RFQ-7", Date: porttest.Day(10, 14),
			Currency: sgd, Lines: lines}, ValidUntil: porttest.Day(11, 13)}, false)
		if err != nil {
			t.Fatal(err)
		}
		if doc, err = quotes.Send(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	state := func(doc storage.Document) (string, []quote.LineState, quote.Draft) {
		t.Helper()
		var st string
		var states []quote.LineState
		var d quote.Draft
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, doc.ID)
			if err != nil {
				return err
			}
			os, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{doc.ID})
			if err != nil {
				return err
			}
			var o *storage.QuoteOutcome
			if x, ok := os[doc.ID]; ok {
				o = &x
			}
			if d, err = quote.Read(doc, sgd); err != nil {
				return err
			}
			if states, _, err = quote.Invoicing(ctx, u, doc.ID, d, sgd, uuid.Nil); err != nil {
				return err
			}
			st = quote.StatusOf(doc, o, states)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return st, states, d
	}
	left := func(states []quote.LineState) string {
		out := ""
		for _, x := range states {
			out += x.Quoted.String() + "/" + x.Invoiced.String() + "/" + x.Drafted.String() + "/" + x.Left().String() + " "
		}
		return out
	}
	issue := func(doc storage.Document, on time.Time, change func(*invoice.Draft)) error {
		t.Helper()
		d, err := invoice.Read(reread(t, ctx, s, b.Org, doc.ID), sgd)
		if err != nil {
			t.Fatal(err)
		}
		d.Date = on
		if change != nil {
			change(&d)
		}
		if _, err := invoices.Revise(ctx, b.Org, doc.ID, d); err != nil {
			t.Fatal(err)
		}
		_, err = invoices.Post(ctx, b.Org, doc.ID)
		return err
	}

	q := send(invoiceLine(t, b, "10", "20.00", "", "SR", "SGD"), invoiceLine(t, b, "1", "300.00", "", "SR", "SGD"))
	if _, err := quotes.Invoice(ctx, b.Org, q.ID, nil); !hasQuoteCode(err, quote.CodeNotAccepted) {
		t.Fatalf("invoicing a quote not accepted: %v", err)
	}
	if _, err := quotes.Accept(ctx, b.Org, q.ID, journal.QuoteAnswer{On: porttest.Day(10, 20)}); err != nil {
		t.Fatal(err)
	}
	inv1, err := quotes.Invoice(ctx, b.Org, q.ID, map[int]money.Quantity{1: quantityOf(t, "4")})
	if err != nil {
		t.Fatal(err)
	}
	d1, err := invoice.Read(inv1, sgd)
	if err != nil || inv1.Type != storage.DocumentInvoice || inv1.Status != storage.DocumentDraft || d1.CustomerID != b.Customer.ID || d1.Reference != "RFQ-7" ||
		d1.Notes != "From quote QT-0001" || !d1.Date.IsZero() || len(d1.Lines) != 1 || d1.Lines[0].Quantity.String() != "4" ||
		d1.Lines[0].UnitPrice.String() != "20.00" || d1.Lines[0].Quote == nil || *d1.Lines[0].Quote != (invoice.QuoteRef{QuoteID: q.ID, Line: 1}) {
		t.Fatalf("INV-0001 drafted from it: %+v %v", d1, err)
	}
	if st, states, _ := state(q); st != quote.StatusAccepted || left(states) != "10/0/4/6 1/0/0/1 " {
		t.Errorf("after the draft: %s %s", st, left(states))
	}
	for name, ask := range map[string]map[int]money.Quantity{"more than is left": {1: quantityOf(t, "7")}, "a line it lacks": {3: quantityOf(t, "1")}} {
		if _, err := quotes.Invoice(ctx, b.Org, q.ID, ask); !hasQuoteCode(err, quote.CodeLeft) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := issue(inv1, porttest.Day(10, 21), nil); err != nil {
		t.Fatalf("issuing INV-0001: %v", err)
	}
	if _, err := quotes.Cancel(ctx, b.Org, q.ID, "Withdrawn"); !hasQuoteCode(err, quote.CodeInvoiced) {
		t.Errorf("cancelling an invoiced quote: %v", err)
	}
	st, states, d := state(q)
	if un := d.Uninvoiced(states); st != quote.StatusAccepted || left(states) != "10/4/0/6 1/0/0/1 " || len(un.Content.Lines) != 2 ||
		un.Content.Lines[0].Quantity.String() != "6" || un.Content.Lines[1].Quantity.String() != "1" {
		t.Errorf("after INV-0001: %s %s %+v", st, left(states), un.Content.Lines)
	}

	inv2, err := quotes.Invoice(ctx, b.Org, q.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := issue(inv2, porttest.Day(10, 22), func(d *invoice.Draft) { d.Lines[0].Quantity = quantityOf(t, "7") }); err != nil {
		t.Fatalf("issuing INV-0002 with 7 delivered: %v", err)
	}
	if st, states, _ := state(q); st != quote.StatusInvoiced || left(states) != "10/11/0/0 1/1/0/0 " {
		t.Errorf("after INV-0002: %s %s", st, left(states))
	}
	if _, err := quotes.Invoice(ctx, b.Org, q.ID, nil); !hasQuoteCode(err, quote.CodeLeft) {
		t.Errorf("invoicing with nothing left: %v", err)
	}

	// Lines that cannot name the quote.
	declined := send(invoiceLine(t, b, "1", "50.00", "", "SR", "SGD"))
	if _, err := quotes.Decline(ctx, b.Org, declined.ID, journal.QuoteAnswer{On: porttest.Day(10, 20)}); err != nil {
		t.Fatal(err)
	}
	naming := func(customer uuid.UUID, id uuid.UUID) invoice.Draft {
		l := invoiceLine(t, b, "1", "20.00", "", "SR", "SGD")
		l.Quote = &invoice.QuoteRef{QuoteID: id, Line: 1}
		return invoice.Draft{CustomerID: customer, Date: porttest.Day(10, 23), Currency: sgd, Lines: []invoice.Line{l}}
	}
	for name, make := range map[string]func() (storage.Document, error){
		"a credit note's line": func() (storage.Document, error) {
			return invoices.CreateCredit(ctx, b.Org, naming(b.Customer.ID, q.ID))
		},
		"an invoice to Globex": func() (storage.Document, error) { return invoices.Create(ctx, b.Org, naming(b.Globex.ID, q.ID)) },
		"a declined quote": func() (storage.Document, error) {
			return invoices.Create(ctx, b.Org, naming(b.Customer.ID, declined.ID))
		},
	} {
		doc, err := make()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); !hasQuoteCode(err, quote.CodeQuoteLine) {
			t.Errorf("%s: %v", name, err)
		}
	}

	if _, err := invoices.Void(ctx, b.Org, inv1.ID, porttest.Day(10, 24), "Raised in error"); err != nil {
		t.Fatal(err)
	}
	if st, states, _ := state(q); st != quote.StatusAccepted || left(states) != "10/7/0/3 1/1/0/0 " {
		t.Errorf("after voiding INV-0001: %s %s", st, left(states))
	}
}
