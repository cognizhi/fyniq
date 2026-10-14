package porttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Books is a small organisation for the storage suites: a bank account, a sales account, a group
// heading with one expense account under it, and October and November 2026 as periods.
type Books struct {
	Org            uuid.UUID
	Bank, Sales    storage.Account
	Expenses, Rent storage.Account // Expenses is a group; Rent sits under it
	Oct, Nov       storage.Period
}

// Day is a calendar date in 2026.
func Day(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

// NewBooks creates Books in s.
func NewBooks(t *testing.T, s storage.Store, name string) Books {
	t.Helper()
	ctx := context.Background()
	org, err := s.CreateOrganisation(ctx, storage.NewOrganisation{Name: name, BaseCurrency: "SGD"})
	if err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}
	b := Books{Org: org.ID}
	err = s.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		create := func(a storage.NewAccount) storage.Account {
			if err != nil {
				return storage.Account{}
			}
			var acc storage.Account
			acc, err = u.Accounts().Create(ctx, a)
			return acc
		}
		b.Bank = create(storage.NewAccount{Code: "1000", Name: "Bank", Type: storage.Asset})
		b.Sales = create(storage.NewAccount{Code: "4000", Name: "Sales", Type: storage.Revenue})
		b.Expenses = create(storage.NewAccount{Code: "6000", Name: "Expenses", Type: storage.Expense, Group: true})
		b.Rent = create(storage.NewAccount{Code: "6100", Name: "Rent", Type: storage.Expense, ParentID: &b.Expenses.ID})
		if err != nil {
			return err
		}
		if b.Oct, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: Day(10, 1), EndsOn: Day(10, 31)}); err != nil {
			return err
		}
		b.Nov, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: Day(11, 1), EndsOn: Day(11, 30)})
		return err
	})
	if err != nil {
		t.Fatalf("set up the books: %v", err)
	}
	return b
}

