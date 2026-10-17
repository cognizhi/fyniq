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

	t.Run("every change is in the organisation's audit trail", func(t *testing.T) {
		b := books(t)
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, Sale("audited", Day(10, 14), b, "12.00"))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			head, err := u.Audit().Head(ctx)
			if err != nil {
				return err
			}
			// organisation, 4 accounts, 2 periods, the entry
			if head.Seq != 8 || len(head.Hash) != 32 || head.Action != "journal_entry.posted" {
				t.Errorf("Head = %d %x %q; want 8 events ending with journal_entry.posted", head.Seq, head.Hash, head.Action)
			}
			if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "test.recorded", ObjectType: "organisation", ObjectID: b.Org}); err != nil {
				return err
			}
			evs, err := u.Audit().Events(ctx, 6, 10)
			if err == nil && (len(evs) != 2 || evs[0].Seq != 7 || evs[1].Seq != 8) {
				t.Errorf("Events(after 6) = %d events; want 7 and 8 (the recorded one is written at commit)", len(evs))
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if head, _ := u.Audit().Head(ctx); head.Seq != 9 || head.Action != "test.recorded" {
				t.Errorf("after commit, Head = %d %q; want the recorded event at 9", head.Seq, head.Action)
			}
			return nil
		})
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

	t.Run("accounts come a page at a time, by code", func(t *testing.T) {
		b := books(t)
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Accounts().List(ctx)
			if err != nil || len(all) < 4 {
				t.Fatalf("List = %d accounts, %v", len(all), err)
			}
			var paged []storage.Account
			after := ""
			for {
				page, err := u.Accounts().Page(ctx, after, 3)
				if err != nil {
					t.Fatalf("Page(%q, 3): %v", after, err)
				}
				paged = append(paged, page...)
				if len(page) < 3 {
					break
				}
				after = page[len(page)-1].Code
			}
			if len(paged) != len(all) {
				t.Fatalf("the pages hold %d accounts, the list %d", len(paged), len(all))
			}
			for i := range all {
				if paged[i].ID != all[i].ID {
					t.Fatalf("page item %d is %s, the list's %s", i, paged[i].Code, all[i].Code)
				}
			}
			if _, err := u.Accounts().Page(ctx, "", 0); storage.CodeOf(err) != storage.CodeInvalid {
				t.Fatalf("a page of 0: want %s, got %v", storage.CodeInvalid, err)
			}
			return nil
		})
	})

	t.Run("a unit of work opened inside another joins it", func(t *testing.T) {
		b, other := books(t), books(t)
		failed := errors.New("the outer work fails")
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "7100", Name: "Outer", Type: storage.Expense}); err != nil {
				return err
			}
			if err := s.Do(ctx, b.Org, func(ctx context.Context, inner storage.UnitOfWork) error {
				_, err := inner.Accounts().Create(ctx, storage.NewAccount{Code: "7200", Name: "Inner", Type: storage.Expense})
				return err
			}); err != nil {
				return err
			}
			if err := s.Do(ctx, other.Org, func(context.Context, storage.UnitOfWork) error { return nil }); storage.CodeOf(err) != storage.CodeNoTenant {
				t.Errorf("joining for another organisation: want %s, got %v", storage.CodeNoTenant, err)
			}
			return failed
		})
		if !errors.Is(err, failed) {
			t.Fatalf("Do = %v", err)
		}
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			for _, code := range []string{"7100", "7200"} {
				if _, err := u.Accounts().ByCode(ctx, code); !errors.Is(err, ports.ErrNotFound) {
					t.Errorf("account %s outlived the rollback: %v", code, err)
				}
			}
			return nil
		})
	})

	t.Run("an idempotency key holds its response, per client, until purged", func(t *testing.T) {
		b := books(t)
		expires := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
		claim := storage.IdempotencyClaim{Client: "web user-1", Key: "key-1", RequestHash: make([]byte, 32),
			CreatedAt: expires.Add(-24 * time.Hour), ExpiresAt: expires}
		claim.RequestHash[0] = 1
		do := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return s.Do(ctx, b.Org, fn) }
		// A claim that rolls back leaves nothing behind.
		_ = do(func(ctx context.Context, u storage.UnitOfWork) error {
			if earlier, err := u.Idempotency().Claim(ctx, claim); err != nil || earlier != nil {
				t.Fatalf("first Claim = %+v, %v", earlier, err)
			}
			return errors.New("the command failed")
		})
		err := do(func(ctx context.Context, u storage.UnitOfWork) error {
			if earlier, err := u.Idempotency().Claim(ctx, claim); err != nil || earlier != nil {
				t.Fatalf("Claim after a rollback = %+v, %v; want a fresh claim", earlier, err)
			}
			return u.Idempotency().Complete(ctx, claim.Client, claim.Key, 201, []byte(`{"id":"x"}`))
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = do(func(ctx context.Context, u storage.UnitOfWork) error {
			earlier, err := u.Idempotency().Claim(ctx, claim)
			if err != nil || earlier == nil || earlier.Status != 201 || string(earlier.Body) != `{"id":"x"}` || earlier.RequestHash[0] != 1 || !earlier.ExpiresAt.Equal(expires) {
				t.Fatalf("Claim of a used key = %+v, %v; want the saved response", earlier, err)
			}
			other := claim
			other.Client = "web user-2"
			if earlier, err := u.Idempotency().Claim(ctx, other); err != nil || earlier != nil {
				t.Fatalf("another client's Claim of the same key = %+v, %v; want a fresh claim", earlier, err)
			}
			if err := u.Idempotency().Complete(ctx, "web user-3", "never-claimed", 200, nil); storage.CodeOf(err) != storage.CodeInvalid {
				t.Fatalf("Complete of an unclaimed key: want %s, got %v", storage.CodeInvalid, err)
			}
			return errors.New("roll back")
		})
		// A concurrent claim of the same key waits for the first unit of work, then reads its response.
		second := claim
		second.Key = "key-2"
		claimed, done := make(chan struct{}), make(chan *storage.StoredResponse, 1)
		go func() {
			<-claimed
			_ = do(func(ctx context.Context, u storage.UnitOfWork) error {
				earlier, err := u.Idempotency().Claim(ctx, second)
				if err != nil {
					t.Errorf("the waiting Claim: %v", err)
				}
				done <- earlier
				return nil
			})
		}()
		err = do(func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Idempotency().Claim(ctx, second); err != nil {
				return err
			}
			close(claimed)
			time.Sleep(200 * time.Millisecond)
			return u.Idempotency().Complete(ctx, second.Client, second.Key, 200, []byte(`{}`))
		})
		if err != nil {
			t.Fatal(err)
		}
		if earlier := <-done; earlier == nil || earlier.Status != 200 {
			t.Fatalf("the waiting Claim = %+v; want the first request's response", earlier)
		}
		// The worker purges expired keys; afterwards the key can be claimed again.
		err = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if n, err := u.Idempotency().Purge(ctx, expires.Add(-time.Hour)); err != nil || n != 0 {
				t.Errorf("Purge before expiry = %d, %v; want 0", n, err)
			}
			n, err := u.Idempotency().Purge(ctx, expires)
			if err == nil && n != 2 {
				t.Errorf("Purge = %d; want both keys", n)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = do(func(ctx context.Context, u storage.UnitOfWork) error {
			if earlier, err := u.Idempotency().Claim(ctx, claim); err != nil || earlier != nil {
				t.Fatalf("Claim after the purge = %+v, %v; want a fresh claim", earlier, err)
			}
			return errors.New("roll back")
		})
	})

	// A key's times come from the caller's clock, which in local mode can be a business date far from
	// the database's (DEV-05): a claim made and expired in 2020 is still a valid claim.
	t.Run("an idempotency key takes both its times from the caller's clock", func(t *testing.T) {
		b := books(t)
		made := time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC)
		claim := storage.IdempotencyClaim{Client: "web user-1", Key: "past-1", RequestHash: make([]byte, 32),
			CreatedAt: made, ExpiresAt: made.Add(24 * time.Hour)}
		var problems []string
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			// Errors, not Fatal: the unit of work must end, or the suite's clean-up waits for it.
			if earlier, err := u.Idempotency().Claim(ctx, claim); err != nil || earlier != nil {
				problems = append(problems, fmt.Sprintf("Claim on a 2020 clock = %+v, %v; want a fresh claim", earlier, err))
				return err
			}
			if earlier, err := u.Idempotency().Claim(ctx, claim); err != nil || earlier == nil || !earlier.ExpiresAt.Equal(claim.ExpiresAt) {
				problems = append(problems, fmt.Sprintf("second Claim = %+v, %v; want the first claim, expiring %s", earlier, err, claim.ExpiresAt))
				return err
			}
			backwards := claim
			backwards.Key, backwards.ExpiresAt = "past-2", made
			if _, err := u.Idempotency().Claim(ctx, backwards); storage.CodeOf(err) != storage.CodeInvalid {
				problems = append(problems, fmt.Sprintf("a claim that expires when it is made: want %s, got %v", storage.CodeInvalid, err))
			}
			return errors.New("roll back")
		})
		for _, p := range problems {
			t.Error(p)
		}
	})
}
