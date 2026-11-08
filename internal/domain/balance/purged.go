package balance

import (
	"context"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The purge after retention (AUD-10): once a year's lines are carried forward and deleted, a reading dated before the
// latest boundary, or as at a revision before its purge, has nothing left to read. Every reading from the boundary on is
// what it was: the carry-forward is dated the day before it, so each report reads it as its opening balance.

// CodePurged refuses a reading the ledger's purge after retention removed the history of.
const CodePurged = "BALANCE-PURGED"

// Purged is the refusal of a reading before the latest ledger boundary, or as at a revision before its purge.
type Purged struct {
	Boundary time.Time // the first day the ledger still holds whole
	Day      time.Time // the day asked for, zero when a revision was
	Revision int64     // the revision asked for, zero when a day was
	Carried  int64     // the ledger revision after the purge
}

func (e *Purged) Error() string {
	if e.Revision > 0 {
		return fmt.Sprintf("%s: revision %d is before the ledger's purge after retention at revision %d; the books before %s were purged",
			CodePurged, e.Revision, e.Carried, e.Boundary.Format(time.DateOnly))
	}
	return fmt.Sprintf("%s: %s is before %s, where the ledger was purged after retention", CodePurged, e.Day.Format(time.DateOnly),
		e.Boundary.Format(time.DateOnly))
}

// latest is the organisation's latest ledger boundary; ok is false when the ledger was never purged.
func latest(ctx context.Context, u storage.UnitOfWork) (storage.LedgerBoundary, bool, error) {
	bounds, err := u.Retention().LedgerBoundaries(ctx)
	if err != nil || len(bounds) == 0 {
		return storage.LedgerBoundary{}, false, err
	}
	return bounds[len(bounds)-1], true, nil
}

// CheckDay refuses a reading of a day before the latest boundary (Purged).
func CheckDay(ctx context.Context, u storage.UnitOfWork, day time.Time) error {
	b, ok, err := latest(ctx, u)
	if err != nil || !ok {
		return err
	}
	if day.Before(b.Boundary) {
		return &Purged{Boundary: b.Boundary, Day: day, Carried: b.CarriedRevision}
	}
	return nil
}

// CheckRevision refuses a reading as at a ledger revision before the latest purge (ENG-46; Purged).
func CheckRevision(ctx context.Context, u storage.UnitOfWork, revision int64) error {
	b, ok, err := latest(ctx, u)
	if err != nil || !ok {
		return err
	}
	if revision < b.CarriedRevision {
		return &Purged{Boundary: b.Boundary, Revision: revision, Carried: b.CarriedRevision}
	}
	return nil
}

// check refuses a snapshot as at a revision before the latest purge.
func (s Snapshot) check(ctx context.Context, u storage.UnitOfWork) error {
	if r, at := s.Revision(); at {
		return CheckRevision(ctx, u, r)
	}
	return nil
}
