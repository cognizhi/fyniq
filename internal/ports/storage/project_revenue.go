package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The ways a fixed-fee project recognises its revenue (FR-PRJ-11): as each milestone is invoiced, or over time by cost-to-cost progress.
const (
	RevenueMilestone = "milestone"
	RevenueOverTime  = "over_time"
)

// ProjectRevenueElection is the accountant's choice for one project, and the estimated total cost an over-time project is measured against.
type ProjectRevenueElection struct {
	ProjectID     uuid.UUID
	Method        string
	EstimatedCost *Decimal // in the project's currency; set for over_time
	// ExcludedCost is the running total of the project's posted costs left out of its progress, with why (the input method leaves out what does not show progress); zero and "" for none.
	ExcludedCost   Decimal
	ExcludedReason string
	Version        int
	SetBy          string
	SetAt          time.Time
}

// ProjectRevenueRun is a month's proposed journal for a project: made once, never changed.
type ProjectRevenueRun struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	Month      time.Time
	JournalID  uuid.UUID
	Adjustment Decimal
	Earned     Decimal
	CreatedBy  string
	CreatedAt  time.Time
}

// NewProjectRevenueRun is a run to record.
type NewProjectRevenueRun struct {
	ProjectID  uuid.UUID
	Month      time.Time
	JournalID  uuid.UUID
	Adjustment Decimal
	Earned     Decimal
	At         time.Time
}

// ProjectRevenue keeps the elections and the monthly runs. Each change is audited in the same transaction (AUD-01).
type ProjectRevenue interface {
	// Election is a project's election; ports.ErrNotFound when none was made (it recognises as each milestone is invoiced).
	Election(ctx context.Context, project uuid.UUID) (ProjectRevenueElection, error)
	// Elect makes or changes the election; version is the one read (0 when none): CodeVersion when it moved on.
	Elect(ctx context.Context, project uuid.UUID, version int, method string, estimated *Decimal, excluded Decimal, reason string, at time.Time) (ProjectRevenueElection, error)
	// Elections are every election, by project.
	Elections(ctx context.Context) ([]ProjectRevenueElection, error)
	// Runs are a project's runs, oldest month first.
	Runs(ctx context.Context, project uuid.UUID) ([]ProjectRevenueRun, error)
	// Record notes a month's run; CodeInvalid when the project's month has one already.
	Record(ctx context.Context, n NewProjectRevenueRun) (ProjectRevenueRun, error)
}
