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
	// SuperAdminFor returns the active Super admin behind a sign-in identity, or ports.ErrNotFound.
	SuperAdminFor(ctx context.Context, issuer, subject string) (uuid.UUID, error)
	// InstallationOrganisation returns the organisation that holds the installation's trail, or
	// ports.ErrNotFound before setup. Super admin changes run in a unit of work of it.
	InstallationOrganisation(ctx context.Context) (uuid.UUID, error)
	// CountSuperAdmins returns how many active Super admins can sign in (CTL-IAM-07).
	CountSuperAdmins(ctx context.Context) (int, error)
}

// NewSetup is what CompleteSetup needs. CodeHash is the SHA-256 of the setup code: the code itself
// never reaches the store.
type NewSetup struct {
	CodeHash         []byte
	DisplayName      string
	Issuer, Subject  string // the person's sign-in identity, as their authenticator verified it
	OrganisationName string
	BaseCurrency     string
	// RecoveryCodeHashes are the SHA-256 of the Super admin's ten printed recovery codes.
	RecoveryCodeHashes [][]byte
	Now                time.Time
}

// Setup is what first-run setup created.
type Setup struct {
	UserID                     uuid.UUID
	OrganisationID             uuid.UUID
	InstallationOrganisationID uuid.UUID
}

// SuperAdmins are the installation's Super admins and their one-time codes (FR-ADM-06/07/09, SEC-35;
// open question 36), in a unit of work of the installation organisation: each change writes its event
// to the installation's trail, and the database refuses a change without one. Codes reach the store
// only as their SHA-256. A code that is not valid, used or expired fails with CodeGrant, whatever
// the reason, so a guess learns nothing; a third Super admin fails with CodeSuperAdmin.
type SuperAdmins interface {
	// Invite creates the invitation for the second Super admin, replacing an unused one.
	Invite(ctx context.Context, by uuid.UUID, codeHash []byte, now, expires time.Time) error
	// AcceptInvitation makes the person signed in with issuer and subject a Super admin (a new user
	// when the identity has none) with ten recovery codes, and returns their user.
	AcceptInvitation(ctx context.Context, a AcceptInvitation) (uuid.UUID, error)
	// SetRecoveryCodes replaces a Super admin's unused recovery codes with ten new ones.
	SetRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes [][]byte, now time.Time) error
	// ConfirmRecovery is the other Super admin's confirmation of a recovery, the second proof that
	// goes with a recovery code (SEC-35); it replaces an unused one.
	ConfirmRecovery(ctx context.Context, by, of uuid.UUID, codeHash []byte, now, expires time.Time) error
	// Recover gives a Super admin back their access with two proofs: a recovery code and the other
	// Super admin's confirmation, or a deployment recovery. The signed-in identity replaces their
	// others, and their recovery codes are replaced with ten new ones.
	Recover(ctx context.Context, r Recovery) (Recovered, error)
}

// AcceptInvitation is what SuperAdmins.AcceptInvitation needs.
type AcceptInvitation struct {
	CodeHash           []byte
	DisplayName        string
	Issuer, Subject    string
	RecoveryCodeHashes [][]byte
	Now                time.Time
}

// Recovery is what SuperAdmins.Recover needs. RecoveryCodeHash is nil with a deployment recovery
// alone; ConfirmationHash is the other Super admin's confirmation or the deployment recovery.
type Recovery struct {
	RecoveryCodeHash   []byte
	ConfirmationHash   []byte
	Issuer, Subject    string
	RecoveryCodeHashes [][]byte // the new set
	Now                time.Time
}

// Recovered is the outcome of a recovery.
type Recovered struct {
	UserID uuid.UUID
	// Via is recovery_confirmation (a recovery code with the other Super admin's confirmation) or
	// deployment_recovery.
	Via string
	// RemovedSubjects are the subjects of the sign-in identities the recovery replaced: their tokens
	// must be revoked (AI-38).
	RemovedSubjects []string
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
