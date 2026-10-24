//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/customercredit"
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

func hasApplyCode(err error, code string) bool {
	var probs ledger.Problems
	if errors.As(err, &probs) {
		return probs.Has(code)
	}
	var ce *customercredit.Error
	return errors.As(err, &ce) && ce.Code == code
}

// FR-AR-04 and FR-AR-18 (p1-sales task 3.1, part), worked by hand. INV-0001 S$1,000.00 and INV-0002 S$500.00 to Acme,
// INV-0003 USD 1,000.00 at 1.34 (S$1,340.00) to Globex, all on 14 Oct, before any GST registration.
//   - CN-0001, S$300.00 to Acme on 15 Oct, applied to INV-0001 on 16 Oct: in one currency at one rate it posts nothing;
//     INV-0001 has S$700.00 open and CN-0001 nothing left.
//   - RC-0001, S$800.00 from Acme on 17 Oct: S$700.00 of INV-0001 and S$100.00 unapplied, applied to INV-0002 on 18 Oct.
//   - CN-0002, USD 100.00 to Globex on 21 Oct at 1.36 (S$136.00), applied to INV-0003 on 22 Oct: Cr 1100 USD 100.00 =
//     S$134.00 at the invoice's rate, Dr 1100 USD 100.00 = S$136.00 at the credit note's, Cr 7030 S$2.00, a gain.
//
// More than is left of a credit, another customer's invoice, and a document that is no credit are refused; an applied
// credit note or receipt is not voided until its applications are; voiding an application reverses its entry and its
// allocations and reopens both; the AR sub-ledger agrees with 1100 throughout, and every entry replays.
// req: FR-AR-04
// req: FR-AR-18
// req: ENG-41
// req: INV-07
func TestCustomerCreditsApplyToInvoices(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Applications Pte Ltd")
	invoices, receipts := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}
	applications := journal.CustomerCreditApplications{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var dbs storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dbs, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
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
	apply := func(credit, inv uuid.UUID, day int, amount string, cur money.Currency) (journal.Applied, error) {
		m, _ := money.Parse(amount, cur)
		return applications.Apply(ctx, b.Org, customercredit.Draft{CreditID: credit, InvoiceID: inv, Date: porttest.Day(10, day), Amount: m})
	}
	reconcile := func(want string) receivables.Ledger {
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
		return l
	}
	open := func(l receivables.Ledger) map[string]string {
		out := map[string]string{}
		for _, c := range l.Customers {
			for _, it := range c.Items {
				out[it.Number] = it.Open.String()
			}
		}
		return out
	}
	inv1, inv2 := post(false, b.Customer.ID, 14, "SGD", "1000.00"), post(false, b.Customer.ID, 14, "SGD", "500.00")
	inv3 := post(false, b.Globex.ID, 14, "USD", "1000.00")

	// CN-0001 applied to INV-0001: an allocation alone.
	cn1 := post(true, b.Customer.ID, 15, "SGD", "300.00")
	a1, err := apply(cn1.ID, inv1.ID, 16, "300.00", sgd)
	if err != nil || a1.Entry.ID != uuid.Nil || len(a1.Allocations) != 2 || a1.Application.Status != storage.DocumentPosted {
		t.Fatalf("CN-0001 to INV-0001: %+v %v", a1, err)
	}
	if got := open(reconcile("2540.00")); got["INV-0001"] != "700.00" || got["CN-0001"] != "" {
		t.Fatalf("after CN-0001: %v", got)
	}
	if _, err := apply(cn1.ID, inv2.ID, 16, "1.00", sgd); !hasApplyCode(err, customercredit.CodeTooMuch) {
		t.Errorf("more than is left of CN-0001: %v", err)
	}

	// RC-0001: the S$700.00 of INV-0001 and S$100.00 unapplied, then applied to INV-0002.
	part, _ := money.Parse("700.00", sgd)
	rest, _ := money.Parse("100.00", sgd)
	rc, err := receipts.Record(ctx, b.Org, receipt.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 17), AccountID: dbs.ID, Currency: sgd, Reference: "FAST",
		Invoices: []receipt.Part{{InvoiceID: inv1.ID, Amount: part}}, Unapplied: rest})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := apply(rc.Receipt.ID, inv2.ID, 18, "100.00", sgd)
	if err != nil || a2.Entry.ID != uuid.Nil {
		t.Fatalf("RC-0001's credit to INV-0002: %+v %v", a2, err)
	}
	if got := open(reconcile("1740.00")); got["INV-0001"] != "" || got["INV-0002"] != "400.00" || got["RC-0001"] != "" {
		t.Fatalf("after RC-0001: %v", got)
	}

	// CN-0002 to Globex in USD, applied across rates: the realised gain to 7030.
	cn2 := post(true, b.Globex.ID, 21, "USD", "100.00")
	a3, err := apply(cn2.ID, inv3.ID, 22, "100.00", usd)
	if err != nil || legsOf(t, s, b.Org, a3.Entry) != "1100 -100 USD = -134; 1100 100 USD = 136; 7030 -2 SGD = -2" ||
		string(a3.Allocations[0].RealisedFX) != "2.0000" || string(a3.Allocations[0].BaseAmount) != "134.0000" || string(a3.Allocations[1].BaseAmount) != "136.0000" {
		t.Fatalf("CN-0002 to INV-0003: %s %+v %v", legsOf(t, s, b.Org, a3.Entry), a3.Allocations, err)
	}
	if got := open(reconcile("1606.00")); got["INV-0003"] != "900.00" || got["CN-0002"] != "" {
		t.Fatalf("after CN-0002: %v", got)
	}

	// Refused: another customer's invoice, a document that is no credit.
	cn3 := post(true, b.Globex.ID, 21, "USD", "5.00")
	if _, err := apply(cn3.ID, inv2.ID, 22, "1.00", usd); !hasApplyCode(err, customercredit.CodeInvoice) {
		t.Errorf("another customer's invoice: %v", err)
	}
	if _, err := apply(inv1.ID, inv2.ID, 22, "1.00", sgd); !hasApplyCode(err, customercredit.CodeCredit) {
		t.Errorf("an invoice as a credit: %v", err)
	}

	// An applied credit note or receipt is not voided until its applications are.
	if _, err := invoices.Void(ctx, b.Org, cn1.ID, porttest.Day(10, 25), "Raised in error"); !hasInvoiceCode(err, invoice.CodeSettled) {
		t.Errorf("voiding an applied credit note: %v", err)
	}
	if _, err := receipts.Void(ctx, b.Org, rc.Receipt.ID, porttest.Day(10, 25), "Bounced"); !hasApplyCode(err, customercredit.CodeApplied) {
		t.Errorf("voiding a receipt whose credit is applied: %v", err)
	}

	// Voiding reverses an application's entry and allocations and reopens both; a void dated before it is refused.
	if _, err := applications.Void(ctx, b.Org, a3.Application.ID, porttest.Day(10, 21), "Wrong invoice"); !hasApplyCode(err, customercredit.CodeVoidDate) {
		t.Errorf("a void before the application: %v", err)
	}
	v3, err := applications.Void(ctx, b.Org, a3.Application.ID, porttest.Day(10, 23), "Wrong invoice")
	if err != nil || legsOf(t, s, b.Org, v3.Entry) != "1100 100 USD = 134; 1100 -100 USD = -136; 7030 2 SGD = 2" || len(v3.Allocations) != 2 ||
		v3.Application.Status != storage.DocumentReversed {
		t.Fatalf("voiding CN-0002's application: %s %+v %v", legsOf(t, s, b.Org, v3.Entry), v3, err)
	}
	if _, err := applications.Void(ctx, b.Org, a3.Application.ID, porttest.Day(10, 23), "Again"); !hasApplyCode(err, customercredit.CodeNotPosted) {
		t.Errorf("voiding twice: %v", err)
	}
	if _, err := applications.Void(ctx, b.Org, a2.Application.ID, porttest.Day(10, 23), "Wrong invoice"); err != nil {
		t.Fatal(err)
	}
	if got := open(reconcile("1597.20")); got["INV-0002"] != "500.00" || got["RC-0001"] != "-100.00" || got["INV-0003"] != "1000.00" || got["CN-0002"] != "-100.00" {
		t.Fatalf("after the voids: %v", got)
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
