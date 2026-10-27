//go:build local

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-CT-03 and QA-ENG-05 (PRD-001 rev 0.152): the database keeps a vendor's account number with the version of the
// change that names it, for a vendor, sealed; it refuses an account for another vendor or currency than the version
// names, one in force before its change is approved, one retired that was never in force, a change approved with no
// approved request, and any change of the number or deletion, for every role and even the table's owner (FY237, FY238).
// req: FR-CT-03
// req: QA-ENG-05
// req: SOD-1
// asvs: V2.3.5, V13.2.2
func TestAttackPayeeBankAccountsKeepTheirChange(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := books(t, "Attack FY238 Pte Ltd")
	sealed := make([]byte, 96)
	_, _ = rand.Read(sealed)
	fp := make([]byte, 32)
	_, _ = rand.Read(fp)
	var vendor, customer storage.Contact
	var change storage.Document
	var account storage.PayeeBankAccount
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if vendor, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Northwind Pte Ltd", Vendor: true}); err != nil {
			return err
		}
		if customer, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Only A Customer", Customer: true}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"contact_id": vendor.ID.String(), "currency": "SGD", "number_last4": "6789",
			"number_fingerprint": hex.EncodeToString(fp)})
		if change, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentPayeeBankChange, Payload: payload}); err != nil {
			return err
		}
		account, err = u.PayeeBankAccounts().Add(ctx, storage.PayeeBankAccount{ChangeID: change.ID, ChangeVersion: 1, ContactID: vendor.ID,
			Currency: "SGD", Sealed: sealed, Last4: "6789", Fingerprint: fp})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	attempt := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return s.Do(ctx, b.Org, fn) }
	for name, a := range map[string]storage.PayeeBankAccount{
		"another vendor than the version names":   {ChangeID: change.ID, ChangeVersion: 1, ContactID: customer.ID, Currency: "SGD", Sealed: sealed, Last4: "6789", Fingerprint: fp},
		"another currency than the version names": {ChangeID: change.ID, ChangeVersion: 1, ContactID: vendor.ID, Currency: "USD", Sealed: sealed, Last4: "6789", Fingerprint: fp},
		"another number than the version names":   {ChangeID: change.ID, ChangeVersion: 1, ContactID: vendor.ID, Currency: "SGD", Sealed: sealed, Last4: "0000", Fingerprint: fp},
	} {
		err := attempt(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.PayeeBankAccounts().Add(ctx, a)
			return err
		})
		if c := storage.CodeOf(err); c != storage.CodePayee && c != storage.CodeDuplicate {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, to := range map[string]string{"in force before its change is approved": storage.AccountInForce, "retired, never in force": storage.AccountRetired} {
		err := attempt(func(ctx context.Context, u storage.UnitOfWork) error {
			return u.PayeeBankAccounts().Move(ctx, account, to)
		})
		if storage.CodeOf(err) != storage.CodePayee {
			t.Errorf("an account %s: %v", name, err)
		}
	}
	err := attempt(func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, change.ID, storage.DocumentPosted)
	})
	if storage.CodeOf(err) != storage.CodePayee {
		t.Errorf("a change approved with no approved request: %v", err)
	}

	// The number never changes and the account never goes: no role may, and the guard refuses even the table's owner.
	for _, role := range allRoles {
		c := conn(t, role)
		for _, stmt := range []string{
			"UPDATE fyniq.payee_bank_account SET number_sealed = '\\x00'::bytea || number_sealed",
			"UPDATE fyniq.payee_bank_account SET contact_id = organisation_id",
			"DELETE FROM fyniq.payee_bank_account",
			"TRUNCATE fyniq.payee_bank_account",
		} {
			err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			wantState(t, role+": "+stmt, err, "42501", "FY238")
		}
	}
	for _, stmt := range []string{
		"UPDATE fyniq.payee_bank_account SET number_last4 = '0000'",
		"DELETE FROM fyniq.payee_bank_account",
	} {
		err := asOrg(ctx, conn(t, "fyniq"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		wantState(t, "the owner: "+stmt, err, "FY238")
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.PayeeBankAccounts().OfChange(ctx, change.ID, 1)
		if err == nil && (got.State != storage.AccountKept || got.Last4 != "6789" || string(got.Sealed) != string(sealed)) {
			t.Errorf("the account after the attacks: %+v", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
