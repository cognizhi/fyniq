//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Race tests run two operations on independent connections with a barrier between their steps
// (QA-ENG-05). Whatever the timing, the result must be one of the serial orders.

// waiting is how long a blocked operation must stay blocked to count as waiting for the lock.
const waiting = 300 * time.Millisecond

// held runs fn in a unit of work and keeps its transaction open until release is closed.
func held(s storage.Store, org uuid.UUID, fn func(context.Context, storage.UnitOfWork) error) (started chan error, release chan struct{}, done chan error) {
	started, release, done = make(chan error, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
			err := fn(ctx, u)
			started <- err
			<-release
			return err
		})
	}()
	return started, release, done
}

// async runs fn in its own unit of work and reports when it finishes.
func async(s storage.Store, org uuid.UUID, fn func(context.Context, storage.UnitOfWork) error) chan error {
	done := make(chan error, 1)
	go func() { done <- s.Do(context.Background(), org, fn) }()
	return done
}

func stillBlocked(t *testing.T, what string, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s finished (%v) while the other transaction held the ledger lock; want it to wait (ENG-47)", what, err)
	case <-time.After(waiting):
	}
}

func appendSale(identity string, b porttest.Books, date time.Time) func(context.Context, storage.UnitOfWork) error {
	return func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, porttest.Sale(identity, date, b, "10.00"))
		return err
	}
}

func closePeriod(id uuid.UUID) func(context.Context, storage.UnitOfWork) error {
	return func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, id)
		return err
	}
}

func reopenPeriod(id uuid.UUID) func(context.Context, storage.UnitOfWork) error {
	return func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Reopen(ctx, id)
		return err
	}
}

