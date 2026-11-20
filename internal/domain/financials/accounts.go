package financials

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ColumnSpec is one column of a custom report (FR-RPT-04, p2-reporting-planning design D1): a run of whole periods, with
// the key and label the report gives it.
type ColumnSpec struct {
	Key, Label string
	Periods    []storage.Period
}

// AccountFigures are each account's figure in each column, as a statement shows it (income, liabilities and equity positive
// as credits, assets and expenses positive as debits), read as at a ledger revision: a P&L account's is the movement over
// the column's periods, a balance-sheet account's the position at the column's last day. only keeps the lines that carry
// every one of these dimension values (FR-DIM-02), as a P&L by dimension does; it narrows the P&L accounts only, for a
// balance sheet by dimension would not balance. The accounts are returned with the figures, in code order.
func AccountFigures(ctx context.Context, u storage.UnitOfWork, base money.Currency, revision int64, all []storage.Period,
	cols []ColumnSpec, only []uuid.UUID) (map[uuid.UUID][]money.Money, []storage.Account, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	var filter func([]uuid.UUID) bool
	if len(only) > 0 {
		filter = func(values []uuid.UUID) bool {
			for _, v := range only {
				if !slices.Contains(values, v) {
					return false
				}
			}
			return true
		}
	}
	out := map[uuid.UUID][]money.Money{}
	put := func(i int, id uuid.UUID, m money.Money) {
		if out[id] == nil {
			out[id] = make([]money.Money, len(cols))
			for j := range out[id] {
				out[id][j] = money.Zero(base)
			}
		}
		out[id][i] = m
	}
	at := balance.At(revision)
	for i, c := range cols {
		if len(c.Periods) == 0 {
			continue // the books lack the months: the column's figures stay zero, and the caller marks them not available
		}
		ids := make([]uuid.UUID, len(c.Periods))
		for j, p := range c.Periods {
			ids[j] = p.ID
		}
		snap := at
		if filter != nil {
			snap = at.Only(filter)
		}
		movement, err := balance.TrialAt(ctx, u, base, snap, ids...)
		if err != nil {
			return nil, nil, err
		}
		for _, row := range movement.Rows {
			if a, ok := byID[row.AccountID]; ok && profitAndLoss(a) {
				put(i, a.ID, shown(ProfitAndLoss, a, row.Base))
			}
		}
		position, err := balance.PositionAt(ctx, u, base, at, all, c.Periods[len(c.Periods)-1])
		if err != nil {
			return nil, nil, err
		}
		for _, row := range position.Rows {
			if a, ok := byID[row.AccountID]; ok && !profitAndLoss(a) {
				put(i, a.ID, shown(BalanceSheet, a, row.Base))
			}
		}
	}
	return out, accounts, nil
}