// Sale is a balanced SGD entry: the bank debited and sales credited by amount.
func Sale(identity string, date time.Time, b Books, amount string) storage.NewEntry {
	return storage.NewEntry{
		PostingDate: date, PostingIdentity: identity, RuleVersion: "test@1",
		Source: storage.Source{Type: "manual_journal", ID: identity, Version: 1},
		Lines: []storage.NewLine{
			{AccountID: b.Bank.ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
			{AccountID: b.Sales.ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
		},
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := storage.CodeOf(err); got != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// Storage checks a storage.Store adapter (NFR-PORT-04). build returns a store that connects as the
// given service's identity: "api" for commands and reads, "worker" for the balances verification
// and rebuild (SEC-03). Each case uses organisations of its own.
func Storage(t *testing.T, build func(t *testing.T, service string) storage.Store) {
	t.Helper()
	ctx := context.Background()
	s, worker := build(t, "api"), build(t, "worker")
	var n int
	books := func(t *testing.T) Books {
		n++
		return NewBooks(t, s, fmt.Sprintf("Conformance %d Pte Ltd", n))
	}

	t.Run("a new organisation starts at revision 0 in its base currency", func(t *testing.T) {
		b := books(t)
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil || o.BaseCurrency != "SGD" || o.BaseMinorUnits != 2 {
				t.Fatalf("Organisation = %+v, %v", o, err)
			}
			if rev, err := u.Ledger().Revision(ctx); err != nil || rev != 0 {
				t.Fatalf("Revision = %d, %v; want 0", rev, err)
			}
			return nil
		})
		if _, err := s.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Nowhere Pte Ltd", BaseCurrency: "XXX"}); storage.CodeOf(err) != storage.CodeAmount {
			t.Fatalf("an unknown base currency: want %s, got %v", storage.CodeAmount, err)
		}
	})

	t.Run("a balanced entry posts with the next revision and its period", func(t *testing.T) {
		b := books(t)
		for i, want := range []int64{1, 2} {
			var e storage.Entry
			err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				e, err = u.Ledger().Append(ctx, Sale(fmt.Sprintf("sale-%d", i), Day(10, 14), b, "100.00"))
				return err
			})
			if err != nil {
				t.Fatalf("Append: %v", err)
			}
			if e.LedgerRevision != want || e.PeriodID != b.Oct.ID || len(e.Lines) != 2 || e.Lines[0].Amount != "100.0000" || e.Lines[1].BaseCurrency != "SGD" {
				t.Fatalf("entry = %+v; want revision %d in October", e, want)
			}
		}
	})

	t.Run("an unbalanced entry is refused at commit", func(t *testing.T) {
		b := books(t)
		e := Sale("unbalanced", Day(10, 14), b, "100.00")
		e.Lines[1].Amount, e.Lines[1].BaseAmount = "-99.99", "-99.99"
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		})
		wantCode(t, err, storage.CodeUnbalanced)
	})

	t.Run("a posting identity is never used twice", func(t *testing.T) {
		b := books(t)
		post := func() error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.Ledger().Append(ctx, Sale("same-identity", Day(10, 14), b, "10.00"))
				return err
			})
		}
		if err := post(); err != nil {
			t.Fatal(err)
		}
		wantCode(t, post(), storage.CodeDuplicate)
	})

	t.Run("a closed period refuses postings until it is reopened", func(t *testing.T) {
		b := books(t)
		do := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return s.Do(ctx, b.Org, fn) }
		var closed storage.Period
		if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			closed, err = u.Periods().Close(ctx, b.Oct.ID)
			return err
		}); err != nil || closed.Status != storage.PeriodClosed || closed.ClosedRevision == nil {
			t.Fatalf("Close = %+v, %v", closed, err)
		}
		wantCode(t, do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Periods().Close(ctx, b.Oct.ID)
			return err
		}), storage.CodePeriodState)
		wantCode(t, do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, Sale("into-closed", Day(10, 31), b, "1.00"))
			return err
		}), storage.CodePeriodClosed)
		if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Periods().Reopen(ctx, b.Oct.ID); err != nil {
				return err
			}
			_, err := u.Ledger().Append(ctx, Sale("after-reopen", Day(10, 31), b, "1.00"))
			return err
		}); err != nil {
			t.Fatalf("reopen and post: %v", err)
		}
		wantCode(t, do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, Sale("no-period", Day(12, 1), b, "1.00"))
			return err
		}), storage.CodeNoPeriod)
	})

	t.Run("only active leaf accounts take postings", func(t *testing.T) {
		b := books(t)
		post := func(identity string, account uuid.UUID) error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				e := Sale(identity, Day(10, 14), b, "5.00")
				e.Lines[1].AccountID = account
				_, err := u.Ledger().Append(ctx, e)
				return err
			})
		}
		wantCode(t, post("to-group", b.Expenses.ID), storage.CodeAccountNotPost)
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			a, err := u.Accounts().SetActive(ctx, b.Rent.ID, false)
			if err == nil && (a.Active || a.Version != 2) {
				t.Errorf("SetActive = %+v", a)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		wantCode(t, post("to-inactive", b.Rent.ID), storage.CodeAccountNotPost)
		wantCode(t, post("to-nowhere", uuid.New()), storage.CodeAccountNotPost)
	})

	t.Run("amounts keep their currency's scale and exact text", func(t *testing.T) {
		b := books(t)
		post := func(e storage.NewEntry) error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.Ledger().Append(ctx, e)
				return err
			})
		}
		wantCode(t, post(Sale("three-decimals", Day(10, 14), b, "1.005")), storage.CodeAmount)
		wantCode(t, post(Sale("not-a-number", Day(10, 14), b, "1e3")), storage.CodeInvalid)
	})

	t.Run("a unit of work commits or rolls back as a whole", func(t *testing.T) {
		b := books(t)
		boom := errors.New("a later write in the same command failed")
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Ledger().Append(ctx, Sale("rolled-back", Day(10, 14), b, "7.00")); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) && storage.CodeOf(err) == "" {
			t.Fatalf("Do = %v; want the command's own error", err)
		}
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if rev, _ := u.Ledger().Revision(ctx); rev != 0 {
				t.Errorf("revision = %d after a rolled-back command; want 0 (no gap, no partial rows)", rev)
			}
			if bal, _ := u.Balances().ForPeriod(ctx, b.Oct.ID); len(bal) != 0 {
				t.Errorf("balances = %+v after a rolled-back command; want none", bal)
			}
			return nil
		})
		var kept storage.UnitOfWork
		_ = s.Do(ctx, b.Org, func(_ context.Context, u storage.UnitOfWork) error { kept = u; return nil })
		_, err = kept.Ledger().Revision(ctx)
		wantCode(t, err, storage.CodeInvalid)
	})

	t.Run("one organisation never sees another's rows", func(t *testing.T) {
		a, b := books(t), books(t)
		var entry storage.Entry
		if err := s.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			entry, err = u.Ledger().Append(ctx, Sale("a-only", Day(10, 14), a, "42.00"))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Ledger().Entry(ctx, entry.ID); !errors.Is(err, ports.ErrNotFound) {
				t.Errorf("B reads A's entry: want ErrNotFound, got %v", err)
			}
			if _, err := u.Accounts().Get(ctx, a.Bank.ID); !errors.Is(err, ports.ErrNotFound) {
				t.Errorf("B reads A's account: want ErrNotFound, got %v", err)
			}
			return nil
		})
		cross := Sale("b-into-a", Day(10, 14), b, "1.00")
		cross.Lines[0].AccountID = a.Bank.ID
		wantCode(t, s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, cross)
			return err
		}), storage.CodeAccountNotPost)
		wantCode(t, s.Do(ctx, uuid.Nil, func(context.Context, storage.UnitOfWork) error { return nil }), storage.CodeNoTenant)
	})

	t.Run("balances follow the lines and rebuild to the same figures", func(t *testing.T) {
		b := books(t)
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			for i, amt := range []string{"100.00", "250.50", "0.01"} {
				if _, err := u.Ledger().Append(ctx, Sale(fmt.Sprintf("bal-%d", i), Day(10, 2+i), b, amt)); err != nil {
					return err
				}
			}
			bal, err := u.Balances().ForPeriod(ctx, b.Oct.ID)
			if err != nil {
				return err
			}
			got := map[uuid.UUID]storage.Decimal{}
			for _, x := range bal {
				got[x.AccountID] = x.BaseAmount
			}
			if got[b.Bank.ID] != "350.5100" || got[b.Sales.ID] != "-350.5100" {
				t.Errorf("balances = %+v; want bank 350.51 and sales -350.51", bal)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if d, err := u.Balances().Differences(ctx); err != nil || len(d) != 0 {
				t.Errorf("Differences = %+v, %v; want none", d, err)
			}
			if rows, err := u.Balances().Rebuild(ctx); err != nil || rows != 2 {
				t.Errorf("Rebuild = %d, %v; want 2 rows", rows, err)
			}
			if d, err := u.Balances().Differences(ctx); err != nil || len(d) != 0 {
				t.Errorf("Differences after Rebuild = %+v, %v; want none", d, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
