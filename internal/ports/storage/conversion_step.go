package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The ACRA steps of a conversion to shares (FR-EQ-09; p1-founders-money design D12).
const (
	StepResolved        = "resolved"         // the shareholders resolved to issue the shares
	StepApprovalLodged  = "approval_lodged"  // their approval was lodged with ACRA (s161(5), s186: within 14 days)
	StepAGMDue          = "agm_due"          // the date by which the next AGM must be held, when it differs from the one worked out
	StepAllotmentLodged = "allotment_lodged" // the return of allotment was lodged (s63, s63B)
)

// ConversionStep is one ACRA step of an approved conversion, as recorded: its kind, the day, ACRA's reference and who
// recorded it. A later step of a kind supersedes the earlier one, which stays.
type ConversionStep struct {
	ID           uuid.UUID
	SettlementID uuid.UUID
	Kind         string
	On           time.Time
	Reference    string
	RecordedBy   uuid.UUID
	RecordedAt   time.Time
}

// ConversionSteps keep the ACRA steps of conversions. Record writes a step, audited on its settlement; the database
// refuses one for a settlement that is not an approved conversion waiting for ACRA, and any change or deletion
// (CodeConversionStep).
type ConversionSteps interface {
	Record(ctx context.Context, s ConversionStep) (ConversionStep, error)
	// Of are a settlement's steps, earliest recorded first.
	Of(ctx context.Context, settlementID uuid.UUID) ([]ConversionStep, error)
}

// The steps an equity transaction records besides a conversion's (FR-EQ-09, FR-EQ-17; p1-equity-register design D4): a
// transfer's instrument signed and notice lodged, and a conversion between classes lodged. A cash issue takes a
// founder's conversion's steps.
const (
	StepInstrumentSigned = "instrument_signed"
	StepTransferLodged   = "transfer_lodged"
	StepConversionLodged = "conversion_lodged"
)

// EquityStep is one ACRA step of an equity transaction, as it happened. A later one of a kind supersedes it; both stay.
type EquityStep struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	Kind          string
	On            time.Time
	Reference     string
	RecordedBy    uuid.UUID
	RecordedAt    time.Time
}

// EquitySteps keep the ACRA steps of equity transactions. Record writes a step, audited on its transaction; the database
// refuses one of a kind its transaction does not take, one for a transaction that is not approved and waiting for ACRA,
// and any change or deletion (CodeEquityStep).
type EquitySteps interface {
	Record(ctx context.Context, s EquityStep) (EquityStep, error)
	// Of are a transaction's steps, earliest recorded first.
	Of(ctx context.Context, transactionID uuid.UUID) ([]EquityStep, error)
}
