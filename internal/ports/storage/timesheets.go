package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The statuses of a time entry and of a submitted week (FR-PRJ-05, FR-PRJ-07).
const (
	TimeDraft     = "draft"
	TimeSubmitted = "submitted"
	TimeApproved  = "approved"
	TimeReturned  = "returned"
)

// TimeEntry is a person's time on a project's task on a day, in whole minutes (FR-PRJ-02). A reversing entry has the
// opposite minutes of the approved entry it reverses; an approved entry never changes.
type TimeEntry struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	ProjectID    uuid.UUID
	TaskID       uuid.UUID
	WorkDate     time.Time
	Minutes      int
	Billable     bool
	Note         string
	Status       string
	SubmissionID *uuid.UUID
	ReversesID   *uuid.UUID
	Version      int
	CreatedAt    time.Time
	ChangedAt    time.Time
}

// NewTimeEntry is a draft entry as it is added: a reversing entry names the approved entry it reverses.
type NewTimeEntry struct {
	UserID     uuid.UUID
	ProjectID  uuid.UUID
	TaskID     uuid.UUID
	WorkDate   time.Time
	Minutes    int
	Billable   bool
	Note       string
	ReversesID *uuid.UUID
}

// TimeEdit changes a draft or returned entry at its version.
type TimeEdit struct {
	ID       uuid.UUID
	Version  int
	TaskID   uuid.UUID
	WorkDate time.Time
	Minutes  int
	Billable bool
	Note     string
}

// TimeRef names an entry at its version.
type TimeRef struct {
	ID      uuid.UUID
	Version int
}

// TimeWeekEdits are the changes of one save of a person's week: one audit event covers them all (design D2).
type TimeWeekEdits struct {
	Add    []NewTimeEntry
	Change []TimeEdit
	Remove []TimeRef
}

// TimeFilter narrows a list of entries; a zero field does not narrow. From and To are days, inclusive.
type TimeFilter struct {
	UserID       uuid.UUID
	ProjectID    uuid.UUID
	From, To     time.Time
	Status       string
	SubmissionID uuid.UUID
	Limit        int
}

// Submission is a person's week on one project, sent to the project's managers for approval (FR-PRJ-07). WeekStart is the
// Monday.
type Submission struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	ProjectID   uuid.UUID
	WeekStart   time.Time
	Status      string
	SubmittedAt time.Time
	DecidedBy   *uuid.UUID
	DecidedAt   *time.Time
	Comment     string
	Version     int
}

// SubmissionFilter narrows a list of submissions; a zero field does not narrow.
type SubmissionFilter struct {
	UserID    uuid.UUID
	ProjectID uuid.UUID
	Status    string
	Limit     int
}

// Timer is a person's running timer (FR-PRJ-05).
type Timer struct {
	UserID    uuid.UUID
	ProjectID uuid.UUID
	TaskID    uuid.UUID
	Billable  bool
	Note      string
	StartedAt time.Time
}

// Timesheets are the organisation's time entries, weekly submissions and running timers (FR-PRJ-02, FR-PRJ-05, FR-PRJ-07).
// Each write records its audit event under the person's week in the unit of work (AUD-01), one event for a saved week.
type Timesheets interface {
	// Entries are the entries the filter names, by day then creation, at most Limit (500 when zero).
	Entries(ctx context.Context, f TimeFilter) ([]TimeEntry, error)
	GetEntry(ctx context.Context, id uuid.UUID) (TimeEntry, error)
	// SaveWeek adds, changes and removes a person's draft and returned entries in one step, with one audit event:
	// CodeVersion when an entry changed since it was read, CodeTime when the database refuses one.
	SaveWeek(ctx context.Context, user uuid.UUID, week time.Time, e TimeWeekEdits) ([]TimeEntry, error)
	// Reverse adds the reversing draft entry of an approved entry.
	Reverse(ctx context.Context, id uuid.UUID) (TimeEntry, error)
	// Submit sends a person's draft and returned entries of a project's week, as one submission at the app's clock.
	// CodeTime when there are none.
	Submit(ctx context.Context, user, project uuid.UUID, week, at time.Time) (Submission, []TimeEntry, error)
	// Decide approves or returns a submission at its version, and its entries with it; decider must be a manager of the
	// project or the Controller, and never the person (the database repeats it, FY602).
	Decide(ctx context.Context, id uuid.UUID, version int, decider uuid.UUID, approve bool, comment string, at time.Time) (Submission, error)
	Submissions(ctx context.Context, f SubmissionFilter) ([]Submission, error)
	GetSubmission(ctx context.Context, id uuid.UUID) (Submission, error)
	// Timer is a person's running timer; ports.ErrNotFound when none runs.
	Timer(ctx context.Context, user uuid.UUID) (Timer, error)
	// StartTimer starts a person's timer; CodeTime when one runs, or the database refuses it.
	StartTimer(ctx context.Context, t Timer) error
	// StopTimer ends a person's timer and adds its time as a draft entry of minutes on day.
	StopTimer(ctx context.Context, user uuid.UUID, day time.Time, minutes int) (TimeEntry, error)
	// DropTimer ends a person's timer without an entry.
	DropTimer(ctx context.Context, user uuid.UUID) error
}

// CodeTime: an entry is a project member's, in whole minutes on a task in use, edited only while a draft or returned and
// never once approved, with a reversing entry the opposite of an approved one; a week is decided by a manager of the
// project or the Controller, never by the person (FR-PRJ-05, FR-PRJ-07; FY602, FY603, FY604).
const CodeTime = "STORAGE-TIME-01"
