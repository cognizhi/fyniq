package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Account recovery (SEC-35, rev 0.198; migration 000242): global rows reached through the functions that check the
// session and the proofs; each change is audited in the trail of the unit of work it runs in. No code or hash is ever
// written to the trail.

func (u *unitOfWork) Recoveries() storage.Recoveries { return recoveries{u} }

type recoveries struct{ u *unitOfWork }

func (r recoveries) Codes(ctx context.Context, user uuid.UUID) (storage.RecoveryCodes, error) {
	if err := r.u.check(); err != nil {
		return storage.RecoveryCodes{}, err
	}
	row, err := r.u.q.UserRecoveryCodes(ctx, user)
	if err != nil {
		return storage.RecoveryCodes{}, mapError(err)
	}
	out := storage.RecoveryCodes{Unused: int(row.Unused)}
	if row.CreatedUnix != 0 {
		out.CreatedAt = time.Unix(row.CreatedUnix, 0).UTC()
	}
	return out, nil
}

func (r recoveries) SetCodes(ctx context.Context, sessionHash []byte, hashes [][]byte, now time.Time) (uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return uuid.Nil, err
	}
	who, err := r.u.q.SetUserRecoveryCodes(ctx, sqlcdb.SetUserRecoveryCodesParams{SessionHash: sessionHash, Hashes: hashes, Now: now})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	return who, r.u.event(as(ctx, who), "recovery_codes.replaced", "user", who, nil, map[string][2]any{"count": {nil, len(hashes)}})
}

func (r recoveries) Confirm(ctx context.Context, by, user, org uuid.UUID, hash []byte, now, expires time.Time) (uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return uuid.Nil, err
	}
	id, err := r.u.q.ConfirmAccountRecovery(ctx, sqlcdb.ConfirmAccountRecoveryParams{ByUser: by, ForUser: user, Org: org, CodeHash: hash,
		Now: now, Expires: expires})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	return id, r.u.event(as(ctx, by), "account_recovery.confirmed", "user", user, nil, map[string][2]any{
		"confirmation_id": {nil, id.String()}, "expires_at": {nil, expires}})
}

func (r recoveries) Recover(ctx context.Context, n storage.NewRecovery) (storage.AccountRecovered, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountRecovered{}, err
	}
	row, err := r.u.q.RecoverAccount(ctx, sqlcdb.RecoverAccountParams{SessionHash: n.SessionHash, CodeHash: n.CodeHash,
		ConfirmationHash: n.ConfirmationHash, NewHashes: n.NewHashes, Now: n.Now, CoolingSeconds: int64(n.CoolingOff / time.Second)})
	if err != nil {
		return storage.AccountRecovered{}, mapError(err)
	}
	out := storage.AccountRecovered{ID: row.RecoveryID, UserID: row.UserID, ConfirmedBy: row.ConfirmedBy, Organisation: row.ConfirmingOrganisation,
		CoolingOffUntil: row.CoolingOffUntil.UTC()}
	return out, r.u.event(as(ctx, out.UserID), "account.recovered", "user", out.UserID, nil, map[string][2]any{
		"recovery_id": {nil, out.ID.String()}, "confirmed_by": {nil, out.ConfirmedBy.String()},
		"confirming_organisation": {nil, out.Organisation.String()}, "cooling_off_until": {nil, out.CoolingOffUntil}})
}
