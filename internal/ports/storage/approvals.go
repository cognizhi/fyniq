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
	RequestedBy                               uuid.UUID
	RequestedAt                               time.Time
	RequesterMFAMethod                        string
	RequesterMFAAt                            time.Time
	ExpiresAt                                 time.Time
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
