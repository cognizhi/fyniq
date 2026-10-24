package receivables

import (
	"context"
	"errors"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Row is what customers owe in one currency, by ageing bucket (current, 1–30, 31–60, 61–90, 90+ days past due), with
// their credits apart (negative) and the total, and the carrying amount of it all in the base currency.
type Row = subledger.Row

// CustomerAgeing is one customer's ageing, a row per currency.
type CustomerAgeing = subledger.GroupAgeing

// Ageing is AR ageing as at a day (FR-AR-10): each customer's open invoices by days past due and credits by currency,
// and the totals by currency, from the sub-ledger.
type Ageing struct {
	AsAt      time.Time
	Customers []CustomerAgeing
	Totals    []Row
}

// Age is the ageing of the AR sub-ledger as at its day, in base for the carrying amounts.
func Age(l Ledger, base money.Currency) (Ageing, error) {
	a, err := subledger.Age(l.AsAt, base, l.groups())
	if err != nil {
		return Ageing{}, err
	}
	return Ageing{AsAt: a.AsAt, Customers: a.Groups, Totals: a.Totals}, nil
}

// Exposure is the unrealised FX on what customers owe in one foreign currency as at a day (FR-AR-10): the total at the
// rate the cascade gives for the day (ENG-40) under RND-FX, less its carrying amount at the documents' own rates
// (ENG-14). Positive is a gain. Missing says why there is no rate, when the cascade has none.
type Exposure struct {
	Currency   money.Currency
	Open       money.Money
	Carrying   money.Money
	Rate       *money.Rate
	AtRate     money.Money
	Unrealised money.Money
	Missing    *fx.Error
}

// Exposures are the unrealised FX of the ageing's totals in currencies other than the base, as at its day, in the order
// of the totals. It is what the month-end revaluation would post for them on that day, before any earlier run.
func Exposures(ctx context.Context, u storage.UnitOfWork, base money.Currency, a Ageing) ([]Exposure, error) {
	var out []Exposure
	for _, r := range a.Totals {
		if r.Currency == base {
			continue
		}
		x := Exposure{Currency: r.Currency, Open: r.Total, Carrying: r.Carrying}
		f, err := fx.Lookup(ctx, u, base, r.Currency, a.AsAt)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			x.Missing = fe
		case err != nil:
			return nil, err
		default:
			rate := f.Rate
			x.Rate = &rate
			if x.AtRate, err = rounding.FX(r.Total, rate); err != nil {
				return nil, err
			}
			if x.Unrealised, err = x.AtRate.Sub(r.Carrying); err != nil {
				return nil, err
			}
		}
		out = append(out, x)
	}
	return out, nil
}
