package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the KPIs' entered counts (FR-KPI-02, rev 0.161)

func (u *unitOfWork) KPIInputs() storage.KPIInputs { return kpiInputs{u} }

type kpiInputs struct{ u *unitOfWork }

func toKPIInput(r sqlcdb.FyniqKpiInput) storage.KPIInput {
	return storage.KPIInput{ID: r.ID, Month: r.Month, Counts: storage.KPICounts{Paying: r.PayingCustomers, New: r.NewCustomers, Lost: r.LostCustomers,
		Headcount: r.Headcount}, EnteredBy: r.EnteredBy, EnteredAt: r.EnteredAt, Version: int(r.Version)}
}

// countsOf are the counts as the audit trail records them: each figure, or nil when not entered.
func countsOf(c storage.KPICounts) map[string]any {
	out := map[string]any{}
	for k, v := range map[string]*int64{"paying_customers": c.Paying, "new_customers": c.New, "lost_customers": c.Lost, "headcount": c.Headcount} {
		if v != nil {
			out[k] = *v
		} else {
			out[k] = nil
		}
	}
	return out
}

func (r kpiInputs) Get(ctx context.Context, month time.Time) (storage.KPIInput, error) {
	if err := r.u.check(); err != nil {
		return storage.KPIInput{}, err
	}
	row, err := r.u.q.GetKPIInput(ctx, month)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.KPIInput{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.KPIInput{}, mapError(err)
	}
	return toKPIInput(row), nil
}

func (r kpiInputs) Range(ctx context.Context, from, to time.Time) ([]storage.KPIInput, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KPIInputsBetween(ctx, sqlcdb.KPIInputsBetweenParams{FromMonth: from, ToMonth: to})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KPIInput, 0, len(rows))
	for _, row := range rows {
		out = append(out, toKPIInput(row))
	}
	return out, nil
}

func (r kpiInputs) Set(ctx context.Context, month time.Time, c storage.KPICounts, enteredBy string, at time.Time, version int) (storage.KPIInput, error) {
	if err := r.u.check(); err != nil {
		return storage.KPIInput{}, err
	}
	if version == 0 {
		row, err := r.u.q.InsertKPIInput(ctx, sqlcdb.InsertKPIInputParams{Month: month, PayingCustomers: c.Paying, NewCustomers: c.New, LostCustomers: c.Lost,
			Headcount: c.Headcount, EnteredBy: enteredBy, EnteredAt: at})
		if err != nil {
			return storage.KPIInput{}, mapError(err) // a month entered meanwhile is CodeVersion (kpi_input_pkey)
		}
		out := toKPIInput(row)
		v := out.Version
		return out, r.u.event(ctx, "kpi_input.entered", "kpi_input", out.ID, &v, map[string][2]any{"month": {nil, month.Format(time.DateOnly)},
			"counts": {nil, countsOf(c)}})
	}
	before, err := r.Get(ctx, month)
	if errors.Is(err, ports.ErrNotFound) {
		return storage.KPIInput{}, &storage.Error{Code: storage.CodeVersion, Message: "the month has no counts at that version"}
	}
	if err != nil {
		return storage.KPIInput{}, err
	}
	row, err := r.u.q.UpdateKPIInput(ctx, sqlcdb.UpdateKPIInputParams{Month: month, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		PayingCustomers: c.Paying, NewCustomers: c.New, LostCustomers: c.Lost, Headcount: c.Headcount, EnteredBy: enteredBy, EnteredAt: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.KPIInput{}, &storage.Error{Code: storage.CodeVersion, Message: "the month's counts changed since they were read"}
	}
	if err != nil {
		return storage.KPIInput{}, mapError(err)
	}
	out := toKPIInput(row)
	v := out.Version
	return out, r.u.event(ctx, "kpi_input.changed", "kpi_input", out.ID, &v, map[string][2]any{"month": {month.Format(time.DateOnly), month.Format(time.DateOnly)},
		"counts": {countsOf(before.Counts), countsOf(c)}})
}
