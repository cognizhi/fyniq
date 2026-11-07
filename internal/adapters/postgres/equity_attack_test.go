//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The equity register's records keep their rules whoever writes them (FR-EQ-15, FR-EQ-16, CMP-PD-07; FY285 to FY287): a
// holder record never holds more than an individual's last 4 identifier characters, a key of its own or a full NRIC; a
// share class belongs to an active share-capital account; and once an account has a recorded class, a share issue of it
// names one of its own classes.
// req: FR-EQ-15
// req: FR-EQ-16
// req: CMP-PD-07
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackEquityRecordsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack equity records Pte Ltd")
	class := shareCapital(t, d)
	api := store(t, "fyniq_api")
	create := func(typ storage.DocumentType, payload string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: typ, Payload: []byte(payload)})
			return err
		})
	}
	holder := fmt.Sprintf(`{"date":"2026-11-07","contact_id":%q,"kind":"individual"`, d.director.ID)
	for name, payload := range map[string]string{
		"a full NRIC as the identifier": holder + `,"id_type":"nric","id_last4":"S1234567D"}`,
		"a full NRIC in the address":    holder + `,"address":"NRIC S1234567D, 1 Raffles Place"}`,
		"a key of its own":              holder + `,"id_number":"S1234567D"}`,
		"an entity's last 4":            fmt.Sprintf(`{"date":"2026-11-07","contact_id":%q,"kind":"entity","id_last4":"123A"}`, d.director.ID),
	} {
		if err := create(storage.DocumentHolderRecord, payload); storage.CodeOf(err) != storage.CodeEquityRecord {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := create(storage.DocumentHolderRecord, holder+`,"id_type":"nric","id_last4":"567D"}`); err != nil {
		t.Fatalf("a record with the last 4 characters: %v", err)
	}
	classOf := func(account fmt.Stringer) string {
		return fmt.Sprintf(`{"date":"2026-11-07","name":"Ordinary","kind":"ordinary","account_id":%q,"votes_per_share":"1","liquidation":{},"dividend":{"kind":"none"},"anti_dilution":"none"}`, account)
	}
	if err := create(storage.DocumentShareClass, classOf(d.loan)); storage.CodeOf(err) != storage.CodeEquityRecord {
		t.Errorf("a class on the director's loan account: %v", err)
	}
	if err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentShareClass, Payload: []byte(classOf(class))})
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	}); err != nil {
		t.Fatalf("recording a class on 3010: %v", err)
	}
	wantState(t, "an issue of 3010 naming no class once 3010 has one", rawShareIssue(t, d, class, "100", "100", true), "FY287")
}

// Every holding moves only with an approved equity transaction or a share issue, whole, never below zero and never
// changed (FR-EQ-17, INV-13; FY288, FY289, FY250, FY249): a transfer nets to zero between two holders and is made
// effective once; a movement of a draft or of another kind of transaction, into a customer, or leaving a holder short is
// refused; a share issue with no movement and an entry from an equity transaction that no conversion names fail at
// commit; and share application monies name a shareholder or investor on every line, who stays one.
// req: FR-EQ-17
// req: INV-13
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackHoldingsMoveOnlyWithTheirTransactions(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack holdings Pte Ltd")
	class := shareCapital(t, d)
	api := store(t, "fyniq_api")
	if err := rawShareIssue(t, d, class, "100", "100", true); err != nil { // Mei Lin holds 100 shares from 14 Oct 2026
		t.Fatal(err)
	}
	var kow, fund storage.Contact
	var monies uuid.UUID
	if err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if kow, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Tan Ah Kow", Shareholder: true}); err != nil {
			return err
		}
		if fund, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Seedling Ventures Pte. Ltd.", Investor: true}); err != nil {
			return err
		}
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2040", Name: "Share application monies", Type: storage.Liability, Role: "share-application-monies"})
		monies = a.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	transaction := func(kind string, posted bool) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentEquityTransaction, Payload: []byte(fmt.Sprintf(`{"kind":%q}`, kind))})
			if err != nil || !posted {
				id = doc.ID
				return err
			}
			id = doc.ID
			return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	on := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	move := func(u storage.UnitOfWork, kind string, tx uuid.UUID, holder uuid.UUID, shares string) error {
		_, err := u.Holdings().Move(ctx, storage.HoldingMovement{ID: uuid.Must(uuid.NewV7()), Kind: kind, TransactionID: &tx, HolderID: holder,
			ClassAccountID: class, Shares: storage.Decimal(shares), EffectiveOn: on, RecordedBy: d.member, RecordedAt: on})
		return err
	}
	transfer := func(tx uuid.UUID, out, in string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := move(u, storage.MovementTransfer, tx, d.director.ID, out); err != nil {
				return err
			}
			return move(u, storage.MovementTransfer, tx, kow.ID, in)
		})
	}
	wantCode := func(name string, err error, codes ...string) {
		t.Helper()
		for _, c := range codes {
			if storage.CodeOf(err) == c {
				return
			}
		}
		t.Errorf("%s: %v (want %v)", name, err, codes)
	}
	wantCode("a transfer still a draft", transfer(transaction("transfer", false), "-60", "60"), storage.CodeHolding)
	wantCode("a transfer's movements under a cash issue", transfer(transaction("issue_cash", true), "-60", "60"), storage.CodeHolding)
	wantCode("a transfer that does not net to zero", transfer(transaction("transfer", true), "-60", "50"), storage.CodeHolding)
	wantCode("a transfer of more shares than the seller holds", transfer(transaction("transfer", true), "-160", "160"), storage.CodeHolding)
	wantCode("shares into a customer", api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		tx := transaction("transfer", true)
		if err := move(u, storage.MovementTransfer, tx, d.director.ID, "-10"); err != nil {
			return err
		}
		return move(u, storage.MovementTransfer, tx, d.customer.ID, "10")
	}), storage.CodeHolding)
	done := transaction("transfer", true)
	if err := transfer(done, "-60", "60"); err != nil {
		t.Fatalf("a transfer of 60 of Mei Lin's 100 shares: %v", err)
	}
	wantCode("the same transfer made effective again", transfer(done, "-1", "1"), storage.CodeHolding)
	wantState(t, "changing a movement", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.holding_movement SET shares = 1000`)
		return err
	}), "FY288", "42501")
	wantState(t, "a share issue with no movement", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
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
VALUES ($1, 1, $2, 'SGD', 50, 1, 'attack', 50), ($1, 2, $3, 'SGD', -50, 1, 'attack', -50)`, entryID, d.bank, class); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO fyniq.share_issue (id, holder_id, class_account_id, shares, price, currency, minor_units, amount, consideration, resolution_refs,
                               effective_on, entry_id, recorded_by, recorded_at)
