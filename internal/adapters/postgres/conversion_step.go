package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the ACRA steps of conversions (FR-EQ-09)

func (u *unitOfWork) ConversionSteps() storage.ConversionSteps { return conversionSteps{u} }

type conversionSteps struct{ u *unitOfWork }

func toConversionStep(r sqlcdb.FyniqConversionStep) storage.ConversionStep {
	out := storage.ConversionStep{ID: r.ID, SettlementID: r.SettlementID, Kind: r.Kind, On: r.OnDate, RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt}
	if r.Reference != nil {
		out.Reference = *r.Reference
	}
	return out
}

func (r conversionSteps) Record(ctx context.Context, x storage.ConversionStep) (storage.ConversionStep, error) {
	if err := r.u.check(); err != nil {
		return storage.ConversionStep{}, err
	}
	var ref *string
	if x.Reference != "" {
		ref = &x.Reference
	}
	row, err := r.u.q.InsertConversionStep(ctx, sqlcdb.InsertConversionStepParams{SettlementID: x.SettlementID, Kind: x.Kind, OnDate: x.On, Reference: ref,
		RecordedBy: x.RecordedBy, RecordedAt: x.RecordedAt})
	if err != nil {
		return storage.ConversionStep{}, mapError(err)
	}
	out := toConversionStep(row)
	if err := r.u.event(ctx, "director_settlement.acra_step", "document", out.SettlementID, nil, map[string][2]any{
		"step": {nil, map[string]any{"id": out.ID.String(), "kind": out.Kind, "on": out.On.Format("2006-01-02"), "reference": out.Reference}},
	}); err != nil {
		return storage.ConversionStep{}, err
	}
	return out, nil
}

func (r conversionSteps) Of(ctx context.Context, settlementID uuid.UUID) ([]storage.ConversionStep, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ConversionStepsOf(ctx, settlementID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ConversionStep, 0, len(rows))
	for _, x := range rows {
		out = append(out, toConversionStep(x))
	}
	return out, nil
}

// ------------------------------------------------------------------ the ACRA steps of equity transactions (FR-EQ-17)

func (u *unitOfWork) EquitySteps() storage.EquitySteps { return equitySteps{u} }

type equitySteps struct{ u *unitOfWork }

func toEquityStep(r sqlcdb.FyniqEquityStep) storage.EquityStep {
	out := storage.EquityStep{ID: r.ID, TransactionID: r.TransactionID, Kind: r.Kind, On: r.OnDate, RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt}
	if r.Reference != nil {
		out.Reference = *r.Reference
	}
	return out
}

func (r equitySteps) Record(ctx context.Context, x storage.EquityStep) (storage.EquityStep, error) {
	if err := r.u.check(); err != nil {
		return storage.EquityStep{}, err
	}
	var ref *string
	if x.Reference != "" {
		ref = &x.Reference
	}
	row, err := r.u.q.InsertEquityStep(ctx, sqlcdb.InsertEquityStepParams{TransactionID: x.TransactionID, Kind: x.Kind, OnDate: x.On, Reference: ref,
		RecordedBy: x.RecordedBy, RecordedAt: x.RecordedAt})
	if err != nil {
		return storage.EquityStep{}, mapError(err)
	}
	out := toEquityStep(row)
	if err := r.u.event(ctx, "equity_transaction.acra_step", "document", out.TransactionID, nil, map[string][2]any{
		"step": {nil, map[string]any{"id": out.ID.String(), "kind": out.Kind, "on": out.On.Format("2006-01-02"), "reference": out.Reference}},
	}); err != nil {
		return storage.EquityStep{}, err
	}
	return out, nil
}

func (r equitySteps) Of(ctx context.Context, transactionID uuid.UUID) ([]storage.EquityStep, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.EquityStepsOf(ctx, transactionID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.EquityStep, 0, len(rows))
	for _, x := range rows {
		out = append(out, toEquityStep(x))
	}
	return out, nil
}
