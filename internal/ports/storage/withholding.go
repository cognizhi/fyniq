package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NewWithholdingDecision is the tax agent's decision on whether tax is withheld from payments to a non-resident
// supplier for one nature of payment (FR-AP-19; PRD-001 rev 0.90): not_applicable, withhold at the rate they advised,
// or treaty, a reduced rate with a certificate of residence. RatePercent is empty for not_applicable. BillID is the
// bill it was recorded from, whose files hold the tax agent's advice; nil when none.
type NewWithholdingDecision struct {
	SupplierID  uuid.UUID
	Nature      string
	Outcome     string
	RatePercent string
	TaxAgent    string
	Note        string
	BillID      *uuid.UUID
}

// WithholdingDecision is a recorded decision: never changed, replaced by a later one for the same supplier and nature
// from when that is recorded (FY174).
type WithholdingDecision struct {
	ID uuid.UUID
	NewWithholdingDecision
	RecordedBy string
	RecordedAt time.Time
}

// WithholdingDecisions are the organisation's withholding decisions. Recording one is audited.
type WithholdingDecisions interface {
	Record(ctx context.Context, d NewWithholdingDecision) (WithholdingDecision, error)
	// InForce is the latest decision for the supplier and nature recorded at or before at, or since ever when at is
	// zero: ports.ErrNotFound when there is none.
	InForce(ctx context.Context, supplier uuid.UUID, nature string, at time.Time) (WithholdingDecision, error)
	// Of are a supplier's decisions, newest first.
	Of(ctx context.Context, supplier uuid.UUID) ([]WithholdingDecision, error)
}
