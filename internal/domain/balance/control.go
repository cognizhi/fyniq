package balance

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MaxControlLines is the most lines of one account in one period Control reads; more is refused (TooMany).
const MaxControlLines = 100000

// SourceTotal is what the entries of one source type posted to a control account in one currency through a day, in that
// currency and in the base currency.
type SourceTotal struct {
	SourceType string
	Amount     money.Money
	Base       money.Money
}

// Control is a control account's balance through a day by currency and by the source type of its entries (INV-07): what
// its sub-ledger is reconciled with. The balance service reads the account's lines a period at a time, as the general
// ledger does (FR-GL-11), since the cached balances do not keep the source. Balance-sheet accounts only: the year-end
// never folds them (FR-GL-10).
func Control(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, through time.Time) ([]SourceTotal, error) {
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return nil, err
	}
	type key struct{ source, currency string }
	totals := map[key]*SourceTotal{}
	var order []key
	for _, p := range periods {
		if p.StartsOn.After(through) {
			continue
		}
		to := p.EndsOn
		if to.After(through) {
			to = through
		}
		lines, err := u.Ledger().AccountLines(ctx, account, p.StartsOn, to, MaxControlLines+1)
		if err != nil {
			return nil, err
		}
		if len(lines) > MaxControlLines {
			return nil, &TooMany{AccountID: account}
		}
		for _, l := range lines {
			cur, err := money.Lookup(l.Currency)
			if err != nil {
				return nil, err
			}
			amount, err := money.ParseStored(string(l.Amount), cur)
			if err != nil {
				return nil, err
			}
			b, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return nil, err
			}
			k := key{l.Source.Type, l.Currency}
			t := totals[k]
			if t == nil {
				t = &SourceTotal{SourceType: l.Source.Type, Amount: money.Zero(cur), Base: money.Zero(base)}
				totals[k] = t
				order = append(order, k)
			}
			if t.Amount, err = t.Amount.Add(amount); err != nil {
				return nil, err
			}
			if t.Base, err = t.Base.Add(b); err != nil {
				return nil, err
			}
		}
	}
	out := make([]SourceTotal, 0, len(order))
	for _, k := range order {
		out = append(out, *totals[k])
	}
	return out, nil
}
