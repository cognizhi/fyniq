package postgres

import (
	"context"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the weekly cash projection snapshots (FR-PLN-04)

func (u *unitOfWork) CashSnapshots() storage.CashSnapshots { return cashSnapshots{u} }

type cashSnapshots struct{ u *unitOfWork }

func toCashSnapshot(r sqlcdb.FyniqCashSnapshot) storage.CashSnapshot {
	return storage.CashSnapshot{ID: r.ID, WeekOf: r.WeekOf, TakenOn: r.TakenOn, Currency: r.Currency, LedgerRevision: r.LedgerRevision, Projection: r.Projection, TakenAt: r.TakenAt}
}

func (r cashSnapshots) List(ctx context.Context) ([]storage.CashSnapshot, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListCashSnapshots(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CashSnapshot, 0, len(rows))
	for _, x := range rows {
		out = append(out, toCashSnapshot(x))
	}
	return out, nil
}

func (r cashSnapshots) Take(ctx context.Context, s storage.CashSnapshot) (storage.CashSnapshot, error) {
	if err := r.u.check(); err != nil {
		return storage.CashSnapshot{}, err
	}
	row, err := r.u.q.InsertCashSnapshot(ctx, sqlcdb.InsertCashSnapshotParams{WeekOf: s.WeekOf, TakenOn: s.TakenOn, Currency: s.Currency, LedgerRevision: s.LedgerRevision, Projection: s.Projection, TakenAt: s.TakenAt})
	if err != nil {
		return storage.CashSnapshot{}, mapError(err)
	}
	out := toCashSnapshot(row)
	return out, r.u.event(ctx, "cash_snapshot.taken", "cash_snapshot", out.ID, nil, map[string][2]any{"week_of": {nil, out.WeekOf.Format("2006-01-02")}, "ledger_revision": {nil, out.LedgerRevision}})
}
