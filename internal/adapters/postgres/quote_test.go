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
