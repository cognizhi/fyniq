package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Passkeys() storage.Passkeys { return passkeys{u} }

type passkeys struct{ u *unitOfWork }

// installation checks the unit of work is the installation organisation's, whose trail records passkeys.
func (r passkeys) installation(ctx context.Context) error {
	if err := r.u.check(); err != nil {
		return err
	}
	org, err := r.u.q.InstallationOrg(ctx)
	if err != nil {
		return mapError(err)
	}
	if org == uuid.Nil || org != r.u.org {
		return &storage.Error{Code: storage.CodeNoTenant, Message: "passkeys run in a unit of work of the installation organisation",
			Hint: "Use Installation.InstallationOrganisation for the unit of work"}
	}
	return nil
}

func toPasskey(c sqlcdb.FyniqWebauthnCredential) storage.Passkey {
	return storage.Passkey{ID: c.ID, UserID: c.UserID, CredentialID: c.CredentialID, PublicKey: c.PublicKey, SignCount: count32(c.SignCount),
		AAGUID: c.Aaguid, Transports: c.Transports, BackupEligible: c.BackupEligible, BackupState: c.BackupState, Name: c.Name,
		CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt, RevokedAt: c.RevokedAt}
}

// count32 is a signature counter as WebAuthn counts it: 0 to 2³²−1, which webauthn_credential_count_range keeps it in.
func count32(n int64) uint32 {
	if n < 0 || n > math.MaxUint32 {
		return math.MaxUint32 // never: the CHECK refuses it; the largest count refuses every later assertion
	}
	return uint32(n)
}

func (r passkeys) Challenge(ctx context.Context, n storage.NewPasskeyChallenge) (uuid.UUID, error) {
	if err := r.installation(ctx); err != nil {
		return uuid.Nil, err
	}
	id, err := r.u.q.IssuePasskeyChallenge(ctx, sqlcdb.IssuePasskeyChallengeParams{SessionHash: n.SessionHash, Purpose: n.Purpose,
		Challenge: n.Challenge, Now: n.Now, ExpiresAt: n.Expires})
	return id, mapError(err)
}

func (r passkeys) Take(ctx context.Context, id uuid.UUID, sessionHash []byte, purpose string, now time.Time) (storage.PasskeyChallenge, error) {
	if err := r.u.check(); err != nil { // any unit of work: the function binds the challenge to its session
		return storage.PasskeyChallenge{}, err
	}
	row, err := r.u.q.TakePasskeyChallenge(ctx, sqlcdb.TakePasskeyChallengeParams{ID: id, SessionHash: sessionHash, Purpose: purpose, Now: now})
	if err != nil {
		return storage.PasskeyChallenge{}, mapError(err)
	}
	if len(row.Challenge) == 0 {
		return storage.PasskeyChallenge{}, ports.ErrNotFound
	}
	return storage.PasskeyChallenge{Challenge: row.Challenge, UserID: row.UserID}, nil
}

func (r passkeys) List(ctx context.Context, userID uuid.UUID, all bool) ([]storage.Passkey, error) {
	if err := r.u.check(); err != nil { // any unit of work: a read
		return nil, err
	}
	rows, err := r.u.q.ListPasskeys(ctx, sqlcdb.ListPasskeysParams{UserID: userID, IncludeRevoked: all})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Passkey, len(rows))
	for i, row := range rows {
		out[i] = toPasskey(row)
	}
	return out, nil
}

func (r passkeys) Register(ctx context.Context, n storage.NewPasskey) (storage.Passkey, error) {
	if err := r.installation(ctx); err != nil {
		return storage.Passkey{}, err
	}
	id, err := r.u.q.RegisterPasskey(ctx, sqlcdb.RegisterPasskeyParams{ChallengeID: n.ChallengeID, SessionHash: n.SessionHash,
		CredentialID: n.CredentialID, PublicKey: n.PublicKey, SignCount: int64(n.SignCount), Aaguid: n.AAGUID, Transports: n.Transports,
		BackupEligible: n.BackupEligible, BackupState: n.BackupState, Name: n.Name, Now: n.Now})
	if err != nil {
		return storage.Passkey{}, mapError(err)
	}
	k, err := r.find(ctx, n.UserID, id)
	if err != nil {
		return storage.Passkey{}, err
	}
	return k, r.u.event(as(ctx, k.UserID), "passkey.registered", "webauthn_credential", k.ID, nil, map[string][2]any{
		"user_id": {nil, k.UserID}, "name": {nil, k.Name}, "aaguid": {nil, uuidOf(k.AAGUID)}, "transports": {nil, k.Transports},
		"backup_eligible": {nil, k.BackupEligible}})
}

// find returns a user's passkey by ID, revoked or not.
func (r passkeys) find(ctx context.Context, userID, id uuid.UUID) (storage.Passkey, error) {
	all, err := r.List(ctx, userID, true)
	if err != nil {
		return storage.Passkey{}, err
	}
	for _, k := range all {
		if k.ID == id {
			return k, nil
		}
	}
	return storage.Passkey{}, &storage.Error{Code: storage.CodePasskey, Message: "the passkey registered is not the user's"}
}

func (r passkeys) StepUp(ctx context.Context, s storage.PasskeyStepUp) error {
	if err := r.installation(ctx); err != nil {
		return err
	}
	return mapError(r.u.q.StepUpSessionWithPasskey(ctx, sqlcdb.StepUpSessionWithPasskeyParams{ChallengeID: s.ChallengeID,
		SessionHash: s.SessionHash, PasskeyID: s.PasskeyID, SignCount: int64(s.SignCount), BackupState: s.BackupState, Now: s.Now}))
}

func (r passkeys) Revoke(ctx context.Context, userID, id uuid.UUID, now time.Time) error {
	if err := r.installation(ctx); err != nil {
		return err
	}
	revoked, err := r.u.q.RevokePasskey(ctx, sqlcdb.RevokePasskeyParams{UserID: userID, ID: id, Now: now})
	if err != nil {
		return mapError(err)
	}
	if !revoked {
		return ports.ErrNotFound
	}
	return r.u.event(as(ctx, userID), "passkey.revoked", "webauthn_credential", id, nil, map[string][2]any{
		"user_id": {nil, userID}, "revoked_at": {nil, now}})
}

// uuidOf shows an authenticator's AAGUID as a UUID, or hex when it is not 16 bytes.
func uuidOf(b []byte) string {
	if id, err := uuid.FromBytes(b); err == nil {
		return id.String()
	}
	return fmt.Sprintf("%x", b)
}

func (r passkeys) ApprovalChallenge(ctx context.Context, n storage.NewApprovalChallenge) (uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return uuid.Nil, err
	}
	id, err := r.u.q.IssueApprovalChallenge(ctx, sqlcdb.IssueApprovalChallengeParams{SessionHash: n.SessionHash, RequestID: n.RequestID,
		EnvelopeHash: n.EnvelopeHash, Nonce: n.Nonce, Now: n.Now, ExpiresAt: n.Expires})
	return id, mapError(err)
}

func (r passkeys) AnswerApproval(ctx context.Context, a storage.PasskeyAnswer) error {
	if err := r.u.check(); err != nil {
		return err
	}
	return mapError(r.u.q.AnswerApprovalChallenge(ctx, sqlcdb.AnswerApprovalChallengeParams{ChallengeID: a.ChallengeID, SessionHash: a.SessionHash,
		PasskeyID: a.PasskeyID, SignCount: int64(a.SignCount), BackupState: a.BackupState, Now: a.Now}))
}
