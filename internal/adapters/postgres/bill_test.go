//go:build local

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// purchasesBooks are Books with the purchase rules' system accounts, a supplier and a USD supplier.
type purchasesBooks struct {
	porttest.Books
	AP, GST, FX     storage.Account
	Supplier, Cloud storage.Contact
}

func newPurchasesBooks(t *testing.T, ctx context.Context, s storage.Store, name string) purchasesBooks {
	t.Helper()
	b := purchasesBooks{Books: porttest.NewBooks(t, s, name)}
	thirty := 30
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		for _, a := range []struct {
			to *storage.Account
			n  storage.NewAccount
		}{
			{&b.AP, storage.NewAccount{Code: "2010", Name: "Accounts payable", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleAccountsPayable)}},
			{&b.GST, storage.NewAccount{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)}},
			{&b.FX, storage.NewAccount{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense, Role: storage.AccountRole(ledger.RoleFXRounding)}},
		} {
			if *a.to, err = u.Accounts().Create(ctx, a.n); err != nil {
				return err
			}
		}
		if b.Supplier, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Atlassian Pty Ltd", Vendor: true, PaymentTermsDays: &thirty}); err != nil {
			return err
		}
		b.Cloud, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Vercel Inc", Vendor: true, Currency: "USD"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

func sgdOf(t *testing.T, s string) money.Money { return moneyOf(t, s, "SGD") }

