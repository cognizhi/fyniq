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

// A director's fee posts only under an approved director_fee.post request that neither its requester nor the director
// paid signed: posting a draft fee with no request at all is refused at commit (FR-EQ-04, FM-9; FY252).
// req: FR-EQ-04
// req: QA-ENG-05
// asvs: V2.2.3
func TestAttackADirectorsFeePostsOnlyByApproval(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack director fee Pte Ltd")
	payload := []byte(`{"date":"2026-10-15","contact_id":"` + d.director.ID.String() + `","period_from":"2026-01-01","period_to":"2026-12-31",` +
		`"amount":{"amount":"6000.00","currency":"SGD"},"account_id":"` + d.sales.String() + `"}`)
	err := store(t, "fyniq_api").Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorFee, Payload: payload})
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	})
	if storage.CodeOf(err) != storage.CodeParty {
		t.Errorf("a director's fee posted with no approved request: %v", err)
	}
}

// A settlement is decided only under an approved director_settlement.approve request that neither its requester nor the
// person settled signed: posting a draft settlement with no request at all is refused at commit (FR-EQ-05, FM-9; FY253).
// req: FR-EQ-05
// req: FM-9
// req: QA-ENG-05
// asvs: V2.2.3
func TestAttackASettlementIsDecidedOnlyByApproval(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack director settlement Pte Ltd")
	payload := []byte(`{"date":"2026-10-15","contact_id":"` + d.director.ID.String() + `","account_id":"` + d.loan.String() + `",` +
		`"amount":{"amount":"100.00","currency":"SGD"},"route":"keep","terms":{"interest_rate":"0"}}`)
	err := store(t, "fyniq_api").Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorSettlement, Payload: payload})
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	})
	if storage.CodeOf(err) != storage.CodeParty {
		t.Errorf("a settlement decided with no approved request: %v", err)
	}
}

// A repayment to a director is posted only by the release of a payment batch, which links it in the same transaction
// (FY254, FR-EQ-05, FM-1): recorded by hand, with no release linking it, it is refused at commit.
// req: FR-EQ-05
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackARepaymentIsPostedOnlyByABatch(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack director repayment Pte Ltd")
	payload := []byte(`{"date":"2026-10-15","contact_id":"` + d.director.ID.String() + `","money_account_id":"` + uuid.NewString() + `",` +
		`"settlements":[]}`)
	err := store(t, "fyniq_api").Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorRepayment, Payload: payload})
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	})
	if storage.CodeOf(err) != storage.CodeBatch {
		t.Errorf("a repayment posted with no release linking it: %v", err)
	}
}

// shareCapital gives d's books 3010 Ordinary share capital, and returns its ID.
func shareCapital(t *testing.T, d directorBooks) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := store(t, "fyniq_api").Do(context.Background(), d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3010", Name: "Ordinary share capital", Type: storage.Equity, Role: storage.RoleShareCapital})
		id = a.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// rawShareIssue posts, as the API with raw SQL, an entry from the source share_issue on 2026-10-14 (Dr the bank, Cr
// class with amount) and, when register is true, the register's row for it with credited as its amount.
func rawShareIssue(t *testing.T, d directorBooks, class uuid.UUID, amount, credited string, register bool) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		issue := uuid.Must(uuid.NewV7())
		var entryID uuid.UUID
		if err := tx.QueryRow(ctx, `
INSERT INTO fyniq.journal_entry (posting_date, posting_identity, source_type, source_id, source_version, rule_version)
VALUES ('2026-10-14', $1, 'share_issue', $1, 1, 'share_issue@1') RETURNING id`, issue.String()).Scan(&entryID); err != nil {
			return err
		}
		if err := auditRaw(ctx, tx, "journal_entry.posted", "journal_entry", entryID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount)
VALUES ($1, 1, $2, 'SGD', $4::numeric, 1, 'attack', $4::numeric), ($1, 2, $3, 'SGD', -$4::numeric, 1, 'attack', -$4::numeric)`, entryID, d.bank, class, amount); err != nil {
			return err
		}
		if !register {
			return nil
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO fyniq.share_issue (id, holder_id, class_account_id, shares, price, currency, minor_units, amount, consideration, resolution_refs,
                               effective_on, entry_id, recorded_by, recorded_at)
VALUES ($1, $2, $3, 100, 1, 'SGD', 2, $4::numeric, 'cash', 'Board resolution 14 Oct 2026', '2026-10-14', $5, $6, now())`,
			issue, d.director.ID, class, credited, entryID, d.member); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "share_issue.recorded", "share_issue", issue)
	})
}

// Share capital moves only with the register of share issues (FR-EQ-08, INV-13; FY255): an entry from the source
// share_issue that no issue names is refused at commit, and so is an issue whose entry credits its class with another
// amount; one that matches commits. An issue with no entry before the books start are set is refused, an issue of a
// class that is not share capital is refused, and an issue never changes.
// req: FR-EQ-08
// req: INV-13
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackShareCapitalMovesOnlyWithTheRegister(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack share issues Pte Ltd")
	class := shareCapital(t, d)
	wantState(t, "a share_issue entry no issue names", rawShareIssue(t, d, class, "100", "100", false), "FY255")
	wantState(t, "an issue whose entry credits another amount", rawShareIssue(t, d, class, "100", "90", true), "FY255")
	wantState(t, "an issue of a class that is not share capital", rawShareIssue(t, d, d.loan, "100", "100", true), "FY255", "FY007", "FY250")
	if err := rawShareIssue(t, d, class, "100", "100", true); err != nil {
		t.Fatalf("an issue posted from the register: %v", err)
	}
	wantState(t, "an issue with no entry and no books start", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
INSERT INTO fyniq.share_issue (id, holder_id, class_account_id, shares, price, currency, minor_units, amount, consideration, resolution_refs,
                               effective_on, recorded_by, recorded_at)
VALUES ($1, $2, $3, 100, 1, 'SGD', 2, 100, 'cash', 'Incorporation subscription', '2025-01-02', $4, now())`, id, d.director.ID, class, d.member); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "share_issue.recorded", "share_issue", id)
	}), "FY255")
	wantState(t, "changing an issue", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.share_issue SET shares = 1000`)
		return err
	}), "FY255", "42501")
}

// A conversion's ACRA steps name an approved conversion waiting for ACRA, and never change (FR-EQ-09; FY256): a step of a
// settlement still a draft is refused, and so is changing one.
// req: FR-EQ-09
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackConversionStepsNameAnApprovedConversion(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack conversion steps Pte Ltd")
	payload := []byte(`{"date":"2026-10-15","contact_id":"` + d.director.ID.String() + `","account_id":"` + d.loan.String() + `",` +
		`"amount":{"amount":"100.00","currency":"SGD"},"route":"convert"}`)
	var draft uuid.UUID
	if err := store(t, "fyniq_api").Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorSettlement, Payload: payload})
		draft = doc.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantState(t, "a step of a draft conversion", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO fyniq.conversion_step (settlement_id, kind, on_date, recorded_by, recorded_at) VALUES ($1, 'resolved', '2026-10-06', $2, now())`,
			draft, d.member); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "director_settlement.acra_step", "document", draft)
	}), "FY256")
	wantState(t, "changing a step", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.conversion_step SET on_date = '2026-10-07'`)
		return err
	}), "FY256", "42501")
}
