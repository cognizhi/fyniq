//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// salesBooks are Books with the sales rules' system accounts, a customer and a USD customer.
type salesBooks struct {
	porttest.Books
	AR, GST, FX, Deferred storage.Account
	Customer, Globex      storage.Contact
	Supplier              storage.Contact
}

func newSalesBooks(t *testing.T, ctx context.Context, s storage.Store, name string) salesBooks {
	t.Helper()
	b := salesBooks{Books: porttest.NewBooks(t, s, name)}
	fourteen := 14
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		for _, a := range []struct {
			to *storage.Account
			n  storage.NewAccount
		}{
			{&b.AR, storage.NewAccount{Code: "1100", Name: "Accounts receivable", Type: storage.Asset, Role: storage.AccountRole(ledger.RoleAccountsReceivable)}},
			{&b.GST, storage.NewAccount{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)}},
			{&b.Deferred, storage.NewAccount{Code: "2400", Name: "Deferred revenue", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleDeferredRevenue)}},
			{&b.FX, storage.NewAccount{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense, Role: storage.AccountRole(ledger.RoleFXRounding)}},
		} {
			if *a.to, err = u.Accounts().Create(ctx, a.n); err != nil {
				return err
			}
		}
		if b.Customer, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Acme Analytics Pte Ltd", Customer: true, PaymentTermsDays: &fourteen}); err != nil {
			return err
		}
		if b.Globex, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Globex Inc", Customer: true, Currency: "USD"}); err != nil {
			return err
		}
		b.Supplier, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Atlassian Pty Ltd", Vendor: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

func hasInvoiceCode(err error, code string) bool {
	var ie *invoice.Error
	return errors.As(err, &ie) && ie.Code == code
}

func invoiceLine(t *testing.T, b salesBooks, q, price, discount, code, cur string) invoice.Line {
	t.Helper()
	qty, err := money.ParseQuantity(q, "quantity")
	if err != nil {
		t.Fatal(err)
	}
	c, err := money.Lookup(cur)
	if err != nil {
		t.Fatal(err)
	}
	p, err := money.ParseUnitPrice(price, c, "unit_price")
	if err != nil {
		t.Fatal(err)
	}
	return invoice.Line{Description: "Consulting", Quantity: qty, UnitPrice: p, Discount: discount, AccountID: b.Sales.ID, GSTCode: code}
}

