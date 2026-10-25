package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The kinds of review log entries (FM-3).
const (
	ReviewApprovedChange   = "approved_change"   // an approved request executed (FM-1)
	ReviewFounderBootstrap = "founder_bootstrap" // the Owner changed roles or invited someone alone, before a second member (FR-ADM-01)
	ReviewSelfApproved     = "self_approved"     // a founder posted alone at or below the self-approval threshold (FM-2)
	ReviewScheduledIssue   = "scheduled_issue"   // the scheduler issued an approved recurring invoice's invoice (FR-AR-07, FM-3)
	ReviewAutoReconciled   = "auto_reconciled"   // a bank line the system reconciled, in the week's sample (FR-BNK-14, FM-3)
)

// ReviewLogEntry is a high-risk action that executed, with its review once it has one.
type ReviewLogEntry struct {
	ID                    uuid.UUID
	Kind                  string
	ObjectType, ObjectKey string
	Summary               string
	Actor                 *uuid.UUID // who acted; nil for a caller with no Fyniq user
	ApprovalID            *uuid.UUID
	LoggedAt              time.Time
	ReviewedBy            *uuid.UUID
	ReviewedAt            *time.Time
	Note                  string
}

// NewReviewLogEntry is what ReviewLog.Log needs.
type NewReviewLogEntry struct {
	Kind                  string
	ObjectType, ObjectKey string
	Summary               string
	Actor                 *uuid.UUID
	ApprovalID            *uuid.UUID
	LoggedAt              time.Time
}

// ReviewLog is the organisation's log of high-risk actions and their reviews (FM-3). The database never changes an
// entry or a review, lets an entry be reviewed once, and refuses a review by the one who acted (CodeApproval). Logging
// and reviewing are audited.
type ReviewLog interface {
	// List returns every entry with its review, oldest first.
	List(ctx context.Context) ([]ReviewLogEntry, error)
	// Log records an entry.
	Log(ctx context.Context, n NewReviewLogEntry) (uuid.UUID, error)
	// Review records the review of an entry.
	Review(ctx context.Context, entryID, by uuid.UUID, at time.Time, note string) error
}
