package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// billPlans keeps the planned payment dates of bills (FR-AP-13; PRD-001 rev 0.93): append-only, the latest in force.
type billPlans struct{ u *unitOfWork }

func (u *unitOfWork) BillPlans() storage.BillPlans { return billPlans{u} }

func (p billPlans) Plan(ctx context.Context, bill uuid.UUID, on *time.Time) (storage.BillPlan, error) {
	if err := p.u.check(); err != nil {
		return storage.BillPlan{}, err
	}
	row, err := p.u.q.InsertBillPaymentPlan(ctx, sqlcdb.InsertBillPaymentPlanParams{BillID: bill, PlannedOn: pgDate(on), RecordedBy: p.u.actor(ctx)})
	if err != nil {
		return storage.BillPlan{}, mapError(err)
	}
	out := storage.BillPlan{ID: row.ID, BillID: row.BillID, PlannedOn: nullDate(row.PlannedOn), RecordedBy: row.RecordedBy, RecordedAt: row.RecordedAt}
	if err := p.u.event(ctx, "document.payment_planned", "document", bill, nil, map[string][2]any{"planned_on": {nil, optDay(out.PlannedOn)}}); err != nil {
		return storage.BillPlan{}, err
	}
	return out, nil
}

func (p billPlans) Of(ctx context.Context, bills []uuid.UUID) (map[uuid.UUID]storage.BillPlan, error) {
	if err := p.u.check(); err != nil {
		return nil, err
	}
	out := map[uuid.UUID]storage.BillPlan{}
	if len(bills) == 0 {
		return out, nil
	}
	rows, err := p.u.q.BillPaymentPlans(ctx, bills)
	if err != nil {
		return nil, mapError(err)
	}
	for _, r := range rows {
		out[r.BillID] = storage.BillPlan{ID: r.ID, BillID: r.BillID, PlannedOn: nullDate(r.PlannedOn), RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt}
	}
	return out, nil
}
