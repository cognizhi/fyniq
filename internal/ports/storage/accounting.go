package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// FrameworkVersion is a recorded version of the accounting framework (FR-ORG-10; PRD-001 rev 0.62): the framework and its
// edition from the financial year that starts on FromOn, until a later version replaces it. Its content never changes;
// the accountant's confirmation of eligibility is recorded on it once, never by its writer.
type FrameworkVersion struct {
	ID          uuid.UUID
	Version     int // the organisation's framework versions are numbered 1, 2, …
	FromOn      time.Time
	Framework   string // frs, sfrs_i or sfrs_se
	Edition     string
	WrittenBy   string
	WrittenAt   time.Time
	ConfirmedBy *string
	ConfirmedAt *time.Time
}

// PolicyVersion is a recorded version of one accounting policy (FR-ORG-10; PRD-001 rev 0.62): its words from the
// financial year that starts on FromOn, until a later version replaces it. Its content never changes; the accountant's
// approval is recorded on it once, never by its writer.
type PolicyVersion struct {
	ID         uuid.UUID
	Area       string
	Version    int // each area's versions are numbered 1, 2, …
	FromOn     time.Time
	Wording    string
	WrittenBy  string
	WrittenAt  time.Time
	ApprovedBy *string
	ApprovedAt *time.Time
}

// AccountingBasis keeps the organisation's accounting framework and policies (FR-ORG-10). The writer is the unit of
// work's actor, and each change is audited with it.
type AccountingBasis interface {
	// RecordFramework records the next framework version, from the financial year that starts on from.
	RecordFramework(ctx context.Context, from time.Time, framework, edition string) (FrameworkVersion, error)
	// Frameworks returns every framework version, oldest first.
	Frameworks(ctx context.Context) ([]FrameworkVersion, error)
	// ConfirmFramework records the actor's confirmation of a framework version: ErrNotFound when there is no such
	// version; CodeAccounting when it is confirmed already or the actor wrote it.
	ConfirmFramework(ctx context.Context, version int) (FrameworkVersion, error)
	// RecordPolicy records an area's next policy version, from the financial year that starts on from.
	RecordPolicy(ctx context.Context, area string, from time.Time, wording string) (PolicyVersion, error)
	// Policies returns every policy version, by area and then version.
	Policies(ctx context.Context) ([]PolicyVersion, error)
	// ApprovePolicy records the actor's approval of an area's policy version: ErrNotFound when there is no such
	// version; CodeAccounting when it is approved already or the actor wrote it.
	ApprovePolicy(ctx context.Context, area string, version int) (PolicyVersion, error)
}
