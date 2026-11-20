package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodePayrollRun is the database refusing a pay run's receipt: an envelope used before, a version received before, a payroll entry with no
// receipt from its own transaction, or a run approved and prepared by one person (PRD-001 FR-PPL-09, FR-PPL-11; p1-payroll-link task 4.1).
const CodePayrollRun = "STORAGE-PAYROLL-RUN-01"

// PayrollMapping is a version of what each department's salary and each kind of deduction post to (FR-EMP-02, design D8): keys are
// department:<dimension value ID> and deduction:<kind>, values the accounts.
type PayrollMapping struct {
	ID      uuid.UUID
	Version int
	Entries map[string]uuid.UUID
	SetBy   string
	SetAt   time.Time
}

// PayrollRunReceipt is what Fyniq received and posted of one version of a run: who prepared and approved it, the mapping it was calculated
// against, the entry it posted and the run's posted totals by line afterwards. It keeps no per-person figure.
type PayrollRunReceipt struct {
	ID                              uuid.UUID
	LinkID                          uuid.UUID
	Epoch                           int
	SourceSystem                    string
	RunID                           uuid.UUID
	Version                         int
	WageMonth                       time.Time
	EnvelopeSHA256                  string
	ApproverIssuer, ApproverSubject string
	PreparerIssuer, PreparerSubject string
	MappingVersion                  int
	EntryID                         uuid.UUID // none when the version changed nothing
	Totals                          map[string]string
	ReceivedAt                      time.Time
}

// PayrollRuns are the pay-run mapping and the receipts of pay runs. Every change is audited.
type PayrollRuns interface {
	// Mapping is the current mapping, or ports.ErrNotFound when none is set.
	Mapping(ctx context.Context) (PayrollMapping, error)
	// SetMapping records the next version of the mapping, audited as payroll_mapping.set.
	SetMapping(ctx context.Context, entries map[string]uuid.UUID, by string, at time.Time) (PayrollMapping, error)
	// LatestReceipt is the receipt of the highest version received of a run, or ports.ErrNotFound.
	LatestReceipt(ctx context.Context, source string, run uuid.UUID) (PayrollRunReceipt, error)
	// ReceiptMonths are the wage months a pay run was posted for, earliest first.
	ReceiptMonths(ctx context.Context) ([]time.Time, error)
	// EnvelopeUsed reports whether an approval envelope's digest was received before.
	EnvelopeUsed(ctx context.Context, sha string) (bool, error)
	// Receive records a receipt, audited as payroll_run.received. CodePayrollRun when the database refuses it.
	Receive(ctx context.Context, r PayrollRunReceipt) (PayrollRunReceipt, error)
}
