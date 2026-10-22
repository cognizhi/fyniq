package balance

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ForeignRow is one account's balance in one foreign currency at a period's end: the amount in the currency, and its
// value in the books in the base currency (ENG-43).
type ForeignRow struct {
	AccountID uuid.UUID
	Amount    money.Money
	Base      money.Money
}

// Foreign returns every account's balance in each currency other than base, through the end of the period through, from
// the materialised balances (ENG-04): every period ending on or before it counts, as balance-sheet balances carry on
// from year to year. The month-end revaluation reads its open amounts and carrying values here (FR-GL-04). An account
// and currency whose amount and base value are both zero are left out; rows are by account, then currency.
func Foreign(ctx context.Context, u storage.UnitOfWork, base money.Currency, all []storage.Period, through storage.Period) ([]ForeignRow, error) {
	type key struct {
		account  uuid.UUID
		currency string
	}
	sums := map[key]*ForeignRow{}
	for _, p := range all {
		if p.EndsOn.After(through.EndsOn) {
			continue
		}
		rows, err := u.Balances().ForPeriod(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.Currency == base.Code {
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
			k := key{r.AccountID, r.Currency}
			s, ok := sums[k]
			if !ok {
				s = &ForeignRow{AccountID: r.AccountID, Amount: money.Zero(cur), Base: money.Zero(base)}
				sums[k] = s
			}
			if s.Amount, err = s.Amount.Add(amount); err != nil {
				return nil, err
			}
			if s.Base, err = s.Base.Add(value); err != nil {
				return nil, err
			}
		}
	}
	out := make([]ForeignRow, 0, len(sums))
	for _, s := range sums {
		if s.Amount.IsZero() && s.Base.IsZero() {
			continue
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID.String() < out[j].AccountID.String()
		}
		return out[i].Amount.Currency().Code < out[j].Amount.Currency().Code
	})
	return out, nil
}
