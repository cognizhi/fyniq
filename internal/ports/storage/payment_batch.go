package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PaymentBatchRelease is the release of an approved payment batch (FR-BNK-12; PRD-001 rev 0.154, rev 0.156; design D1,
// D7): who released it, when and under which approval, and the file it wrote, sealed by the application (SEC-11) and
// bound to the batch, with the SHA-256 of the clear file and its count and total. Written once, never changed.
type PaymentBatchRelease struct {
	ID           uuid.UUID
	BatchID      uuid.UUID
	BatchVersion int
	ApprovalID   uuid.UUID
	ReleasedBy   uuid.UUID
	ReleasedAt   time.Time
	Writer       string // the file's writer and its version, such as payment-list@1
	FileName     string
	MediaType    string
	Sealed       []byte
	SHA256       []byte // of the clear file
	Payments     int
	Currency     string
	Total        Decimal
}

// PaymentBatches keep the releases of payment batches and the payments each posted. A release is audited on its batch,
// with the file's name and hash only; a link on its payment. The database refuses a release of a batch that is not
// approved at its current version, a second release, and any change or deletion (CodeBatch); a link to a payment that
// is not a posted bill payment or reimbursement of the batch's account and date (CodeBatch); and, at commit, a payment
// recorded by hand of a bill or claim in an approved, unreleased batch (CodeBatchHeld).
type PaymentBatches interface {
	Release(ctx context.Context, r PaymentBatchRelease) (PaymentBatchRelease, error)
	// ReleaseOf is a batch's release, nil while it has none.
	ReleaseOf(ctx context.Context, batchID uuid.UUID) (*PaymentBatchRelease, error)
	// Link records a payment the release posted.
	Link(ctx context.Context, releaseID, paymentID uuid.UUID) error
	// Payments are the payments a release posted, in the order they were linked.
	Payments(ctx context.Context, releaseID uuid.UUID) ([]uuid.UUID, error)
}
