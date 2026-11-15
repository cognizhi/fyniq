package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeAIProposal is the database's refusal of an AI proposal's step (FY342): one out of its lifecycle, by another role
// than the step's, changing what was proposed or a decision made, or a delete (PRD-001 §9.5; p1-ai-proposals-core task
// 2.1).
const CodeAIProposal = "STORAGE-AI-PROPOSAL-01"

// The proposal's states (§9.5, "Proposal lifecycle").
const (
	ProposalDraft      = "draft"
	ProposalInvalid    = "invalid"
	ProposalPending    = "pending"
	ProposalApproved   = "approved"
	ProposalRejected   = "rejected"
	ProposalExpired    = "expired"
	ProposalSuperseded = "superseded"
	ProposalExecuting  = "executing"
	ProposalExecuted   = "executed"
	ProposalStale      = "stale"
	ProposalFailed     = "failed"
)

// AIEvidence is a record or document a proposal relied on: its kind, ID and title.
type AIEvidence struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// AIProposal is a proposal (§9.5): what it proposes, why, its dry run, its tier and originator, its decision and its
// execution. Payload is the exact JSON that executes; PayloadSHA256 its hash in hex, which the database checks.
type AIProposal struct {
	ID                uuid.UUID
	TaskType          string
	TaskVersion       int
	TaskSHA256        string
	Payload           string
	PayloadSHA256     string
	ItemCount         int
	Rationale         string
	Evidence          []AIEvidence
	Impact            json.RawMessage
	BaseVersions      map[string]int64
	RiskTier          int
	RequiredApprovals int
	// The originator: the agent, the person it acted for, the turn or the agent's run, the model, the prompt and the
	// tools' pinned hash.
	Agent       string
	RequestedBy uuid.UUID
	Turn        *uuid.UUID
	Run         *uuid.UUID
	Model       string
	Prompt      string
	ToolsSHA256 string

	Status          string
	Reason          string
	ApprovalRequest *uuid.UUID
	DecidedBy       *uuid.UUID
	DecidedAt       *time.Time
	ApprovedItems   []int
	ApprovedPayload string
	ApprovedSHA256  string
	SupersededBy    *uuid.UUID
	Execution       json.RawMessage
	StartedAt       *time.Time
	FinishedAt      *time.Time
	ProposedAt      time.Time
	ExpiresAt       time.Time
	Version         int
}

// AIProposalQuery narrows a list of proposals: by status, by the person they acted for, by the turn; After pages by
// ID, newest first.
type AIProposalQuery struct {
	Statuses    []string
	RequestedBy *uuid.UUID
	Turn        *uuid.UUID
	After       uuid.UUID
	Limit       int
}

// AIProposals keeps an organisation's AI proposals. Each step is audited in its transaction, and takes the version it
// read: a proposal moved meanwhile is ports.ErrConflict. Each step runs as its role: the API makes, validates, decides
// and supersedes; the worker expires; the executor runs (design D2).
type AIProposals interface {
	// Create keeps a new Draft.
	Create(ctx context.Context, p AIProposal) (AIProposal, error)
	Get(ctx context.Context, id uuid.UUID) (AIProposal, error)
	List(ctx context.Context, q AIProposalQuery) ([]AIProposal, error)
	// Invalid ends a Draft whose validation or dry run failed, with why.
	Invalid(ctx context.Context, id uuid.UUID, version int, reason string) (AIProposal, error)
	// Pending keeps a Draft's dry run, the versions it read and its approval request.
	Pending(ctx context.Context, id uuid.UUID, version int, impact json.RawMessage, base map[string]int64, request *uuid.UUID) (AIProposal, error)
	// Approve records a person's approval of the payload, or of a batch's items, re-hashed.
	Approve(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, items []int, payload string, at time.Time) (AIProposal, error)
	// Reject records a person's rejection, with their reason.
	Reject(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, reason string, at time.Time) (AIProposal, error)
	// Supersede ends a Pending proposal that a revision replaces.
	Supersede(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time) (AIProposal, error)
	// Expire ends a Pending proposal past its expiry; the worker's.
	Expire(ctx context.Context, id uuid.UUID, version int, at time.Time) (AIProposal, error)
	// Start moves an Approved proposal to Executing; the executor's.
	Start(ctx context.Context, id uuid.UUID, version int, at time.Time) (AIProposal, error)
	// Finish ends an Executing proposal: Executed, Stale or Failed, with the execution's record and, unless Executed,
	// why; the executor's.
	Finish(ctx context.Context, id uuid.UUID, version int, status, reason string, execution json.RawMessage, at time.Time) (AIProposal, error)
}
