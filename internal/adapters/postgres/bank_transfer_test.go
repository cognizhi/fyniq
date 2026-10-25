//go:build local

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-BNK-09 and the posting rule "Transfer between money accounts" (rev 0.140; p1-bank-import-reconcile task 4.4,
// design D6), worked by hand:
//   - S$5,000.00 from DBS to savings on 6 October, both lines imported: one transfer, posted once (Dr 1030, Cr 1010),
//     matched on both accounts;
//   - S$1,355.00 out of DBS on 21 October for USD 1,000.00 into Wise, a S$5.00 fee reported, at the day's 1.3472:
//     Dr 1021 USD 1,000.00 = S$1,347.20, Cr 1010 S$1,355.00, Dr 6460 S$5.00 (the bank charges account), Dr 7030 S$2.80;
//     in transit until Wise's line arrives, then matched with nothing posted again;
//   - a conversion on 22 October, a day with no USD rate, is refused with FX-01 and writes nothing.
//
// The replay reproduces every transfer from its record and the rates its entry kept.
// req: FR-BNK-09
// req: ledger/posting-rules Transfer between money accounts
// req: ENG-40
// req: ENG-06
func TestTransfersAreRecordedOnceAndMatchedOnBothAccounts(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	b := newCategoryBooks(t, "Transfers Pte Ltd", now)
	s := store(t, "fyniq_api")
	var savings storage.MoneyAccount
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1030", Name: "DBS savings", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		if err != nil {
			return err
		}
		if savings, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: "DBS savings",
			Currency: "SGD", At: now}); err != nil {
			return err
		}
		if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6460", Name: "Bank charges", Type: storage.Expense}); err != nil {
			return err
		}
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 21), Rate: "1.3472", Quote: "1.3472",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	dbs := importLines(t, b.Org, b.sgd, now,
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 6), Amount: "-5000.00", Description: "TO SAVINGS", Fingerprint: fingerprint("t1")},
		storage.NewBankLine{Position: 2, BookingDate: porttest.Day(10, 21), Amount: "-1355.00", Description: "WISE", Fingerprint: fingerprint("t2")},
		storage.NewBankLine{Position: 3, BookingDate: porttest.Day(10, 22), Amount: "-100.00", Description: "WISE", Fingerprint: fingerprint("t3")})
	sav := importLines(t, b.Org, savings, now,
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 6), Amount: "5000.00", Description: "FROM CURRENT", Fingerprint: fingerprint("t4")})
	txns := journal.BankTransactions{Store: s}
	inTransit := func() []storage.BankTransfer {
		var out []storage.BankTransfer
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.BankMatching().InTransit(ctx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Both sides known: recorded once, matched on both accounts.
	one, err := txns.RecordTransfer(ctx, b.Org, journal.TransferRequest{FromLineID: &dbs[0].ID, ToLineID: &sav[0].ID, At: now})
	if err != nil || legsOf(t, s, b.Org, *one.Entry) != "1030 5000 SGD = 5000; 1010 -5000 SGD = -5000" || len(one.Matches) != 2 ||
		!one.Entry.PostingDate.Equal(porttest.Day(10, 6)) || one.Entry.Source.Type != ledger.SourceBankTransfer {
		t.Fatalf("one transfer: %+v %v", one, err)
	}
	for _, id := range []storage.BankLine{dbs[0], sav[0]} {
		if l := lineOf(t, b.Org, id.ID); l.Status != storage.LineTransferred {
			t.Errorf("line %s: %s", l.Description, l.Status)
		}
	}

	// The Wise conversion, in transit until its other side arrives.
	usd, fee := moneyOf(t, "1000.00", "USD"), moneyOf(t, "5.00", "SGD")
	wise, err := txns.RecordTransfer(ctx, b.Org, journal.TransferRequest{FromLineID: &dbs[1].ID, ToAccountID: b.usd.ID, Received: &usd, Fee: &fee, At: now})
	if err != nil || legsOf(t, s, b.Org, *wise.Entry) != "1021 1000 USD = 1347.2; 1010 -1355 SGD = -1355; 6460 5 SGD = 5; 7030 2.8 SGD = 2.8" {
		t.Fatalf("the Wise conversion: %+v %v", wise, err)
	}
	if transit := inTransit(); !slices.ContainsFunc(transit, func(x storage.BankTransfer) bool { return x.ID == wise.Transfer.ID }) ||
		slices.ContainsFunc(transit, func(x storage.BankTransfer) bool { return x.ID == one.Transfer.ID }) {
		t.Fatalf("in transit: %+v", transit)
	}
	if _, err := txns.CompleteTransfer(ctx, b.Org, wise.Transfer.ID, dbs[2].ID, "", now); !isBankTxn(err, journal.CodeTransferLine) {
		t.Fatalf("a line of the paying account that is not the missing side: %v", err)
	}
	arrived := importLines(t, b.Org, b.usd, now,
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 21), Amount: "1000.00", Description: "FROM DBS", Fingerprint: fingerprint("t5")})
	before := count(t, b.Org, "journal_entry")
	done, err := txns.CompleteTransfer(ctx, b.Org, wise.Transfer.ID, arrived[0].ID, "", now)
	if err != nil || done.Transfer.ToLineID == nil || *done.Transfer.ToLineID != arrived[0].ID || count(t, b.Org, "journal_entry") != before {
		t.Fatalf("its other side: %+v %v", done, err)
	}
	if slices.ContainsFunc(inTransit(), func(x storage.BankTransfer) bool { return x.ID == wise.Transfer.ID }) {
		t.Error("a completed transfer is no longer in transit")
	}

	// No rate on 22 October: refused, nothing written.
	before = count(t, b.Org, "bank_transfer")
	_, err = txns.RecordTransfer(ctx, b.Org, journal.TransferRequest{FromLineID: &dbs[2].ID, ToAccountID: b.usd.ID, Received: &usd, At: now})
	var probs ledger.Problems
	if !errors.As(err, &probs) || !probs.Has(fx.CodeNoRate) || probs.Has(ledger.CodeMissingRate) || count(t, b.Org, "bank_transfer") != before {
		t.Fatalf("a conversion with no rate: %v", err)
	}
	if _, err := txns.RecordTransfer(ctx, b.Org, journal.TransferRequest{At: now}); !isBankTxn(err, journal.CodeTransferLine) {
		t.Errorf("a transfer of no line: %v", err)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && (len(r.Mismatches) > 0 || r.Entries != 2) {
			t.Errorf("the replay: %d entries, %+v", r.Entries, r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
