package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NewPractice is what Practices.Open needs: the person who opens it, from the organisation the unit of work is for, and what to call it.
type NewPractice struct {
	User     uuid.UUID
	Name     string // as shown: the caller labels it as a practice one
	Currency string
	Now      time.Time
}

// Practice is the practice organisation opened: its ID, and the creator's membership and Owner role in it.
type Practice struct {
	OrganisationID, MembershipID, AssignmentID uuid.UUID
}

// Practices opens practice organisations (FR-UX-36). Only an Owner of the unit of work's organisation, which keeps books, opens one, and
// only while they hold none (CodePractice, CodePracticeHeld): the database decides both. The new organisation has nothing of the source's.
// Both organisations' audit events are written in the unit of work's transaction.
type Practices interface {
	Open(ctx context.Context, n NewPractice) (Practice, error)
	// IsPractice says whether an organisation is a practice one.
	IsPractice(ctx context.Context, org uuid.UUID) (bool, error)
}

// SessionOrganisation is an organisation a session's person is an active member of.
type SessionOrganisation struct {
	ID       uuid.UUID
	Name     string
	Practice bool
	Chosen   bool
}

// SessionOrganisations are the organisations a person works in and the choice of which a session is in (FR-UX-36): the person's own
// memberships and nothing else of any tenant. A choice names an organisation they are an active member of, or it is refused.
type SessionOrganisations interface {
	OrganisationsOf(ctx context.Context, tokenHash []byte, now time.Time) ([]SessionOrganisation, error)
	// SwitchOrganisation makes org the session's organisation; false when the person is no active member of it.
	SwitchOrganisation(ctx context.Context, tokenHash []byte, org uuid.UUID, now time.Time) (bool, error)
}
