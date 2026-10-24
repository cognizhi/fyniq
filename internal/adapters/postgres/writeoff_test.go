//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-17 and the posting rule "Bad-debt write-off" (p1-sales task 2.3), worked by hand:
//   - INV-0001, S$1,000.00, received S$900.00; the S$100.00 left written off as a bad debt to 6470: Cr 1100 100.00, Dr 6470
//     100.00; the invoice is settled;
//   - a discount writes off to 4095 and charges to 7030 when no account is named; a bad debt names one (WRITEOFF-06); more
//     than is open is refused (WRITEOFF-04);
//   - INV-0002, USD 100.01 at 1.3333 (S$133.34), received USD 50.00 (S$66.67 cleared): the USD 50.01 left at its rate is
//     S$66.68, a cent more than it carries; the write-off clears the S$66.67 left with a base-only S$0.01 back on 1100;
//   - voiding a write-off reopens its invoice.
//
// The AR sub-ledger agrees with 1100 throughout, and the replay reproduces every entry.
// req: FR-AR-17
// req: INV-07
// req: ledger/posting-rules Bad-debt write-off
func TestWriteOffsSettleWhatIsLeft(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Write-offs Pte Ltd")
	invoices, receipts, writeOffs := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}, journal.WriteOffs{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var dbs, wallet, badDebts, discounts storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		for _, x := range []struct {
			to *storage.Account
			n  storage.NewAccount
		}{
			{&dbs, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}},
			{&wallet, storage.NewAccount{Code: "1021", Name: "Wise USD", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}},
			{&badDebts, storage.NewAccount{Code: "6470", Name: "Bad debts", Type: storage.Expense}},
			{&discounts, storage.NewAccount{Code: "4095", Name: "Discounts & refunds given", Type: storage.Revenue}},
		} {
			if *x.to, err = u.Accounts().Create(ctx, x.n); err != nil {
				return err
			}
		}
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 14), Rate: "1.3333", Quote: "1.3333",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	issue := func(customer uuid.UUID, cur, price string) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: customer, Date: porttest.Day(10, 14), Currency: money.MustLookup(cur),
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", cur)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	receive := func(customer uuid.UUID, into storage.Account, cur money.Currency, rate *money.Rate, id uuid.UUID, amount string) {
		t.Helper()
		m, _ := money.Parse(amount, cur)
		if _, err := receipts.Record(ctx, b.Org, receipt.Draft{CustomerID: customer, Date: porttest.Day(10, 20), AccountID: into.ID, Currency: cur, Rate: rate,
			Invoices: []receipt.Part{{InvoiceID: id, Amount: m}}, Unapplied: money.Zero(cur)}); err != nil {
			t.Fatal(err)
		}
	}
	draft := func(id uuid.UUID, amount string, cur money.Currency, reason string, account uuid.UUID) writeoff.Draft {
		m, _ := money.Parse(amount, cur)
		return writeoff.Draft{InvoiceID: id, Date: porttest.Day(10, 31), Amount: m, Reason: reason, AccountID: account, Note: "Written off after follow-up"}
	}
	check := func(d writeoff.Draft) journal.CheckedWriteOff {
		t.Helper()
		doc, err := writeOffs.Create(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		var c journal.CheckedWriteOff
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			c, err = journal.CheckWriteOff(ctx, u, doc)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return c
	}

	acme := issue(b.Customer.ID, "SGD", "1000.00")
	receive(b.Customer.ID, dbs, sgd, nil, acme.ID, "900.00")

	// Defaults and refusals.
	if c := check(draft(acme.ID, "10.00", sgd, writeoff.ReasonDiscount, uuid.Nil)); len(c.Problems) > 0 || c.Prepared.WriteOff.Account != discounts.ID {
		t.Errorf("a discount: %+v", c.Problems)
	}
	if c := check(draft(acme.ID, "10.00", sgd, writeoff.ReasonCharges, uuid.Nil)); len(c.Problems) > 0 || c.Prepared.WriteOff.Account != b.FX.ID {
		t.Errorf("charges: %+v", c.Problems)
	}
	if c := check(draft(acme.ID, "10.00", sgd, writeoff.ReasonBadDebt, uuid.Nil)); !c.Problems.Has(writeoff.CodeAccount) {
		t.Errorf("a bad debt with no account: %+v", c.Problems)
	}
	if c := check(draft(acme.ID, "100.01", sgd, writeoff.ReasonBadDebt, badDebts.ID)); !c.Problems.Has(writeoff.CodeTooMuch) {
		t.Errorf("more than is open: %+v", c.Problems)
	}

	// The bad debt posts and settles the invoice.
	doc, err := writeOffs.Create(ctx, b.Org, draft(acme.ID, "100.00", sgd, writeoff.ReasonBadDebt, badDebts.ID))
	if err != nil {
		t.Fatal(err)
	}
	p, err := writeOffs.Post(ctx, b.Org, doc.ID)
	if err != nil || p.WriteOff.Number != "WO-0001" || legsOf(t, s, b.Org, p.Entry) != "1100 -100 SGD = -100; 6470 100 SGD = 100" ||
		p.Allocation.DocumentID != acme.ID || p.Entry.Memo != "Write-off of INV-0001 to Acme Analytics Pte Ltd: bad debt" {
		t.Fatalf("the bad debt: %s %+v %v", legsOf(t, s, b.Org, p.Entry), p, err)
	}
	if _, err := writeOffs.Post(ctx, b.Org, doc.ID); err == nil {
		t.Error("posting twice")
	}

	// A USD write-off clears the cent RND-FX left.
	globex := issue(b.Globex.ID, "USD", "100.01")
	rate, err := money.ParseRate("1.36", usd, sgd, "document")
	if err != nil {
		t.Fatal(err)
	}
	receive(b.Globex.ID, wallet, usd, &rate, globex.ID, "50.00")
	cent, err := writeOffs.Create(ctx, b.Org, draft(globex.ID, "50.01", usd, writeoff.ReasonCrossCurrency, uuid.Nil))
	if err != nil {
		t.Fatal(err)
	}
	pc, err := writeOffs.Post(ctx, b.Org, cent.ID)
	if err != nil || legsOf(t, s, b.Org, pc.Entry) != "1100 -50.01 USD = -66.68; 1100 0 USD = 0.01; 7030 66.67 SGD = 66.67" {
		t.Fatalf("the USD write-off: %s %v", legsOf(t, s, b.Org, pc.Entry), err)
	}

	reconcile := func() (receivables.Reconciliation, receivables.Ledger) {
		t.Helper()
		var r receivables.Reconciliation
		var l receivables.Ledger
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, l, err = receivables.Reconcile(ctx, u, sgd, b.AR.ID, porttest.Day(10, 31))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r, l
	}
	if r, l := reconcile(); r.Unexplained() || len(l.Customers) != 0 || !r.Base.Control.IsZero() {
		t.Fatalf("after the write-offs: %+v %+v", r, l)
	}

	// Voiding the bad debt reopens INV-0001's S$100.00.
	if _, err := writeOffs.Void(ctx, b.Org, doc.ID, porttest.Day(10, 31), "Paid after all"); err != nil {
		t.Fatal(err)
	}
	if r, l := reconcile(); r.Unexplained() || len(l.Customers) != 1 || l.Customers[0].Items[0].Open.String() != "100.00" {
		t.Fatalf("after the void: %+v %+v", r, l)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rep, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(rep.Mismatches) > 0 {
			t.Errorf("the replay: %+v", rep.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
