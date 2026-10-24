//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/customercredit"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-04 and FR-AR-18 (p1-sales task 3.1, part), worked by hand, before any GST registration.
//   - CN-0001, S$109.00 to Acme on 15 Oct, refunded from DBS on 16 Oct (CR-0001): Dr 1100 109.00, Cr 1010 109.00.
//   - RC-0001, S$600.00 from Acme on 17 Oct: S$500.00 of INV-0001 and S$100.00 unapplied, S$40.00 of which is refunded on
//     18 Oct (CR-0002); S$60.00 stays a credit with Acme.
//   - CN-0002, USD 100.00 to Globex on 21 Oct at 1.36 (S$136.00), refunded from the USD wallet on 22 Oct at the cascade's
//     1.30 (CR-0003): Dr 1100 USD 100.00 = S$136.00, Cr 1021 USD 100.00 = S$130.00, Cr 7030 S$6.00, a gain.
//
// More than is left of a credit, and a document that is no credit, are refused; a refunded credit note or receipt is
// not voided until its refunds are; voiding a refund reverses its entry and allocation and reopens the credit; the AR
// sub-ledger agrees with 1100, and every entry replays.
// req: FR-AR-04
// req: FR-AR-18
// req: ENG-41
// req: INV-07
func TestCustomerRefundsSettleCredits(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Refunds Pte Ltd")
	invoices, receipts, refunds := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}, journal.CustomerRefunds{Store: s}
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
		for day, rate := range map[int]string{21: "1.36", 22: "1.30"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	post := func(credit bool, customer uuid.UUID, day int, cur, price string) storage.Document {
		t.Helper()
		create := invoices.Create
		if credit {
			create = invoices.CreateCredit
		}
		doc, err := create(ctx, b.Org, invoice.Draft{CustomerID: customer, Date: porttest.Day(10, day), Currency: money.MustLookup(cur),
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", cur)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return reread(t, ctx, s, b.Org, doc.ID)
	}
	refund := func(credit uuid.UUID, day int, from storage.Account, amount string, cur money.Currency) (journal.Refunded, error) {
		m, _ := money.Parse(amount, cur)
		return refunds.Record(ctx, b.Org, customercredit.RefundDraft{CreditID: credit, Date: porttest.Day(10, day), AccountID: from.ID, Reference: "FAST", Amount: m})
	}
	reconcile := func(want string) map[string]string {
		t.Helper()
		var r receivables.Reconciliation
		var l receivables.Ledger
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, l, err = receivables.Reconcile(ctx, u, sgd, b.AR.ID, porttest.Day(10, 31))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if r.Unexplained() || r.Base.SubLedger.String() != want || r.Base.Control.String() != want {
			t.Fatalf("the reconciliation, want %s: %+v", want, r)
		}
		out := map[string]string{}
		for _, c := range l.Customers {
			for _, it := range c.Items {
				out[it.Number] = it.Open.String()
			}
		}
		return out
	}

	// CR-0001 refunds all of CN-0001.
	cn1 := post(true, b.Customer.ID, 15, "SGD", "109.00")
	r1, err := refund(cn1.ID, 16, dbs, "109.00", sgd)
	if err != nil || r1.Refund.Number != "CR-0001" || legsOf(t, s, b.Org, r1.Entry) != "1100 109 SGD = 109; 1010 -109 SGD = -109" ||
		r1.Allocation.DocumentID != cn1.ID || r1.Entry.Memo != "Acme Analytics Pte Ltd, Refund of CN-0001, FAST" {
		t.Fatalf("CR-0001: %s %q %+v %v", legsOf(t, s, b.Org, r1.Entry), r1.Entry.Memo, r1.Allocation, err)
	}
	if _, err := refund(cn1.ID, 16, dbs, "1.00", sgd); !hasApplyCode(err, customercredit.CodeTooMuch) {
		t.Errorf("more than is left of CN-0001: %v", err)
	}

	// CR-0002 refunds S$40.00 of what RC-0001 left unapplied.
	inv1 := post(false, b.Customer.ID, 14, "SGD", "500.00")
	part, _ := money.Parse("500.00", sgd)
	rest, _ := money.Parse("100.00", sgd)
	rc, err := receipts.Record(ctx, b.Org, receipt.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 17), AccountID: dbs.ID, Currency: sgd, Reference: "FAST",
		Invoices: []receipt.Part{{InvoiceID: inv1.ID, Amount: part}}, Unapplied: rest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := refund(rc.Receipt.ID, 18, dbs, "40.00", sgd); err != nil {
		t.Fatal(err)
	}
	if got := reconcile("-60.00"); got["RC-0001"] != "-60.00" || got["CN-0001"] != "" || got["INV-0001"] != "" {
		t.Fatalf("after CR-0002: %v", got)
	}

	// CR-0003 refunds CN-0002 in USD at the cascade's rate: the realised gain to 7030.
	cn2 := post(true, b.Globex.ID, 21, "USD", "100.00")
	r3, err := refund(cn2.ID, 22, wallet, "100.00", usd)
	if err != nil || legsOf(t, s, b.Org, r3.Entry) != "1100 100 USD = 136; 1021 -100 USD = -130; 7030 -6 SGD = -6" ||
		string(r3.Allocation.RealisedFX) != "6.0000" || string(r3.Allocation.BaseAmount) != "136.0000" {
		t.Fatalf("CR-0003: %s %+v %v", legsOf(t, s, b.Org, r3.Entry), r3.Allocation, err)
	}
	if _, err := refund(inv1.ID, 22, dbs, "1.00", sgd); !hasApplyCode(err, customercredit.CodeCredit) {
		t.Errorf("an invoice as a credit: %v", err)
	}

	// A refunded credit note or receipt is not voided until its refunds are.
	if _, err := invoices.Void(ctx, b.Org, cn1.ID, porttest.Day(10, 25), "Raised in error"); !hasInvoiceCode(err, invoice.CodeSettled) {
		t.Errorf("voiding a refunded credit note: %v", err)
	}
	if _, err := receipts.Void(ctx, b.Org, rc.Receipt.ID, porttest.Day(10, 25), "Bounced"); !hasApplyCode(err, customercredit.CodeApplied) {
		t.Errorf("voiding a receipt whose credit is refunded: %v", err)
	}

	// Voiding CR-0003 reverses its entry and allocation and reopens CN-0002.
	if _, err := refunds.Void(ctx, b.Org, r3.Refund.ID, porttest.Day(10, 21), "Bounced"); !hasApplyCode(err, customercredit.CodeVoidDate) {
		t.Errorf("a void before the refund: %v", err)
	}
	v3, err := refunds.Void(ctx, b.Org, r3.Refund.ID, porttest.Day(10, 23), "Bounced")
	if err != nil || legsOf(t, s, b.Org, v3.Entry) != "1100 -100 USD = -136; 1021 100 USD = 130; 7030 6 SGD = 6" || v3.Refund.Status != storage.DocumentReversed {
		t.Fatalf("voiding CR-0003: %s %+v %v", legsOf(t, s, b.Org, v3.Entry), v3, err)
	}
	if _, err := refunds.Void(ctx, b.Org, r3.Refund.ID, porttest.Day(10, 23), "Again"); !hasApplyCode(err, customercredit.CodeNotPosted) {
		t.Errorf("voiding twice: %v", err)
	}
	if got := reconcile("-196.00"); got["CN-0002"] != "-100.00" || got["RC-0001"] != "-60.00" {
		t.Fatalf("after the void: %v", got)
	}

	// Every entry replays.
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
