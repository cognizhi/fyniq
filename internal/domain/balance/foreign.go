package balance

import (
	"context"
	"sort"
	"time"

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
	if err := CheckDay(ctx, u, through.EndsOn); err != nil {
		return nil, err
	}
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

// ForeignPerson is one person's balance on a director's account in a currency other than the base: the amount in the
// currency, and its value in the books in the base currency. Party is uuid.Nil for lines that name no one, which only
// lines posted before director's accounts named their person can be.
type ForeignPerson struct {
	Party  uuid.UUID
	Amount money.Money
	Base   money.Money
}

// ForeignByPerson is a director's account's balance in each currency other than base through a day, person by person
// (FR-EQ-01, ENG-04), by currency: the month-end revaluation revalues a director's account per person, since every line
// on it names whose it is (FY250). The person is not in the cached balances, so the lines are read as People reads them.
// A person whose amount and base value are both zero is left out; each currency's people are in the order of their IDs.
func ForeignByPerson(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, through time.Time) (map[string][]ForeignPerson, error) {
	if err := CheckDay(ctx, u, through); err != nil {
		return nil, err
	}
	type key struct {
		party    uuid.UUID
		currency string
	}
	sums := map[key]*ForeignPerson{}
	err := controlLines(ctx, u, account, through, func(l storage.AccountLine) error {
		if l.Currency == base.Code {
			return nil
		}
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return err
		}
		amount, err := money.ParseStored(string(l.Amount), cur)
		if err != nil {
			return err
		}
		value, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return err
		}
		k := key{currency: l.Currency}
		if l.PartyID != nil {
			k.party = *l.PartyID
		}
		s, ok := sums[k]
		if !ok {
			s = &ForeignPerson{Party: k.party, Amount: money.Zero(cur), Base: money.Zero(base)}
			sums[k] = s
		}
		if s.Amount, err = s.Amount.Add(amount); err != nil {
			return err
		}
		s.Base, err = s.Base.Add(value)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := map[string][]ForeignPerson{}
	for k, s := range sums {
		if s.Amount.IsZero() && s.Base.IsZero() {
			continue
		}
		out[k.currency] = append(out[k.currency], *s)
	}
	for _, people := range out {
		sort.Slice(people, func(i, j int) bool { return people[i].Party.String() < people[j].Party.String() })
	}
	return out, nil
}
