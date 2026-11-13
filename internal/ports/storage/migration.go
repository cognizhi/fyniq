package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeMigration is the database's refusal of a migration's change (FY325, FY326, FY327): a migration started in an
// organisation with a posted entry, a step added to a closed migration, a step's report changed, a mapping version out of
// order, a sign-off of anything but the latest version, a workbook, step or mapping added once the migration has a
// commit, a commit not under its own approved request or not of the latest valid workbook and signed mapping, a record
// joining a commit that is not running, a commit committed before it holds every record, or anything removed.
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

// MigrationMapping is a version of a migration's mapping (§17.1 M2): the workbook's set-up mapped onto Fyniq's, as
// JSON, with the M1 step whose workbook it maps.
type MigrationMapping struct {
	ID      uuid.UUID
	RunID   uuid.UUID
	Version int
	StepID  uuid.UUID
	Mapping []byte
	SHA256  string
	MadeBy  string
	MadeAt  time.Time
}

// The commit's paths (§17.1 M3) and states.
const (
	PathHistory    = "history"
	PathCleanStart = "clean_start"

	CommitRunning   = "running"
	CommitCommitted = "committed"
)

// MigrationCommit is a migration's commit (M3, design D3, rev 0.258): recorded when its approval executes, running in
// batches until it holds every record it planned, then committed.
type MigrationCommit struct {
	ID          uuid.UUID
	RunID       uuid.UUID
	ApprovalID  uuid.UUID
	Path        string
	StepID      uuid.UUID
	MappingID   uuid.UUID
	Records     int // planned
	Status      string
	ApprovedBy  string
	ApprovedAt  time.Time
	CommittedAt *time.Time
}

// MigrationRecord is a record the commit imported: its business key from its row, what the row became, its sheet and
// row, and the Fyniq record it made.
type MigrationRecord struct {
	ID         uuid.UUID
	RunID      uuid.UUID
	CommitID   uuid.UUID
	Key        string
	Kind       string
	Sheet      string
	Row        int
	RecordType string
	RecordID   uuid.UUID
	ImportedBy string
	ImportedAt time.Time
}

// The things a migration's sign-off signs.
const (
	SignOffMapping        = "mapping"        // M2
	SignOffReconciliation = "reconciliation" // M4: the parity report and its register, over the report's hash (rev 0.266)
)

// MigrationDecision is the Accountant's decision on a line of the difference register (FR-MIG-02, design D8; rev
// 0.266): the line's key, its class and note, and the figures it was decided on, so a decision on figures that have
// since moved no longer counts. Deciding a line again takes its next version.
type MigrationDecision struct {
	ID        uuid.UUID
	RunID     uuid.UUID
	Line      string
	Class     string // a, b or c
	Note      string
	Fyniq     string // Fyniq's figure, or a check's statuses, when decided
	Workbook  string // the workbook's
	Version   int
	DecidedBy string
	DecidedAt time.Time
}

// MigrationSignOff is a sign-off of a migration's mapping, over the SHA-256 of the version signed (rev 0.256).
type MigrationSignOff struct {
	ID            uuid.UUID
	RunID         uuid.UUID
	Subject       string
	MappingID     *uuid.UUID
	SubjectSHA256 string
	SignedBy      string
	SignedAt      time.Time
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
	// Mapping is a migration's latest mapping: ports.ErrNotFound when it has none yet.
	Mapping(ctx context.Context, run uuid.UUID) (MigrationMapping, error)
	// Map keeps the mapping's next version, audited as migration.mapped: CodeMigration when m.Version is not the next,
	// or its step not a valid workbook of the migration.
	Map(ctx context.Context, m MigrationMapping) (MigrationMapping, error)
	// SignOffs are a migration's sign-offs, oldest first.
	SignOffs(ctx context.Context, run uuid.UUID) ([]MigrationSignOff, error)
	// SignOff keeps a sign-off, audited as migration.<subject>_signed: CodeMigration when it does not sign the latest
	// version over its hash.
	SignOff(ctx context.Context, s MigrationSignOff) (MigrationSignOff, error)
	// MappingVersion is a mapping version by its ID: ports.ErrNotFound when there is none.
	MappingVersion(ctx context.Context, id uuid.UUID) (MigrationMapping, error)
	// Commit is a migration's commit: ports.ErrNotFound before its approval executes.
	Commit(ctx context.Context, run uuid.UUID) (MigrationCommit, error)
	// StartCommit records a migration's commit as its approval executes, audited as migration.commit_approved:
	// CodeMigration when it is not under its own approved request, or not of the latest valid workbook and its
	// signed mapping; CodeDuplicate when the migration has one.
	StartCommit(ctx context.Context, c MigrationCommit) (MigrationCommit, error)
	// LockCommit is a commit, locked until the unit of work ends, so two batches never import the same record:
	// ports.ErrNotFound when there is none.
	LockCommit(ctx context.Context, id uuid.UUID) (MigrationCommit, error)
	// RecordByKey is a commit's record with a business key: ports.ErrNotFound when it is not imported yet.
	RecordByKey(ctx context.Context, commit uuid.UUID, key string) (MigrationRecord, error)
	// Records are a commit's imported records, in the order imported.
	Records(ctx context.Context, commit uuid.UUID) ([]MigrationRecord, error)
	// Import keeps a record the commit imported, in the record's own unit of work, audited as migration.record_imported:
	// CodeMigration when the commit is not running, CodeDuplicate when its key is imported already.
	Import(ctx context.Context, r MigrationRecord) (MigrationRecord, error)
	// Committed marks a running commit committed, audited as migration.committed: CodeMigration when it does not
	// hold every record it planned, CodeMigration when it is not running.
	Committed(ctx context.Context, id uuid.UUID, at time.Time) (MigrationCommit, error)
	// RecordOf is the migration record that made a record: ports.ErrNotFound when the import did not make it.
	RecordOf(ctx context.Context, recordType string, id uuid.UUID) (MigrationRecord, error)
	// Decisions are the run's decisions on its difference register's lines, by line.
	Decisions(ctx context.Context, run uuid.UUID) ([]MigrationDecision, error)
	// Decide decides a line of the register: the first decision of a line when d.Version is 0, else the line again at
	// that version, as its next (CodeVersion when it changed since). Audited as migration.difference_decided.
	Decide(ctx context.Context, d MigrationDecision) (MigrationDecision, error)
}
