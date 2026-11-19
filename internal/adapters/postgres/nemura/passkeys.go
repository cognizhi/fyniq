package nemura

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/passkey"
)

// ChallengeTTL is how long a passkey ceremony may take (WebAuthn's challenge, five minutes at most).
const ChallengeTTL = 5 * time.Minute

// Purposes of a passkey challenge.
const (
	PurposeRegister = "register"
	PurposeStepUp   = "step_up"
)

// ErrNoChallenge is an answer with no open challenge for its session: it was never asked, was used, or expired.
var ErrNoChallenge = errors.New("the passkey ceremony was not started, or its challenge was used or has expired")

func keysOf(ctx context.Context, tx pgx.Tx, member uuid.UUID) ([]passkey.Key, error) {
	rows, err := tx.Query(ctx, `SELECT credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state, revoked_at IS NOT NULL
FROM nemura.passkey WHERE member_id = $1 ORDER BY created_at`, member)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []passkey.Key
	for rows.Next() {
		var k passkey.Key
		var count int64
		if err := rows.Scan(&k.CredentialID, &k.PublicKey, &count, &k.AAGUID, &k.Transports, &k.BackupEligible, &k.BackupState, &k.Revoked); err != nil {
			return nil, err
		}
		k.SignCount = uint32(count) // #nosec G115 -- the column is checked to 0..4294967295
		out = append(out, k)
	}
	return out, rows.Err()
}

func keepChallenge(ctx context.Context, tx pgx.Tx, session, member uuid.UUID, purpose string, challenge []byte, at time.Time) error {
	_, err := tx.Exec(ctx, "INSERT INTO nemura.passkey_challenge (session_id, member_id, purpose, challenge, created_at, expires_at) VALUES ($1, $2, $3, $4, $5, $6)",
		session, member, purpose, challenge, at, at.Add(ChallengeTTL))
	return err
}

