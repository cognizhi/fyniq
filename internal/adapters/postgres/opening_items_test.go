//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Opening open items through the posting services (FR-GL-06, Appendix B.2; rev 0.257) [accountant to confirm]. The books
// start on 1 October 2026. Globex's USD invoice of 1,000.00, issued on 20 September and unpaid then, at 1.35, posts on 1
// October: Dr 1100 USD 1,000.00 (S$1,350.00), Cr 3900 S$1,350.00, with no revenue or GST. Atlassian's bill of S$500.00
// with GST 45.00, received on 25 September, posts then: Dr 3900 S$545.00, Cr 2010 S$545.00. The AR and AP sub-ledgers
// agree with 1100 and 2010 from the books start (INV-07), so the opening balances would carry S$1,350.00 and S$545.00
// on 3900 and it nets to zero. Globex pays USD 1,000.00 on 21 October at 1.36: the invoice settles at its stored rate and
// S$10.00 of realised gain goes to 7030 (ENG-41). An opening item dated on the books start is refused (LEDGER-50), and
// every entry replays under its rule (ENG-06).
// req: FR-GL-06
// req: INV-07
// req: ledger/posting-rules Credit documents and opening items
// req: ENG-06
func TestOpeningItemsPostAtTheBooksStartAndSettleAfterIt(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Opening Items Pte Ltd")
	invoices, receipts, bills := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}, journal.Bills{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	start := porttest.Day(10, 1)
	var ap, clearing, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		if _, err = u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		if ap, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2010", Name: "Accounts payable", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleAccountsPayable)}); err != nil {
			return err
		}
		if clearing, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3900", Name: "Opening balance clearing", Type: storage.Equity,
			Role: storage.AccountRole(ledger.RoleOpeningClearing)}); err != nil {
			return err
		}
		if wallet, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1030", Name: "USD wallet", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}); err != nil {
			return err
		}
		if _, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			GSTNumber: "202612345K", ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: start}); err != nil {
			return err
		}
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 21), Rate: "1.36", Quote: "1.36",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	rate, err := money.ParseRate("1.35", usd, sgd, "document")
	if err != nil {
		t.Fatal(err)
	}
	opening := invoice.Draft{CustomerID: b.Globex.ID, Date: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), Currency: usd, Rate: &rate, OpeningItem: true,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "1000.00", "", "OS", "USD")}}
	doc, err := invoices.Create(ctx, b.Org, opening)
	if err != nil {
		t.Fatal(err)
	}
	e, err := invoices.Post(ctx, b.Org, doc.ID)
	if err != nil || legsOf(t, s, b.Org, e) != "1100 1000 USD = 1350; 3900 -1350 SGD = -1350" || !e.PostingDate.Equal(start) || e.RuleVersion != ledger.RuleOpeningInvoice {
		t.Fatalf("the opening invoice: %s on %s under %s: %v", legsOf(t, s, b.Org, e), e.PostingDate, e.RuleVersion, err)
	}
	doc = reread(t, ctx, s, b.Org, doc.ID)
	if !invoice.IsOpening(doc) || invoice.IdentityOf(doc) != ledger.InvoiceIdentity(doc.ID, doc.Version, ledger.RuleOpeningInvoice) {
		t.Errorf("the opening invoice's identity: %s", invoice.IdentityOf(doc))
	}

	// An SGD opening invoice at 9%, issued while registered: its total, S$109.00, on 1100; no GST posted, it was in the
	// return before the books start.
	taxed, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), Currency: sgd,
		OpeningItem: true, Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	if e, err = invoices.Post(ctx, b.Org, taxed.ID); err != nil || legsOf(t, s, b.Org, e) != "1100 109 SGD = 109; 3900 -109 SGD = -109" {
		t.Fatalf("the taxed opening invoice: %s: %v", legsOf(t, s, b.Org, e), err)
	}

	billDoc, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "AU-0900", Date: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
		Currency: sgd, OpeningItem: true, Lines: []bill.Line{{AccountID: b.Rent.ID, Description: "September rent", Net: sgdOf(t, "500.00"), GSTCode: "TX",
			GST: sgdOf(t, "45.00")}}})
	if err != nil {
		t.Fatal(err)
	}
	if e, err = bills.Post(ctx, b.Org, billDoc.ID); err != nil || legsOf(t, s, b.Org, e) != "2010 -545 SGD = -545; 3900 545 SGD = 545" || !e.PostingDate.Equal(start) {
		t.Fatalf("the opening bill: %s: %v", legsOf(t, s, b.Org, e), err)
	}

	// The sub-ledgers agree with their control accounts from the books start (INV-07).
	agree := func(on time.Time, ar, payable string) {
		t.Helper()
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, _, err := receivables.Reconcile(ctx, u, sgd, b.AR.ID, on)
			if err != nil {
				return err
			}
			if r.Unexplained() || r.Base.SubLedger.String() != ar || r.Base.Control.String() != ar {
				t.Errorf("receivables on %s, want %s: %+v", on.Format(time.DateOnly), ar, r)
			}
			p, _, err := payables.Reconcile(ctx, u, sgd, ap.ID, on)
			if err != nil {
				return err
			}
			if p.Unexplained() || p.Base.SubLedger.String() != payable || p.Base.Control.String() != payable {
				t.Errorf("payables on %s, want %s: %+v", on.Format(time.DateOnly), payable, p)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	agree(start, "1459.00", "545.00")

	// Paid after the books start: settled at its stored rate, the difference realised.
	paid := moneyOf(t, "1000.00", "USD")
	rc, err := receipts.Record(ctx, b.Org, receipt.Draft{CustomerID: b.Globex.ID, Date: porttest.Day(10, 21), AccountID: wallet.ID, Currency: usd,
		Reference: "SWIFT 7731", Invoices: []receipt.Part{{InvoiceID: doc.ID, Amount: paid}}, Unapplied: money.Zero(usd)})
	if err != nil || legsOf(t, s, b.Org, rc.Entry) != "1100 -1000 USD = -1350; 1030 1000 USD = 1360; 7030 -10 SGD = -10" {
		t.Fatalf("the receipt: %s: %v", legsOf(t, s, b.Org, rc.Entry), err)
	}
	agree(porttest.Day(10, 31), "109.00", "545.00")

	// The clearing account holds what the opening balances will carry: S$1,459.00 against AR, S$545.00 against AP.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		lines, err := u.Ledger().AccountLines(ctx, clearing.ID, porttest.Day(1, 1), porttest.Day(12, 31), 100)
		sum := money.Zero(sgd)
		for _, l := range lines {
			x, _ := money.ParseStored(string(l.BaseAmount), sgd)
			sum, _ = sum.Add(x)
		}
		if err == nil && sum.String() != "-914.00" {
			t.Errorf("3900 holds %s, want -914.00", sum)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// An opening item dated on the books start is no opening item.
	late := opening
	late.Date = start
	lateDoc, err := invoices.Create(ctx, b.Org, late)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, lateDoc.ID); !hasProblem(err, ledger.CodeOpeningItem) {
		t.Errorf("an opening item on the books start: %v", err)
	}

	// Every entry replays under the rule that posted it (ENG-06).
	r, err := journal.ReplayOrganisation(ctx, s, b.Org)
	if err != nil || len(r.Mismatches) != 0 || r.Entries < 4 {
		t.Errorf("the replay: %+v %v", r, err)
	}
}
