package storage

import (
	"context"
	"slices"
	"strings"
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
	// SuperAdminUsers returns the users of the active Super admins, longest-serving first, for the worker to tell them
	// of what is the deployment's to fix (FR-NTF-01).
	SuperAdminUsers(ctx context.Context) ([]uuid.UUID, error)
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
	// RecordIdentityEmail keeps the email the provider verified at a sign-in on the identity, if it has a user, so mail
	// the person sends to an email-in address is known as theirs (FR-INT-22, rev 0.106).
	RecordIdentityEmail(ctx context.Context, issuer, subject, email string, now time.Time) error
	// Session returns the live session behind a token's hash at now, or ports.ErrNotFound: unknown,
	// ended, idle for longer than its organisation's timeout (idle when it has none, rev 0.180), or past its expiry. The identity's user, their organisation and
	// their live roles are read at that moment, so a role change applies at the next request.
	Session(ctx context.Context, tokenHash []byte, now time.Time, idle time.Duration) (Session, error)
	// EndSession ends a session: its holder signed out.
	EndSession(ctx context.Context, tokenHash []byte, now time.Time) error
	// UserSessions returns a user's live sessions at now, newest first (FR-ADM-03): their devices.
	UserSessions(ctx context.Context, userID uuid.UUID, now time.Time, idle time.Duration) ([]Device, error)
	// IdleTimeout is an organisation's idle timeout at now: its setting security.idle_timeout_minutes in force on its
	// date, or fallback when it has none (SEC-05, rev 0.180). Session applies the same, so the devices listed agree.
	IdleTimeout(ctx context.Context, org uuid.UUID, now time.Time, fallback time.Duration) (time.Duration, error)
	// EndUserSession ends one of a user's sessions, or returns ports.ErrNotFound when they have no live one with that hash.
	EndUserSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, now time.Time) error
	// LinkIdentity links a sign-in identity, just signed in with its provider, to the user of a live session that passed a
	// second factor in the last 5 minutes (FR-ADM-08, rev 0.196), recording it in the installation's trail, and returns
	// the user: CodeIdentityStepUp when the session did not; CodeIdentity when any user has the identity already. A
	// matching email never links.
	LinkIdentity(ctx context.Context, l IdentityLink) (uuid.UUID, error)
	// UnlinkIdentity removes one of the identities of a live session's user, the session having passed a second factor
	// in the last 5 minutes, recording it in the installation's trail, and returns the user: CodeIdentityStepUp when the
	// session did not; ports.ErrNotFound for an identity not theirs; CodeIdentity for the one the session signed in with,
	// or their last.
	UnlinkIdentity(ctx context.Context, sessionHash []byte, issuer, subject string, now time.Time) (uuid.UUID, error)
	// SignInMethods are the providers' switches a Super admin set, by provider; a provider with none is on and accepts any
	// domain (FR-ADM-08, rev 0.197).
	SignInMethods(ctx context.Context) ([]SignInMethod, error)
	// SignInEssential are the providers some active Super admin signs in with alone: turning one off is refused.
	SignInEssential(ctx context.Context) ([]string, error)
	// SignInCounts are how many sessions each provider started since since, by provider.
	SignInCounts(ctx context.Context, since time.Time) (map[string]int, error)
	// Identities are a user's sign-in identities: the one they joined or set up with first, then those linked, in order.
	Identities(ctx context.Context, userID uuid.UUID) ([]Identity, error)
}

// SignInMethod is a provider's switches: whether it signs people in, and the email domains it accepts (none: any).
type SignInMethod struct {
	Provider       string
	Enabled        bool
	AllowedDomains []string
	Version        int // 0: never set
	ChangedBy      uuid.UUID
	ChangedAt      time.Time
}

// Accepts reports whether the switches let an account with email (verified or not) sign in.
func (m SignInMethod) Accepts(email string, verified bool) bool {
	if !m.Enabled {
		return false
	}
	if len(m.AllowedDomains) == 0 {
		return true
	}
	_, domain, ok := strings.Cut(strings.ToLower(email), "@")
	return ok && verified && slices.Contains(m.AllowedDomains, domain)
}

// MethodOf is provider's switches among all: on, accepting any domain, when none were set.
func MethodOf(all []SignInMethod, provider string) SignInMethod {
	for _, m := range all {
		if m.Provider == provider {
			return m
		}
	}
	return SignInMethod{Provider: provider, Enabled: true}
}

// SignInMethodChange is a Super admin's change of a provider's switches, at the version they read (0: never set).
type SignInMethodChange struct {
	Provider       string   `json:"provider"`
	Version        int      `json:"version"`
	Enabled        bool     `json:"enabled"`
	AllowedDomains []string `json:"allowed_domains"`
	EndSessions    bool     `json:"end_sessions,omitempty"`
}

