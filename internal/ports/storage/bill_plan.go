package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BillPlan is the payment date planned for a posted bill (FR-AP-13; PRD-001 rev 0.93): kept beside the bill, posting
// nothing, never changed (FY175). The latest for a bill is in force; one with no date unschedules it.
type BillPlan struct {
	ID         uuid.UUID
	BillID     uuid.UUID
	PlannedOn  *time.Time
	RecordedBy string
	RecordedAt time.Time
}

// BillPlans are the organisation's planned payment dates. Planning is audited on the bill.
type BillPlans interface {
	// Plan records a bill's planned payment date, nil to unschedule it.
	Plan(ctx context.Context, bill uuid.UUID, on *time.Time) (BillPlan, error)
	// Of are the plans in force of the bills named, by bill; a bill never planned is absent.
	Of(ctx context.Context, bills []uuid.UUID) (map[uuid.UUID]BillPlan, error)
}
