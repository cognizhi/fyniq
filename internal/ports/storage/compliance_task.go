package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeComplianceTask is the database's refusal of a compliance task's change (FY351): a second open task for a finding's
// key comes as a conflict instead; this is one by another role, a change to what it is about, a change once closed,
// or a delete (migration 000385).
const CodeComplianceTask = "STORAGE-COMPLIANCE-TASK-01"

// The states of a compliance task (AI-65).
const (
	TaskOpen      = "open"
	TaskDone      = "done"      // a person did it
	TaskDismissed = "dismissed" // a person dismissed it, with why
	TaskCleared   = "cleared"   // its finding cleared on a later run
)

// ComplianceTask is what a person must do about a finding (PRD-001 AI-65; p1-scheduled-agents design D7): its owner, a
// role or a member, and its due date; raised by a run's finding or an approved proposal; one open per finding's key.
type ComplianceTask struct {
	ID         uuid.UUID
	FindingKey string
	Title      string
	Detail     string
	OwnerRole  string
	OwnerUser  *uuid.UUID
	DueOn      *time.Time // a date
	Escalated  bool
	Source     string // agent_run, ai_proposal or system
	Run        *uuid.UUID
	Proposal   *uuid.UUID
	Status     string
	Reason     string
	CreatedAt  time.Time
	ClosedAt   *time.Time
	ClosedBy   *uuid.UUID
	Version    int
}

// NewComplianceTask is a task to raise for a finding.
type NewComplianceTask struct {
	FindingKey string
	Title      string
	Detail     string
	OwnerRole  string
	OwnerUser  *uuid.UUID
	DueOn      *time.Time
	Escalated  bool
	CreatedAt  time.Time
}

// TaskChange is what a repeat run updates on an open task.
type TaskChange struct {
	Title     string
	Detail    string
	OwnerRole string
	OwnerUser *uuid.UUID
	DueOn     *time.Time
	Escalated bool
}

// ComplianceTaskQuery pages an organisation's tasks, newest first: in these states (any when empty), and whose finding's
// key starts with KeyPrefix.
type ComplianceTaskQuery struct {
	Statuses  []string
	KeyPrefix string
	After     uuid.UUID
	Limit     int
}

// ComplianceTasks keeps an organisation's compliance tasks. Each change is audited in its transaction, and takes the
// version it read. The worker raises a run's tasks, updates and clears them; the executor raises an approved
// proposal's; the API closes one for a person.
type ComplianceTasks interface {
	// RaiseForRun raises run's task for a finding: a conflict when its key has an open task already.
	RaiseForRun(ctx context.Context, run uuid.UUID, t NewComplianceTask) (ComplianceTask, error)
	// RaiseForProposal raises an approved proposal's task: a conflict when its key has an open task already.
	RaiseForProposal(ctx context.Context, proposal uuid.UUID, t NewComplianceTask) (ComplianceTask, error)
	// RaiseSystem raises the platform's own task for a finding no run and no proposal made, such as a statutory pack update waiting (ENG-33): the worker's; a conflict when its key has an open task already.
	RaiseSystem(ctx context.Context, t NewComplianceTask) (ComplianceTask, error)
	Get(ctx context.Context, id uuid.UUID) (ComplianceTask, error)
	// OpenFor is the finding's open task, or ports.ErrNotFound.
	OpenFor(ctx context.Context, findingKey string) (ComplianceTask, error)
	List(ctx context.Context, q ComplianceTaskQuery) ([]ComplianceTask, error)
	Update(ctx context.Context, id uuid.UUID, version int, c TaskChange) (ComplianceTask, error)
	Clear(ctx context.Context, id uuid.UUID, version int, at time.Time) (ComplianceTask, error)
	// Close marks the task done, or dismissed with reason, for by.
	Close(ctx context.Context, id uuid.UUID, version int, status, reason string, by uuid.UUID, at time.Time) (ComplianceTask, error)
}