// asAt is a report's figure "as at revision rev": the base total per account of every entry with
// revision ≤ rev (ENG-46), read in one statement.
func asAt(t *testing.T, org uuid.UUID, rev int64) map[uuid.UUID]string {
	t.Helper()
	ctx := context.Background()
	out := map[uuid.UUID]string{}
	err := asOrg(ctx, conn(t, "fyniq_readonly"), org, func(tx pgx.Tx) error {
		r, err := tx.Query(ctx, `
SELECT l.account_id, sum(l.base_amount)::text FROM fyniq.journal_line l
  JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
 WHERE e.ledger_revision <= $1 GROUP BY l.account_id`, rev)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var id uuid.UUID
			var sum string
			if err := r.Scan(&id, &sum); err != nil {
				return err
			}
			out[id] = sum
		}
		return r.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func period(t *testing.T, s storage.Store, org, id uuid.UUID) storage.Period {
	t.Helper()
	var p storage.Period
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		p, err = u.Periods().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

// req: ENG-47
// req: INV-03
// req: QA-ENG-05
// asvs: V15.4.2
func TestRacePostingAgainstCloseOfTheSamePeriod(t *testing.T) {
	s := store(t, "fyniq_api")

	t.Run("the posting holds the lock first: both commit, and the close includes it", func(t *testing.T) {
		b := books(t, "Race post-then-close Pte Ltd")
		started, release, posted := held(s, b.Org, appendSale("first", b, porttest.Day(10, 31)))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		closed := async(s, b.Org, closePeriod(b.Oct.ID))
		stillBlocked(t, "the close", closed)
		close(release)
		if err := <-posted; err != nil {
			t.Fatalf("posting: %v", err)
		}
		if err := <-closed; err != nil {
			t.Fatalf("close: %v", err)
		}
		if p := period(t, s, b.Org, b.Oct.ID); p.ClosedRevision == nil || *p.ClosedRevision != 1 {
			t.Fatalf("closed at revision %v; want 1, the posting's", p.ClosedRevision)
		}
	})

	t.Run("the close holds the lock first: the posting waits, then is refused", func(t *testing.T) {
		b := books(t, "Race close-then-post Pte Ltd")
		if err := s.Do(context.Background(), b.Org, appendSale("before", b, porttest.Day(10, 3))); err != nil {
			t.Fatal(err)
		}
		report := asAt(t, b.Org, 1)
		started, release, closed := held(s, b.Org, closePeriod(b.Oct.ID))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		posted := async(s, b.Org, appendSale("late", b, porttest.Day(10, 31)))
		stillBlocked(t, "the posting", posted)
		close(release)
		if err := <-closed; err != nil {
			t.Fatalf("close: %v", err)
		}
		if err := <-posted; storage.CodeOf(err) != storage.CodePeriodClosed {
			t.Fatalf("posting: want %s, got %v", storage.CodePeriodClosed, err)
		}
		if again := asAt(t, b.Org, 1); fmt.Sprint(again) != fmt.Sprint(report) {
			t.Fatalf("the report as at revision 1 changed from %v to %v", report, again)
		}
	})
}

// req: ENG-47
// req: QA-ENG-05
// asvs: V15.4.2
func TestRaceReopenAgainstPosting(t *testing.T) {
	s := store(t, "fyniq_api")
	b := books(t, "Race reopen Pte Ltd")
	if err := s.Do(context.Background(), b.Org, closePeriod(b.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	started, release, reopened := held(s, b.Org, reopenPeriod(b.Oct.ID))
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	posted := async(s, b.Org, appendSale("after reopen", b, porttest.Day(10, 20)))
	stillBlocked(t, "the posting", posted)
	close(release)
	if err := <-reopened; err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := <-posted; err != nil {
		t.Fatalf("posting after the reopen committed: %v; want it to see the open period", err)
	}
	// The other order: a posting into the closed period is refused at once, and the reopen after it
	// commits.
	if err := s.Do(context.Background(), b.Org, closePeriod(b.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(context.Background(), b.Org, appendSale("before reopen", b, porttest.Day(10, 21))); storage.CodeOf(err) != storage.CodePeriodClosed {
		t.Fatalf("posting into the closed period: want %s, got %v", storage.CodePeriodClosed, err)
	}
	if err := s.Do(context.Background(), b.Org, reopenPeriod(b.Oct.ID)); err != nil {
		t.Fatal(err)
	}
}

// req: ENG-46
// req: QA-ENG-05
// asvs: V15.4.1
// req: data/data-model Reports as at a ledger revision
func TestRaceRevisionsAreGapFreeAndAsAtIsStable(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "Race revisions Pte Ltd")

	// A report reads revision N while a back-dated posting waits to commit: as at N stays the same.
	if err := s.Do(ctx, b.Org, appendSale("opening", b, porttest.Day(10, 20))); err != nil {
		t.Fatal(err)
	}
	started, release, backDated := held(s, b.Org, appendSale("back-dated", b, porttest.Day(10, 1)))
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	report := asAt(t, b.Org, 1)
	close(release)
	if err := <-backDated; err != nil {
		t.Fatal(err)
	}
	if again := asAt(t, b.Org, 1); fmt.Sprint(again) != fmt.Sprint(report) {
		t.Fatalf("as at revision 1 changed after a back-dated posting: %v → %v", report, again)
	}

	// Concurrent postings, some of which abort after taking a revision: no gaps.
	var wg sync.WaitGroup
	var mu sync.Mutex
	committed := 2
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				e := porttest.Sale(fmt.Sprintf("concurrent-%d", i), porttest.Day(10, 1+i%28), b, "1.00")
				switch i % 3 {
				case 1: // unbalanced: refused at commit, after the revision was taken
					e.Lines[1].Amount, e.Lines[1].BaseAmount = "-0.99", "-0.99"
				case 2: // the command fails after posting
					if _, err := u.Ledger().Append(ctx, e); err != nil {
						return err
					}
					return fmt.Errorf("later step failed")
				}
				_, err := u.Ledger().Append(ctx, e)
				return err
			})
			if err == nil {
				mu.Lock()
				committed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var revisions []int64
	err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
		r, err := tx.Query(ctx, "SELECT ledger_revision FROM fyniq.journal_entry ORDER BY 1")
		if err != nil {
			return err
		}
		revisions, err = pgx.CollectRows(r, pgx.RowTo[int64])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != committed {
		t.Fatalf("%d entries for %d committed postings", len(revisions), committed)
	}
	for i, rev := range revisions {
		if rev != int64(i+1) {
			t.Fatalf("revisions %v are not 1..%d without gaps (ENG-46)", revisions, committed)
		}
	}
}

// roundHalfAway rounds n / d half away from zero: RND-FX on integers.
func roundHalfAway(n, d int64) int64 {
	q, r := n/d, n%d
	if 2*abs(r) >= d {
		if n < 0 {
			return q - 1
		}
		return q + 1
	}
	return q
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func asDecimal(cents int64) storage.Decimal {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return storage.Decimal(fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100))
}

// req: INV-08
// req: INV-09
// req: data/data-model Balances are a rebuildable cache
func TestRandomPostingsKeepTheCacheAndTrialBalanceExact(t *testing.T) {
	ctx := context.Background()
	s, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := books(t, "Random postings Pte Ltd")
	accounts := []uuid.UUID{b.Bank.ID, b.Sales.ID, b.Rent.ID}
	rates := []int64{13500, 13472, 9100, 7, 1} // USD-ish rates ×10⁴; the expected values use integers only
	rng := rand.New(rand.NewPCG(20261014, 1))
	for i := range 150 {
		date := porttest.Day(time.Month(10+rng.IntN(2)), 1+rng.IntN(28))
		e := storage.NewEntry{PostingDate: date, PostingIdentity: fmt.Sprintf("random-%d", i), RuleVersion: "test@1",
			Source: storage.Source{Type: "manual_journal", ID: fmt.Sprint(i), Version: 1}}
		var baseSum int64
		for range 1 + rng.IntN(5) {
			cents := int64(rng.IntN(2_000_000)) - 1_000_000
			if rng.IntN(2) == 0 { // a USD line, converted under RND-FX
				rate := rates[rng.IntN(len(rates))]
				base := roundHalfAway(cents*rate, 10_000)
				baseSum += base
				e.Lines = append(e.Lines, storage.NewLine{AccountID: accounts[rng.IntN(3)], Currency: "USD", Amount: asDecimal(cents),
					FXRate: storage.Decimal(fmt.Sprintf("%d.%04d", rate/10_000, rate%10_000)), FXRateSource: "test", BaseAmount: asDecimal(base)})
			} else {
				baseSum += cents
				e.Lines = append(e.Lines, storage.NewLine{AccountID: accounts[rng.IntN(3)], Currency: "SGD", Amount: asDecimal(cents),
					FXRate: "1", FXRateSource: "base", BaseAmount: asDecimal(cents)})
			}
		}
		e.Lines = append(e.Lines, storage.NewLine{AccountID: accounts[rng.IntN(3)], Currency: "SGD", Amount: asDecimal(-baseSum),
			FXRate: "1", FXRateSource: "base", BaseAmount: asDecimal(-baseSum)})
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		}); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
	verify := func(when string) []storage.BalanceDifference {
		var diff []storage.BalanceDifference
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			diff, err = u.Balances().Differences(ctx)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		return diff
	}
	trialBalance := func(when string) {
		err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
			r, err := tx.Query(ctx, "SELECT period_id, sum(base_amount)::text FROM fyniq.balance GROUP BY period_id")
			if err != nil {
				return err
			}
			defer r.Close()
			n := 0
			for r.Next() {
				var p uuid.UUID
				var sum string
				if err := r.Scan(&p, &sum); err != nil {
					return err
				}
				if n++; sum != "0.0000" {
					t.Errorf("%s: the trial balance of period %s nets to %s; want 0 (INV-09)", when, p, sum)
				}
			}
			if n != 2 {
				t.Errorf("%s: %d periods in the trial balance; want October and November", when, n)
			}
			return r.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if d := verify("after random postings"); len(d) != 0 {
		t.Fatalf("the cache differs from the lines: %+v (INV-08)", d)
	}
	trialBalance("after random postings")

	// Drift in the cache is found, and a rebuild repairs it.
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.balance SET base_amount = base_amount + 0.01 WHERE account_id = $1", b.Rent.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if d := verify("after corrupting the cache"); len(d) == 0 {
		t.Fatal("a corrupted cache went unnoticed (INV-08)")
	}
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Balances().Rebuild(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if d := verify("after the rebuild"); len(d) != 0 {
		t.Fatalf("the rebuilt cache differs from the lines: %+v", d)
	}
	trialBalance("after the rebuild")
}

// G4-053: a back-dated posting and the close of its period on two connections. Either the posting commits first and
// the close fails "checks are stale", or the close commits first and the posting is refused; never an entry in a
// closed period, and the close's snapshot is of its own revision.
// req: FR-GL-07
// req: ENG-47
// req: QA-ENG-05
// asvs: V15.4.2
func TestRaceBackDatedPostingAgainstTheAttestedClose(t *testing.T) {
	s := store(t, "fyniq_api")
	svc := closing.Service{Store: s}
	as := func(ctx context.Context, who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	checked := func(b porttest.Books) storage.CloseCheck {
		t.Helper()
		checklistDone(t, s, b.Org, b.Oct.ID)
		c, err := svc.RunChecks(as(context.Background(), "preparer"), b.Org, b.Oct.ID)
		if err != nil || !c.Clean {
			t.Fatalf("the checks: %v %+v", err, c)
		}
		return c
	}

	t.Run("the posting holds the lock first: it commits, and the close finds the checks stale", func(t *testing.T) {
		b := books(t, "Race G4-053 post-first Pte Ltd")
		c := checked(b)
		started, release, posted := held(s, b.Org, appendSale("back-dated", b, porttest.Day(10, 15)))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		closed := make(chan error, 1)
		go func() {
			_, _, err := svc.Close(as(context.Background(), "attester"), b.Org, b.Oct.ID, c.ID, "Reviewed.")
			closed <- err
		}()
		stillBlocked(t, "the close", closed)
		close(release)
		if err := <-posted; err != nil {
			t.Fatalf("posting: %v", err)
		}
		var ce *closing.Error
		if err := <-closed; !errors.As(err, &ce) || ce.Code != closing.CodeStale {
			t.Fatalf("close: want %s, got %v", closing.CodeStale, err)
		}
		if p := period(t, s, b.Org, b.Oct.ID); p.Status != storage.PeriodOpen {
			t.Fatalf("October is %s after a stale close", p.Status)
		}
	})

	t.Run("the close holds the lock first: it commits, and the posting is refused", func(t *testing.T) {
		b := books(t, "Race G4-053 close-first Pte Ltd")
		c := checked(b)
		var rec storage.PeriodClose
		started, release, closed := held(s, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			rec, _, err = svc.Close(as(ctx, "attester"), b.Org, b.Oct.ID, c.ID, "Reviewed.")
			return err
		})
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		posted := async(s, b.Org, appendSale("too late", b, porttest.Day(10, 15)))
		stillBlocked(t, "the posting", posted)
		close(release)
		if err := <-closed; err != nil {
			t.Fatalf("close: %v", err)
		}
		if err := <-posted; storage.CodeOf(err) != storage.CodePeriodClosed {
			t.Fatalf("posting: want %s, got %v", storage.CodePeriodClosed, err)
		}
		if p := period(t, s, b.Org, b.Oct.ID); p.ClosedRevision == nil || *p.ClosedRevision != rec.ClosedRevision {
			t.Fatalf("the period closed at %v, the close records %d", p.ClosedRevision, rec.ClosedRevision)
		}
	})
}
