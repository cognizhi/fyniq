package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the spend anomaly alerts (FR-KPI-06, rev 0.540)

func (u *unitOfWork) KPIAlerts() storage.KPIAlerts { return kpiAlerts{u} }

type kpiAlerts struct{ u *unitOfWork }

func toKPIAlert(r sqlcdb.FyniqKpiAlert) storage.KPIAlert {
	return storage.KPIAlert{ID: r.ID, Kind: r.Kind, Category: r.Category, Month: r.Month, Currency: r.Currency, Spend: storage.Decimal(r.Spend), Mean: storage.Decimal(r.Mean),
		HistoryMonths: int(r.HistoryMonths), LedgerRevision: r.LedgerRevision, RaisedAt: r.RaisedAt}
}

func (r kpiAlerts) Raise(ctx context.Context, a storage.KPIAlert) (storage.KPIAlert, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.KPIAlert{}, false, err
	}
	row, err := r.u.q.RaiseKPIAlert(ctx, sqlcdb.RaiseKPIAlertParams{Kind: a.Kind, Category: a.Category, Month: a.Month, Currency: a.Currency, Spend: string(a.Spend), Mean: string(a.Mean),
		HistoryMonths: int32(min(max(a.HistoryMonths, 0), 1000)), LedgerRevision: a.LedgerRevision, RaisedAt: a.RaisedAt}) // #nosec G115 -- bounded
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.KPIAlert{}, false, nil // raised already
	}
	if err != nil {
		return storage.KPIAlert{}, false, mapError(err)
	}
	out := toKPIAlert(row)
	return out, true, r.u.event(ctx, "kpi_alert.raised", "kpi_alert", out.ID, nil, map[string][2]any{"kind": {nil, out.Kind}, "category": {nil, out.Category},
		"month": {nil, out.Month.Format(time.DateOnly)}, "spend": {nil, string(out.Spend)}, "mean": {nil, string(out.Mean)}})
}

func (r kpiAlerts) List(ctx context.Context) ([]storage.KPIAlert, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListKPIAlerts(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KPIAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, toKPIAlert(row))
	}
	return out, nil
}
