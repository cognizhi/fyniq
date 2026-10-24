package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The outcomes of a sent quote or sales order (FR-AR-01).
const (
	QuoteAccepted = "accepted"
	QuoteDeclined = "declined"
	QuoteExpired  = "expired"
)

// NewQuoteOutcome is the customer's answer to a sent quote or order, or its expiry: the day, and for an acceptance
// the name of who accepted it on the customer's side; Name and Note are empty when none.
type NewQuoteOutcome struct {
	DocumentID uuid.UUID
	Outcome    string
	On         time.Time
	Name       string
	Note       string
}

// QuoteOutcome is a recorded outcome: one per quote, never changed (FY193).
type QuoteOutcome struct {
	NewQuoteOutcome
	RecordedBy string
	RecordedAt time.Time
}

// QuoteOutcomes are the outcomes of the organisation's quotes and orders. Recording one is audited.
type QuoteOutcomes interface {
	// Record records an outcome: CodeQuoteOutcome when the quote has one already or is not a sent quote or order.
	Record(ctx context.Context, n NewQuoteOutcome) (QuoteOutcome, error)
	// Of are the outcomes of the quotes named that have one.
	Of(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]QuoteOutcome, error)
}
