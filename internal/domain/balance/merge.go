package balance

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Holding is what an account holds in one currency and dimension set: the amount in the currency and its base amount.
type Holding struct {
	Amount money.Money
	Base   money.Money
	Values []uuid.UUID
}

// Holdings are what an account holds over periods, each currency and dimension set apart (ENG-04; FR-COA-04): the
// materialised balances of each period summed, the sets that hold nothing left out, by currency and then set. An
// account merge reads them over the periods to its date once no line on the account is dated after it (LinesAfter), so
// each period holds only lines up to the date.
func Holdings(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, periods []storage.Period, base money.Currency) ([]Holding, error) {
	type key struct{ currency, set string }
	sums := map[key]*Holding{}
	var keys []key
	for _, p := range periods {
		rows, err := u.Balances().ForPeriodBySet(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.AccountID != account {
				continue
			}
			cur, err := money.Lookup(r.Currency)
			if err != nil {
				return nil, err
			}
			amount, err := money.ParseStored(string(r.Amount), cur)
			if err != nil {
				return nil, err
			}
			value, err := money.ParseStored(string(r.BaseAmount), base)
			if err != nil {
				return nil, err
			}
			values := slices.Clone(r.Values)
			slices.SortFunc(values, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
			k := key{r.Currency, fmt.Sprint(values)}
			h, ok := sums[k]
			if !ok {
				h = &Holding{Amount: money.Zero(cur), Base: money.Zero(base), Values: values}
				sums[k] = h
				keys = append(keys, k)
			}
			if h.Amount, err = h.Amount.Add(amount); err != nil {
				return nil, err
			}
			if h.Base, err = h.Base.Add(value); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		if c := strings.Compare(a.currency, b.currency); c != 0 {
			return c
		}
		return strings.Compare(a.set, b.set)
	})
	out := make([]Holding, 0, len(keys))
	for _, k := range keys {
		if h := sums[k]; !h.Amount.IsZero() || !h.Base.IsZero() {
			out = append(out, *h)
		}
	}
	return out, nil
}

// LinesAfter reports whether a line on the account is dated after the day (ENG-04): a merge dated before an account's
// last posting would leave that posting behind (FR-COA-04).
func LinesAfter(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, day time.Time) (bool, error) {
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return false, err
	}
	last := day
	for _, p := range periods {
		if p.EndsOn.After(last) {
			last = p.EndsOn
		}
	}
	if !last.After(day) {
		return false, nil
	}
	lines, err := u.Ledger().AccountLines(ctx, account, day.AddDate(0, 0, 1), last, 1)
	return len(lines) > 0, err
}
