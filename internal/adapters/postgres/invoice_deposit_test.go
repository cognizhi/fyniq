//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-09 (p1-sales task 3.2, part; the owner's decision of 24 Oct 2026: the final invoice deducts the deposit), worked
// by hand. The company is GST-registered from 1 October.
//   - DP-0001, a part payment of S$1,090.00 from Acme on 14 Oct: Cr 2410 1,000.00, Cr 2200 90.00.
//   - INV-0001, S$2,000.00 at 9% on 20 Oct, deducting S$600.00 of it: RND-DEPOSIT takes S$600.00 of its base amount and
//     S$49.54 of its GST (90 × 600 ÷ 1,090 = 49.541…): Dr 1100 1,580.00, Dr 2410 550.46, Cr 4000 2,000.00, Cr 2200 130.46.
//   - INV-0002, S$1,000.00 at 9% on 21 Oct, deducting the S$490.00 left: all that is left, S$490.00 and S$40.46: Dr 1100
//     600.00, Dr 2410 449.54, Cr 4000 1,000.00, Cr 2200 49.54. DP-0001 is used whole, and 2410 holds nothing of it.
//   - Voiding INV-0002 reopens S$490.00 of DP-0001; editing INV-0001 to deduct S$500.00 takes S$500.00 and S$41.28.
//   - DP-0002, a security deposit of USD 100.00 from Globex into the wallet on 14 Oct at 1.40 (S$140.00); INV-0003, USD
//     1,000.00 out of scope on 20 Oct at 1.30, deducting it: Dr 1100 USD 900.00 = 1,170.00, Dr 2410 140.00, Cr 4000 USD
//     1,000.00 = 1,300.00, Cr 7030 10.00 of realised FX.
//
// More than is left, another customer's deposit and a credit note that deducts one are refused; a deposit an invoice
// uses is not voided; the AR sub-ledger agrees with 1100, and every entry replays.
// req: FR-AR-09
// req: RND-DEPOSIT
// req: ledger/posting-rules Posting an invoice
// req: INV-07
func TestInvoicesDeductDeposits(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Deducted Deposits Pte Ltd")
	invoices, deposits := journal.Invoices{Store: s}, journal.CustomerDeposits{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var dbs, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2410", Name: "Customer deposits & retainers", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleCustomerDeposits)}); err != nil {
			return err
		}
		if dbs, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if wallet, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1021", Name: "Wise USD", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.40", 20: "1.30"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		_, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 1)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m := func(v string, cur money.Currency) money.Money {
		x, err := money.Parse(v, cur)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	dp1, err := deposits.Record(ctx, b.Org, deposit.Draft{CustomerID: b.Customer.ID, Kind: deposit.KindPartPayment, Date: porttest.Day(10, 14), AccountID: dbs.ID,
		Currency: sgd, Amount: m("1090.00", sgd), GSTCode: "SR"})
	if err != nil {
		t.Fatal(err)
	}
	dp2, err := deposits.Record(ctx, b.Org, deposit.Draft{CustomerID: b.Globex.ID, Kind: deposit.KindSecurity, Date: porttest.Day(10, 14), AccountID: wallet.ID,
		Currency: usd, Amount: m("100.00", usd)})
	if err != nil {
		t.Fatal(err)
	}
	draft := func(customer uuid.UUID, day int, cur money.Currency, price, code string, uses ...invoice.DepositUse) invoice.Draft {
		return invoice.Draft{CustomerID: customer, Date: porttest.Day(10, day), Currency: cur, Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", code, cur.Code)},
			Deposits: uses}
	}
	issue := func(d invoice.Draft) (storage.Document, storage.Entry) {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		e, err := invoices.Post(ctx, b.Org, doc.ID)
		if err != nil {
			t.Fatalf("issuing: %v", err)
		}
		return doc, e
	}
	open := func(id uuid.UUID) deposit.Open {
		t.Helper()
		var o deposit.Open
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			o, err = deposit.OpenDeposit(ctx, u, id, sgd, uuid.Nil)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return o
	}

	inv1, e := issue(draft(b.Customer.ID, 20, sgd, "2000.00", "SR", invoice.DepositUse{DepositID: dp1.Deposit.ID, Amount: m("600.00", sgd)}))
	if got := legsOf(t, s, b.Org, e); got != "1100 1580 SGD = 1580; 2410 550.46 SGD = 550.46; 4000 -2000 SGD = -2000; 2200 -130.46 SGD = -130.46" {
		t.Fatalf("INV-0001: %s", got)
	}
	inv2, e := issue(draft(b.Customer.ID, 21, sgd, "1000.00", "SR", invoice.DepositUse{DepositID: dp1.Deposit.ID, Amount: m("490.00", sgd)}))
	if got := legsOf(t, s, b.Org, e); got != "1100 600 SGD = 600; 2410 449.54 SGD = 449.54; 4000 -1000 SGD = -1000; 2200 -49.54 SGD = -49.54" {
		t.Fatalf("INV-0002: %s", got)
	}
	if o := open(dp1.Deposit.ID); !o.Left.IsZero() || !o.BaseLeft.IsZero() || !o.GSTLeft.IsZero() || o.Used.String() != "1090.00" {
		t.Errorf("DP-0001 used whole: %+v", o)
	}

	// Refused: more than is left, another customer's deposit, a credit note that deducts one; a used deposit stays.
	var probs ledger.Problems
	over, err := invoices.Create(ctx, b.Org, draft(b.Customer.ID, 22, sgd, "100.00", "SR", invoice.DepositUse{DepositID: dp1.Deposit.ID, Amount: m("1.00", sgd)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, over.ID); !asProblems(err, &probs) || !probs.Has(invoice.CodeDeposit) {
		t.Errorf("more than is left: %v", err)
	}
	other, err := invoices.Create(ctx, b.Org, draft(b.Customer.ID, 22, usd, "100.00", "OS", invoice.DepositUse{DepositID: dp2.Deposit.ID, Amount: m("1.00", usd)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, other.ID); !asProblems(err, &probs) || !probs.Has(invoice.CodeDeposit) {
		t.Errorf("another customer's deposit: %v", err)
	}
	credit, err := invoices.CreateCredit(ctx, b.Org, draft(b.Globex.ID, 22, usd, "10.00", "OS", invoice.DepositUse{DepositID: dp2.Deposit.ID, Amount: m("1.00", usd)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, credit.ID); !asProblems(err, &probs) || !probs.Has(invoice.CodeDeposit) {
		t.Errorf("a credit note deducting a deposit: %v", err)
	}
	if _, err := deposits.Void(ctx, b.Org, dp1.Deposit.ID, porttest.Day(10, 25), "Recorded twice"); !hasDepositCode(err, deposit.CodeUsed) {
		t.Errorf("voiding a used deposit: %v", err)
	}

	// Voiding INV-0002 reopens S$490.00 of DP-0001; editing INV-0001 to deduct S$500.00 takes it afresh.
	if _, err := invoices.Void(ctx, b.Org, inv2.ID, porttest.Day(10, 23), "Raised in error"); err != nil {
		t.Fatal(err)
	}
	if o := open(dp1.Deposit.ID); o.Left.String() != "490.00" || o.BaseLeft.String() != "490.00" || o.GSTLeft.String() != "40.46" {
		t.Errorf("DP-0001 after the void: %+v", o)
	}
	ed, err := invoices.Edit(ctx, b.Org, inv1.ID, draft(b.Customer.ID, 20, sgd, "2000.00", "SR", invoice.DepositUse{DepositID: dp1.Deposit.ID, Amount: m("500.00", sgd)}),
		"S$500 of the deposit, not S$600", "")
	if err != nil || legsOf(t, s, b.Org, ed.Entry) != "1100 1680 SGD = 1680; 2410 458.72 SGD = 458.72; 4000 -2000 SGD = -2000; 2200 -138.72 SGD = -138.72" {
		t.Fatalf("editing INV-0001: %s %v", legsOf(t, s, b.Org, ed.Entry), err)
	}
	if o := open(dp1.Deposit.ID); o.Left.String() != "590.00" || o.GSTLeft.String() != "48.72" {
		t.Errorf("DP-0001 after the edit: %+v", o)
	}

	// INV-0003 in USD deducts DP-0002, which came in at 1.40: the realised FX to 7030.
	_, e = issue(draft(b.Globex.ID, 20, usd, "1000.00", "OS", invoice.DepositUse{DepositID: dp2.Deposit.ID, Amount: m("100.00", usd)}))
	if got := legsOf(t, s, b.Org, e); got != "1100 900 USD = 1170; 2410 140 SGD = 140; 4000 -1000 USD = -1300; 7030 -10 SGD = -10" {
		t.Fatalf("INV-0003: %s", got)
	}

	// The AR sub-ledger: INV-0001's S$1,680.00 and INV-0003's USD 900.00 (S$1,170.00); 1100 agrees.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, _, err := receivables.Reconcile(ctx, u, sgd, b.AR.ID, porttest.Day(10, 31))
		if err == nil && (r.Unexplained() || r.Base.SubLedger.String() != "2850.00") {
			t.Errorf("the reconciliation: %+v", r)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Every entry replays: each deposit's part as its version took it.
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
