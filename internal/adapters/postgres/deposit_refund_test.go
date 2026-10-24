//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/customercredit"
	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-09 (p1-sales task 3.2), worked by hand. The company is GST-registered from 1 October.
//   - DP-0001, a part payment of S$1,090.00 from Acme on 14 Oct: Cr 2410 1,000.00, Cr 2200 90.00. CR-0001 refunds
//     S$545.00 of it from DBS on 16 Oct (deposit_refund@1): its part S$545.00 with S$45.00 of GST (RND-DEPOSIT): Dr 2410
//     500.00, Dr 2200 45.00, Cr 1010 545.00.
//   - DP-0002, a security deposit of USD 100.00 from Globex into the wallet on 14 Oct at 1.40 (S$140.00); CR-0002 refunds
//     it on 22 Oct at the cascade's 1.30: Dr 2410 140.00, Cr 1021 USD 100.00 = S$130.00, Cr 7030 S$10.00 of realised FX.
//   - A manual journal credits 2410 with S$30.00 on 25 Oct, which no deposit supports.
//
// More than is left of a deposit is refused; a refunded deposit is not voided; voiding a refund reopens the deposit; the
// deposits sub-ledger agrees with 2410 (S$640.00 held, the journal's S$30.00 explained); the close says ✔ and ⚠; every
// entry replays.
// req: FR-AR-09
// req: ledger/posting-rules Customer refunds and deposits
// req: RND-DEPOSIT
// req: ENG-41
func TestDepositsAreRefundedAndReconciled(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Refunded Deposits Pte Ltd")
	deposits, refunds := journal.CustomerDeposits{Store: s}, journal.CustomerRefunds{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var held, dbs, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if held, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2410", Name: "Customer deposits & retainers", Type: storage.Liability,
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
		for day, rate := range map[int]string{14: "1.40", 22: "1.30"} {
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
	refund := func(credit uuid.UUID, day int, from storage.Account, amount string, cur money.Currency) (journal.Refunded, error) {
		return refunds.Record(ctx, b.Org, customercredit.RefundDraft{CreditID: credit, Date: porttest.Day(10, day), AccountID: from.ID, Reference: "FAST", Amount: m(amount, cur)})
	}

	r1, err := refund(dp1.Deposit.ID, 16, dbs, "545.00", sgd)
	if err != nil || r1.Refund.Number != "CR-0001" || legsOf(t, s, b.Org, r1.Entry) != "2410 500 SGD = 500; 2200 45 SGD = 45; 1010 -545 SGD = -545" ||
		r1.Entry.RuleVersion != ledger.RuleDepositRefund || string(r1.Allocation.Tax) != "45.0000" || r1.Entry.Memo != "Acme Analytics Pte Ltd, Refund of DP-0001, FAST" {
		t.Fatalf("CR-0001: %s %+v %v", legsOf(t, s, b.Org, r1.Entry), r1.Allocation, err)
	}
	if _, err := refund(dp1.Deposit.ID, 16, dbs, "545.01", sgd); !hasApplyCode(err, customercredit.CodeTooMuch) {
		t.Errorf("more than is left of DP-0001: %v", err)
	}
	r2, err := refund(dp2.Deposit.ID, 22, wallet, "100.00", usd)
	if err != nil || legsOf(t, s, b.Org, r2.Entry) != "2410 140 SGD = 140; 1021 -100 USD = -130; 7030 -10 SGD = -10" ||
		string(r2.Allocation.RealisedFX) != "10.0000" || string(r2.Allocation.BaseAmount) != "140.0000" {
		t.Fatalf("CR-0002: %s %+v %v", legsOf(t, s, b.Org, r2.Entry), r2.Allocation, err)
	}
	if _, err := deposits.Void(ctx, b.Org, dp1.Deposit.ID, porttest.Day(10, 25), "Recorded twice"); !hasDepositCode(err, deposit.CodeUsed) {
		t.Errorf("voiding a refunded deposit: %v", err)
	}

	// Voiding CR-0002 reopens DP-0002.
	v2, err := refunds.Void(ctx, b.Org, r2.Refund.ID, porttest.Day(10, 23), "Bounced")
	if err != nil || legsOf(t, s, b.Org, v2.Entry) != "2410 -140 SGD = -140; 1021 100 USD = 130; 7030 10 SGD = 10" {
		t.Fatalf("voiding CR-0002: %s %v", legsOf(t, s, b.Org, v2.Entry), err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := deposit.OpenDeposit(ctx, u, dp2.Deposit.ID, sgd, uuid.Nil)
		if err == nil && (o.Left.String() != "100.00" || o.BaseLeft.String() != "140.00") {
			t.Errorf("DP-0002 after the void: %+v", o)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Every entry so far replays: each refund's part of its deposit as it took it.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A manual journal credits 2410 with S$30.00; the deposits sub-ledger agrees, the journal explained.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 25), PostingIdentity: "manual_journal:" + uuid.NewString() + ":v1:journal@1",
			RuleVersion: "journal@1", Memo: "Retainer", Source: storage.Source{Type: ledger.SourceManualJournal, ID: uuid.NewString(), Version: 1},
			Lines: []storage.NewLine{
				{AccountID: dbs.ID, Currency: "SGD", Amount: "30.00", FXRate: "1", FXRateSource: "base", BaseAmount: "30.00"},
				{AccountID: held.ID, Currency: "SGD", Amount: "-30.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-30.00"},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, l, err := deposit.Reconcile(ctx, u, sgd, held.ID, porttest.Day(10, 31))
		if err != nil {
			return err
		}
		// DP-0001 holds S$500.00 and DP-0002 S$140.00 again; 2410 holds those and the journal's S$30.00.
		if r.Unexplained() || r.Base.SubLedger.String() != "640.00" || r.Base.Control.String() != "670.00" || len(l.Customers) != 2 {
			t.Errorf("the reconciliation: %+v %+v", r, l)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The close: ✔ the deposits agree, and ⚠ the journal's S$30.00 no deposit supports.
	cc, err := closing.Service{Store: s}.RunChecks(ctx, b.Org, b.Oct.ID)
	if err != nil {
		t.Fatal(err)
	}
	all, err := closing.Read(cc.Result)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]closing.Check{}
	for _, c := range all {
		got[c.Key] = c
	}
	if c := got["deposits"]; c.Severity != closing.OK || c.Detail != "2 deposit(s) held" {
		t.Errorf("the deposits check: %+v", c)
	}
	if c := got["deposits-unsupported"]; c.Severity != closing.Warning ||
		c.Detail != "2410 holds 30.00 SGD from manual journals: support them with the customers' statements, or record the deposits" {
		t.Errorf("the unsupported amounts: %+v", c)
	}

}
