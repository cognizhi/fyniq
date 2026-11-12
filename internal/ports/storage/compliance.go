package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ControlRun is one run of a verification of the Compliance Center (CMP-01, p1-compliance-center D4): the control and
// check of the registry it answers for, the window it read, whether it found nothing, and what it found (the first 50
// items as JSON, and how many there were).
type ControlRun struct {
	ID        uuid.UUID
	Control   string // CTL-FIN-08
	Frequency string // N
	Check     string // verify payments.dual-control
	Since     time.Time
	RanAt     time.Time
	OK        bool
	Count     int
	Items     []byte
	Detail    string
}

// DualControlApproval is an approval of a payment batch or of a bank change (CTL-FIN-08): how many signed it, and how
// many of them prepared it or asked for it.
type DualControlApproval struct {
	ID            uuid.UUID
	ObjectType    string
	ObjectKey     string
	DecidedAt     time.Time
	Signatures    int
	OwnSignatures int
}

// DualControlRelease is a payment batch's release (CTL-FIN-08): its approval and that approval's signatures, or its
// break-glass and whether the break-glass's Owner used it for this batch.
type DualControlRelease struct {
	BatchID        uuid.UUID
	ReleasedAt     time.Time
	ApprovalID     *uuid.UUID
	BreakGlassID   *uuid.UUID
	Approved       bool
	Signatures     int
	OwnSignatures  int
	BreakGlassUsed bool
}

// MemberSecondFactor is an active member's second factors (CTL-IAM-01, SEC-01): their passkeys, their sign-ins that
// passed a second factor, and their sessions seen in the window that passed none.
type MemberSecondFactor struct {
	UserID          uuid.UUID
	Passkeys        int
	SessionsWith    int
	SessionsWithout int
	LastWithout     *time.Time
}

// UnreconciledRead is a report export without its audit event, or an audited read without its request details
// (CTL-AUD-05).
type UnreconciledRead struct {
	Kind      string // report_export, or the event's action
	ObjectKey string
	At        time.Time
	Reason    string
}

// ControlState is a control's status for the organisation as compliance.run last worked it out (CMP-01, design D1): its
// words, each check's status and words as JSON, when it last turned to fail or overdue (while it is), and when the run
// was and the status last changed.
type ControlState struct {
	ID           uuid.UUID
	Control      string
	Status       string // pass, fail, overdue, not_applicable or not_assessed
	Detail       string
	Checks       []byte
	FailingSince *time.Time
	RunAt        time.Time
	ChangedAt    time.Time
	Version      int
}

// Compliance is what the Compliance Center's verifications read and record, in the organisation's unit of work.
type Compliance interface {
	// States are the controls' latest states.
	States(ctx context.Context) ([]ControlState, error)
	// SaveState writes a control's state at version (0: its first), auditing a change of status (AUD-01):
	// control.failed when it turns to fail or overdue, control.recovered when it leaves them, control.status_changed
	// otherwise.
	SaveState(ctx context.Context, s ControlState, version int) (ControlState, error)
	// RecordRun appends a verification's run; a run never changes (FY307).
	RecordRun(ctx context.Context, r ControlRun) (ControlRun, error)
	// LatestRuns are the latest run of each control's verifications.
	LatestRuns(ctx context.Context) ([]ControlRun, error)
	// DualControl are the approvals decided and the batches released since.
	DualControl(ctx context.Context, since time.Time) ([]DualControlApproval, []DualControlRelease, error)
	// SecondFactors are the active members' second factors, with their sessions seen since without one.
	SecondFactors(ctx context.Context, since time.Time) ([]MemberSecondFactor, error)
	// UnreconciledReads are the report exports asked for since without their audit event, and the latest sample events
	// of each action of actions since without their request details.
	UnreconciledReads(ctx context.Context, since time.Time, actions []string, sample int) ([]UnreconciledRead, error)
}
