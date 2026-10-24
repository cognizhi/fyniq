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
	if err != nil || sent.Number != "QT-0001" || sent.Status != storage.DocumentPosted || quote.Status(sent) != quote.StatusSent || entries() != before {
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
	if err != nil || cancelled.Status != storage.DocumentReversed || quote.Status(cancelled) != quote.StatusCancelled || entries() != before {
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
