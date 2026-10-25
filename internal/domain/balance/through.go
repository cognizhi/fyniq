package balance

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Through is a chart account's balance in a currency through a day, and its value in the base currency (ENG-04): the
// materialised balances of every period that ends by then, and the lines of the period the day falls in, up to it. A
// money account's book balance for its reconciliation and the cash position read it (FR-BNK-10, FR-BNK-13).
func Through(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, cur, base money.Currency, through time.Time) (money.Money, money.Money, error) {
	amount, value := money.Zero(cur), money.Zero(base)
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return amount, value, err
	}
	add := func(rawAmount, rawBase storage.Decimal) error {
		a, err := money.ParseStored(string(rawAmount), cur)
		if err != nil {
			return err
		}
		b, err := money.ParseStored(string(rawBase), base)
		if err != nil {
			return err
		}
		amount, _ = amount.Add(a)
		value, _ = value.Add(b)
		return nil
	}
	for _, p := range periods {
		switch {
		case !p.EndsOn.After(through):
			rows, err := u.Balances().ForPeriod(ctx, p.ID)
			if err != nil {
				return amount, value, err
			}
			for _, r := range rows {
				if r.AccountID == account && r.Currency == cur.Code {
					if err := add(r.Amount, r.BaseAmount); err != nil {
						return amount, value, err
					}
				}
			}
		case !p.StartsOn.After(through):
			lines, err := u.Ledger().AccountLines(ctx, account, p.StartsOn, through, 1_000_000)
			if err != nil {
				return amount, value, err
			}
			for _, l := range lines {
				if l.Currency == cur.Code {
					if err := add(l.Amount, l.BaseAmount); err != nil {
						return amount, value, err
					}
				}
			}
		}
	}
	return amount, value, nil
}