// FR-AR-03 and the posting rules "Posting an invoice" and "Amounts of a posted invoice" (p1-sales tasks 1.1 to 1.3): an
// invoice is drafted, checked, issued with its number, voided and edited through the posting service, each in one
// transaction with its entry. Its lines are quantities at a price less a discount (RND-LINE); it bears GST only once
// the organisation is GST-registered on its date; a USD invoice takes the cascade's rate and its GST on the SGD net
// (ENG-42); every entry replays (ENG-06).
// req: FR-AR-03
// req: ledger/posting-rules Posting an invoice
// req: ledger/posting-rules Amounts of a posted invoice
// req: RND-LINE
// req: ENG-42
// req: FR-GL-12
func TestInvoicesPostThroughThePostingService(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Invoices Pte Ltd")
	invoices := journal.Invoices{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	oct14 := porttest.Day(10, 14)

	// Not GST-registered: no GST; three days at 100.00 less 10%, due on the customer's 14 days.
	draft := invoice.Draft{CustomerID: b.Customer.ID, Reference: "PO-778", Date: oct14, Currency: sgd, Lines: []invoice.Line{invoiceLine(t, b, "3", "100.00", "10", "SR", "SGD")}}
	doc, err := invoices.Create(ctx, b.Org, draft)
	if err != nil {
		t.Fatal(err)
	}
	c, err := invoices.Check(ctx, b.Org, doc.ID, nil)
	if err != nil || len(c.Problems) > 0 || !c.Prepared.DueDate.Equal(porttest.Day(10, 28)) || c.Receivable.String() != "270.00" || c.Prepared.Registered {
		t.Fatalf("the check of an unregistered company's invoice: %+v %v", c, err)
	}
	e, err := invoices.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, e); got != "1100 270 SGD = 270; 4000 -270 SGD = -270" {
		t.Errorf("not registered: %s", got)
	}
	if e.Memo != "Acme Analytics Pte Ltd INV-0001" {
		t.Errorf("the memo: %q", e.Memo)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, err = u.Documents().Get(ctx, doc.ID)
		return err
	}); err != nil || doc.Number != "INV-0001" || doc.Status != storage.DocumentPosted || doc.PostedBy != "user:ar-specialist" {
		t.Fatalf("the issued invoice: %+v %v", doc, err)
	}
	if _, err := invoices.Post(ctx, b.Org, doc.ID); !hasInvoiceCode(err, invoice.CodeNotDraft) {
		t.Errorf("issuing twice: %v", err)
	}

	// Registered from 1 October: 9% on the standard-rated line, none on the zero-rated one.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: oct14})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	taxed, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: oct14, Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "1000.00", "", "SR", "SGD"), invoiceLine(t, b, "1", "500.00", "", "ZR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	if e, err = invoices.Post(ctx, b.Org, taxed.ID); err != nil || legsOf(t, s, b.Org, e) != "1100 1590 SGD = 1590; 4000 -1000 SGD = -1000; 4000 -500 SGD = -500; 2200 -90 SGD = -90" {
		t.Errorf("registered: %s %v", legsOf(t, s, b.Org, e), err)
	}

	// ENG-42: USD 1,000.00 at the cascade's 1.34, its GST on the SGD net: S$120.60.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: oct14, Rate: "1.34", Quote: "1.34", QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	usdInvoice, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Globex.ID, Date: oct14, Currency: usd, Lines: []invoice.Line{invoiceLine(t, b, "1", "1000.00", "", "SR", "USD")}})
	if err != nil {
		t.Fatal(err)
	}
	if e, err = invoices.Post(ctx, b.Org, usdInvoice.ID); err != nil ||
		legsOf(t, s, b.Org, e) != "1100 1090 USD = 1460.6; 4000 -1000 USD = -1340; 2200 -120.6 SGD = -120.6" {
		t.Errorf("ENG-42: %s %v", legsOf(t, s, b.Org, e), err)
	}

	// The refusals, all together: a supplier, a code for purchases, a due date before the invoice, no EUR rate.
	bad, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Supplier.ID, Date: oct14, DueDate: porttest.Day(10, 1), Currency: money.MustLookup("EUR"),
		Lines: []invoice.Line{invoiceLine(t, b, "1", "5.00", "", "TX", "EUR")}})
	if err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, err := invoices.Post(ctx, b.Org, bad.ID); !asProblems(err, &probs) {
		t.Fatalf("a refused invoice: %v", err)
	}
	for _, want := range []string{invoice.CodeCustomer, invoice.CodeGSTCode, invoice.CodeDueDate, fx.CodeNoRate} {
		if !probs.Has(want) {
			t.Errorf("no %s among %v", want, probs)
		}
	}
	if probs.Has(ledger.CodeMissingRate) {
		t.Errorf("the engine's missing rate beside FX-01's: %v", probs)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		bad, err = u.Documents().Get(ctx, bad.ID)
		return err
	}); err != nil || bad.Number != "" || bad.Status != storage.DocumentDraft {
		t.Fatalf("a refused invoice took a number: %+v %v", bad, err)
	}

	// An edit reverses the version it had and posts the next, keeping the number; a void reverses it.
	draft.Lines = []invoice.Line{invoiceLine(t, b, "4", "100.00", "10", "SR", "SGD")}
	ed, err := invoices.Edit(ctx, b.Org, doc.ID, draft, "Four days, not three", "")
	if err != nil {
		t.Fatal(err)
	}
	if legsOf(t, s, b.Org, ed.Reversal) != "1100 -270 SGD = -270; 4000 270 SGD = 270" || legsOf(t, s, b.Org, ed.Entry) != "1100 392.4 SGD = 392.4; 4000 -360 SGD = -360; 2200 -32.4 SGD = -32.4" {
		t.Errorf("the edit: %s then %s", legsOf(t, s, b.Org, ed.Reversal), legsOf(t, s, b.Org, ed.Entry))
	}
	if _, err := invoices.Void(ctx, b.Org, taxed.ID, oct14, "Sent to the wrong customer"); err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Void(ctx, b.Org, taxed.ID, oct14, "Again"); !hasInvoiceCode(err, invoice.CodeNotPosted) {
		t.Errorf("voiding twice: %v", err)
	}
	if _, err := invoices.Revise(ctx, b.Org, doc.ID, draft); !hasInvoiceCode(err, invoice.CodeNotDraft) {
		t.Errorf("revising an issued invoice: %v", err)
	}

	// Every entry replays: the rate the cascade gave, and the GST the entries bore.
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