func moneyOf(t *testing.T, s, cur string) money.Money {
	t.Helper()
	c, err := money.Lookup(cur)
	if err != nil {
		t.Fatal(err)
	}
	m, err := money.Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// legsOf renders an entry's lines as "code amount currency = base".
func legsOf(t *testing.T, s storage.Store, org uuid.UUID, e storage.Entry) string {
	t.Helper()
	codes := map[uuid.UUID]string{}
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		for _, a := range all {
			codes[a.ID] = a.Code
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, l := range e.Lines {
		parts = append(parts, codes[l.AccountID]+" "+strings.TrimRight(strings.TrimRight(string(l.Amount), "0"), ".")+" "+l.Currency+" = "+
			strings.TrimRight(strings.TrimRight(string(l.BaseAmount), "0"), "."))
	}
	return strings.Join(parts, "; ")
}

// FR-AP-02 and the posting rules "Posting a bill" and "Amounts of a posted bill" (p1-purchases task 1.2): a bill is
// drafted, checked, posted with its number, voided and edited through the posting service, each in one transaction
// with its entry. Its input tax is claimable only once the organisation is GST-registered on the bill's date; a USD
// bill takes the cascade's rate and the GST as the tax invoice states it in SGD (G4-023); the warnings (⚠) are the
// stated GST against RND-GST-EXCL (G4-024), a duplicate, and a foreign line with no SGD GST stated.
// req: FR-AP-02
// req: ledger/posting-rules Posting a bill
// req: ledger/posting-rules Amounts of a posted bill
// req: ENG-42
// req: FR-GL-12
func TestBillsPostThroughThePostingService(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Bills Pte Ltd")
	bills := journal.Bills{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	tx := func(net, gst string) bill.Line {
		return bill.Line{AccountID: b.Rent.ID, Description: "Confluence", Net: sgdOf(t, net), GSTCode: "TX", GST: sgdOf(t, gst)}
	}
	oct14 := porttest.Day(10, 14)

	// Not GST-registered: the GST stays in the cost; the due date is the supplier's 30 days.
	draft := bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "AU-1182", Date: oct14, Currency: sgd, Lines: []bill.Line{tx("1000.00", "90.00")}}
	doc, err := bills.Create(ctx, b.Org, draft)
	if err != nil {
		t.Fatal(err)
	}
	c, err := bills.Check(ctx, b.Org, doc.ID, nil)
	if err != nil || len(c.Problems) > 0 || !c.Prepared.DueDate.Equal(porttest.Day(11, 13)) || c.Payable.String() != "1090.00" || len(c.Prepared.Warnings) != 0 {
		t.Fatalf("the check of an unregistered company's bill: %+v %v", c, err)
	}
	e, err := bills.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, e); got != "6100 1090 SGD = 1090; 2010 -1090 SGD = -1090" {
		t.Errorf("not registered: %s", got)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, err = u.Documents().Get(ctx, doc.ID)
		return err
	}); err != nil || doc.Number != "BILL-0001" || doc.Status != storage.DocumentPosted || doc.PostedBy != "user:ap-specialist" {
		t.Fatalf("the posted bill: %+v %v", doc, err)
	}
	if _, err := bills.Post(ctx, b.Org, doc.ID); !hasCode(err, bill.CodeNotDraft) {
		t.Errorf("posting twice: %v", err)
	}

	// Registered from 1 October: claimable, and the same bill again is a duplicate (⚠).
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: oct14})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	again, err := bills.Create(ctx, b.Org, draft)
	if err != nil {
		t.Fatal(err)
	}
	c, err = bills.Check(ctx, b.Org, again.ID, nil)
	if err != nil || len(c.Prepared.Warnings) != 1 || c.Prepared.Warnings[0].Code != bill.WarnDuplicate || !strings.Contains(c.Prepared.Warnings[0].Message, "BILL-0001") {
		t.Fatalf("the duplicate: %+v %v", c.Prepared.Warnings, err)
	}
	if e, err = bills.Post(ctx, b.Org, again.ID); err != nil || legsOf(t, s, b.Org, e) != "6100 1000 SGD = 1000; 2200 90 SGD = 90; 2010 -1090 SGD = -1090" {
		t.Errorf("registered: %s %v", legsOf(t, s, b.Org, e), err)
	}

	// G4-024: three lines of S$10.06 whose GST the supplier rounded per line (0.91 each) are a cent above RND-GST-EXCL.
	g4024, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "AU-1190", Date: oct14, Currency: sgd,
		Lines: []bill.Line{tx("10.06", "0.91"), tx("10.06", "0.91"), tx("10.06", "0.91")}})
	if err != nil {
		t.Fatal(err)
	}
	if c, err = bills.Check(ctx, b.Org, g4024.ID, nil); err != nil || len(c.Prepared.Warnings) != 1 || c.Prepared.Warnings[0].Code != bill.WarnGSTDiffers ||
		!strings.Contains(c.Prepared.Warnings[0].Message, "differs by 0.01 SGD") {
		t.Errorf("G4-024: %+v %v", c.Prepared.Warnings, err)
	}

	// G4-023: USD 1,000.00 + 90.00 at the cascade's rate for the day, 1.34, the tax invoice stating S$121.50.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: oct14, Rate: "1.34", Quote: "1.34", QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stated := sgdOf(t, "121.50")
	foreign := bill.Draft{SupplierID: b.Cloud.ID, SupplierRef: "INV-77", Date: oct14, Currency: usd, Lines: []bill.Line{
		{AccountID: b.Rent.ID, Net: moneyOf(t, "1000.00", "USD"), GSTCode: "TX", GST: moneyOf(t, "90.00", "USD"), StatedGST: &stated}}}
	usdBill, err := bills.Create(ctx, b.Org, foreign)
	if err != nil {
		t.Fatal(err)
	}
	if e, err = bills.Post(ctx, b.Org, usdBill.ID); err != nil ||
		legsOf(t, s, b.Org, e) != "6100 1000 USD = 1340; 2200 121.5 SGD = 121.5; 2010 -1090 USD = -1460.6; 7030 -0.9 SGD = -0.9" {
		t.Errorf("G4-023: %s %v", legsOf(t, s, b.Org, e), err)
	}

	// The refusals, all together: no supplier, a code that is not for purchases, a due date before the bill, no rate.
	bad, err := bills.Create(ctx, b.Org, bill.Draft{Date: oct14, DueDate: porttest.Day(10, 1), Currency: money.MustLookup("EUR"),
		Lines: []bill.Line{{AccountID: b.Rent.ID, Net: moneyOf(t, "5.00", "EUR"), GSTCode: "SR", GST: moneyOf(t, "0.00", "EUR")}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bills.Post(ctx, b.Org, bad.ID)
	var probs ledger.Problems
	if !asProblems(err, &probs) {
		t.Fatalf("a bad bill: %v", err)
	}
	var got []string
	for _, p := range probs {
		got = append(got, p.Code)
	}
	slices.Sort(got)
	if want := []string{bill.CodeSupplier, bill.CodeGSTCode, bill.CodeDueDate, fx.CodeNoRate}; !sameSet(got, want) {
		t.Errorf("a bad bill's problems: %v, want %v", got, want)
	}

	// Void the first bill; edit the second: its number stays, its entry is reversed and posted again.
	if _, err := bills.Void(ctx, b.Org, doc.ID, porttest.Day(10, 20), "Entered twice"); err != nil {
		t.Fatal(err)
	}
	edit := draft
	edit.Lines = []bill.Line{tx("900.00", "81.00")}
	ed, err := bills.Edit(ctx, b.Org, again.ID, edit, "The supplier's credit for a seat", "")
	if err != nil || legsOf(t, s, b.Org, ed.Reversal) != "6100 -1000 SGD = -1000; 2200 -90 SGD = -90; 2010 1090 SGD = 1090" ||
		legsOf(t, s, b.Org, ed.Entry) != "6100 900 SGD = 900; 2200 81 SGD = 81; 2010 -981 SGD = -981" {
		t.Fatalf("the edit: %s / %s %v", legsOf(t, s, b.Org, ed.Reversal), legsOf(t, s, b.Org, ed.Entry), err)
	}

	// The replay reproduces every bill, though the USD rate is withdrawn and the registration changed since.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rates, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: "USD"})
		if err != nil {
			return err
		}
		_, err = u.FXRates().Withdraw(ctx, rates[0].ID, "Typed for the test")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(r.Mismatches) > 0 || r.Entries < 6 {
			t.Errorf("the replay: %d entries, %+v", r.Entries, r.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func hasCode(err error, code string) bool {
	var be *bill.Error
	return errors.As(err, &be) && be.Code == code
}

func asProblems(err error, out *ledger.Problems) bool { return errors.As(err, out) }

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
