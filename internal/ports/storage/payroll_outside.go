package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The items of the payroll go-live gate (PRD-001 FR-PAY-15): how a month's statutory outputs are met.
const (
	ArrangePayslips = "payslips"    // itemised payslips (FR-PAY-08): a provider, Nemura or a documented manual process
	ArrangeKETs     = "kets"        // key employment terms for new employees
	ArrangeCPF      = "cpf_sdl_shg" // CPF, SDL and SHG submission and payment by the due date
	ArrangeAIS      = "ais"         // the Auto-Inclusion Scheme, where it applies
	ArrangeIR21     = "ir21"        // IR21, where it applies
)

// The methods an arrangement is met by.
const (
	MethodNemura        = "nemura"
	MethodProvider      = "provider"
	MethodManual        = "manual"
	MethodNotApplicable = "not_applicable"
)

// PayrollArrangement is how one item of the gate is met from a month, with the evidence that shows it (FR-DOC-05).
type PayrollArrangement struct {
	ID             uuid.UUID
	Item, Method   string
	Provider, Note string
	EvidenceID     uuid.UUID // uuid.Nil for Nemura's own
	EffectiveFrom  time.Time
	RecordedBy     string
	RecordedAt     time.Time
}

// PayrollImport is a provider's payroll journal or a file import for a pay period and version, kept as the draft journal it made (FR-PPL-05, NM-EXT-03).
type PayrollImport struct {
	ID, JournalID uuid.UUID
	SourceSystem  string
	Period        time.Time
	Version       int
	FileSHA256    string
	LineCount     int
	Totals        map[string]string // account|values -> signed base amount, the period's totals after this version
	CreatedBy     string
	CreatedAt     time.Time
}

// PayrollOutside are the arrangements of the go-live gate and the imports of payroll that is not run in Nemura. Both are append-only and audited.
type PayrollOutside interface {
	// Arrangements are the arrangement of each item in force for a month: the latest that holds from a month no later than it.
	Arrangements(ctx context.Context, month time.Time) ([]PayrollArrangement, error)
	// ArrangementHistory is every arrangement ever recorded, newest first.
	ArrangementHistory(ctx context.Context) ([]PayrollArrangement, error)
	// Arrange records an arrangement, audited as payroll_arrangement.recorded (the item, method and evidence's ID).
	Arrange(ctx context.Context, a PayrollArrangement) (PayrollArrangement, error)
	// Import records an import, audited as payroll_import.recorded (the system, period, version, file hash and counts, never a figure).
	Import(ctx context.Context, i PayrollImport) (PayrollImport, error)
	// LatestImport is the highest version imported of a system's period, or ports.ErrNotFound.
	LatestImport(ctx context.Context, system string, period time.Time) (PayrollImport, error)
	// ImportVersion is one version, or ports.ErrNotFound.
	ImportVersion(ctx context.Context, system string, period time.Time, version int) (PayrollImport, error)
	// Imports are the imports, newest period first.
	Imports(ctx context.Context) ([]PayrollImport, error)
}
