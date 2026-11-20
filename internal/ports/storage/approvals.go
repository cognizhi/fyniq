package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ApprovalRequest is a change waiting for, or decided by, the approval engine (FR-APR-03, FR-APR-10): the
// envelope exactly as signed with its SHA-256, the change that executes, and where it stands.
type ApprovalRequest struct {
	ID                 uuid.UUID
	ObjectType         string
	ObjectKey          string // the setting's key, the membership's ID, or the invitation's code hash in hex
	Operation          string
	Summary            string
	Change             []byte // a JSON object: what executes on the final approval
	Envelope           string // RFC 8785 canonical JSON
	EnvelopeHash       []byte // SHA-256 of Envelope; the database recomputes it
	PolicyVersion      string
	PolicySteps        []ApprovalStep // the steps of the policy it started under, which it keeps
	RequestedBy        uuid.UUID
	RequestedAt        time.Time
	RequesterMFAMethod string
	RequesterMFAAt     time.Time
	ExpiresAt          time.Time
	Status             string // pending, approved, rejected, void or expired
	DecidedAt          *time.Time
	StatusReason       string
	Version            int
}

// NewApprovalRequest is what Approvals.Request needs.
type NewApprovalRequest struct {
	ObjectType, ObjectKey, Operation, Summary string
	Change                                    []byte
	Envelope                                  string
	EnvelopeHash                              []byte
	PolicyVersion                             string
	PolicySteps                               []ApprovalStep
	RequestedBy                               uuid.UUID
	RequestedAt                               time.Time
	RequesterMFAMethod                        string
	RequesterMFAAt                            time.Time
	ExpiresAt                                 time.Time
}

// ApprovalStep is one ordered step of an approval policy: Quorum people who hold the approve right on the object's
// Appendix D row sign, narrowed to those holding one of Roles, or to the members Users (never both); neither
// narrows nothing (FR-APR-01, rev 0.40).
type ApprovalStep struct {
	Quorum int         `json:"quorum"`
	Roles  []string    `json:"roles"`
	Users  []uuid.UUID `json:"users"`
}

// ApprovalRule is a rule of an approval policy (FR-APR-01, rev 0.178): its conditions and the steps a request that
// matches them takes.
type ApprovalRule struct {
	Conditions ApprovalConditions `json:"conditions"`
	Steps      []ApprovalStep     `json:"steps"`
}

// ApprovalConditions are a rule's conditions, each written out so the database compares them exactly: the amount at
// or above which it holds, "" for none; and the departments (dimension values), accounts and vendors it names, empty
// for none.
type ApprovalConditions struct {
	AmountAtLeast string      `json:"amount_at_least"`
	Departments   []uuid.UUID `json:"departments"`
	Accounts      []uuid.UUID `json:"accounts"`
	Vendors       []uuid.UUID `json:"vendors"`
}

// ApprovalSignature is a signed approval or rejection: the electronic signature of §14.2 (FR-APR-03).
type ApprovalSignature struct {
	ID           uuid.UUID
	RequestID    uuid.UUID
	Step         int
	Signer       uuid.UUID
	Decision     string // approve or reject
	EnvelopeHash []byte
	MFAMethod    string // session_step_up, passkey or totp
	MFAAt        time.Time
	SignedAt     time.Time
	Reason       string // a rejection's
	VoidedAt     *time.Time
	Passkey      *PasskeyAssertion // a passkey-signed approval's assertion; nil for any other
	OnBehalfOf   *uuid.UUID        // the delegator a delegate signed for (FR-APR-06); nil for a person's own signature
}

// PasskeyAssertion is what a passkey signed for an approval (FR-APR-03): kept with the signature so it can be verified
// again later against the passkey's public key.
type PasskeyAssertion struct {
	ChallengeID       uuid.UUID // the approval challenge it answered: 16 random bytes and the envelope's SHA-256
	PasskeyID         uuid.UUID
	AuthenticatorData []byte
	ClientData        []byte // the clientDataJSON
	Signature         []byte
}

// NewApprovalSignature is what Approvals.Sign needs.
type NewApprovalSignature struct {
	RequestID    uuid.UUID
	Step         int
	Signer       uuid.UUID
	Decision     string
	EnvelopeHash []byte
	MFAMethod    string
	MFAAt        time.Time
	SignedAt     time.Time
	Reason       string
	Passkey      *PasskeyAssertion // with MFAMethod passkey
	// SelectionSHA256 is the hash of the items of an AI proposal its approver approves, re-hashed (AI-32); "" for the whole.
	SelectionSHA256 string
	// OnBehalfOf is the delegator a delegate signs for (FR-APR-06); the database checks a live delegation on the day.
	OnBehalfOf *uuid.UUID
}

