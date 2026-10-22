//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func payCode(err error, code string) bool {
	var pe *billpayment.Error
	return errors.As(err, &pe) && pe.Code == code
}

func problemCodes(err error) []string {
	var probs ledger.Problems
	if !errors.As(err, &probs) {
		return nil
	}
	var out []string
	for _, p := range probs {
		out = append(out, p.Code)
	}
	return out
}

// settledOf is what payments settle of a bill, net of their voids.
func settledOf(t *testing.T, s storage.Store, org, id uuid.UUID) string {
	t.Helper()
	var out storage.Decimal
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Allocations().Settled(ctx, []uuid.UUID{id})
		out = m[id]
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// FR-AP-02 and FR-AR-18 (p1-purchases task 3.1; the owner's decision of 22 Oct 2026): a payment recorded by hand posts
// bill_payment@1 with its number and an allocation per bill, in one transaction; it settles at most what a bill has
// open; a bill it settles is not voided or edited until the payment is; and a void reverses the entry and the
// allocations, which reopens the bill. A USD bill paid from the USD wallet realises its FX against its stored rate
// (ENG-41); one paid in three parts clears the cent RND-FX left with a base-only line on 2010, which the database
// accepts from a bill payment and from no other source, on accounts payable only (INV-05).
// req: FR-AP-02
// req: FR-AR-18
// req: ENG-41
// req: INV-05
// req: ledger/posting-rules Customer and supplier payments
func TestBillPaymentsRecordAndVoidThroughThePostingService(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Payments Pte Ltd")
	bills, payments := journal.Bills{Store: s}, journal.BillPayments{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	oct14, oct20, oct21 := porttest.Day(10, 14), porttest.Day(10, 20), porttest.Day(10, 21)
	var current, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if current, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if wallet, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1021", Name: "Wise USD", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.34", 20: "1.30", 21: "1.5"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate), Quote: storage.Decimal(rate),
				QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	post := func(d bill.Draft) storage.Document {
		t.Helper()
		doc, err := bills.Create(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	line := func(net string, cur money.Currency) bill.Line {
		return bill.Line{AccountID: b.Rent.ID, Net: moneyOf(t, net, cur.Code), GSTCode: "TX", GST: money.Zero(cur)}
	}
	rent := post(bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "AU-1182", Date: oct14, Currency: sgd, Lines: []bill.Line{line("1090.00", sgd)}})
	pay := func(from storage.Account, cur money.Currency, on string, id uuid.UUID, amount string) billpayment.Draft {
		d := billpayment.Draft{SupplierID: b.Supplier.ID, Date: oct20, AccountID: from.ID, Currency: cur, Reference: "FAST " + on,
			Bills: []billpayment.Part{{BillID: id, Amount: moneyOf(t, amount, cur.Code)}}, Unapplied: money.Zero(cur)}
		return d
	}

	// Part of it, then more than is open (✖), then the rest.
	first, err := payments.Record(ctx, b.Org, pay(current, sgd, "1", rent.ID, "400.00"))
	if err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, first.Entry); got != "2010 400 SGD = 400; 1010 -400 SGD = -400" || first.Payment.Number != "PAY-0001" ||
		first.Payment.Status != storage.DocumentPosted || len(first.Allocations) != 1 || first.Allocations[0].Amount != "400.0000" {
		t.Errorf("the first part: %s %+v", got, first)
	}
	if _, err := payments.Record(ctx, b.Org, pay(current, sgd, "2", rent.ID, "690.01")); !sameSet(problemCodes(err), []string{billpayment.CodeOverpaid}) {
		t.Errorf("more than is open: %v", err)
	}
	rest, err := payments.Record(ctx, b.Org, pay(current, sgd, "3", rent.ID, "690.00"))
	if err != nil || legsOf(t, s, b.Org, rest.Entry) != "2010 690 SGD = 690; 1010 -690 SGD = -690" || rest.Payment.Number != "PAY-0002" {
		t.Fatalf("the rest: %s %v", legsOf(t, s, b.Org, rest.Entry), err)
	}
	if got := settledOf(t, s, b.Org, rent.ID); got != "1090.0000" {
		t.Errorf("settled: %s", got)
	}
	if _, err := bills.Void(ctx, b.Org, rent.ID, oct21, "Entered twice"); !hasCode(err, bill.CodePaid) {
		t.Errorf("voiding a paid bill: %v", err)
	}
	if _, err := bills.Edit(ctx, b.Org, rent.ID, bill.Draft{SupplierID: b.Supplier.ID, Date: oct14, Currency: sgd, Lines: []bill.Line{line("1000.00", sgd)}},
		"A seat less", ""); !hasCode(err, bill.CodePaid) {
		t.Errorf("editing a paid bill: %v", err)
	}

	// A USD bill booked at 1.34 and paid from the wallet at 1.30: a gain of S$43.60 (ENG-41).
	cloud := post(bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "INV-77", Date: oct14, Currency: usd, Lines: []bill.Line{line("1090.00", usd)}})
	usdPaid, err := payments.Record(ctx, b.Org, pay(wallet, usd, "4", cloud.ID, "1090.00"))
	if err != nil || legsOf(t, s, b.Org, usdPaid.Entry) != "2010 1090 USD = 1460.6; 1021 -1090 USD = -1417; 7030 -43.6 SGD = -43.6" ||
		usdPaid.Allocations[0].RealisedFX != "43.6000" || usdPaid.Allocations[0].BaseAmount != "1460.6000" {
		t.Errorf("the USD bill: %s %+v %v", legsOf(t, s, b.Org, usdPaid.Entry), usdPaid.Allocations, err)
	}
	// From the SGD account, a USD bill needs what the account paid.
	noPaid := pay(current, usd, "5", cloud.ID, "1.00")
	if _, err := payments.Record(ctx, b.Org, noPaid); !sameSet(problemCodes(err), []string{ledger.CodePaidAmount, billpayment.CodeOverpaid}) {
		t.Errorf("a USD bill from the SGD account, with nothing paid named, and nothing open: %v", err)
	}

	// USD 0.03 at 1.5 carries S$0.05; parts of 0.01 carry S$0.02, so the last clears the residual, −S$0.01.
	cents := post(bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "INV-78", Date: porttest.Day(10, 21), Currency: usd, Lines: []bill.Line{line("0.03", usd)}})
	var last journal.Recorded
	for i := range 3 {
		d := pay(wallet, usd, "6", cents.ID, "0.01")
		d.Date = oct21
		if last, err = payments.Record(ctx, b.Org, d); err != nil {
			t.Fatalf("part %d: %v", i+1, err)
		}
	}
	if got := legsOf(t, s, b.Org, last.Entry); got != "2010 0.01 USD = 0.02; 2010 0 USD = -0.01; 1021 -0.01 USD = -0.02; 7030 0.01 SGD = 0.01" ||
		last.Allocations[0].BaseAmount != "0.0100" {
		t.Errorf("the last part: %s %+v", got, last.Allocations)
	}

	// A void dated before the payment, a void, and a void again.
	if _, err := payments.Void(ctx, b.Org, rest.Payment.ID, porttest.Day(10, 19), "Bounced"); !payCode(err, billpayment.CodeVoidDate) {
		t.Errorf("a void before the payment: %v", err)
	}
	voided, err := payments.Void(ctx, b.Org, rest.Payment.ID, oct21, "Bounced")
	if err != nil || legsOf(t, s, b.Org, voided.Entry) != "2010 -690 SGD = -690; 1010 690 SGD = 690" || len(voided.Allocations) != 1 ||
		voided.Allocations[0].Amount != "-690.0000" || voided.Allocations[0].ReversesID == nil || *voided.Allocations[0].ReversesID != rest.Allocations[0].ID {
		t.Fatalf("the void: %s %+v %v", legsOf(t, s, b.Org, voided.Entry), voided.Allocations, err)
	}
	if _, err := payments.Void(ctx, b.Org, rest.Payment.ID, oct21, "Bounced"); !payCode(err, billpayment.CodeNotPosted) {
		t.Errorf("a void again: %v", err)
	}
	if got := settledOf(t, s, b.Org, rent.ID); got != "400.0000" {
		t.Errorf("settled after the void: %s", got)
	}
	if _, err := payments.Void(ctx, b.Org, first.Payment.ID, oct21, "Bounced too"); err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Void(ctx, b.Org, rent.ID, oct21, "Entered twice"); err != nil {
		t.Errorf("voiding the bill once its payments are voided: %v", err)
	}
	if _, err := payments.Record(ctx, b.Org, pay(current, sgd, "7", rent.ID, "1.00")); !sameSet(problemCodes(err), []string{billpayment.CodeBill}) {
		t.Errorf("paying a voided bill: %v", err)
	}

	// The replay reproduces every payment, though the rates were withdrawn since.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rates, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: "USD"})
		if err != nil {
			return err
		}
		for _, r := range rates {
			if _, err := u.FXRates().Withdraw(ctx, r.ID, "Typed for the test"); err != nil {
				return err
			}
		}
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(r.Mismatches) > 0 {
			t.Errorf("the replay: %+v", r.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// INV-05 and FR-AR-18 under attack: an allocation never changes or goes, settles a bill from a bill payment only, and
// is reversed once, exactly; a base-only line comes from a bill payment only on accounts payable and within S$1.00.
// req: INV-05
// req: FR-AR-18
// req: QA-ENG-05
// asvs: V2.2.3, V13.2.2
func TestAttackAllocationsAndBaseOnlyResiduals(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Allocation attacks Pte Ltd")
	sgd := money.MustLookup("SGD")
	var current storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		current, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := journal.Bills{Store: s}.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, Date: porttest.Day(10, 14), Currency: sgd,
		Lines: []bill.Line{{AccountID: b.Rent.ID, Net: sgdOf(t, "100.00"), GSTCode: "TX", GST: money.Zero(sgd)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (journal.Bills{Store: s}).Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	paid, err := journal.BillPayments{Store: s}.Record(ctx, b.Org, billpayment.Draft{SupplierID: b.Supplier.ID, Date: porttest.Day(10, 15), AccountID: current.ID,
		Currency: sgd, Bills: []billpayment.Part{{BillID: doc.ID, Amount: sgdOf(t, "60.00")}}, Unapplied: money.Zero(sgd)})
	if err != nil {
		t.Fatal(err)
	}
	a := paid.Allocations[0]
	add := func(n storage.NewAllocation) error {
		return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Allocations().Add(ctx, n)
			return err
		})
	}
	reversal := storage.NewAllocation{SourceID: a.SourceID, SourceVersion: 1, DocumentID: a.DocumentID, On: porttest.Day(10, 16), Currency: "SGD",
		Amount: "-60.00", BaseCurrency: "SGD", BaseAmount: "-60.00", RealisedFX: "0.00", ReversesID: &a.ID}
	for name, n := range map[string]storage.NewAllocation{
		"a bill settling a bill": {SourceID: doc.ID, SourceVersion: 1, DocumentID: doc.ID, On: porttest.Day(10, 16), Currency: "SGD", Amount: "1.00",
			BaseCurrency: "SGD", BaseAmount: "1.00", RealisedFX: "0.00"},
		"a reversal of another amount": func() storage.NewAllocation { x := reversal; x.Amount, x.BaseAmount = "-50.00", "-50.00"; return x }(),
		"a reversal dated before":      func() storage.NewAllocation { x := reversal; x.On = porttest.Day(10, 14); return x }(),
	} {
		if err := add(n); !storageCode(err, storage.CodeAllocation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := add(reversal); err != nil {
		t.Fatal(err)
	}
	if err := add(reversal); !storageCode(err, storage.CodeDuplicate) {
		t.Errorf("a second reversal: %v", err)
	}
	owner := conn(t, "fyniq_migrator")
	for name, sql := range map[string]string{
		"an update": "UPDATE fyniq.allocation SET amount = 1 WHERE id = $1",
		"a delete":  "DELETE FROM fyniq.allocation WHERE id = $1",
	} {
		wantState(t, name+" as the owner", asOrg(ctx, owner, b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, a.ID)
			return err
		}), "FY170")
	}
	wantState(t, "an update as the API", asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.allocation SET amount = 1 WHERE id = $1", a.ID)
		return err
	}), "42501")

	// Base-only lines from a bill payment: on 2010 within S$1.00 commits; on 7030, or above S$1.00, does not.
	residual := func(account uuid.UUID, base string) error {
		return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 16), PostingIdentity: "bill_payment:attack:" + uuid.NewString(),
				RuleVersion: ledger.RuleBillPayment, Memo: "Attack", Source: storage.Source{Type: ledger.SourceBillPayment, ID: paid.Payment.ID.String(), Version: 1},
				Lines: []storage.NewLine{
					{AccountID: account, Currency: "USD", Amount: "0.00", FXRate: "1.5", FXRateSource: "document", BaseAmount: storage.Decimal(base)},
					{AccountID: b.FX.ID, Currency: "SGD", Amount: storage.Decimal(negated(base)), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(negated(base))},
				}})
			return err
		})
	}
	if err := residual(b.AP.ID, "-0.01"); err != nil {
		t.Errorf("a residual on 2010: %v", err)
	}
	for name, err := range map[string]error{"on 7030": residual(b.FX.ID, "-0.01"), "of S$1.01": residual(b.AP.ID, "-1.01")} {
		if !storageCode(err, storage.CodeAmount) {
			t.Errorf("a residual %s: %v", name, err)
		}
	}
}

func negated(s string) string {
	if s[0] == '-' {
		return s[1:]
	}
	return "-" + s
}

func storageCode(err error, code string) bool {
	var se *storage.Error
	return errors.As(err, &se) && se.Code == code
}
