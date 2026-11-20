// Package identity is who a person is in Nemura and how they stay signed in (PRD-002 NM-ARC-01, NM-ARC-06, ADR-N3): codes for first-run
// setup and invitations, and session tokens, all opaque random values that are kept only as their SHA-256. People are matched by the
// identity provider's issuer, subject and tenant, never by email alone (NM-INT-12's rule). Codes and tokens carry the organisation they
// belong to, so a lookup can set its tenant first and row-level security does the rest.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// How long things last. A session ends at its expiry whatever the activity; the idle limit is enforced by the web app's step-up
// freshness, not here. [owner may adjust: the values are the platform's, as Fyniq's]
const (
	SessionTTL    = 12 * time.Hour
	InvitationTTL = 7 * 24 * time.Hour
	StepUpWindow  = 5 * time.Minute
)

// Identity is the provider's name for a person: the issuer, the subject and the tenant. Email is never part of it.
type Identity struct{ Issuer, Subject, Tenant string }

// Via is how a session was signed in: the provider the person used and the identity it proved (issuer and subject). The provider is what the switch to Fyniq asks for, so the person is not shown a
// sign-in page again; the identity is what a pay run's approver is named by, because a member can be linked to one identity per issuer and the one that signs is the one that matters. A session made
// before this was recorded has none (the zero value).
type Via struct{ Provider, Issuer, Subject string }

// Valid reports whether the identity names a person.
func (i Identity) Valid() bool { return i.Issuer != "" && i.Subject != "" && i.Tenant != "" }

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("identity: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Hash is what is kept of a secret: its SHA-256.
func Hash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// New makes a value for an organisation: the value to give the person ("<organisation>.<secret>") and the hash to keep.
func New(org uuid.UUID) (value string, hash []byte) {
	secret := random()
	return org.String() + "." + secret, Hash(secret)
}

// ErrMalformed is returned for a value that is not "<organisation>.<secret>".
var ErrMalformed = errors.New("identity: not a code or token of this kind")

// Parse splits a value into its organisation and the hash of its secret.
func Parse(value string) (org uuid.UUID, hash []byte, err error) {
	prefix, secret, ok := strings.Cut(value, ".")
	if !ok || len(secret) < 20 {
		return uuid.Nil, nil, ErrMalformed
	}
	org, err = uuid.Parse(prefix)
	if err != nil {
		return uuid.Nil, nil, ErrMalformed
	}
	return org, Hash(secret), nil
}

// NewSetupCode makes the one-time first-run setup code: shown once at deployment, kept as its hash. It names no organisation.
func NewSetupCode() (display string, hash []byte) {
	secret := random()
	return secret, Hash(secret)
}

// RecoveryTTL is how long an Owner's recovery code works (FR-ADM-09, as Fyniq's deployment recovery).
const RecoveryTTL = 24 * time.Hour

// Operators reads the two names of the people who issue a recovery, as "name, name": exactly two, each 1 to 100 characters, and different
// (compared without regard to case). The database checks the same again (NM110).
func Operators(list string) ([]string, error) {
	var names []string
	for _, n := range strings.Split(list, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if len(names) != 2 || strings.EqualFold(names[0], names[1]) || len(names[0]) > 100 || len(names[1]) > 100 {
		return nil, &Refusal{CodeRecoveryOperators, "A recovery needs exactly two different operators, by name: OPERATORS=\"<name>, <name>\"."}
	}
	return names, nil
}

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// The codes of the refusals.
const (
	CodeSignInRefused = "NM_SIGNIN_REFUSED" // the code or token is wrong, used, expired or revoked: one answer for all, so it tells an attacker nothing
	CodeSetupRefused  = "NM_SETUP_REFUSED"  // the setup code is wrong, or setup is done
	CodeStepUp        = "NM_STEP_UP_REQUIRED"

	CodeRecoveryOperators = "NM_RECOVERY_OPERATORS" // not exactly two different operator names
	CodeRecoveryMember    = "NM_RECOVERY_MEMBER"    // the member is not an active Owner of that organisation
)

// Refused is the one refusal for any code or token that does not open: it never says which part was wrong.
func Refused() *Refusal {
	return &Refusal{CodeSignInRefused, "That code or session is not valid. Sign in again, or ask for a new invitation."}
}
