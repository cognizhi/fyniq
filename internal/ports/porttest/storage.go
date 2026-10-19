package porttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

	// req: FR-INT-14
	t.Run("a preview runs every check the commit would, then saves nothing", func(t *testing.T) {
		b := books(t)
		head := func() int64 {
			var seq int64
			if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				h, err := u.Audit().Head(ctx)
				seq = h.Seq
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return seq
		}
		revision := func() int64 {
			var rev int64
			if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				rev, err = u.Ledger().Revision(ctx)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return rev
		}
		before := head()
		var previewed storage.Entry
		committed := false
		err := s.Do(storage.WithPreview(ctx), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			u.AfterCommit(func(context.Context) { committed = true })
			var err error
			previewed, err = u.Ledger().Append(ctx, Sale("previewed", Day(10, 14), b, "100.00"))
			return err
		})
		if !errors.Is(err, storage.ErrPreviewed) || previewed.LedgerRevision != 1 || len(previewed.Lines) != 2 {
			t.Fatalf("a valid preview: %v, %+v", err, previewed)
		}
		if committed || head() != before || revision() != 0 {
			t.Fatalf("the preview left something behind: after-commit ran %v, audit head %d (was %d), revision %d", committed, head(), before, revision())
		}

		// The deferred balance check (INV-01) runs before the rollback, so the preview refuses what the
		// commit would.
		e := Sale("unbalanced-preview", Day(10, 14), b, "100.00")
		e.Lines[1].Amount, e.Lines[1].BaseAmount = "-99.99", "-99.99"
		wantCode(t, s.Do(storage.WithPreview(ctx), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		}), storage.CodeUnbalanced)

		// A unit of work that joins an open one stays part of it: the outer one commits.
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return s.Do(storage.WithPreview(ctx), b.Org, func(ctx context.Context, inner storage.UnitOfWork) error {
				_, err := inner.Ledger().Append(ctx, Sale("previewed", Day(10, 14), b, "100.00"))
				return err
			})
		}); err != nil || revision() != 1 {
			t.Fatalf("a joined preview: %v, revision %d; want the outer unit of work to commit", err, revision())
		}
	})

	// req: data/data-model Idempotency keys and posting identity
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

	// req: data/data-model Tenancy and row-level security
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

	// req: data/data-model Balances are a rebuildable cache
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

	// req: FR-ORG-01
	// req: FR-ORG-02
	// req: data/data-model Versioning of records
	t.Run("an organisation's profile changes only at the version it was read, audited, within the database's checks", func(t *testing.T) {
		b := books(t)
		read := func() storage.Organisation {
			var o storage.Organisation
			if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				o, err = u.Organisation(ctx)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return o
		}
		update := func(version int, p storage.OrganisationProfile) (storage.Organisation, error) {
			var o storage.Organisation
			err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				o, err = u.UpdateOrganisation(ctx, version, p)
				return err
			})
			return o, err
		}
		org := read()
		if org.Profile.TimeZone != "Asia/Singapore" || org.Profile.FYStartMonth != 1 || org.Profile.LegalName != "" {
			t.Fatalf("a new organisation's profile: %+v", org.Profile)
		}
		start, end := Day(3, 15), time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
		p := org.Profile
		p.LegalName, p.UEN, p.BankAccountNumber, p.PayNowUEN = "Conformance Pte. Ltd.", "202312345K", "012-345678-9", "202312345K"
		p.FYStartMonth, p.BooksStart, p.FirstYearEnd = 4, &start, &end
		updated, err := update(org.Version, p)
		if err != nil || updated.Version != org.Version+1 || updated.Profile.UEN != "202312345K" || updated.Profile.FYStartMonth != 4 ||
			!updated.Profile.FirstYearEnd.Equal(end) || !read().Profile.BooksStart.Equal(start) {
			t.Fatalf("the update: %v, %+v", err, updated)
		}
		_ = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if h, _ := u.Audit().Head(ctx); h.Action != "organisation.updated" {
				t.Errorf("the last audit event is %q; want organisation.updated", h.Action)
			}
			return nil
		})

		// A change based on the version before is refused (FR-INT-09).
		_, err = update(org.Version, p)
		wantCode(t, err, storage.CodeVersion)

		// The database refuses what no profile may hold, whatever the domain checked.
		bad := p
		bad.UEN = "abc"
		_, err = update(updated.Version, bad)
		wantCode(t, err, storage.CodeOrganisation)
		bad = p
		notTheDayBefore := time.Date(2027, 3, 30, 0, 0, 0, 0, time.UTC)
		bad.FirstYearEnd = &notTheDayBefore
		_, err = update(updated.Version, bad)
		wantCode(t, err, storage.CodeOrganisation)
	})

	// req: FR-ORG-03
	// req: data/data-model Effective-dated reference data
	// req: data/data-model Valid time and system time for master data
	t.Run("GST registrations are effective-dated: overlaps are refused, every version recorded and audited", func(t *testing.T) {
		b := books(t)
		do := func(fn func(ctx context.Context, g storage.GSTRegistrations) error) error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(ctx, u.GSTRegistrations()) })
		}
		recorded := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
		deregistered := Day(12, 31)
		first := storage.NewGSTRegistration{RegisteredFrom: Day(4, 1), DeregisteredFrom: &deregistered, GSTNumber: "202312345K",
			ReturnFrequency: storage.ReturnQuarterly, QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice, RecordedAt: recorded}
		var reg storage.GSTRegistration
		if err := do(func(ctx context.Context, g storage.GSTRegistrations) error {
			var err error
			reg, err = g.Create(ctx, first)
			return err
		}); err != nil || reg.Version != 1 || !reg.RecordedAt.Equal(recorded) || !reg.DeregisteredFrom.Equal(deregistered) {
			t.Fatalf("Create: %v, %+v", err, reg)
		}

		// Valid time: a registration overlapping it is refused; one starting the day it ends is not.
		overlapping := first
		overlapping.RegisteredFrom, overlapping.DeregisteredFrom = Day(12, 1), nil
		wantCode(t, do(func(ctx context.Context, g storage.GSTRegistrations) error {
			_, err := g.Create(ctx, overlapping)
			return err
		}), storage.CodeOverlap)
		next := first
		next.RegisteredFrom, next.DeregisteredFrom = deregistered, nil
		if err := do(func(ctx context.Context, g storage.GSTRegistrations) error { _, err := g.Create(ctx, next); return err }); err != nil {
			t.Fatalf("a registration starting the day the last one ends: %v", err)
		}

		// System time: a change is the next version, recorded when it was made, at the version read.
		later := recorded.Add(24 * time.Hour)
		changed := first
		changed.ReturnFrequency, changed.RecordedAt = storage.ReturnMonthly, later
		var updated storage.GSTRegistration
		if err := do(func(ctx context.Context, g storage.GSTRegistrations) error {
			var err error
			updated, err = g.Update(ctx, reg.ID, reg.Version, changed)
			return err
		}); err != nil || updated.Version != 2 || updated.ReturnFrequency != storage.ReturnMonthly || !updated.RecordedAt.Equal(later) {
			t.Fatalf("Update: %v, %+v", err, updated)
		}
		wantCode(t, do(func(ctx context.Context, g storage.GSTRegistrations) error {
			_, err := g.Update(ctx, reg.ID, reg.Version, changed)
			return err
		}),
			storage.CodeVersion)
		_ = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if h, _ := u.Audit().Head(ctx); h.Action != "gst_registration.updated" {
				t.Errorf("the last audit event is %q; want gst_registration.updated", h.Action)
			}
			list, err := u.GSTRegistrations().List(ctx)
			if err != nil || len(list) != 2 || !list[0].RegisteredFrom.Equal(Day(4, 1)) {
				t.Errorf("List: %v, %+v", err, list)
			}
			return err
		})

		// The database refuses what no registration may hold: a deregistration on or before its start,
		// and any basis but invoice basis (ENG-45).
		backwards := changed
		before := Day(4, 1)
		backwards.DeregisteredFrom = &before
		wantCode(t, do(func(ctx context.Context, g storage.GSTRegistrations) error {
			_, err := g.Update(ctx, reg.ID, 2, backwards)
			return err
		}), storage.CodeOverlap)
		cash := changed
		cash.AccountingBasis = "cash"
		wantCode(t, do(func(ctx context.Context, g storage.GSTRegistrations) error {
			_, err := g.Update(ctx, reg.ID, 2, cash)
			return err
		}), storage.CodeOrganisation)
	})

	// req: FR-ORG-04
	t.Run("a sequence numbers each document once, in order, never backwards; a rollback takes no number", func(t *testing.T) {
		b := books(t)
		draft := func(ctx context.Context, u storage.UnitOfWork) (storage.Document, error) {
			return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"memo":"numbered"}`)})
		}
		number := func(fail bool) (string, error) {
			var n string
			failed := errors.New("the posting fails after it is numbered")
			err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				doc, err := draft(ctx, u)
				if err != nil {
					return err
				}
				if n, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentJournal); err != nil {
					return err
				}
				if again, err := u.Sequences().Number(ctx, doc.ID, storage.DocumentJournal); err != nil || again != n {
					t.Errorf("numbering the same document again: %q, %v; want %q", again, err, n)
				}
				if fail {
					return failed
				}
				return nil
			})
			if fail && errors.Is(err, failed) {
				err = nil
			}
			return n, err
		}
		var seq storage.Sequence
		_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			seq, _ = u.Sequences().Get(ctx, storage.DocumentJournal)
			return nil
		})
		if seq.Version != 0 || seq.Prefix != "JNL-" || seq.NextNumber != 1 || !seq.GapFree {
			t.Fatalf("a sequence not set up yet: %+v", seq)
		}
		for i, want := range []string{"JNL-0001", "JNL-0002"} {
			if n, err := number(false); err != nil || n != want {
				t.Fatalf("number %d: %q, %v; want %s", i+1, n, err, want)
			}
		}
		if _, err := number(true); err != nil {
			t.Fatal(err)
		}
		if n, err := number(false); err != nil || n != "JNL-0003" {
			t.Fatalf("after a rolled-back numbering: %q, %v; want JNL-0003", n, err)
		}

		update := func(version int, set storage.SequenceSettings) (storage.Sequence, error) {
			var out storage.Sequence
			err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				out, err = u.Sequences().Update(ctx, storage.DocumentJournal, version, set)
				return err
			})
			return out, err
		}
		set := storage.SequenceSettings{Prefix: "JV/", NextNumber: 100, Digits: 5, GapFree: true}
		changed, err := update(1, set)
		if err != nil || changed.Version != 2 || changed.NextNumber != 100 {
			t.Fatalf("Update: %v, %+v", err, changed)
		}
		if n, err := number(false); err != nil || n != "JV/00100" {
			t.Fatalf("after the change: %q, %v; want JV/00100", n, err)
		}
		_, err = update(1, set)
		wantCode(t, err, storage.CodeVersion)
		back := set
		back.NextNumber = 5
		_, err = update(2, back)
		wantCode(t, err, storage.CodeSequence)
		_ = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if h, _ := u.Audit().Head(ctx); h.Action != "document.numbered" {
				t.Errorf("the last audit event is %q; want document.numbered", h.Action)
			}
			return nil
		})
	})

	// req: FR-COA-03
	// req: FR-COA-04
	// req: FR-COA-05
	t.Run("an account changes at its version; a system account keeps its role and stays active", func(t *testing.T) {
		b := books(t)
		do := func(fn func(ctx context.Context, a storage.Accounts) error) error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(ctx, u.Accounts()) })
		}
		change := func(a storage.Account) storage.AccountChange {
			return storage.AccountChange{Name: a.Name, ParentID: a.ParentID, Group: a.Group, Active: a.Active, AccountAttributes: a.AccountAttributes}
		}
		c := change(b.Rent)
		c.Name, c.ReportGroup, c.Guidance = "Rent & co-working", "Premises & office", "Office rent and co-working."
		var renamed storage.Account
		if err := do(func(ctx context.Context, a storage.Accounts) error {
			var err error
			renamed, err = a.Update(ctx, b.Rent.ID, b.Rent.Version, c)
			return err
		}); err != nil || renamed.Version != b.Rent.Version+1 || renamed.Name != "Rent & co-working" || renamed.Code != b.Rent.Code {
			t.Fatalf("Update: %v, %+v", err, renamed)
		}
		wantCode(t, do(func(ctx context.Context, a storage.Accounts) error {
			_, err := a.Update(ctx, b.Rent.ID, b.Rent.Version, c)
			return err
		}),
			storage.CodeVersion)

		// The hierarchy: a group cannot sit under its own child.
		g := change(b.Expenses)
		g.ParentID = &b.Rent.ID
		wantCode(t, do(func(ctx context.Context, a storage.Accounts) error {
			_, err := a.Update(ctx, b.Expenses.ID, b.Expenses.Version, g)
			return err
		}),
			storage.CodeAccountTree)

		// System accounts: one per role, and they stay active.
		var suspense storage.Account
		if err := do(func(ctx context.Context, a storage.Accounts) error {
			var err error
			suspense, err = a.Create(ctx, storage.NewAccount{Code: "2999", Name: "Suspense", Type: storage.Liability, Role: "suspense"})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		wantCode(t, do(func(ctx context.Context, a storage.Accounts) error {
			_, err := a.Create(ctx, storage.NewAccount{Code: "2998", Name: "Suspense too", Type: storage.Liability, Role: "suspense"})
			return err
		}), storage.CodeAccountFixed)
		off := change(suspense)
		off.Active = false
		wantCode(t, do(func(ctx context.Context, a storage.Accounts) error {
			_, err := a.Update(ctx, suspense.ID, suspense.Version, off)
			return err
		}),
			storage.CodeAccountFixed)
		_ = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if h, _ := u.Audit().Head(ctx); h.Action != "account.created" {
				t.Errorf("the last audit event is %q; refused changes must leave none", h.Action)
			}
			return nil
		})
	})

	// req: FR-CT-01
	// req: FR-CT-04
	t.Run("contacts change at their version, page by role and find their possible duplicates", func(t *testing.T) {
		b := books(t)
		do := func(fn func(ctx context.Context, c storage.Contacts) error) error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(ctx, u.Contacts()) })
		}
		thirty := 30
		northwind := storage.ContactDetails{LegalName: "Northwind Analytics Pte. Ltd.", Customer: true, UENOrTaxID: "202312345K",
			BillingEmail: "accounts@northwind.example", Country: "SG", Currency: "SGD", PaymentTermsDays: &thirty, DefaultAccountID: &b.Sales.ID}
		var created storage.Contact
		if err := do(func(ctx context.Context, c storage.Contacts) error {
			var err error
			created, err = c.Create(ctx, northwind)
			return err
		}); err != nil || created.Version != 1 || !created.Active || *created.PaymentTermsDays != 30 || *created.DefaultAccountID != b.Sales.ID {
			t.Fatalf("Create: %v, %+v", err, created)
		}
		vendor := storage.ContactDetails{LegalName: "Acme Cloud Pte Ltd", Vendor: true, RemittanceEmail: "ACCOUNTS@northwind.example"}
		var acme storage.Contact
		if err := do(func(ctx context.Context, c storage.Contacts) error {
			var err error
			acme, err = c.Create(ctx, vendor)
			return err
		}); err != nil {
			t.Fatal(err)
		}

		// Possible duplicates: the name with other case and spacing, the UEN, an email in common.
		var dups []storage.ContactDuplicate
		if err := do(func(ctx context.Context, c storage.Contacts) error {
			var err error
			dups, err = c.Duplicates(ctx, uuid.Nil, storage.ContactDetails{LegalName: "northwind  analytics pte. ltd.", UENOrTaxID: "202312345k"})
			return err
		}); err != nil || len(dups) != 1 || dups[0].ID != created.ID || !dups[0].SameName || !dups[0].SameUEN || dups[0].SameEmail {
			t.Fatalf("duplicates of the name and UEN: %v, %+v", err, dups)
		}
		if err := do(func(ctx context.Context, c storage.Contacts) error {
			var err error
			dups, err = c.Duplicates(ctx, acme.ID, vendor)
			return err
		}); err != nil || len(dups) != 1 || dups[0].ID != created.ID || !dups[0].SameEmail {
			t.Fatalf("duplicates by email, leaving itself out: %v, %+v", err, dups)
		}

		// Pages, by role.
		for role, want := range map[string]int{"": 2, "customer": 1, "vendor": 1} {
			var page []storage.Contact
			if err := do(func(ctx context.Context, c storage.Contacts) error {
				var err error
				page, err = c.Page(ctx, uuid.Nil, role, 10)
				return err
			}); err != nil || len(page) != want {
				t.Errorf("Page(%q): %d, %v; want %d", role, len(page), err, want)
			}
		}

		// A change at its version; a stale version, and a contact that is neither, refused.
		changed := northwind
		changed.Vendor = true
		var updated storage.Contact
		if err := do(func(ctx context.Context, c storage.Contacts) error {
			var err error
			updated, err = c.Update(ctx, created.ID, 1, changed, false)
			return err
		}); err != nil || updated.Version != 2 || !updated.Vendor || updated.Active {
			t.Fatalf("Update: %v, %+v", err, updated)
		}
		wantCode(t, do(func(ctx context.Context, c storage.Contacts) error {
			_, err := c.Update(ctx, created.ID, 1, changed, true)
			return err
		}),
			storage.CodeVersion)
		neither := changed
		neither.Customer, neither.Vendor = false, false
		wantCode(t, do(func(ctx context.Context, c storage.Contacts) error {
			_, err := c.Update(ctx, created.ID, 2, neither, true)
			return err
		}),
			storage.CodeInvalid)
	})

	// req: FR-ITM-01
	t.Run("items keep a price per currency, change at their version with their prices audited, and page by code", func(t *testing.T) {
		b := books(t)
		do := func(fn func(ctx context.Context, it storage.Items) error) error {
			return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(ctx, u.Items()) })
		}
		create := func(code string, d storage.ItemDetails) (storage.Item, error) {
			var out storage.Item
			err := do(func(ctx context.Context, it storage.Items) error {
				var err error
				out, err = it.Create(ctx, code, d)
				return err
			})
			return out, err
		}
		consult := storage.ItemDetails{Name: "HR consulting", Description: "HR consulting, by the hour", Type: storage.ItemService, Unit: "hour",
			RevenueAccountID: &b.Sales.ID, GSTCode: "SR", Prices: []storage.ItemPrice{{Currency: "USD", UnitPrice: "135.5"}, {Currency: "SGD", UnitPrice: "180.123456"}}}
		created, err := create("CONSULT-HR", consult)
		wantPrices := []storage.ItemPrice{{Currency: "SGD", UnitPrice: "180.123456"}, {Currency: "USD", UnitPrice: "135.500000"}}
		if err != nil || created.Version != 1 || !created.Active || created.Code != "CONSULT-HR" || *created.RevenueAccountID != b.Sales.ID ||
			!slices.Equal(created.Prices, wantPrices) {
			t.Fatalf("Create: %v, %+v", err, created)
		}
		_, err = create("CONSULT-HR", storage.ItemDetails{Name: "Again", Type: storage.ItemService})
		wantCode(t, err, storage.CodeDuplicate)
		if _, err := create("BOOK-01", storage.ItemDetails{Name: "The handbook", Type: storage.ItemProduct}); err != nil {
			t.Fatal(err)
		}

		// Pages by code.
		var page []storage.Item
		if err := do(func(ctx context.Context, it storage.Items) error {
			var err error
			page, err = it.Page(ctx, "", 1)
			return err
		}); err != nil || len(page) != 1 || page[0].Code != "BOOK-01" || len(page[0].Prices) != 0 {
			t.Fatalf("the first page: %v, %+v", err, page)
		}
		if err := do(func(ctx context.Context, it storage.Items) error {
			var err error
			page, err = it.Page(ctx, "BOOK-01", 10)
			return err
		}); err != nil || len(page) != 1 || page[0].ID != created.ID || !slices.Equal(page[0].Prices, wantPrices) {
			t.Fatalf("the next page: %v, %+v", err, page)
		}

		// A change at its version replaces the prices, and the audit event (read as the worker) has them before and after.
		changed := consult
		changed.Prices = []storage.ItemPrice{{Currency: "SGD", UnitPrice: "200"}}
		var updated storage.Item
		if err := do(func(ctx context.Context, it storage.Items) error {
			var err error
			updated, err = it.Update(ctx, created.ID, 1, changed, false)
			return err
		}); err != nil || updated.Version != 2 || updated.Active || !slices.Equal(updated.Prices, []storage.ItemPrice{{Currency: "SGD", UnitPrice: "200.000000"}}) {
			t.Fatalf("Update: %v, %+v", err, updated)
		}
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			head, err := u.Audit().Head(ctx)
			if err != nil {
				return err
			}
			evs, err := u.Audit().Events(ctx, head.Seq-1, 1)
			if err == nil && (head.Action != "item.updated" || len(evs) != 1 ||
				!strings.Contains(evs[0].Body, "SGD 180.123456, USD 135.500000") || !strings.Contains(evs[0].Body, "SGD 200.000000")) {
				t.Errorf("the change's audit event: %q %+v", head.Action, evs)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}

		// A stale version, a price that is not a number, one below zero and two in one currency are refused.
		update := func(version int, d storage.ItemDetails) error {
			return do(func(ctx context.Context, it storage.Items) error {
				_, err := it.Update(ctx, created.ID, version, d, true)
				return err
			})
		}
		wantCode(t, update(1, changed), storage.CodeVersion)
		for name, ps := range map[string][]storage.ItemPrice{
			"not a number":        {{Currency: "SGD", UnitPrice: "NaN"}},
			"below zero":          {{Currency: "SGD", UnitPrice: "-1"}},
			"two in one currency": {{Currency: "SGD", UnitPrice: "1"}, {Currency: "SGD", UnitPrice: "2"}},
			"not a currency code": {{Currency: "sgd", UnitPrice: "1"}},
		} {
			bad := changed
			bad.Prices = ps
			if err := update(2, bad); storage.CodeOf(err) != storage.CodeInvalid {
				t.Errorf("a price %s: %v", name, err)
			}
		}
	})

	// req: FR-ORG-07
	// req: FR-ORG-08
	t.Run("setting changes are recorded at the key's next version, the region never, and the records' retention only lengthens", func(t *testing.T) {
		b := books(t)
		var recordWas func(key, was, value string, from time.Time, version int) (storage.SettingChange, error)
		record := func(key, value string, from time.Time, version int) (storage.SettingChange, error) {
			return recordWas(key, "", value, from, version)
		}
		recordWas = func(key, was, value string, from time.Time, version int) (storage.SettingChange, error) {
			var c storage.SettingChange
			err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				c, err = u.Settings().Record(ctx, key, was, value, from, version)
				return err
			})
			return c, err
		}
		if c, err := record("sales.payment_terms_days", "45", Day(time.November, 1), 1); err != nil || c.Version != 1 || c.Value != "45" ||
			!c.EffectiveFrom.Equal(Day(time.November, 1)) {
			t.Fatalf("a change: %v, %+v", err, c)
		}
		_, err := record("sales.payment_terms_days", "60", Day(time.November, 1), 1)
		wantCode(t, err, storage.CodeVersion) // another change took version 1 first
		if _, err := recordWas("sales.payment_terms_days", "45", "60", Day(time.November, 1), 2); err != nil {
			t.Fatal(err)
		}
		// Its audit event (read as the worker) has the value before and after.
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			head, err := u.Audit().Head(ctx)
			if err != nil {
				return err
			}
			evs, err := u.Audit().Events(ctx, head.Seq-1, 1)
			if err == nil && (head.Action != "organisation_setting.changed" || len(evs) != 1 ||
				!strings.Contains(evs[0].Body, `"value":["45","60"]`)) {
				t.Errorf("the change's audit event: %q %+v", head.Action, evs)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		_, err = record("data.region", "europe-west1", Day(time.November, 1), 1)
		wantCode(t, err, storage.CodeInvalid)
		var changes []storage.SettingChange
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			changes, err = u.Settings().Changes(ctx, "sales.payment_terms_days")
			return err
		}); err != nil || len(changes) != 2 || changes[0].Version != 1 || changes[1].Value != "60" {
			t.Fatalf("Changes: %v, %+v", err, changes)
		}

		// The records' retention: never below 7 years, never below the value in effect, and never so that a
		// change already recorded for later would shorten it.
		key := "retention.records_years"
		_, err = record(key, "6", Day(time.November, 1), 1)
		wantCode(t, err, storage.CodeInvalid)
		if _, err := record(key, "10", Day(time.December, 1), 1); err != nil {
			t.Fatalf("lengthening: %v", err)
		}
		_, err = record(key, "9", Day(time.December, 15), 2)
		wantCode(t, err, storage.CodeInvalid)
		_, err = record(key, "12", Day(time.November, 1), 2)
		wantCode(t, err, storage.CodeInvalid)
		if _, err := record(key, "8", Day(time.November, 1), 2); err != nil {
			t.Errorf("lengthening before a longer change: %v", err)
		}
	})

	// req: FR-INT-16
	t.Run("a rate-limit window counts a client's requests by kind until the worker purges it", func(t *testing.T) {
		b, other := books(t), books(t)
		minute := Day(10, 15).Add(9 * time.Hour)
		count := func(org uuid.UUID, client, kind string, start time.Time) int {
			t.Helper()
			var n int
			if err := s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				n, err = u.RateLimits().Count(ctx, client, kind, start)
				return err
			}); err != nil {
				t.Fatalf("Count(%s, %s, %s): %v", client, kind, start, err)
			}
			return n
		}
		for want := 1; want <= 3; want++ {
			if got := count(b.Org, "web user-a", storage.RateRead, minute); got != want {
				t.Fatalf("read %d in the window counted %d", want, got)
			}
		}
		for _, c := range []struct {
			org          uuid.UUID
			client, kind string
			start        time.Time
		}{
			{b.Org, "web user-a", storage.RateWrite, minute},                 // another kind
			{b.Org, "web user-b", storage.RateRead, minute},                  // another client
			{b.Org, "web user-a", storage.RateRead, minute.Add(time.Minute)}, // the next window
			{other.Org, "web user-a", storage.RateRead, minute},              // another organisation
		} {
			if got := count(c.org, c.client, c.kind, c.start); got != 1 {
				t.Errorf("%s %s at %s counted %d; want a window of its own", c.client, c.kind, c.start, got)
			}
		}
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.RateLimits().Count(ctx, "web user-a", "delete", minute)
			return err
		}); err == nil {
			t.Error("a kind other than read or write was counted")
		}
		purge := func(store storage.Store) (int64, error) {
			var n int64
			err := store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				n, err = u.RateLimits().Purge(ctx, minute.Add(time.Minute))
				return err
			})
			return n, err
		}
		if _, err := purge(s); err == nil {
			t.Error("the API's role purged rate-limit windows; only the worker may")
		}
		if n, err := purge(worker); err != nil || n != 3 {
			t.Fatalf("the worker's purge: %d, %v; want the 3 windows of that minute", n, err)
		}
		if got := count(b.Org, "web user-a", storage.RateRead, minute); got != 1 {
			t.Errorf("after the purge the window counted %d; want a new window", got)
		}
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
