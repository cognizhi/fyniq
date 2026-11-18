//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/perf"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The harness grows a ledger through the posting engine, as the API posts: each journal a payment of expenses from the
// money account, the months before this one opened as periods, and the balances agreeing with the ledger afterwards
// (QA-PERF-01; the engine's invariants hold at any size).
// req: QA-PERF-01
// req: NFR-SCALE-01
func TestTheHarnessGrowsALedgerThroughTheEngine(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Perf Pte Ltd")
	api := store(t, "fyniq_api")
	var bank storage.Account
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if bank, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "Operating account", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		_, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "6200", Name: "Software", Type: storage.Expense})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := (perf.Grower{Store: api, Org: b.Org, Clock: clock.System{}, Lines: 500, PerJournal: 10, Seed: 3}).Grow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Journals != 50 || rep.Lines != 500 || rep.Money != bank.ID || len(rep.Expenses) == 0 || rep.Periods < perf.Months-2 || len(rep.Sample) == 0 ||
		rep.Currency != "SGD" {
		t.Fatalf("the growth: %+v", rep)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		diffs, err := u.Balances().Differences(ctx)
		if err == nil && len(diffs) != 0 {
			t.Errorf("the balances disagree with the ledger: %+v", diffs)
		}
		periods, err2 := u.Periods().List(ctx)
		if err2 == nil && len(periods) < perf.Months {
			t.Errorf("periods: %d", len(periods))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Growing again adds to it; the periods are there already.
	again, err := (perf.Grower{Store: api, Org: b.Org, Clock: clock.System{}, Lines: 100, PerJournal: 50, Seed: 4}).Grow(ctx)
	if err != nil || again.Journals != 2 || again.Periods != 0 {
		t.Fatalf("growing again: %+v %v", again, err)
	}
}
