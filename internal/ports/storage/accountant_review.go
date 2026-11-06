package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The states of an accountant's review (FM-7).
const (
	AccountantReviewInReview   = "in_review"
	AccountantReviewSignedOff  = "signed_off"
	AccountantReviewSuperseded = "superseded" // its month reopened; it is kept
)

// AccountantReview is the accountant's review of one closed month (FM-7, rev 0.176): the close it covers, who opened
// it, its queries and adjusting journals, and its sign-off or the reopening that superseded it.
type AccountantReview struct {
	ID           uuid.UUID
	PeriodID     uuid.UUID
	CloseID      uuid.UUID
	ClosedAt     time.Time // when the close it covers was attested
	CloseHash    []byte    // the SHA-256 of that close's snapshot (AUD-04)
	OpenedBy     uuid.UUID
	OpenedAt     time.Time
	Status       string
	SignedBy     *uuid.UUID
	SignedAt     *time.Time
	Statement    string
	SupersededAt *time.Time
	ReopenID     *uuid.UUID
	Version      int
	Queries      []ReviewQuery   // oldest first
	Journals     []ReviewJournal // oldest first
}

// ReviewQuery is a query of an accountant's review, with management's answer once given.
type ReviewQuery struct {
	ID         uuid.UUID
	AskedBy    uuid.UUID
	AskedAt    time.Time
	Question   string
	AnsweredBy *uuid.UUID
	AnsweredAt *time.Time
	Answer     string
}

// ReviewJournal is an adjusting journal an accountant's review raised.
type ReviewJournal struct {
	JournalID uuid.UUID
	LinkedBy  uuid.UUID
	LinkedAt  time.Time
}

// AccountantReviews are the accountant's reviews (FM-7). Each change is audited on its review and moves its version.
// The database refuses a review of a month that is not closed or of an older close, a second review of a close, a
// query, answer or journal once the review is signed off or superseded, a second answer, a sign-off while a query has
// no answer, a reopening that leaves a review of the close it undoes live, and any other change (CodeAccountantReview).
type AccountantReviews interface {
	// List returns every review with its queries and journals, oldest first.
	List(ctx context.Context) ([]AccountantReview, error)
	// Get returns a review with its queries and journals, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (AccountantReview, error)
	// Open opens a review of a closed month's latest close.
	Open(ctx context.Context, periodID, closeID, by uuid.UUID, at time.Time) (AccountantReview, error)
	// Ask adds a query to a review at version: CodeVersion when it changed since.
	Ask(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time, question string) (AccountantReview, error)
	// Answer answers a query of a review at version.
	Answer(ctx context.Context, id uuid.UUID, version int, query, by uuid.UUID, at time.Time, answer string) (AccountantReview, error)
	// Link links an adjusting journal to a review at version.
	Link(ctx context.Context, id uuid.UUID, version int, journal, by uuid.UUID, at time.Time) (AccountantReview, error)
	// SignOff signs a review off at version.
	SignOff(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time, statement string) (AccountantReview, error)
	// Supersede supersedes every live review of a close a reopening undoes, in the reopening's unit of work.
	Supersede(ctx context.Context, closeID, reopenID uuid.UUID, at time.Time) error
}
