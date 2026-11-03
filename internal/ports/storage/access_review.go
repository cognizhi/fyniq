package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAccessReview is an attestation the database refuses: one changed or deleted (FY247), or one before its quarter
// ends.
const CodeAccessReview = "STORAGE-ACCESS-01"

// AccessReviewAttestation is an Owner's attestation of the user access review for a calendar quarter (PRD-001
// FR-CTL-06, SEC-06; rev 0.162): the report as they saw it, which the database hashes and never changes (FY247).
type AccessReviewAttestation struct {
	ID         uuid.UUID
	Quarter    time.Time // the quarter's first day
	AttestedBy string    // "type:id"
	AttestedAt time.Time
	Note       string
	Report     string // the report's canonical JSON
	ReportHash []byte // its SHA-256
}

// NewAccessReviewAttestation is what AccessReviews.Attest needs.
type NewAccessReviewAttestation struct {
	Quarter    time.Time
	AttestedBy string
	AttestedAt time.Time
	Note       string
	Report     string
}

// MemberSignIn is an active member's last sign-in, by any of their identities, and how that session passed its second
// factor: "sign_in", "passkey" or "" for none.
type MemberSignIn struct {
	UserID     uuid.UUID
	LastSignIn time.Time
	StepUp     string
}

// AccessReviews are the organisation's quarterly access review attestations and what the review reads that the
// installation keeps: the members' sign-ins (FR-CTL-06).
type AccessReviews interface {
	// List returns every attestation, earliest quarter first.
	List(ctx context.Context) ([]AccessReviewAttestation, error)
	// Attest records a quarter's attestation, audited access_review.attested; a quarter attested already is
	// CodeDuplicate.
	Attest(ctx context.Context, n NewAccessReviewAttestation) (AccessReviewAttestation, error)
	// SignIns returns the active members who have signed in, each once; a member who never did has none.
	SignIns(ctx context.Context) ([]MemberSignIn, error)
}
