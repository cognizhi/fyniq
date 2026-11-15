package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeAgentRun is the database's refusal of an agent's run's step (FY350): one out of its course, by another role than
// the worker, a change to what the run is, a report written before it ends, or a delete (migration 000384).
const CodeAgentRun = "STORAGE-AGENT-RUN-01"

// The states of an agent's run (AI-65, AI-67).
const (
	AgentRunQueued  = "queued"
	AgentRunRunning = "running"
	AgentRunDone    = "done"
	AgentRunFailed  = "failed"
	AgentRunSkipped = "skipped"
)

// AgentRun is a scheduled agent's run (PRD-001 §9.12; p1-scheduled-agents design D3): the agent and its skill as
// pinned, what triggered it and the slot it was due at, the Owner it acts for, its state and, once done, its report.
type AgentRun struct {
	ID           uuid.UUID
	Agent        string // the skill's name, such as company-secretary
	SkillVersion int
	SkillSHA256  string
	Trigger      string // schedule:<entry>, event:<kind>:<key> or run_now
	SlotAt       time.Time
	ActsFor      uuid.UUID
	RequestedBy  *uuid.UUID // who chose Run now
	Status       string
	Reason       string
	QueuedAt     time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Report       *AgentReport // once done
	Version      int
}

// AgentReport is a run's report (AI-65): what it read, so the run can be reproduced; its findings; Fyniq's summary; and
// the model's words as written, when the model was asked.
type AgentReport struct {
	Fingerprint    string
	LedgerRevision int64
	PackVersions   map[string]string
	KBSnapshot     string
	Findings       json.RawMessage // an array of findings
	Summary        string
	ModelUsed      bool
	Model          string
	ModelText      string
	Tokens         int
	ToolCalls      int
}

// NewAgentRun is a run to queue for its slot.
type NewAgentRun struct {
	Agent        string
	SkillVersion int
	SkillSHA256  string
	Trigger      string
	SlotAt       time.Time
	ActsFor      uuid.UUID
	RequestedBy  *uuid.UUID
	QueuedAt     time.Time
}

// AgentRunQuery pages an organisation's runs, newest first.
type AgentRunQuery struct {
	Agent    string
	Statuses []string
	After    uuid.UUID
	Limit    int
}

// AgentRuns keeps an organisation's agents' runs. Each step is audited in its transaction, and takes the version it
// read: a run moved meanwhile is a version conflict. The worker queues, starts and ends runs; the API queues one for
// Run now and reads them.
type AgentRuns interface {
	// Queue keeps a slot's run once: a slot already queued gives the run it has, with queued false.
	Queue(ctx context.Context, r NewAgentRun) (run AgentRun, queued bool, err error)
	Get(ctx context.Context, id uuid.UUID) (AgentRun, error)
	List(ctx context.Context, q AgentRunQuery) ([]AgentRun, error)
	// LastDone is the agent's latest run that is done, or ports.ErrNotFound.
	LastDone(ctx context.Context, agent string) (AgentRun, error)
	Start(ctx context.Context, id uuid.UUID, version int, at time.Time) (AgentRun, error)
	Skip(ctx context.Context, id uuid.UUID, version int, reason string, at time.Time) (AgentRun, error)
	Fail(ctx context.Context, id uuid.UUID, version int, reason string, at time.Time) (AgentRun, error)
	Finish(ctx context.Context, id uuid.UUID, version int, report AgentReport, at time.Time) (AgentRun, error)
}
