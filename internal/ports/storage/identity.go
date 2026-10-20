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
	// JoinOrganisation redeems an invitation for a sign-in identity: the user behind it, or a new one, becomes a
	// member of the inviting organisation with the invitation's roles, in one audited transaction. A wrong,
	// used, withdrawn or expired code, or a sign-in already a member there, fails with CodeGrant and changes
	// nothing.
	JoinOrganisation(ctx context.Context, j NewJoin) (Joined, error)
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
	// SessionsEnded is how many of the user's first-party sessions the recovery ended: all but those of
	// the identity recovering (SEC-35).
	SessionsEnded int
}

// Sessions are the web app's first-party sessions (SEC-05, SEC-08): the browser holds an opaque token
// in an HttpOnly cookie, and the store keeps only its SHA-256. They are the installation's, outside any
// organisation's unit of work.
type Sessions interface {
	// StartSession records a new session for a sign-in identity, with the provider used (FR-ADM-08).
	StartSession(ctx context.Context, s NewSession) error
	// Session returns the live session behind a token's hash at now, or ports.ErrNotFound: unknown,
	// ended, idle for longer than idle, or past its expiry. The identity's user, their organisation and
	// their live roles are read at that moment, so a role change applies at the next request.
	Session(ctx context.Context, tokenHash []byte, now time.Time, idle time.Duration) (Session, error)
	// EndSession ends a session: its holder signed out.
	EndSession(ctx context.Context, tokenHash []byte, now time.Time) error
}

// NewSession is what StartSession needs. TokenHash is the SHA-256 of the session token: the token
// itself never reaches the store.
type NewSession struct {
	TokenHash                 []byte
	Issuer, Subject, Provider string
	// StepUpAt is when the sign-in passed a second factor (SEC-01); zero when it passed none.
	StepUpAt     time.Time
	Now, Expires time.Time
}

// Session is a live session.
type Session struct {
	Issuer, Subject, Provider string
	// UserID is the identity's user, Nil before it has one (before first-run setup or an invitation);
	// Organisation is the user's organisation, Nil when they are a member of none.
	UserID, Organisation uuid.UUID
	Roles                []string // the user's live roles in Organisation
	StepUpAt             time.Time
	// StepUpMethod is how the session passed its last second factor: sign_in (at the provider, TOTP) or passkey;
	// empty when it passed none.
	StepUpMethod string
	ExpiresAt    time.Time
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
	// List returns the active members with their names and live roles at now, by membership ID (the order they
	// joined).
	List(ctx context.Context, now time.Time) ([]Member, error)
	// Count returns how many active members the organisation has, under the members' lock for the rest of the
	// unit of work: someone joining waits for it, so a role change sees the count it is decided on.
	Count(ctx context.Context) (int, error)
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