// Approvals are the organisation's approval requests and their signatures. Every change is audited. The
// database refuses a signature without a hash or a second factor, one whose hash is not the request's, one by
// the requester and one on a request not pending or expired (CodeApproval); it voids a pending request when its
// record changes.
type Approvals interface {
	// Request records a pending request: CodeApproval when one for the same object is pending already.
	Request(ctx context.Context, n NewApprovalRequest) (ApprovalRequest, error)
	// Get returns a request, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (ApprovalRequest, error)
	// LastApproved is the latest approved request to change the record, or ports.ErrNotFound.
	LastApproved(ctx context.Context, objectType, objectKey string) (ApprovalRequest, error)
	// Lock is Get with the request's row locked until commit: taken first by the command that decides it (ENG-47
	// step 1, as a document is).
	Lock(ctx context.Context, id uuid.UUID) (ApprovalRequest, error)
	// List returns the requests with status (every one when status is ""), oldest first.
	List(ctx context.Context, status string) ([]ApprovalRequest, error)
	// Signatures returns a request's signatures, in the order they were made.
	Signatures(ctx context.Context, requestID uuid.UUID) ([]ApprovalSignature, error)
	// Sign records a signature.
	Sign(ctx context.Context, s NewApprovalSignature) (ApprovalSignature, error)
	// Decide moves a pending request at version to status (approved, rejected, void or expired) at at, with a
	// reason: CodeVersion when it changed since.
	Decide(ctx context.Context, id uuid.UUID, version int, status, reason string, at time.Time) (ApprovalRequest, error)
}

// ApprovalPolicy is a version of an organisation's approval policy for an object type (FR-APR-01, FR-APR-09): its
// steps, and the approved request that made it.
type ApprovalPolicy struct {
	ID         uuid.UUID
	ObjectType string
	Version    int
	Steps      []ApprovalStep // its default steps
	Rules      []ApprovalRule // ordered: the first that matches a request decides its steps (rev 0.178)
	RequestID  uuid.UUID
	CreatedBy  uuid.UUID // who asked for it
	CreatedAt  time.Time
}

// NewApprovalPolicy is what ApprovalPolicies.Add needs.
type NewApprovalPolicy struct {
	ObjectType string
	Version    int
	Steps      []ApprovalStep
	Rules      []ApprovalRule
	RequestID  uuid.UUID
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
}

// ApprovalPolicies are the versions of the organisation's approval policies. The database adds a version only as
// the next, with the approved request that holds exactly its steps, never changes one (CodeApproval), and voids
// every pending request of its object type when one is added; Add audits the version and the voids.
type ApprovalPolicies interface {
	// List returns every version, by object type and version.
	List(ctx context.Context) ([]ApprovalPolicy, error)
	// Add records a version.
	Add(ctx context.Context, n NewApprovalPolicy) (ApprovalPolicy, error)
}

// ApprovalDelegation is an approver's delegation of their authority to another member for some days (FR-APR-06): it lends no right, it only makes
// the delegate eligible for a step the delegator could sign, as themselves, with the signature recording on whose behalf.
type ApprovalDelegation struct {
	ID        uuid.UUID
	Delegator uuid.UUID
	Delegate  uuid.UUID
	StartsOn  time.Time // the first day, a date
	EndsOn    time.Time // the last day, a date
	Reason    string
	Status    string // active or ended
	EndedAt   *time.Time
	EndedBy   *uuid.UUID
	Version   int
	CreatedAt time.Time
}

// NewApprovalDelegation is what ApprovalDelegations.Create needs: the delegator's own delegation.
type NewApprovalDelegation struct {
	Delegator, Delegate uuid.UUID
	StartsOn, EndsOn    time.Time
	Reason              string
	At                  time.Time
}

// ApprovalDelegations are the organisation's delegations of approval authority. The database refuses one to oneself, one over 90 days, one changed other than
// by being ended once, and a delete (CodeApproval). Every change is audited.
type ApprovalDelegations interface {
	// Create records a delegation.
	Create(ctx context.Context, n NewApprovalDelegation) (ApprovalDelegation, error)
	// Get returns a delegation, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (ApprovalDelegation, error)
	// Lock is Get with the row locked until commit.
	Lock(ctx context.Context, id uuid.UUID) (ApprovalDelegation, error)
	// List returns every delegation, oldest first.
	List(ctx context.Context) ([]ApprovalDelegation, error)
	// End ends an active delegation at version by user at at: CodeVersion when it changed since.
	End(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time) (ApprovalDelegation, error)
}

// ApprovalReminder is a reminder or the escalation of a pending approval that was sent (FR-APR-08).
type ApprovalReminder struct {
	Kind   string // reminder or escalation
	Number int    // a reminder's, from 1 to 3; an escalation's, 1
	SentAt time.Time
}

// ApprovalReminders are the log of the reminders and escalations sent for pending approvals. Only the worker records one, and nobody changes one: it
// notifies and never signs, decides or extends a request.
type ApprovalReminders interface {
	// Sent returns what was sent for a request, in order.
	Sent(ctx context.Context, requestID uuid.UUID) ([]ApprovalReminder, error)
	// Record logs one sent at at: CodeApproval when that one was recorded already.
	Record(ctx context.Context, requestID uuid.UUID, kind string, number int, at time.Time) error
}
