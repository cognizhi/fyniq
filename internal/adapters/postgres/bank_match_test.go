//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
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

// openOn is what is open of a document in the AR or AP sub-ledger as at 31 October, in its currency.
func openOn(t *testing.T, s storage.Store, org, control, id uuid.UUID, ar bool) string {
	t.Helper()
	out := "closed"
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var groups [][]receivables.Item
		if ar {
			l, err := receivables.Open(ctx, u, money.MustLookup("SGD"), control, porttest.Day(10, 31))
			if err != nil {
				return err
			}
			for _, c := range l.Customers {
				groups = append(groups, c.Items)
			}
		} else {
			l, err := payables.Open(ctx, u, money.MustLookup("SGD"), control, porttest.Day(10, 31))
			if err != nil {
				return err
			}
			for _, sp := range l.Suppliers {
				groups = append(groups, sp.Items)
			}
		}
		for _, items := range groups {
			for _, it := range items {
				if it.DocumentID == id {
					out = it.Open.String() + " " + it.Open.Currency().Code
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// FR-BNK-07, FR-BNK-08 and FR-AR-18 (p1-bank-import-reconcile task 4.3, design D5), worked by hand. Acme's INV-0001 of
// S$1,000.00 and INV-0003 of S$200.00, Globex's INV-0002 of USD 10,000.00 at 1.35 (S$13,500.00), and Atlassian's bill
// AU-1182 of S$1,000.00, all of 14 October; RC-0001, S$500.00 from Acme recorded by hand on 21 October.
//   - Suggestions: "inv-0001 " names INV-0001 once normalised; "AU-1182" names the bill by its supplier number exactly;
//     S$200.00 from "ACME ANALYTICS PTE LTD" is INV-0003's amount from its customer; S$500.00 from Acme on 22 October
//     is RC-0001, recorded by hand, ranked first.
//   - Accepting INV-0001 records a receipt from the line: Dr 1010 1,000.00, Cr 1100 1,000.00.
//   - S$500.00 is refused as a new receipt (recorded by hand already) and matched to RC-0001, posting nothing.
//   - G4-038: USD 9,710.00 into the USD wallet at 1.32 is refused as INV-0002 in full (USD 290.00 left, S$391.50 at the
//     invoice's rate, above the S$50.00 cap) and accepted as a part: realised FX 9,710 × (1.32 − 1.35) = −S$291.30 to
//     7030, and INV-0002 stays part-paid with USD 290.00 open; S$880.00 out is refused as AU-1182 in full and accepted
//     as a part, and S$120.00 stays open.
//   - A receipt made from a line is not voided while it is matched (FY229), and no line matches a document that does not
//     move its amount (FY228); unmatching voids the receipt made from the line and reopens INV-0001, and leaves RC-0001
//     as recorded.
//
// The replay reproduces every entry.
// req: FR-BNK-07
// req: FR-BNK-08
// req: FR-AR-18
// req: FR-BNK-06
// req: ENG-41
func TestMatchingABankLineSettlesWhatItPays(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Matching Pte Ltd")
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var ap storage.Account
	var dbs, wise storage.MoneyAccount
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if ap, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2010", Name: "Accounts payable", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleAccountsPayable)}); err != nil {
			return err
		}
		for _, x := range []struct {
			code, name, cur string
			to              *storage.MoneyAccount
		}{{"1010", "DBS current", "SGD", &dbs}, {"1021", "Wise USD", "USD", &wise}} {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: x.code, Name: x.name, Type: storage.Asset,
				AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: x.cur}})
			if err != nil {
				return err
			}
			if *x.to, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: x.name,
				Currency: x.cur, At: now}); err != nil {
				return err
			}
		}
		for day, rate := range map[int]string{14: "1.35", 21: "1.32"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	invoices, bills := journal.Invoices{Store: s}, journal.Bills{Store: s}
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
		doc, _ = getDoc(t, s, b.Org, doc.ID)
		return doc
	}
	inv1, inv2, inv3 := issue(b.Customer.ID, "SGD", "1000.00"), issue(b.Globex.ID, "USD", "10000.00"), issue(b.Customer.ID, "SGD", "200.00")
	rentBill, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "AU-1182", Date: porttest.Day(10, 14), Currency: sgd,
		Lines: []bill.Line{{AccountID: b.Rent.ID, Net: sgdOf(t, "1000.00"), GSTCode: "TX", GST: money.Zero(sgd)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, rentBill.ID); err != nil {
		t.Fatal(err)
	}
	rc1, err := journal.CustomerReceipts{Store: s}.Record(ctx, b.Org, receipt.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 21),
		AccountID: dbs.AccountID, Currency: sgd, Unapplied: sgdOf(t, "500.00")})
	if err != nil {
		t.Fatal(err)
	}
	sgdLines := importLines(t, b.Org, dbs, now,
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 20), Amount: "1000.00", Reference: "inv-0001 ", Description: "GIRO", Fingerprint: fingerprint("m1")},
		storage.NewBankLine{Position: 2, BookingDate: porttest.Day(10, 21), Amount: "-880.00", Reference: "AU-1182", Description: "ATLASSIAN", Fingerprint: fingerprint("m2")},
		storage.NewBankLine{Position: 3, BookingDate: porttest.Day(10, 22), Amount: "500.00", Description: "ACME DEPOSIT", Fingerprint: fingerprint("m3")},
		storage.NewBankLine{Position: 4, BookingDate: porttest.Day(10, 23), Amount: "200.00", Counterparty: "ACME ANALYTICS PTE LTD", Fingerprint: fingerprint("m4")})
	usdLines := importLines(t, b.Org, wise, now,
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 21), Amount: "9710.00", Reference: "INV-0002", Description: "GLOBEX", Fingerprint: fingerprint("m5")})
	in1, out1, held, amountOnly, dollars := sgdLines[0], sgdLines[1], sgdLines[2], sgdLines[3], usdLines[0]

	// The suggestions, best first.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, c := range []struct {
			line        storage.BankLine
			found, kind string
			doc         uuid.UUID
		}{
			{in1, banking.FoundNormalised, storage.MatchReceipt, inv1.ID},
			{out1, banking.FoundReference, storage.MatchBillPayment, rentBill.ID},
			{amountOnly, banking.FoundAmount, storage.MatchReceipt, inv3.ID},
			{held, banking.FoundRecorded, storage.MatchRecorded, rc1.Receipt.ID},
			{dollars, banking.FoundReference, storage.MatchReceipt, inv2.ID},
		} {
			got, err := banking.Suggest(ctx, u, c.line, nil)
			if err != nil {
				return err
			}
			if len(got) == 0 || got[0].Found != c.found || got[0].Kind != c.kind || got[0].DocumentID != c.doc {
				t.Errorf("%s %s: %+v", c.line.Reference, c.line.Amount, got)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	txns := journal.BankTransactions{Store: s}
	sum := func(s, cur string) money.Money { return moneyOf(t, s, cur) }

	// INV-0001 in full: a receipt from the line.
	m1, err := txns.MatchInvoices(ctx, b.Org, journal.InvoiceMatch{LineID: in1.ID, CustomerID: b.Customer.ID,
		Invoices: []receipt.Part{{InvoiceID: inv1.ID, Amount: sum("1000.00", "SGD"), InFull: true}}, At: now})
	if err != nil || m1.Line.Status != storage.LineMatched || m1.Match.Kind != storage.MatchReceipt || m1.Document.Type != storage.DocumentCustomerReceipt ||
		legsOf(t, s, b.Org, *m1.Entry) != "1100 -1000 SGD = -1000; 1010 1000 SGD = 1000" || !m1.Entry.PostingDate.Equal(porttest.Day(10, 20)) {
		t.Fatalf("INV-0001 matched: %s %v", legsOf(t, s, b.Org, *m1.Entry), err)
	}
	if got := openOn(t, s, b.Org, b.AR.ID, inv1.ID, true); got != "closed" {
		t.Fatalf("INV-0001 after the match: %s", got)
	}

	// S$500.00 is RC-0001's: refused as a new receipt, matched as recorded.
	if _, err := txns.MatchInvoices(ctx, b.Org, journal.InvoiceMatch{LineID: held.ID, CustomerID: b.Customer.ID, At: now}); !isBankTxn(err, journal.CodeRecorded) {
		t.Fatalf("money recorded twice: %v", err)
	}
	before := count(t, b.Org, "journal_entry")
	m3, err := txns.MatchRecorded(ctx, b.Org, held.ID, rc1.Receipt.ID, "", now)
	if err != nil || m3.Entry != nil || m3.Match.Kind != storage.MatchRecorded || m3.Line.Status != storage.LineMatched || count(t, b.Org, "journal_entry") != before {
		t.Fatalf("matched to RC-0001: %+v %v", m3, err)
	}
	if _, err := txns.MatchRecorded(ctx, b.Org, amountOnly.ID, rc1.Receipt.ID, "", now); !isBankTxn(err, journal.CodeNotMoving) {
		t.Fatalf("a recorded receipt of another amount: %v", err)
	}

	// G4-038: USD 9,710.00 of INV-0002, refused in full, accepted as a part.
	_, err = txns.MatchInvoices(ctx, b.Org, journal.InvoiceMatch{LineID: dollars.ID, CustomerID: b.Globex.ID,
		Invoices: []receipt.Part{{InvoiceID: inv2.ID, Amount: sum("9710.00", "USD"), InFull: true}}, At: now})
	if !hasReceiptCode(err, "", receipt.CodeTolerance) {
		t.Fatalf("INV-0002 in full beyond tolerance: %v", err)
	}
	m5, err := txns.MatchInvoices(ctx, b.Org, journal.InvoiceMatch{LineID: dollars.ID, CustomerID: b.Globex.ID,
		Invoices: []receipt.Part{{InvoiceID: inv2.ID, Amount: sum("9710.00", "USD")}}, At: now})
	if err != nil {
		t.Fatal(err)
	}
	if got := openOn(t, s, b.Org, b.AR.ID, inv2.ID, true); got != "290.00 USD" {
		t.Fatalf("INV-0002 part-paid: %s", got)
	}
	if got := balances(t, s, b.Org); got["7030"] != "291.30" {
		t.Fatalf("the realised loss: %s (%s)", got["7030"], legsOf(t, s, b.Org, *m5.Entry))
	}
	_, err = txns.MatchBills(ctx, b.Org, journal.BillMatch{LineID: out1.ID, SupplierID: b.Supplier.ID,
		Bills: []billpayment.Part{{BillID: rentBill.ID, Amount: sum("880.00", "SGD"), InFull: true}}, At: now})
	if !hasPaymentCode(err, billpayment.CodeTolerance) {
		t.Fatalf("AU-1182 in full beyond tolerance: %v", err)
	}
	if _, err := txns.MatchBills(ctx, b.Org, journal.BillMatch{LineID: out1.ID, SupplierID: b.Supplier.ID,
		Bills: []billpayment.Part{{BillID: rentBill.ID, Amount: sum("880.00", "SGD")}}, At: now}); err != nil {
		t.Fatal(err)
	}
	if got := openOn(t, s, b.Org, ap.ID, rentBill.ID, false); got != "120.00 SGD" {
		t.Fatalf("AU-1182 part-paid: %s", got)
	}

	// The wrong direction, and parts beyond the line.
	if _, err := txns.MatchBills(ctx, b.Org, journal.BillMatch{LineID: amountOnly.ID, SupplierID: b.Supplier.ID, At: now}); !isBankTxn(err, journal.CodeDirection) {
		t.Errorf("money in to bills: %v", err)
	}
	if _, err := txns.MatchInvoices(ctx, b.Org, journal.InvoiceMatch{LineID: amountOnly.ID, CustomerID: b.Customer.ID,
		Invoices: []receipt.Part{{InvoiceID: inv3.ID, Amount: sum("300.00", "SGD")}}, At: now}); !isBankTxn(err, journal.CodeOverParts) {
		t.Errorf("parts beyond the line: %v", err)
	}

	// The database's backstops.
	if _, err := (journal.CustomerReceipts{Store: s}).Void(ctx, b.Org, m5.Document.ID, porttest.Day(10, 25), "Wrong customer"); storage.CodeOf(err) != storage.CodeBanking {
		t.Errorf("a matched receipt voided by hand: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: amountOnly.ID, Kind: storage.MatchRecorded, DocumentID: &m5.Document.ID,
			Basis: storage.BasisPerson, At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Errorf("a line matched to a document of another amount: %v", err)
	}

	// Unmatched: the receipt from the line is voided and INV-0001 reopens; RC-0001 stays as recorded.
	if line, err := txns.Unmatch(ctx, b.Org, in1.ID, "Paid by another customer", now); err != nil || line.Status != storage.LineUnreconciled {
		t.Fatalf("unmatch INV-0001: %+v %v", line, err)
	}
	if got := openOn(t, s, b.Org, b.AR.ID, inv1.ID, true); got != "1000.00 SGD" {
		t.Fatalf("INV-0001 after the unmatch: %s", got)
	}
	if _, err := txns.Unmatch(ctx, b.Org, held.ID, "Not this receipt", now); err != nil {
		t.Fatal(err)
	}
	if doc, _ := getDoc(t, s, b.Org, rc1.Receipt.ID); doc.Status != storage.DocumentPosted {
		t.Errorf("RC-0001 after the unmatch: %s", doc.Status)
	}
	if _, err := txns.Unmatch(ctx, b.Org, amountOnly.ID, "None", now); !isBankTxn(err, journal.CodeNotCategory) {
		t.Errorf("an unmatch of a line not matched: %v", err)
	}
	if _, err := txns.Categorise(ctx, b.Org, journal.Categorisation{Draft: banktransaction.Draft{LineID: m5.Line.ID,
		Parts: []banktransaction.Part{{AccountID: b.Sales.ID, GSTCode: "NA"}}}, At: now}); !isBankTxn(err, journal.CodeLineDone) {
		t.Errorf("a matched line categorised: %v", err)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("the replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = usd
}

func hasPaymentCode(err error, code string) bool {
	var probs ledger.Problems
	if errors.As(err, &probs) && probs.Has(code) {
		return true
	}
	var e *billpayment.Error
	return errors.As(err, &e) && e.Code == code
}

func getDoc(t *testing.T, s storage.Store, org, id uuid.UUID) (storage.Document, error) {
	t.Helper()
	var d storage.Document
	err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		d, err = u.Documents().Get(ctx, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return d, err
}
