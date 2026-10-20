package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The purposes of a passkey challenge.
const (
	PurposeRegister = "register" // registering a new passkey
	PurposeStepUp   = "step_up"  // stepping a session up
)

// Passkey is a person's WebAuthn credential (FR-ADM-02, SEC-01): the public key an authenticator made for Fyniq.
type Passkey struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	CredentialID   []byte // the authenticator's ID for it
	PublicKey      []byte // its COSE key
	SignCount      uint32
	AAGUID         []byte
	Transports     []string
	BackupEligible bool
	BackupState    bool
	Name           string
	CreatedAt      time.Time
	LastUsedAt     *time.Time
	RevokedAt      *time.Time
}

// NewPasskeyChallenge is a challenge for a session's ceremony: 32 random bytes, valid until Expires (at most 5
// minutes after Now).
type NewPasskeyChallenge struct {
	SessionHash []byte // the SHA-256 of the session token that asks
	Purpose     string // PurposeRegister or PurposeStepUp
	Challenge   []byte
	Now         time.Time
	Expires     time.Time
}

// PasskeyChallenge is a challenge taken by its session: its bytes and its user.
type PasskeyChallenge struct {
	Challenge []byte
	UserID    uuid.UUID
}

// NewPasskey is a passkey that answered a registration challenge.
type NewPasskey struct {
	ChallengeID    uuid.UUID
	SessionHash    []byte
	UserID         uuid.UUID // the session's user, who registers it
	CredentialID   []byte
	PublicKey      []byte
	SignCount      uint32
	AAGUID         []byte
	Transports     []string
	BackupEligible bool
	BackupState    bool
	Name           string
	Now            time.Time
}

// PasskeyStepUp is a passkey's answer to a session's step-up challenge: the session's step-up dates from Now.
type PasskeyStepUp struct {
	ChallengeID uuid.UUID
	SessionHash []byte
	PasskeyID   uuid.UUID
	SignCount   uint32 // the counter the assertion carried
	BackupState bool
	Now         time.Time
}

// Passkeys are people's passkeys and the challenges their ceremonies answer. They are the installation's, outside any
// organisation, and run in a unit of work of the installation organisation, whose trail records every passkey
// registered or revoked. The database issues a registration challenge only to a session that passed a second factor in
// the last 5 minutes, lets each challenge be taken once by its own session before it expires and answered once, never
// lets a passkey's counter go back or its key change, and steps a session up only as the answer to its own challenge
// by a live passkey of its user (CodePasskey).
type Passkeys interface {
	// Challenge issues a challenge and returns its ID.
	Challenge(ctx context.Context, n NewPasskeyChallenge) (uuid.UUID, error)
	// Take takes a challenge for its session and purpose, or returns ports.ErrNotFound: unknown, another session's,
	// taken already or expired. A challenge taken stays taken, whatever follows.
	Take(ctx context.Context, id uuid.UUID, sessionHash []byte, purpose string, now time.Time) (PasskeyChallenge, error)
	// List returns a user's passkeys, oldest first: live ones, or all with all.
	List(ctx context.Context, userID uuid.UUID, all bool) ([]Passkey, error)
	// Register records a passkey and its audit event.
	Register(ctx context.Context, n NewPasskey) (Passkey, error)
	// StepUp records a passkey's answer to a step-up challenge and steps the session up.
	StepUp(ctx context.Context, s PasskeyStepUp) error
	// Revoke revokes one of a user's live passkeys with its audit event, or returns ports.ErrNotFound.
	Revoke(ctx context.Context, userID, id uuid.UUID, now time.Time) error
}
