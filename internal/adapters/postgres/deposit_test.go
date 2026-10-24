//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func hasDepositCode(err error, code string) bool {
	var probs ledger.Problems
	if errors.As(err, &probs) {
		return probs.Has(code)
	}
	var de *deposit.Error
	return errors.As(err, &de) && de.Code == code
}

// FR-AR-09 and the posting rule "Customer refunds and deposits" (p1-sales task 3.2, part; the owner's decisions of 24 Oct
// 2026), worked by hand. The company registers for GST from 16 October.
//   - DP-0001, a security deposit of S$500.00 from Acme into DBS on 14 Oct: Dr 1010 500.00, Cr 2410 500.00.
//   - DP-0002, a part payment of S$1,090.00 on 14 Oct, before the registration: no GST, Cr 2410 1,090.00.
//   - DP-0003, a part payment of S$1,090.00 on 20 Oct, standard-rated: Cr 2410 1,000.00, Cr 2200 90.00 (ENG-45).
//   - DP-0004, a part payment of USD 1,000.00 from Globex into the USD wallet on 21 Oct at the cascade's 1.35: S$1,350.00,
//     Cr 2200 S$111.47 (RND-GST-INCL), Cr 2410 S$1,238.53, in the base currency.
//
// A security deposit with a GST code, a part payment with a purchase code, an unknown kind and a supplier are refused,
// and take no number; a void reverses the entry; what is left of each deposit is its amount; every entry replays.
// req: FR-AR-09
// req: ledger/posting-rules Customer refunds and deposits
// req: ENG-45
// req: RND-GST-INCL
func TestCustomerDepositsPostThroughThePostingService(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Deposits Pte Ltd")
	deposits := journal.CustomerDeposits{Store: s}
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
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 21), Rate: "1.35", Quote: "1.35",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record := func(d deposit.Draft) (journal.RecordedDeposit, error) { return deposits.Record(ctx, b.Org, d) }
	draft := func(kind string, day int, into storage.Account, cur money.Currency, amount, code string) deposit.Draft {
		m, _ := money.Parse(amount, cur)
		return deposit.Draft{CustomerID: b.Customer.ID, Kind: kind, Date: porttest.Day(10, day), AccountID: into.ID, Currency: cur, Amount: m, GSTCode: code,
			Reference: "FAST"}
	}

	security, err := record(draft(deposit.KindSecurity, 14, dbs, sgd, "500.00", ""))
	if err != nil || security.Deposit.Number != "DP-0001" || legsOf(t, s, b.Org, security.Entry) != "1010 500 SGD = 500; 2410 -500 SGD = -500" ||
		security.Entry.Memo != "Acme Analytics Pte Ltd DP-0001" {
		t.Fatalf("DP-0001: %s %q %v", legsOf(t, s, b.Org, security.Entry), security.Entry.Memo, err)
	}
	early, err := record(draft(deposit.KindPartPayment, 14, dbs, sgd, "1090.00", "SR"))
	if err != nil || legsOf(t, s, b.Org, early.Entry) != "1010 1090 SGD = 1090; 2410 -1090 SGD = -1090" {
		t.Fatalf("DP-0002, before the registration: %s %v", legsOf(t, s, b.Org, early.Entry), err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 16), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 14)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	taxed, err := record(draft(deposit.KindPartPayment, 20, dbs, sgd, "1090.00", "SR"))
	if err != nil || legsOf(t, s, b.Org, taxed.Entry) != "1010 1090 SGD = 1090; 2410 -1000 SGD = -1000; 2200 -90 SGD = -90" {
		t.Fatalf("DP-0003: %s %v", legsOf(t, s, b.Org, taxed.Entry), err)
	}
	usdDraft := draft(deposit.KindPartPayment, 21, wallet, usd, "1000.00", "SR")
	usdDraft.CustomerID = b.Globex.ID
	usdDeposit, err := record(usdDraft)
	if err != nil || legsOf(t, s, b.Org, usdDeposit.Entry) != "1021 1000 USD = 1350; 2410 -1238.53 SGD = -1238.53; 2200 -111.47 SGD = -111.47" {
		t.Fatalf("DP-0004: %s %v", legsOf(t, s, b.Org, usdDeposit.Entry), err)
	}

	// What is left of each: all of it; what 2410 holds, at the rate it came in.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := deposit.OpenDeposit(ctx, u, usdDeposit.Deposit.ID, sgd, uuid.Nil)
		if err != nil {
			return err
		}
		if o.Left.String() != "1000.00" || o.Base.String() != "1350.00" || o.Net.String() != "1238.53" || o.GST.String() != "111.47" || o.Kind != deposit.KindPartPayment {
			t.Errorf("DP-0004 open: %+v", o)
		}
		if _, err := deposit.OpenDeposit(ctx, u, b.Customer.ID, sgd, uuid.Nil); !hasDepositCode(err, deposit.CodeNotDeposit) {
			t.Errorf("a contact as a deposit: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The refusals, each with its code, and no number taken.
	supplier := draft(deposit.KindSecurity, 20, dbs, sgd, "1.00", "")
	supplier.CustomerID = b.Supplier.ID
	for _, x := range []struct {
		name string
		d    deposit.Draft
		code string
	}{
		{"a security deposit with a GST code", draft(deposit.KindSecurity, 20, dbs, sgd, "1.00", "SR"), deposit.CodeGSTCode},
		{"a part payment with a purchase code", draft(deposit.KindPartPayment, 20, dbs, sgd, "1.00", "TX"), deposit.CodeGSTCode},
		{"an unknown kind", draft("advance", 20, dbs, sgd, "1.00", ""), deposit.CodeKind},
		{"a supplier", supplier, deposit.CodeCustomer},
	} {
		if _, err := record(x.d); !hasDepositCode(err, x.code) {
			t.Errorf("%s: %v", x.name, err)
		}
	}
	if next, err := record(draft(deposit.KindSecurity, 22, dbs, sgd, "1.00", "")); err != nil || next.Deposit.Number != "DP-0005" {
		t.Errorf("the refusals took numbers: %+v %v", next.Deposit, err)
	}

	// A void reverses DP-0001; twice, or before its date, is refused.
	if _, err := deposits.Void(ctx, b.Org, security.Deposit.ID, porttest.Day(10, 13), "Returned"); !hasDepositCode(err, deposit.CodeVoidDate) {
		t.Errorf("a void before the deposit: %v", err)
	}
	v, err := deposits.Void(ctx, b.Org, security.Deposit.ID, porttest.Day(10, 25), "Recorded twice")
	if err != nil || legsOf(t, s, b.Org, v.Entry) != "1010 -500 SGD = -500; 2410 500 SGD = 500" || v.Deposit.Status != storage.DocumentReversed ||
		!strings.Contains(v.Entry.Memo, "Recorded twice") {
		t.Fatalf("voiding DP-0001: %s %+v %v", legsOf(t, s, b.Org, v.Entry), v, err)
	}
	if _, err := deposits.Void(ctx, b.Org, security.Deposit.ID, porttest.Day(10, 25), "Again"); !hasDepositCode(err, deposit.CodeNotPosted) {
		t.Errorf("voiding twice: %v", err)
	}

	// Every entry replays: the cascade's rate and the GST the entries bore.
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
