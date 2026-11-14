package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeMigration is the database's refusal of a migration's change (FY325): a migration started in an organisation with
// a posted entry, a step added to a closed migration, a step's report changed, or anything removed.
const CodeMigration = "STORAGE-MIGRATION-01"

// The migration's steps (PRD-001 §17.1) and a step's states (p1-workbook-migration design D11).
const (
	StepWorkbook = "M1" // the frozen workbook uploaded and validated

	StepValidating = "validating"
	StepValid      = "valid"
	StepInvalid    = "invalid"
)

// The migration's file kinds.
const (
	MigrationWorkbook = "workbook" // as uploaded
	MigrationChecks   = "checks"   // the Checks export made from it
)

// MigrationRun is the organisation's move from the workbook (FR-MIG-01, §17.1): one per organisation, started only
// in one without a posted entry (rev 0.254).
type MigrationRun struct {
	ID        uuid.UUID
	Status    string // open; closed at M6
	StartedBy string
	StartedAt time.Time
}

// MigrationFile is a file a migration keeps: a workbook as uploaded, or the Checks export made from it.
type MigrationFile struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	Kind           string
	FileName       string
	MediaType      string
	Size           int64
	SHA256         string // of the file as kept
	OriginalSHA256 string // of the file as received
	StorageKey     string
	DataClass      DataClass // payroll for a workbook, general for a Checks export
	AddedBy        string
	AddedAt        time.Time
}

// MigrationStep is an attempt at one of a migration's steps: M1's is a workbook validated in the background.
type MigrationStep struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	Step           string
	State          string
	WorkbookFileID uuid.UUID
	StartedBy      string
	StartedAt      time.Time
	// Once validated: the report and its SHA-256, its counts, when, and the Checks export.
	ChecksFileID *uuid.UUID
	Report       []byte
	ReportSHA256 string
	Errors       int
	Warnings     int
	ValidatedAt  *time.Time
}

// MigrationReport is a validating step's report, as the validation recorded it.
type MigrationReport struct {
	State        string // valid or invalid
	ChecksFileID uuid.UUID
	Report       []byte // JSON
	ReportSHA256 string
	Errors       int
	Warnings     int
	ValidatedAt  time.Time
}

// Migrations keep the organisation's migration from the workbook; every change is audited on its run (AUD-01).
type Migrations interface {
	// Posted says whether the organisation holds a posted entry: a migration starts only in one that holds none.
	Posted(ctx context.Context) (bool, error)
	// Start starts the organisation's migration, audited as migration.started: CodeMigration when it holds a posted
	// entry, CodeDuplicate when it has one already.
	Start(ctx context.Context, by string, at time.Time) (MigrationRun, error)
	// Runs are the organisation's migrations: at most one.
	Runs(ctx context.Context) ([]MigrationRun, error)
	// Run is a migration: ports.ErrNotFound when there is none.
	Run(ctx context.Context, id uuid.UUID) (MigrationRun, error)
	// Files are a migration's files, oldest first.
	Files(ctx context.Context, run uuid.UUID) ([]MigrationFile, error)
	// Steps are a migration's steps, oldest first.
	Steps(ctx context.Context, run uuid.UUID) ([]MigrationStep, error)
	// Step is a step: ports.ErrNotFound when there is none.
	Step(ctx context.Context, id uuid.UUID) (MigrationStep, error)
	// AddWorkbook keeps a workbook on its migration and starts a step validating it, audited as
	// migration.workbook_received: CodeMigration when the migration is closed, CodeDuplicate while that step validates
	// another.
	AddWorkbook(ctx context.Context, step string, f MigrationFile) (MigrationStep, MigrationFile, error)
	// Validated keeps a validating step's report with the Checks export made for it, audited as migration.validated;
	// CodeMigration when the step is no longer validating.
	Validated(ctx context.Context, id uuid.UUID, checks MigrationFile, r MigrationReport) (MigrationStep, error)
}
