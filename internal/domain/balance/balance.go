// Package balance is the balance service (ENG-04): the one place ledger figures come from. It reads
// the materialised balances, which the database keeps from the journal lines in the posting
// transaction and proves equal to them (INV-08). Nothing else sums journal lines; an architecture
// test keeps every other package away from the balances.
package balance

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Row is one account's base-currency total.
type Row struct {
	AccountID uuid.UUID
	Base      money.Money
}

// TrialBalance is every account's base-currency total over some periods, and their sum, which is
// zero when the books balance (INV-09).
type TrialBalance struct {
	Rows  []Row
	Total money.Money
}

// Trial returns the trial balance of the given periods, in the organisation's base currency.
func Trial(ctx context.Context, u storage.UnitOfWork, base money.Currency, periods ...uuid.UUID) (TrialBalance, error) {
	totals := map[uuid.UUID]money.Money{}
	for _, p := range periods {
		rows, err := u.Balances().ForPeriod(ctx, p)
		if err != nil {
			return TrialBalance{}, err
		}
		for _, r := range rows {
			amount, err := money.ParseStored(string(r.BaseAmount), base)
			if err != nil {
				return TrialBalance{}, err
			}
			cur, ok := totals[r.AccountID]
			if !ok {
				cur = money.Zero(base)
			}
			if totals[r.AccountID], err = cur.Add(amount); err != nil {
				return TrialBalance{}, err
			}
		}
	}
	tb := TrialBalance{Total: money.Zero(base)}
	for id, amount := range totals {
		tb.Rows = append(tb.Rows, Row{AccountID: id, Base: amount})
		var err error
		if tb.Total, err = tb.Total.Add(amount); err != nil {
			return TrialBalance{}, err
		}
	}
	sort.Slice(tb.Rows, func(i, j int) bool { return tb.Rows[i].AccountID.String() < tb.Rows[j].AccountID.String() })
	return tb, nil
}