VALUES ($1, $2, $3, 50, 1, 'SGD', 2, 50, 'cash', 'Board resolution 14 Oct 2026', '2026-10-14', $4, $5, now())`,
			issue, d.director.ID, class, entryID, d.member); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "share_issue.recorded", "share_issue", issue)
	}), "FY288")
	wantState(t, "an entry from an equity transaction no conversion names", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		var entryID uuid.UUID
		if err := tx.QueryRow(ctx, `
INSERT INTO fyniq.journal_entry (posting_date, posting_identity, source_type, source_id, source_version, rule_version)
VALUES ('2026-11-02', $1, 'equity_transaction', $2, 1, 'equity_transaction.class_conversion@1') RETURNING id`, uuid.NewString(), done.String()).Scan(&entryID); err != nil {
			return err
		}
		if err := auditRaw(ctx, tx, "journal_entry.posted", "journal_entry", entryID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount)
VALUES ($1, 1, $2, 'SGD', 50, 1, 'attack', 50), ($1, 2, $3, 'SGD', -50, 1, 'attack', -50)`, entryID, d.bank, class)
		return err
	}), "FY289")

	// Share application monies name their subscriber (B.7; FY250, FY249).
	monyLine := func(party *uuid.UUID) error {
		return asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
			id, err := entry(ctx, tx, "share application monies", "2026-11-02")
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount, party_id)
VALUES ($1, 1, $2, 'SGD', 600000, 1, 'attack', 600000, NULL), ($1, 2, $3, 'SGD', -600000, 1, 'attack', -600000, $4)`, id, d.bank, monies, party)
			return err
		})
	}
	wantState(t, "a line on 2040 naming no subscriber", monyLine(nil), "FY250")
	wantState(t, "a line on 2040 naming a customer", monyLine(&d.customer.ID), "FY250")
	if err := monyLine(&fund.ID); err != nil {
		t.Fatalf("an investor's application monies: %v", err)
	}
	wantCode("unmarking the investor with application monies", api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, fund.ID)
		if err != nil {
			return err
		}
		det := c.ContactDetails
		det.Investor, det.Customer = false, true
		_, err = u.Contacts().Update(ctx, c.ID, c.Version, det, true)
		return err
	}), storage.CodeParty)
}

// An equity transaction's ACRA steps are of its kind, recorded between its approval and its effective date, and never
// change (FR-EQ-09, FR-EQ-17; FY290): a step of a draft, a step its kind does not take, a step once it is effective and
// a change are refused; a step of an approved transfer is recorded.
// req: FR-EQ-17
// req: FR-EQ-09
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackEquityStepsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack equity steps Pte Ltd")
	api := store(t, "fyniq_api")
	transaction := func(kind string, posted bool) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentEquityTransaction, Payload: []byte(fmt.Sprintf(`{"kind":%q}`, kind))})
			id = doc.ID
			if err != nil || !posted {
				return err
			}
			return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	record := func(tx uuid.UUID, kind string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.EquitySteps().Record(ctx, storage.EquityStep{TransactionID: tx, Kind: kind, On: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
				RecordedBy: d.member, RecordedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
			return err
		})
	}
	for name, err := range map[string]error{
		"a step of a draft":            record(transaction("transfer", false), storage.StepInstrumentSigned),
		"a transfer's resolution":      record(transaction("transfer", true), storage.StepAllotmentLodged),
		"an issue's instrument signed": record(transaction("issue_cash", true), storage.StepInstrumentSigned),
		"a step of a reversal":         record(transaction("reversal", true), storage.StepResolved),
	} {
		if storage.CodeOf(err) != storage.CodeEquityStep {
			t.Errorf("%s: %v", name, err)
		}
	}
	transfer := transaction("transfer", true)
	if err := record(transfer, storage.StepInstrumentSigned); err != nil {
		t.Fatalf("the instrument of an approved transfer signed: %v", err)
	}
	wantState(t, "changing a step", asOrg(ctx, conn(t, "fyniq_api"), d.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.equity_step SET on_date = '2026-11-03'`)
		return err
	}), "FY290", "42501")
}
