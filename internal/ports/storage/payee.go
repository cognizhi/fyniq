package storage

import (
	"context"

	"github.com/google/uuid"
)

// PayeeBankAccount is a payee's bank account number as a change of its bank details keeps it, one for each version of
// the change: a vendor's (FR-CT-03; PRD-001 rev 0.152) or a person's own (FR-AP-22; rev 0.154). It is sealed by the
// application (SEC-11) and bound to its change, with its last four characters and its keyed fingerprint within the
// organisation (FR-CT-04, AUD-02). The rest of the account (the bank, the name, the country) is in the change's version.
type PayeeBankAccount struct {
	ID            uuid.UUID
	ChangeID      uuid.UUID
	ChangeVersion int
	ContactID     uuid.UUID // the vendor; uuid.Nil for a person's account
	MemberUserID  uuid.UUID // the person; uuid.Nil for a vendor's account
	Currency      string
	Sealed        []byte
	Last4         string
	Fingerprint   []byte
	State         string // AccountKept, AccountInForce or AccountRetired
}

// The states of a vendor's bank account.
const (
	AccountKept    = "kept"     // its change is not approved (yet)
	AccountInForce = "in_force" // the vendor's account for its currency: its change was approved, and no other one since
	AccountRetired = "retired"  // a change approved later replaced it
)

// PayeeBankAccounts keep the account numbers of the changes of vendors' and people's bank details. Each is kept with its change's
// current draft version, which audits it, and is put in force or retired with a change's approval, audited on its change
// with the last four characters and the fingerprint only (AUD-02). The database refuses an account for a change that is
// not a draft at that version, for another vendor or currency than the version names, or for a contact that is not a
// vendor, or a person's that is not a Singapore account of an active member; one in force before its change is approved,
// or retired but while another change for its payee and currency is approved; any other change, and any deletion
// (CodePayee); and two in force for one payee and currency (CodeDuplicate).
type PayeeBankAccounts interface {
	Add(ctx context.Context, a PayeeBankAccount) (PayeeBankAccount, error)
	// OfChange is a change's account at one of its versions, or ports.ErrNotFound.
	OfChange(ctx context.Context, changeID uuid.UUID, version int) (PayeeBankAccount, error)
	// OfContact is the account of each change of a vendor's bank details, at the change's current version, newest
	// change first.
	OfContact(ctx context.Context, contactID uuid.UUID) ([]PayeeBankAccount, error)
	// OfMember is the account of each change of a person's own bank details, at the change's current version, newest
	// change first.
	OfMember(ctx context.Context, userID uuid.UUID) ([]PayeeBankAccount, error)
	// WithFingerprint are the accounts in force with the fingerprint, any vendor's or person's.
	WithFingerprint(ctx context.Context, fingerprint []byte) ([]PayeeBankAccount, error)
	// Move puts a kept account in force, or retires one in force; one already there is left as it is.
	Move(ctx context.Context, a PayeeBankAccount, to string) error
}
