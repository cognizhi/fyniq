package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// The report builder (PRD-001 FR-RPT-04, FR-RPT-07; p2-reporting-planning design D1 to D3): saved definitions and their schedules.

// CodeReportDefinition is the database's refusal of a definition changed other than as its next version, or deleted (FYR01).
const CodeReportDefinition = "STORAGE-RDEF-01"

// CodeReportSchedule is the database's refusal of a schedule changed other than as its next version, or deleted (FYR02).
const CodeReportSchedule = "STORAGE-RSCH-01"

// CodeReportScheduleRun is the database's refusal of a recorded run changed or deleted (FYR03).
const CodeReportScheduleRun = "STORAGE-RSCH-02"

// CodeReportName is the refusal of a second live definition with a name already used (report_definition_name_live).
const CodeReportName = "STORAGE-RDEF-02"

// The visibilities of a definition.
const (
	ReportPrivate = "private"
	ReportShared  = "shared"
)

// ReportDefinition is a saved report at its current version.
type ReportDefinition struct {
	ID          uuid.UUID
	Name        string
	Description string
	Definition  json.RawMessage
	Visibility  string
	OwnerID     uuid.UUID
	Version     int
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedBy   string
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

// ReportDefinitions are the saved reports.
type ReportDefinitions interface {
	// Get is a definition, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (ReportDefinition, error)
	// List are the live definitions (not archived), by name; with archived, the archived ones too.
	List(ctx context.Context, archived bool) ([]ReportDefinition, error)
	// Create saves a definition at version 1; a name already used by a live one is CodeReportName.
	Create(ctx context.Context, d ReportDefinition) (ReportDefinition, error)
	// Replace saves a change as the definition's next version after version, refused with CodeVersion when it is no longer at version.
	Replace(ctx context.Context, d ReportDefinition, version int) (ReportDefinition, error)
	// Archive ends a definition after version; its schedules stop with it.
	Archive(ctx context.Context, id uuid.UUID, by string, at time.Time, version int) (ReportDefinition, error)
}

// The cadences and period rules of a schedule.
const (
	CadenceWeekly     = "weekly"
	CadenceMonthly    = "monthly"
	PeriodLastClosed  = "last_closed_month"
	PeriodMonthToDate = "month_to_date"
	ChannelEmail      = "email"
	ChannelSlack      = "slack"
)

// ReportSchedule says when a definition is sent and to whom.
type ReportSchedule struct {
	ID           uuid.UUID
	DefinitionID uuid.UUID
	Cadence      string
	Weekday      *int
	DayOfMonth   *int
	PeriodRule   string
	Channel      string
	Recipients   []uuid.UUID
	SlackChannel string
	Active       bool
	NextRunOn    time.Time
	LastRunOn    *time.Time
	Version      int
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ReportScheduleRun is one run: what was sent and whom it skipped.
type ReportScheduleRun struct {
	ID             uuid.UUID
	ScheduleID     uuid.UUID
	RanOn          time.Time
	PeriodLabel    string
	LedgerRevision int64
	Sent           int
	Skipped        json.RawMessage // [{"user_id":"…","why":"…"}]
	RanAt          time.Time
}

// ReportSchedules are the report builder's schedules and their runs.
type ReportSchedules interface {
	Get(ctx context.Context, id uuid.UUID) (ReportSchedule, error)
	// ForDefinition are a definition's schedules, oldest first.
	ForDefinition(ctx context.Context, definition uuid.UUID) ([]ReportSchedule, error)
	// Due are the active schedules whose next run is on or before day, of live definitions.
	Due(ctx context.Context, day time.Time) ([]ReportSchedule, error)
	Create(ctx context.Context, s ReportSchedule) (ReportSchedule, error)
	Replace(ctx context.Context, s ReportSchedule, version int) (ReportSchedule, error)
	// Ran records a run and moves the schedule on to next, as the worker does: the run once per schedule and day.
	Ran(ctx context.Context, run ReportScheduleRun, next time.Time, version int) (ReportSchedule, error)
	// Runs are a schedule's runs, newest first.
	Runs(ctx context.Context, schedule uuid.UUID, limit int) ([]ReportScheduleRun, error)
}
