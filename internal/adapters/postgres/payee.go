package postgres

import (
	"context"
	"encoding/hex"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the account numbers of payees' bank details (FR-CT-03, FR-AP-22)

func (u *unitOfWork) PayeeBankAccounts() storage.PayeeBankAccounts { return payeeBankAccounts{u} }

type payeeBankAccounts struct{ u *unitOfWork }

func toPayeeBankAccount(r sqlcdb.FyniqPayeeBankAccount) storage.PayeeBankAccount {
	out := storage.PayeeBankAccount{ID: r.ID, ChangeID: r.ChangeID, ChangeVersion: int(r.ChangeVersion), Currency: r.Currency,
		Sealed: r.NumberSealed, Last4: r.NumberLast4, Fingerprint: r.NumberFingerprint, State: r.State}
	if r.ContactID != nil {
		out.ContactID = *r.ContactID
	}
	if r.MemberUserID != nil {
		out.MemberUserID = *r.MemberUserID
	}
	return out
}

// orNil is an ID as an optional column: nil for uuid.Nil.
func orNil(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// payeeOf is how the audit trail names an account's payee: the vendor, or the person.
func payeeOf(a storage.PayeeBankAccount) (string, string) {
	if a.MemberUserID != uuid.Nil {
		return "member_user_id", a.MemberUserID.String()
	}
	return "contact_id", a.ContactID.String()
}

// maskedAccount is how the audit trail shows an account number: its last four characters, and its fingerprint (AUD-02).
func maskedAccount(a storage.PayeeBankAccount) map[string]any {
	return map[string]any{"last4": a.Last4, "fingerprint": hex.EncodeToString(a.Fingerprint)}
}

func (r payeeBankAccounts) Add(ctx context.Context, a storage.PayeeBankAccount) (storage.PayeeBankAccount, error) {
	if err := r.u.check(); err != nil {
		return storage.PayeeBankAccount{}, err
	}
	row, err := r.u.q.InsertPayeeBankAccount(ctx, sqlcdb.InsertPayeeBankAccountParams{ChangeID: a.ChangeID, ChangeVersion: small(a.ChangeVersion),
		ContactID: orNil(a.ContactID), MemberUserID: orNil(a.MemberUserID), Currency: a.Currency, NumberSealed: a.Sealed, NumberLast4: a.Last4,
		NumberFingerprint: a.Fingerprint})
	if err != nil {
		return storage.PayeeBankAccount{}, mapError(err)
	}
	out := toPayeeBankAccount(row)
	v := out.ChangeVersion
	field, payee := payeeOf(out)
	if err := r.u.event(ctx, "document.bank_account_kept", "document", out.ChangeID, &v, map[string][2]any{
		"account_number": {nil, maskedAccount(out)}, field: {nil, payee}, "currency": {nil, out.Currency}}); err != nil {
		return storage.PayeeBankAccount{}, err
	}
	return out, nil
}

func (r payeeBankAccounts) OfChange(ctx context.Context, changeID uuid.UUID, version int) (storage.PayeeBankAccount, error) {
	if err := r.u.check(); err != nil {
		return storage.PayeeBankAccount{}, err
	}
	row, err := r.u.q.PayeeBankAccountOfChange(ctx, sqlcdb.PayeeBankAccountOfChangeParams{ChangeID: changeID, ChangeVersion: small(version)})
	if err != nil {
		return storage.PayeeBankAccount{}, mapError(err)
	}
	return toPayeeBankAccount(row), nil
}

func (r payeeBankAccounts) OfContact(ctx context.Context, contactID uuid.UUID) ([]storage.PayeeBankAccount, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PayeeBankAccountsOfContact(ctx, &contactID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayeeBankAccount, len(rows))
	for i, x := range rows {
		out[i] = toPayeeBankAccount(x)
	}
	return out, nil
}

func (r payeeBankAccounts) OfMember(ctx context.Context, userID uuid.UUID) ([]storage.PayeeBankAccount, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PayeeBankAccountsOfMember(ctx, &userID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayeeBankAccount, len(rows))
	for i, x := range rows {
		out[i] = toPayeeBankAccount(x)
	}
	return out, nil
}

func (r payeeBankAccounts) WithFingerprint(ctx context.Context, fingerprint []byte) ([]storage.PayeeBankAccount, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PayeeBankAccountsWithFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayeeBankAccount, len(rows))
	for i, x := range rows {
		out[i] = toPayeeBankAccount(x)
	}
	return out, nil
}

func (r payeeBankAccounts) Move(ctx context.Context, a storage.PayeeBankAccount, to string) error {
	if err := r.u.check(); err != nil {
		return err
	}
	n, err := r.u.q.MovePayeeBankAccount(ctx, sqlcdb.MovePayeeBankAccountParams{ID: a.ID, State: to})
	if err != nil || n == 0 {
		return mapError(err)
	}
	v := a.ChangeVersion
	return r.u.event(ctx, "document.bank_account_"+to, "document", a.ChangeID, &v, map[string][2]any{
		"state": {a.State, to}, "account_number": {maskedAccount(a), maskedAccount(a)}, "currency": {a.Currency, a.Currency}})
}
