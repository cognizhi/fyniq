package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// withholdingDecisions keeps the tax agent's withholding decisions (FR-AP-19; PRD-001 rev 0.90): append-only, the
// latest for a supplier and nature in force from when it is recorded.
type withholdingDecisions struct{ u *unitOfWork }

func (u *unitOfWork) WithholdingDecisions() storage.WithholdingDecisions { return withholdingDecisions{u} }

func toWithholdingDecision(r sqlcdb.WithholdingDecisionInForceRow) storage.WithholdingDecision {
	return storage.WithholdingDecision{ID: r.ID, RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt,
		NewWithholdingDecision: storage.NewWithholdingDecision{SupplierID: r.SupplierID, Nature: r.Nature, Outcome: r.Outcome,
			RatePercent: r.RatePercent, TaxAgent: r.TaxAgent, Note: str(r.Note), BillID: r.BillID}}
}

func (w withholdingDecisions) Record(ctx context.Context, d storage.NewWithholdingDecision) (storage.WithholdingDecision, error) {
	if err := w.u.check(); err != nil {
		return storage.WithholdingDecision{}, err
	}
	row, err := w.u.q.InsertWithholdingDecision(ctx, sqlcdb.InsertWithholdingDecisionParams{SupplierID: d.SupplierID, Nature: d.Nature,
		Outcome: d.Outcome, RatePercent: opt(d.RatePercent), TaxAgent: d.TaxAgent, Note: opt(d.Note), BillID: d.BillID, RecordedBy: w.u.actor(ctx)})
	if err != nil {
		return storage.WithholdingDecision{}, mapError(err)
	}
	out := toWithholdingDecision(sqlcdb.WithholdingDecisionInForceRow(row))
	changes := map[string][2]any{
		"supplier_id": {nil, out.SupplierID.String()}, "nature": {nil, out.Nature}, "outcome": {nil, out.Outcome},
		"rate_percent": {nil, nilIfEmpty(out.RatePercent)}, "tax_agent": {nil, out.TaxAgent}, "note": {nil, nilIfEmpty(out.Note)},
	}
	if out.BillID != nil {
		changes["bill_id"] = [2]any{nil, out.BillID.String()}
	}
	if err := w.u.event(ctx, "withholding_decision.recorded", "withholding_decision", out.ID, nil, changes); err != nil {
		return storage.WithholdingDecision{}, err
	}
	return out, nil
}

func (w withholdingDecisions) InForce(ctx context.Context, supplier uuid.UUID, nature string, at time.Time) (storage.WithholdingDecision, error) {
	if err := w.u.check(); err != nil {
		return storage.WithholdingDecision{}, err
	}
	var when *time.Time
	if !at.IsZero() {
		when = &at
	}
	row, err := w.u.q.WithholdingDecisionInForce(ctx, sqlcdb.WithholdingDecisionInForceParams{SupplierID: supplier, Nature: nature, At: when})
	if err != nil {
		return storage.WithholdingDecision{}, mapError(err)
	}
	return toWithholdingDecision(row), nil
}

func (w withholdingDecisions) Of(ctx context.Context, supplier uuid.UUID) ([]storage.WithholdingDecision, error) {
	if err := w.u.check(); err != nil {
		return nil, err
	}
	rows, err := w.u.q.WithholdingDecisionsOf(ctx, supplier)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.WithholdingDecision, 0, len(rows))
	for _, r := range rows {
		out = append(out, toWithholdingDecision(sqlcdb.WithholdingDecisionInForceRow(r)))
	}
	return out, nil
}
