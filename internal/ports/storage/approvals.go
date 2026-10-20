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
	Steps      []ApprovalStep
	RequestID  uuid.UUID
	CreatedBy  uuid.UUID // who asked for it
	CreatedAt  time.Time
}

// NewApprovalPolicy is what ApprovalPolicies.Add needs.
type NewApprovalPolicy struct {
	ObjectType string
	Version    int
	Steps      []ApprovalStep
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
