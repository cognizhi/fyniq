//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// directorBooks are books with a director's account (2500), a director, a customer and a member who joined by an
// invitation.
type directorBooks struct {
	org                uuid.UUID
	bank, sales, loan  uuid.UUID
	director, customer storage.Contact
	member             uuid.UUID
}

func newDirectorBooks(t *testing.T, name string) directorBooks {
	t.Helper()
	ctx := context.Background()
	b := books(t, name)
	st := store(t, "fyniq_api")
	d := directorBooks{org: b.Org, bank: b.Bank.ID, sales: b.Sales.ID}
	hash := make([]byte, 32)
	hash[0], hash[1] = 9, byte(len(name))
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		loan, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2500", Name: "Loan from director / shareholder", Type: storage.Liability,
			Role: "director-loan"})
		if err != nil {
			return err
		}
		d.loan = loan.ID
		if d.director, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Mei Lin Tan", Director: true, Shareholder: true}); err != nil {
			return err
		}
		if d.customer, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Northwind Analytics", Customer: true}); err != nil {
			return err
		}
		_, err = u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: "Mei Lin Tan", Roles: []string{"owner"},
			ExpiresAt: clock.System{}.Now().Add(time.Hour), CreatedAt: clock.System{}.Now()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	j, err := st.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: "Mei Lin Tan", Issuer: "urn:fyniq:attack", Subject: uuid.NewString(),
		Now: clock.System{}.Now()})
	if err != nil {
		t.Fatal(err)
	}
	d.member = j.UserID
	return d
}

// rawLoanLine posts an entry of the bank and the director's account with raw SQL as the API, the account's line naming
// party (nil: none); onBank names party on the bank's line too.
func rawLoanLine(t *testing.T, d directorBooks, party *uuid.UUID, onBank bool) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		id, err := entry(ctx, tx, uuid.NewString(), "2026-10-14")
		if err != nil {
			return err
		}
		var bankParty *uuid.UUID
		if onBank {
			bankParty = party
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount, party_id)
VALUES ($1, 1, $2, 'SGD', 1000, 1, 'attack', 1000, $3)`, id, d.bank, bankParty); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount, party_id)
VALUES ($1, 2, $2, 'SGD', -1000, 1, 'attack', -1000, $3)`, id, d.loan, party)
		return err
	})
}

// Every line on a director's account names a director or shareholder of the organisation, and no other line names a
// person; a contact named by a line stays one (B.7, FR-EQ-01; FY249, FY250).
// req: FR-EQ-01
// req: QA-ENG-05
// asvs: V2.2.3
func TestAttackDirectorAccountLinesNameTheirPerson(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack director lines Pte Ltd")
	wantState(t, "a line on 2500 with no person", rawLoanLine(t, d, nil, false), "FY250")
	wantState(t, "a line on 2500 naming a customer", rawLoanLine(t, d, &d.customer.ID, false), "FY250")
	wantState(t, "a person on the bank's line", rawLoanLine(t, d, &d.director.ID, true), "FY250")
	other := newDirectorBooks(t, "Attack director lines B Pte Ltd")
	wantState(t, "a line naming another organisation's director", rawLoanLine(t, d, &other.director.ID, false), "FY250", "23503")
	if err := rawLoanLine(t, d, &d.director.ID, false); err != nil {
		t.Fatalf("a line on 2500 naming its director: %v", err)
	}

	// Through the port too: the adapter writes the person, and reads it back.
	var posted storage.Entry
	if err := store(t, "fyniq_api").Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		posted, err = u.Ledger().Append(ctx, storage.NewEntry{PostingDate: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
			PostingIdentity: "attack:" + uuid.NewString(), RuleVersion: "attack@1", Source: storage.Source{Type: "attack", ID: uuid.NewString(), Version: 1},
			Lines: []storage.NewLine{
				{AccountID: d.bank, Currency: "SGD", Amount: "250.00", FXRate: "1", FXRateSource: "base", BaseAmount: "250.00"},
				{AccountID: d.loan, Currency: "SGD", Amount: "-250.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-250.00", PartyID: &d.director.ID},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if p := posted.Lines[1].PartyID; p == nil || *p != d.director.ID || posted.Lines[0].PartyID != nil {
		t.Errorf("the posted lines' people: %v, %v", posted.Lines[0].PartyID, posted.Lines[1].PartyID)
	}

	// The director, named by lines, stays a director or shareholder.
	err := asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.contact SET is_director = false, is_shareholder = false, is_customer = true WHERE id = $1", d.director.ID)
		return err
	})
	wantState(t, "unmark a director named by lines", err, "FY249")
	if err := asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE fyniq.contact SET is_director = false WHERE id = $1", d.director.ID); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "contact.updated", "contact", d.director.ID)
	}); err != nil {
		t.Errorf("a director named by lines who stays a shareholder: %v", err)
	}
}

// A contact is created with no member, and its member changes only under an approved request for the change, signed
// by neither its requester nor the member linked (FR-EQ-01, FM-1; FY251).
// req: FR-EQ-01
// req: FM-1
// asvs: V8.2.1
func TestAttackAContactsMemberChangesOnlyByApproval(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack member link Pte Ltd")
	for _, role := range []string{"fyniq_api", "fyniq_migrator"} { // the API may not write the column on insert at all
		err := asOrg(ctx, conn(t, role), d.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO fyniq.contact (legal_name, is_director, member_user_id) VALUES ('Arjun Rao', true, $1)", d.member)
			return err
		})
		wantState(t, "create a contact with its member as "+role, err, "FY251", "42501")
	}
	err := asOrg(ctx, conn(t, "fyniq_migrator"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.contact (legal_name, is_director, member_user_id) VALUES ('Arjun Rao', true, $1)", d.member)
		return err
	})
	wantState(t, "create a contact with its member as the migrator", err, "FY251")
	err = asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE fyniq.contact SET member_user_id = $1, version = version + 1 WHERE id = $2", d.member, d.director.ID); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "contact.member_linked", "contact", d.director.ID)
	})
	wantState(t, "link a member with no approved request", err, "FY251")
	err = store(t, "fyniq_api").Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Contacts().LinkMember(ctx, d.director.ID, d.director.Version, &d.member)
		return err
	})
	if storage.CodeOf(err) != storage.CodeParty {
		t.Errorf("the port links a member with no approved request: %v", err)
	}
	err = asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.contact SET member_user_id = $1, is_director = false, is_shareholder = false WHERE id = $2", d.member, d.customer.ID)
		return err
	})
	wantState(t, "a customer signing in as a member", err, "23514", "FY251")
}
