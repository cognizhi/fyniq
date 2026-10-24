//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func hasReceiptCode(err error, field, code string) bool {
	var probs ledger.Problems
	if errors.As(err, &probs) {
		for _, p := range probs {
			if p.Code == code && (field == "" || p.Field == field) {
				return true
			}
		}
	}
	var re *receipt.Error
	return errors.As(err, &re) && re.Code == code
}

// FR-AR-18 and the posting rule "Customer and supplier payments" (p1-sales task 2.1; the owner's decision of 24 Oct 2026),
// worked by hand. INV-0001 to Acme, S$1,090.00 on 14 Oct; INV-0002 to Globex, USD 1,000.00 at 1.34 (S$1,340.00).
//   - RC-0001, S$400.00 from Acme into DBS on 20 Oct: Dr 1010 400.00, Cr 1100 400.00; INV-0001 has S$690.00 open.
//   - RC-0002, USD 1,000.00 from Globex into the USD wallet at 1.36 on 21 Oct: Cr 1100 USD 1,000.00 = S$1,340.00, Dr 1021
//     USD 1,000.00 = S$1,360.00, Cr 7030 S$20.00, a realised gain; INV-0002 is paid.
//   - RC-0003, S$700.00 from Acme on 22 Oct: the S$690.00 open of INV-0001 and S$10.00 unapplied, a credit with Acme.
//
// A receipt of more than an invoice has open, and one of another customer's invoice, are refused; an invoice receipts
// settle is not voided until they are; the AR sub-ledger agrees with 1100 with the credit in it; voiding RC-0003
// reopens INV-0001's S$690.00; the replay reproduces every entry. The database refuses an allocation from a payment of
// bills to an invoice (FY171).
// req: FR-AR-18
// req: ENG-41
// req: INV-07
// req: ledger/posting-rules Customer and supplier payments
func TestCustomerReceiptsSettleInvoices(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Receipts Pte Ltd")
	invoices, receipts := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var dbs, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dbs, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if wallet, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1021", Name: "Wise USD", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.34", 21: "1.36"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
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
	acme := issue(b.Customer.ID, "SGD", "1090.00")
	globex := issue(b.Globex.ID, "USD", "1000.00")
	draft := func(customer uuid.UUID, day int, into storage.Account, cur money.Currency, unapplied string, parts ...receipt.Part) receipt.Draft {
		u, _ := money.Parse(unapplied, cur)
		return receipt.Draft{CustomerID: customer, Date: porttest.Day(10, day), AccountID: into.ID, Currency: cur, Reference: "FAST", Invoices: parts, Unapplied: u}
	}
	part := func(id uuid.UUID, amount string, cur money.Currency) receipt.Part {
		m, _ := money.Parse(amount, cur)
		return receipt.Part{InvoiceID: id, Amount: m}
	}

	r1, err := receipts.Record(ctx, b.Org, draft(b.Customer.ID, 20, dbs, sgd, "0", part(acme.ID, "400.00", sgd)))
	if err != nil || r1.Receipt.Number != "RC-0001" || legsOf(t, s, b.Org, r1.Entry) != "1100 -400 SGD = -400; 1010 400 SGD = 400" ||
		len(r1.Allocations) != 1 || r1.Allocations[0].DocumentID != acme.ID {
		t.Fatalf("RC-0001: %s %+v %v", legsOf(t, s, b.Org, r1.Entry), r1.Allocations, err)
	}
	r2, err := receipts.Record(ctx, b.Org, draft(b.Globex.ID, 21, wallet, usd, "0", part(globex.ID, "1000.00", usd)))
	if err != nil || legsOf(t, s, b.Org, r2.Entry) != "1100 -1000 USD = -1340; 1021 1000 USD = 1360; 7030 -20 SGD = -20" ||
		string(r2.Allocations[0].RealisedFX) != "20.0000" {
		t.Fatalf("RC-0002: %s %+v %v", legsOf(t, s, b.Org, r2.Entry), r2.Allocations, err)
	}

	// Refused: more than INV-0001 has open, and another customer's invoice.
	if _, err := receipts.Record(ctx, b.Org, draft(b.Customer.ID, 22, dbs, sgd, "0", part(acme.ID, "700.00", sgd))); !hasReceiptCode(err, "invoices[1].amount", receipt.CodeOverpaid) {
		t.Errorf("over-allocation: %v", err)
	}
	if _, err := receipts.Record(ctx, b.Org, draft(b.Customer.ID, 22, wallet, usd, "0", part(globex.ID, "1.00", usd))); !hasReceiptCode(err, "invoices[1].invoice_id", receipt.CodeInvoice) {
		t.Errorf("another customer's invoice: %v", err)
	}

	r3, err := receipts.Record(ctx, b.Org, draft(b.Customer.ID, 22, dbs, sgd, "10.00", part(acme.ID, "690.00", sgd)))
	if err != nil || legsOf(t, s, b.Org, r3.Entry) != "1100 -690 SGD = -690; 1100 -10 SGD = -10; 1010 700 SGD = 700" {
		t.Fatalf("RC-0003: %s %v", legsOf(t, s, b.Org, r3.Entry), err)
	}

	// An invoice receipts settle is not voided until they are.
	if _, err := invoices.Void(ctx, b.Org, acme.ID, porttest.Day(10, 25), "Raised in error"); !hasInvoiceCode(err, invoice.CodeSettled) {
		t.Errorf("voiding a settled invoice: %v", err)
	}

	// The AR sub-ledger: INV-0001 and INV-0002 paid, and the S$10.00 credit with Acme; 1100 agrees.
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
	r, l := reconcile()
	if r.Unexplained() || r.Base.SubLedger.String() != "-10.00" || r.Base.Control.String() != "-10.00" || len(l.Customers) != 1 ||
		len(l.Customers[0].Items) != 1 || l.Customers[0].Items[0].Kind != receivables.KindCredit || l.Customers[0].Items[0].Open.String() != "-10.00" {
		t.Fatalf("the sub-ledger after the receipts: %+v %+v", r, l)
	}

	// Voiding RC-0003 reopens INV-0001's S$690.00 and takes the credit away.
	v, err := receipts.Void(ctx, b.Org, r3.Receipt.ID, porttest.Day(10, 23), "Bounced")
	if err != nil || v.Receipt.Status != storage.DocumentReversed || len(v.Allocations) != 1 {
		t.Fatalf("voiding RC-0003: %+v %v", v, err)
	}
	r, l = reconcile()
	if r.Unexplained() || r.Base.SubLedger.String() != "690.00" || l.Customers[0].Items[0].DocumentID != acme.ID || l.Customers[0].Items[0].Open.String() != "690.00" {
		t.Fatalf("the sub-ledger after the void: %+v %+v", r, l)
	}
	if _, err := receipts.Void(ctx, b.Org, r3.Receipt.ID, porttest.Day(10, 23), "Again"); !hasReceiptCode(err, "", receipt.CodeNotPosted) {
		t.Errorf("voiding twice: %v", err)
	}

	// The database refuses an allocation that settles an invoice from anything but a customer receipt (FY171).
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.allocation (source_id, source_version, document_id, on_date, currency, minor_units, amount,
  base_currency, base_minor_units, base_amount, realised_fx, written_by, written_at)
VALUES ($1, 1, $2, '2026-10-24', 'SGD', 2, 1.00, 'SGD', 2, 1.00, 0, 'user:attack-test', now())`, globex.ID, acme.ID)
		return err
	}); sqlstate(err) != "FY171" {
		t.Errorf("an allocation from an invoice to an invoice: %v", err)
	}

	// The replay reproduces every invoice and receipt.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rep, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(rep.Mismatches) > 0 || rep.Entries != 6 {
			t.Errorf("the replay: %d entries, %+v", rep.Entries, rep.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
