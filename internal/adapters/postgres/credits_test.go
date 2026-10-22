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
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func creditCode(err error, code string) bool {
	var ce *credits.Error
	return errors.As(err, &ce) && ce.Code == code
}

// FR-AP-03 and FR-AR-18 (p1-purchases task 4.1): a supplier credit is applied to the supplier's bills by an application,
// which settles both by the amount applied. Worked by hand: a USD bill of 100.00 booked at 1.34 (S$134.00) and a USD
// credit note of 100.00 booked at 1.30 (S$130.00).
//   - Applying USD 60.00 posts Dr 2010 S$80.40, Cr 2010 S$78.00, Cr 7030 S$2.40: a gain;
//   - applying the other USD 40.00 settles both, clearing what each carries: Dr S$53.60, Cr S$52.00, Cr 7030 S$1.60;
//   - more than is open is refused; the credit is not voided while applied; voiding the second application reopens
//     USD 40.00 of each.
//
// In SGD a payment that left S$25.00 unapplied pays S$25.00 of another bill by an application that posts nothing; the
// payment is not voided until that application is. The sub-ledger agrees with 2010 throughout, and the replay reproduces
// every entry.
// req: FR-AP-03
// req: FR-AR-18
// req: ENG-41
// req: INV-07
func TestCreditsAreAppliedToBills(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Applied credits Pte Ltd")
	bills, payments, apply := journal.Bills{Store: s}, journal.BillPayments{Store: s}, journal.CreditApplications{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var current storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if current, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.34", 20: "1.30"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	post := func(credit bool, day int, cur money.Currency, net string) storage.Document {
		t.Helper()
		d := bill.Draft{SupplierID: b.Supplier.ID, Date: porttest.Day(10, day), Currency: cur,
			Lines: []bill.Line{{AccountID: b.Rent.ID, Net: moneyOf(t, net, cur.Code), GSTCode: "TX", GST: money.Zero(cur)}}}
		create := bills.Create
		if credit {
			create = bills.CreateCredit
		}
		doc, err := create(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	reconciled := func() payables.Ledger {
		t.Helper()
		var l payables.Ledger
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, got, err := payables.Reconcile(ctx, u, sgd, b.AP.ID, porttest.Day(10, 31))
			if err == nil && r.Unexplained() {
				t.Errorf("the sub-ledger differs from 2010: %+v", r)
			}
			l = got
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return l
	}
	usdBill := post(false, 14, usd, "100.00")
	usdCredit := post(true, 20, usd, "100.00")
	applyOf := func(credit, to uuid.UUID, day int, amount string, cur money.Currency) (journal.Applied, error) {
		return apply.Apply(ctx, b.Org, credits.Draft{CreditID: credit, BillID: to, Date: porttest.Day(10, day), Amount: moneyOf(t, amount, cur.Code)})
	}
	first, err := applyOf(usdCredit.ID, usdBill.ID, 21, "60.00", usd)
	if err != nil || legsOf(t, s, b.Org, first.Entry) != "2010 60 USD = 80.4; 2010 -60 USD = -78; 7030 -2.4 SGD = -2.4" ||
		first.Allocations[0].RealisedFX != "2.4000" || first.Allocations[1].BaseAmount != "78.0000" {
		t.Fatalf("applying USD 60.00: %s %+v %v", legsOf(t, s, b.Org, first.Entry), first.Allocations, err)
	}
	if _, err := applyOf(usdCredit.ID, usdBill.ID, 21, "40.01", usd); !sameSet(problemCodes(err), []string{credits.CodeTooMuch, credits.CodeTooMuch}) {
		t.Errorf("more than is open: %v", err)
	}
	second, err := applyOf(usdCredit.ID, usdBill.ID, 22, "40.00", usd)
	if err != nil || legsOf(t, s, b.Org, second.Entry) != "2010 40 USD = 53.6; 2010 -40 USD = -52; 7030 -1.6 SGD = -1.6" {
		t.Fatalf("applying the rest: %s %v", legsOf(t, s, b.Org, second.Entry), err)
	}
	if l := reconciled(); len(l.Suppliers) != 0 {
		t.Errorf("the bill and the credit are settled: %+v", l)
	}
	if _, err := bills.Void(ctx, b.Org, usdCredit.ID, porttest.Day(10, 25), "Entered twice"); !creditCode(err, credits.CodeApplied) {
		t.Errorf("voiding an applied credit: %v", err)
	}
	if _, err := apply.Void(ctx, b.Org, second.Application.ID, porttest.Day(10, 23), "Applied to the wrong bill"); err != nil {
		t.Fatal(err)
	}
	if l := reconciled(); len(l.Suppliers) != 1 || len(l.Suppliers[0].Items) != 2 {
		t.Errorf("after the void, USD 40.00 of each is open: %+v", l)
	}

	// SGD: what a payment left unapplied pays another bill, posting nothing.
	rent := post(false, 15, sgd, "100.00")
	other := post(false, 16, sgd, "40.00")
	paid, err := payments.Record(ctx, b.Org, billpayment.Draft{SupplierID: b.Supplier.ID, Date: porttest.Day(10, 20), AccountID: current.ID, Currency: sgd,
		Bills: []billpayment.Part{{BillID: rent.ID, Amount: sgdOf(t, "100.00")}}, Unapplied: sgdOf(t, "25.00")})
	if err != nil {
		t.Fatal(err)
	}
	fromPayment, err := applyOf(paid.Payment.ID, other.ID, 21, "25.00", sgd)
	if err != nil || len(fromPayment.Entry.Lines) != 0 || len(fromPayment.Allocations) != 2 {
		t.Fatalf("applying a payment's credit: %+v %v", fromPayment, err)
	}
	if _, err := applyOf(paid.Payment.ID, other.ID, 21, "0.01", sgd); !sameSet(problemCodes(err), []string{credits.CodeTooMuch}) {
		t.Errorf("more than is left of the payment's credit: %v", err)
	}
	if _, err := payments.Void(ctx, b.Org, paid.Payment.ID, porttest.Day(10, 25), "Bounced"); !creditCode(err, credits.CodeApplied) {
		t.Errorf("voiding a payment whose credit is applied: %v", err)
	}
	// FY056: an application that posted nothing is reversed only with the reversal of its allocations.
	wantState(t, "reversing an application by its status alone", asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.document SET status = 'reversed' WHERE id = $1", fromPayment.Application.ID)
		return err
	}), "FY056")
	reconciled()
	if _, err := apply.Void(ctx, b.Org, fromPayment.Application.ID, porttest.Day(10, 25), "Wrong bill"); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Void(ctx, b.Org, paid.Payment.ID, porttest.Day(10, 25), "Bounced"); err != nil {
		t.Errorf("voiding the payment once its application is voided: %v", err)
	}
	reconciled()
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("the replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
