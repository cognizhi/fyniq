package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Installation is the installation-wide part of the store: first-run setup and sign-in identities
// (FR-ADM-06, FR-ADM-08). Users belong to the installation, not to an organisation.
type Installation interface {
	// SetupDone reports whether first-run setup has been completed.
	SetupDone(ctx context.Context) (bool, error)
	// CompleteSetup consumes the one-time setup code and, in one transaction, creates the first Super
	// admin with their sign-in identity, the installation's own audit trail, and the first organisation
	// with that user as its Owner. A wrong or used code fails with CodeSetup and changes nothing.
	CompleteSetup(ctx context.Context, s NewSetup) (Setup, error)
	// UserForIdentity returns the active user behind a sign-in identity, or ports.ErrNotFound.
	UserForIdentity(ctx context.Context, issuer, subject string) (uuid.UUID, error)
}

// NewSetup is what CompleteSetup needs. CodeHash is the SHA-256 of the setup code: the code itself
// never reaches the store.
type NewSetup struct {
	CodeHash         []byte
	DisplayName      string
	Issuer, Subject  string // the person's sign-in identity, as their authenticator verified it
	OrganisationName string
	BaseCurrency     string
}

// Setup is what first-run setup created.
type Setup struct {
	UserID                     uuid.UUID
	OrganisationID             uuid.UUID
	InstallationOrganisationID uuid.UUID
}

// Members are an organisation's memberships and role assignments (§11.4). A role change goes with a
// revocation of the user's tokens (internal/members, AI-38).
type Members interface {
	// Add makes a user a member.
	Add(ctx context.Context, userID uuid.UUID) (Membership, error)
	// Membership returns a user's membership, or ports.ErrNotFound.
	Membership(ctx context.Context, userID uuid.UUID) (Membership, error)
	// Assign gives a member a role, until expires (nil: no expiry).
	Assign(ctx context.Context, membershipID uuid.UUID, role string, expires *time.Time) (RoleAssignment, error)
	// Revoke ends a role assignment; it is never changed.
	Revoke(ctx context.Context, assignmentID uuid.UUID) error
	// Roles returns the roles a user holds now: live, unexpired assignments of an active membership.
	Roles(ctx context.Context, userID uuid.UUID, now time.Time) ([]RoleAssignment, error)
}

// Membership is a user's membership of the organisation.
type Membership struct {
	ID      uuid.UUID
	UserID  uuid.UUID
	Status  string // active or removed
	Version int
}

// RoleAssignment is a role a member holds.
type RoleAssignment struct {
	ID           uuid.UUID
	MembershipID uuid.UUID
	Role         string
	ExpiresAt    *time.Time
	RevokedAt    *time.Time
}
