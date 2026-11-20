package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// InvoiceSubmission is a document's submission through InvoiceNow, one for each version of the document (FR-TAX-10).
type InvoiceSubmission struct {
	ID, DocumentID, DocumentUUID uuid.UUID
	Version                      int
	Kind                         string // peppol, extracted_sale, extracted_purchase or aggregated
	Direction                    string // sale or purchase
	Status                       string // queued, sent, acknowledged or failed
	Recipient                    string // the buyer's participant ID, for a Peppol submission
	TransmissionID               string
	IRASAck                      string
	LastError                    string
	PayloadSHA256                []byte
	CreatedAt, UpdatedAt         time.Time
	CreatedBy                    string
}

// NewInvoiceSubmission queues the next version of a document's submission. The store gives it the next version number, and the
// document's UUID: the first version's for every later one.
type NewInvoiceSubmission struct {
	DocumentID uuid.UUID
	// DocumentUUID is the UUID the document was built with, kept when this is its first version; a later version keeps the first's.
	DocumentUUID  uuid.UUID
	Kind          string
	Direction     string
	Recipient     string
	PayloadSHA256 []byte
	At            time.Time
}

// InvoiceSubmissionMove is a change of a submission's status, with what the access point or IRAS said.
type InvoiceSubmissionMove struct {
	Status         string
	TransmissionID string // kept when empty
	IRASAck        string
	Error          string
	At             time.Time
}

// InvoiceSubmissionFilter narrows a page of submissions, newest first; After is the cursor.
type InvoiceSubmissionFilter struct {
	Status     string
	DocumentID *uuid.UUID
	After      *uuid.UUID
	Limit      int
}

// InvoiceEvent is one entry of a submission's history.
type InvoiceEvent struct {
	At     time.Time
	Status string
	Detail string
	Actor  string
}

// InvoiceExclusion is a document left out of the submissions, with its reason (FR-TAX-10).
type InvoiceExclusion struct {
	ID, DocumentID uuid.UUID
	Reason, Detail string
	RecordedBy     string
	RecordedAt     time.Time
	WithdrawnAt    *time.Time
	WithdrawnBy    string
}

// InvoiceContingency is the decision to file a return with a gap in its submissions (FR-TAX-11).
type InvoiceContingency struct {
	ID                   uuid.UUID
	PeriodFrom, PeriodTo time.Time
	Reason, Evidence     string
	RequestedBy          string
	RequestedAt          time.Time
	ApprovedBy           string
	ApprovedAt           *time.Time
	WithdrawnAt          *time.Time
}

// NewInvoiceContingency is a contingency to request.
type NewInvoiceContingency struct {
	PeriodFrom, PeriodTo time.Time
	Reason, Evidence     string
	At                   time.Time
}

// InvoiceNow is the submissions through InvoiceNow, their history, the exclusions and the contingencies. Every change is
// audited in its transaction. Legal moves, the one live exclusion of a document and the approver who is not the requester
// are the database's rules (CodeInvoiceNow).
type InvoiceNow interface {
	// Queue adds the next version of the document's submission, queued.
	Queue(ctx context.Context, n NewInvoiceSubmission) (InvoiceSubmission, error)
	Submission(ctx context.Context, id uuid.UUID) (InvoiceSubmission, error)
	// Submissions is a page, newest first.
	Submissions(ctx context.Context, f InvoiceSubmissionFilter) ([]InvoiceSubmission, error)
	// Latest is the latest version's submission of each of the documents.
	Latest(ctx context.Context, documents []uuid.UUID) ([]InvoiceSubmission, error)
	// Move changes a submission's status along the legal path and records the history.
	Move(ctx context.Context, id uuid.UUID, m InvoiceSubmissionMove) (InvoiceSubmission, error)
	Events(ctx context.Context, id uuid.UUID) ([]InvoiceEvent, error)
	// Exclude records a document's exclusion; CodeInvoiceNow for a second live one.
	Exclude(ctx context.Context, document uuid.UUID, reason, detail string, at time.Time) (InvoiceExclusion, error)
	WithdrawExclusion(ctx context.Context, id uuid.UUID, at time.Time) (InvoiceExclusion, error)
	// Exclusions are the live exclusions of the documents.
	Exclusions(ctx context.Context, documents []uuid.UUID) ([]InvoiceExclusion, error)
	RequestContingency(ctx context.Context, n NewInvoiceContingency) (InvoiceContingency, error)
	// ApproveContingency approves it as the signed-in person; CodeInvoiceNow when they requested it.
	ApproveContingency(ctx context.Context, id uuid.UUID, at time.Time) (InvoiceContingency, error)
	Contingency(ctx context.Context, id uuid.UUID) (InvoiceContingency, error)
	// Contingencies are the live ones that cover any day of from to to, newest first.
	Contingencies(ctx context.Context, from, to time.Time) ([]InvoiceContingency, error)
}