// takeChallenge finds the session's latest open challenge for the purpose and uses it, once.
func takeChallenge(ctx context.Context, tx pgx.Tx, session uuid.UUID, purpose string, at time.Time) ([]byte, error) {
	var id uuid.UUID
	var challenge []byte
	err := tx.QueryRow(ctx, `SELECT id, challenge FROM nemura.passkey_challenge WHERE session_id = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3
ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, session, purpose, at).Scan(&id, &challenge)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoChallenge
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, "UPDATE nemura.passkey_challenge SET used_at = $2 WHERE id = $1", id, at)
	return challenge, err
}

// BeginRegistration returns what the browser passes to navigator.credentials.create for a member's new passkey and keeps its challenge,
// bound to the session, for five minutes.
func BeginRegistration(ctx context.Context, tx pgx.Tx, rp *passkey.RelyingParty, at time.Time, session, member uuid.UUID, name string) ([]byte, error) {
	keys, err := keysOf(ctx, tx, member)
	if err != nil {
		return nil, err
	}
	creation, challenge, err := rp.Creation(passkey.UserOf(member, name, keys))
	if err != nil {
		return nil, err
	}
	if err := keepChallenge(ctx, tx, session, member, PurposeRegister, challenge, at); err != nil {
		return nil, err
	}
	return json.Marshal(creation)
}

// FinishRegistration checks the browser's answer to the registration challenge (already taken, see CompleteRegistration) and keeps the
// passkey, with its audit event.
func FinishRegistration(ctx context.Context, tx pgx.Tx, rp *passkey.RelyingParty, org uuid.UUID, at time.Time, member uuid.UUID, memberName, passkeyName string, challenge, response []byte) (uuid.UUID, error) {
	keys, err := keysOf(ctx, tx, member)
	if err != nil {
		return uuid.Nil, err
	}
	reg, err := rp.Register(passkey.UserOf(member, memberName, keys), challenge, response)
	if err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.passkey (id, member_id, credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state, name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, id, member, reg.CredentialID, reg.PublicKey, int64(reg.SignCount), reg.AAGUID, reg.Transports, reg.BackupEligible, reg.BackupState, passkeyName); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(member), "nemura.passkey.registered", audit.Object{Type: "passkey", ID: id}, map[string]audit.Change{"name": {nil, passkeyName}}, "")
}

// BeginStepUp returns what the browser passes to navigator.credentials.get to confirm the session with a passkey, and keeps its challenge.
func BeginStepUp(ctx context.Context, tx pgx.Tx, rp *passkey.RelyingParty, at time.Time, session, member uuid.UUID, name string) ([]byte, error) {
	keys, err := keysOf(ctx, tx, member)
	if err != nil {
		return nil, err
	}
	var live int
	for _, k := range keys {
		if !k.Revoked {
			live++
		}
	}
	if live == 0 {
		return nil, ErrNoPasskey
	}
	u := passkey.UserOf(member, name, keys)
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, err
	}
	assertion, err := rp.Assertion(u, challenge)
	if err != nil {
		return nil, err
	}
	if err := keepChallenge(ctx, tx, session, member, PurposeStepUp, challenge, at); err != nil {
		return nil, err
	}
	return json.Marshal(assertion)
}

// ErrNoPasskey is a step-up asked of a member who has registered no passkey.
var ErrNoPasskey = errors.New("the member has no passkey to confirm with: register one first")

// FinishStepUp checks the browser's answer to the step-up challenge: one of the member's live passkeys signed exactly that challenge, for this
// origin and relying party, with the person verified, and its counter did not go back. The session is then fresh for five minutes. The
// challenge is used once, whatever the answer.
func FinishStepUp(ctx context.Context, tx pgx.Tx, rp *passkey.RelyingParty, org uuid.UUID, at time.Time, session, member uuid.UUID, memberName string, challenge, response []byte) error {
	keys, err := keysOf(ctx, tx, member)
	if err != nil {
		return err
	}
	got, err := rp.Verify(passkey.UserOf(member, memberName, keys), challenge, response)
	if err != nil {
		return err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, "UPDATE nemura.passkey SET sign_count = $2, backup_state = $3, last_used_at = $4 WHERE member_id = $5 AND credential_id = $1 AND revoked_at IS NULL RETURNING id",
		got.CredentialID(), int64(got.SignCount), got.BackupState, at, member).Scan(&id); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(member), "nemura.passkey.used", audit.Object{Type: "passkey", ID: id}, nil, ""); err != nil {
		return err
	}
	return RecordStepUp(ctx, tx, org, at, session, member)
}

// BeginRegistrationFor, BeginStepUpFor, CompleteRegistration and CompleteStepUp run the ceremonies in transactions of their own. The
// challenge is taken, and so used, in a transaction that commits before the answer is checked, so a wrong answer never leaves it open to
// another try: a challenge is used once, whatever the answer.

// BeginRegistrationFor starts a registration.
func (s *Store) BeginRegistrationFor(ctx context.Context, rp *passkey.RelyingParty, org uuid.UUID, at time.Time, session, member uuid.UUID, name string) ([]byte, error) {
	var out []byte
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = BeginRegistration(ctx, tx, rp, at, session, member, name)
		return err
	})
	return out, err
}

// BeginStepUpFor starts a step-up.
func (s *Store) BeginStepUpFor(ctx context.Context, rp *passkey.RelyingParty, org uuid.UUID, at time.Time, session, member uuid.UUID, name string) ([]byte, error) {
	var out []byte
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = BeginStepUp(ctx, tx, rp, at, session, member, name)
		return err
	})
	return out, err
}

func (s *Store) take(ctx context.Context, org, session uuid.UUID, purpose string, at time.Time) ([]byte, error) {
	var challenge []byte
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		challenge, err = takeChallenge(ctx, tx, session, purpose, at)
		return err
	})
	return challenge, err
}

// CompleteRegistration checks the answer to the session's open registration challenge and keeps the passkey.
func (s *Store) CompleteRegistration(ctx context.Context, rp *passkey.RelyingParty, org uuid.UUID, at time.Time, session, member uuid.UUID, memberName, passkeyName string, response []byte) (uuid.UUID, error) {
	challenge, err := s.take(ctx, org, session, PurposeRegister, at)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = FinishRegistration(ctx, tx, rp, org, at, member, memberName, passkeyName, challenge, response)
		return err
	})
	return id, err
}

// CompleteStepUp checks the answer to the session's open step-up challenge and steps the session up.
func (s *Store) CompleteStepUp(ctx context.Context, rp *passkey.RelyingParty, org uuid.UUID, at time.Time, session, member uuid.UUID, memberName string, response []byte) error {
	challenge, err := s.take(ctx, org, session, PurposeStepUp, at)
	if err != nil {
		return err
	}
	return s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		return FinishStepUp(ctx, tx, rp, org, at, session, member, memberName, challenge, response)
	})
}
