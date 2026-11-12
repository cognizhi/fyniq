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

// CodeRisk is the database's refusal of a risk version out of order, a review of an earlier version or setting no later
// review date, and of a version or review changed or deleted (FY309).
const CodeRisk = "STORAGE-RISK-01"

// RiskFields are what a version of a risk says (CMP-06, design D7).
type RiskFields struct {
	Title, Description string
	Owner              string // the role that answers for it: owner, administrator, controller, accountant, dpo or auditor
	Likelihood, Impact string // low, medium or high
	Mitigation         string
	ReviewMonths       int
	NextReviewOn       time.Time // a date
	State              string    // open or closed
}

// NewRisk is a risk to add at version 1: its reference (empty: the next, R and one more than the highest used), what it
// says, and who saved it when.
type NewRisk struct {
	Ref string
	RiskFields
	SavedBy string // the actor, as records name it: user:<id>
	At      time.Time
}

// RiskReview is a review of a risk's version: who reviewed it, when, a note, and the next review date it set.
type RiskReview struct {
	Version      int
	ReviewedBy   string // the actor, as records name it: user:<id>
	ReviewedAt   time.Time
	Note         string
	NextReviewOn time.Time
}

// Risk is a risk at its latest version, with its latest review. Its NextReviewOn is the review date the latest review
// of that version set, or else the version's own.
type Risk struct {
	ID        uuid.UUID
	Ref       string
	CreatedAt time.Time
	Version   int
	RiskFields
	SavedBy    string
	SavedAt    time.Time
	LastReview *RiskReview
}

// CodePolicy is the database's refusal of a draft revision or a version out of order, an acknowledgement of a version
// not published or given twice, and any change or deletion in the policy library (FY310).
const CodePolicy = "STORAGE-POLICY-01"

// PolicyDraft is a revision of the organisation's draft of a policy (CMP-07, design D8).
type PolicyDraft struct {
	Key      string
	Revision int
	Body     string
	SHA256   []byte
	SavedBy  string
	SavedAt  time.Time
}

// PublishedPolicy is a published version of a policy: it never changes.
type PublishedPolicy struct {
	ID          uuid.UUID
	Key         string
	Version     int
	Title, Body string
	SHA256      []byte
	From        string // fyniq-policy-draft@1, or "revision N" of the organisation's draft
	PublishedBy string
	PublishedAt time.Time
}

// PolicyAcknowledgement is a member's acknowledgement of a published version.
type PolicyAcknowledgement struct {
	Key     string
	Version int
	By      string // user:<id>
	At      time.Time
}

// PolicyLibrary is the organisation's policy library: each policy's latest draft revision, every published version and
// every acknowledgement.
type PolicyLibrary struct {
	Drafts           []PolicyDraft
	Versions         []PublishedPolicy
	Acknowledgements []PolicyAcknowledgement
}

// Compliance is what the Compliance Center's verifications read and record, in the organisation's unit of work.
type Compliance interface {
	// Policies is the policy library (CMP-07).
	Policies(ctx context.Context) (PolicyLibrary, error)
	// SavePolicyDraft saves revision of the policy's draft, audited as policy.draft_saved.
	SavePolicyDraft(ctx context.Context, d PolicyDraft) (PolicyDraft, error)
	// PublishPolicy publishes the policy's next version, audited as policy.published.
	PublishPolicy(ctx context.Context, v PublishedPolicy) (PublishedPolicy, error)
	// AcknowledgePolicy records a member's acknowledgement of a published version, audited as policy.acknowledged.
	AcknowledgePolicy(ctx context.Context, a PolicyAcknowledgement) error
	// Risks are the organisation's risks at their latest versions, in the order of their references (CMP-06).
	Risks(ctx context.Context) ([]Risk, error)
	// CreateRisk adds a risk at version 1, audited as risk.saved.
	CreateRisk(ctx context.Context, n NewRisk) (Risk, error)
	// ReviseRisk saves a risk's next version over version, audited as risk.saved; CodeVersion when version is not its
	// latest.
	ReviseRisk(ctx context.Context, id uuid.UUID, f RiskFields, savedBy string, at time.Time, version int) (Risk, error)
	// ReviewRisk records a review of the risk's latest version, which must be version, audited as risk.reviewed.
	ReviewRisk(ctx context.Context, id uuid.UUID, r RiskReview) (Risk, error)
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