// SignInMethods changes the providers' switches, in a unit of work of the installation organisation, whose trail records
// each change (FR-ADM-08, FR-ADM-07).
type SignInMethodWriter interface {
	// List is Sessions.SignInMethods as the unit of work sees it.
	List(ctx context.Context) ([]SignInMethod, error)
	// Essential is Sessions.SignInEssential as the unit of work sees it.
	Essential(ctx context.Context) ([]string, error)
	// Set applies c by by, as approved by change (nil: none needed, with one Super admin): CodeVersion at another version;
	// CodeSignInMethod for a domain that is not one, or when it leaves a Super admin no way to sign in.
	Set(ctx context.Context, by uuid.UUID, change *uuid.UUID, c SignInMethodChange, at time.Time) (SignInMethod, error)
}

// IdentityLink is what Sessions.LinkIdentity needs.
type IdentityLink struct {
	SessionHash               []byte
	Issuer, Subject, Provider string
	Now                       time.Time
}

// Identity is one of a person's sign-in identities: the provider it came from, when it was added, when it was linked (zero
// for the one they joined or set up with) and when it last signed in (zero for never).
type Identity struct {
	Issuer, Subject, Provider string
	CreatedAt, LinkedAt       time.Time
	LastUsedAt                time.Time
}

// Device is one of a user's live sessions.
type Device struct {
	TokenHash    []byte // the session token's SHA-256: never shown; the API derives the device's ID from it
	Provider     string
	SignedInAt   time.Time
	LastSeenAt   time.Time
	ExpiresAt    time.Time
	StepUpMethod string // sign_in, passkey or empty
	UserAgent    string // the browser that began it, as it said; empty when it said nothing
}

// NewSession is what StartSession needs. TokenHash is the SHA-256 of the session token: the token
// itself never reaches the store.
type NewSession struct {
	TokenHash                 []byte
	Issuer, Subject, Provider string
	// StepUpAt is when the sign-in passed a second factor (SEC-01); zero when it passed none.
	StepUpAt     time.Time
	Now, Expires time.Time
	// UserAgent is the browser's User-Agent, kept to tell devices apart (FR-ADM-03); at most 300 characters are kept.
	UserAgent string
}

// Session is a live session.
type Session struct {
	Issuer, Subject, Provider string
	// UserID is the identity's user, Nil before it has one (before first-run setup or an invitation);
	// Organisation is the user's organisation, Nil when they are a member of none.
	UserID, Organisation uuid.UUID
	Roles                []string // the user's live roles in Organisation
	StepUpAt             time.Time
	// StepUpMethod is how the session passed its last second factor: sign_in (at the provider, TOTP), passkey or
	// recovery (SEC-35); empty when it passed none.
	StepUpMethod string
	ExpiresAt    time.Time
	// CoolingOffUntil is when the cooling-off after the user's recovery ends; zero when they are in none (SEC-35).
	CoolingOffUntil time.Time
}

// Recoveries keep the account recovery of every person (SEC-35, rev 0.198; migration 000242): their recovery codes, the
// confirmations an Owner or Administrator gives, and each recovery. The rows are global; each change is audited in the
// trail of the unit of work it runs in.
type Recoveries interface {
	// Codes says how many of user's recovery codes are unused, and when they were made (zero: never).
	Codes(ctx context.Context, user uuid.UUID) (RecoveryCodes, error)
	// SetCodes replaces the recovery codes of a live session's user, the session having passed a second factor in the
	// last 5 minutes (CodeRecovery otherwise), with ten keyed hashes; returns the user.
	SetCodes(ctx context.Context, sessionHash []byte, hashes [][]byte, now time.Time) (uuid.UUID, error)
	// Confirm records by's confirmation, as an Owner or Administrator of org, of user's recovery: a one-time code
	// (SHA-256 hash) until expires, replacing an unused one. CodeRecovery for their own, or by someone in a cooling-off.
	Confirm(ctx context.Context, by, user, org uuid.UUID, hash []byte, now, expires time.Time) (uuid.UUID, error)
	// Recover recovers the user of a live session with one of their recovery codes and a confirmation (CodeRecovery when
	// either is wrong, used or expired: the same for each).
	Recover(ctx context.Context, r NewRecovery) (AccountRecovered, error)
}

// RecoveryCodes is the state of a person's recovery codes.
type RecoveryCodes struct {
	Unused    int
	CreatedAt time.Time
}

// NewRecovery is what Recoveries.Recover needs: the code's keyed hash, the confirmation's SHA-256, ten new keyed hashes.
type NewRecovery struct {
	SessionHash, CodeHash, ConfirmationHash []byte
	NewHashes                               [][]byte
	Now                                     time.Time
	CoolingOff                              time.Duration
}

// AccountRecovered is a person's recovery done: who recovered, who confirmed it in which organisation, and until when
// they cool off.
type AccountRecovered struct {
	ID, UserID, ConfirmedBy, Organisation uuid.UUID
	CoolingOffUntil                       time.Time
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
	// Remove ends a membership at its version (rev 0.165): each role not yet revoked, expired ones included, then the
	// membership, audited user_membership.removed. The last Owner keeps the role (CodeMembership); another version is
	// CodeVersion. A removed person who accepts a new invitation is a member again.
	Remove(ctx context.Context, membershipID uuid.UUID, version int) error
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
