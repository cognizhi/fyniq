package budget

import (
	"context"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ForMonths is the budget column of a P&L (FR-RPT-02; p1-planning design D6): each account's budget for the months from
// the first of from to the first of to, from each month's financial year's budget, signed as the ledger signs it (debits
// positive), so the statement lays it out as it lays out the books. carries keeps the lines of a P&L by dimension, such
// as a department's; nil keeps every line. ok is false when a month lies outside the financial years or in a year with
// no recorded budget: the statement then leaves the column out, as it does a comparative the books do not cover.
func ForMonths(ctx context.Context, u storage.UnitOfWork, base money.Currency, cal period.Calendar, from, to time.Time,
	carries func(values []uuid.UUID) bool) (out map[uuid.UUID]money.Money, ok bool, err error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, false, err
	}
	income := map[uuid.UUID]bool{}
	for _, a := range accounts {
		income[a.ID] = a.Type == storage.Revenue
	}
	c := apd.BaseContext.WithPrecision(60)
	sums := map[uuid.UUID]*apd.Decimal{}
	years := map[int]*Budget{}
	for m := firstOf(from); !m.After(firstOf(to)); m = m.AddDate(0, 1, 0) {
		n := cal.Number(m.AddDate(0, 1, -1)) // its last day: the books may start within it
		if n < 1 {
			return nil, false, nil
		}
		b, seen := years[n]
		if !seen {
			_, got, found, err := OfYear(ctx, u, cal.Year(n).StartsOn)
			if err != nil {
				return nil, false, err
			}
			if found {
				b = &got
			}
			years[n] = b
		}
		if b == nil {
			return nil, false, nil
		}
		k := -1
		for i, x := range cal.Year(n).Months {
			if firstOf(x.StartsOn).Equal(m) {
				k = i
			}
		}
		if k < 0 {
			return nil, false, nil
		}
		for _, l := range b.Lines {
			if k >= len(l.Amounts) || l.Amounts[k] == "" {
				continue
			}
			id, err := uuid.Parse(l.AccountID)
			if err != nil {
				return nil, false, err
			}
			if carries != nil {
				var values []uuid.UUID
				for _, v := range l.Dimensions {
					vid, err := uuid.Parse(v)
					if err != nil {
						return nil, false, err
					}
					values = append(values, vid)
				}
				if !carries(values) {
					continue
				}
			}
			x, _, err := apd.NewFromString(l.Amounts[k])
			if err != nil {
				return nil, false, err
			}
			if income[id] {
				x.Neg(x)
			}
			if sums[id] == nil {
				sums[id] = apd.New(0, 0)
			}
			if _, err := c.Add(sums[id], sums[id], x); err != nil {
				return nil, false, err
			}
		}
	}
	out = make(map[uuid.UUID]money.Money, len(sums))
	for id, x := range sums {
		if out[id], err = money.FromRounded(x, base); err != nil {
			return nil, false, err
		}
	}
	return out, true, nil
}

func firstOf(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC) }
