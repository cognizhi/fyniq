//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The QA-ENG-05 attack suite: each test bypasses the application, connects as a database role and
// tries a write the invariants forbid. Every attempt must fail, and the books must be unchanged.

var allRoles = append([]string{"fyniq_migrator"}, serviceRoles...)

// entry inserts a journal entry directly, as a writer that skips the posting engine would. Like any
// correct writer it also writes the entry's audit event, so the attacks reach the invariant they aim
// at; TestAttackChangesWithoutAnAuditEventFail covers the event's absence.
func entry(ctx context.Context, tx pgx.Tx, identity, date string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
INSERT INTO fyniq.journal_entry (posting_date, posting_identity, source_type, source_id, source_version, rule_version)
VALUES ($1, $2, 'attack', $2, 1, 'attack@1') RETURNING id`, date, identity).Scan(&id)
	if err != nil {
		return id, err
	}
	return id, auditRaw(ctx, tx, "journal_entry.posted", "journal_entry", id)
}

// auditRaw writes a minimal audit event about an object with raw SQL, for the organisation of the
// transaction.
func auditRaw(ctx context.Context, tx pgx.Tx, action, objectType string, id uuid.UUID) error {
	eventID := uuid.Must(uuid.NewV7())
	var org string
	if err := tx.QueryRow(ctx, "SELECT current_setting('app.org_id')").Scan(&org); err != nil {
		return err
	}
	body := fmt.Sprintf(`{"schema":"fyniq-audit-event@1","event_id":%q,"organisation_id":%q,"occurred_at":"2026-10-14T00:00:00Z",`+
		`"actor":{"type":"user","id":"attack-test"},"on_behalf_of":null,"originator":null,"approval_ids":[],"action":%q,`+
		`"object":{"type":%q,"id":%q,"version":null},"changes":{},"reason":"","request":{"id":"","ip":"","user_agent":"","session":""}}`,
		eventID, org, action, objectType, id)
	_, err := tx.Exec(ctx, "INSERT INTO fyniq.audit_event (id, body) VALUES ($1, $2)", eventID, body)
	return err
}

// line inserts one journal line directly.
func line(ctx context.Context, tx pgx.Tx, entryID uuid.UUID, n int, account uuid.UUID, currency, amount, rate, base string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount)
VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, 'attack', $7::numeric)`, entryID, n, account, currency, amount, rate, base)
	return err
}

// postRaw posts a two-line SGD entry with raw SQL and returns its id.
func postRaw(t *testing.T, c *pgx.Conn, b porttest.Books, identity, date, amount string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error {
		var err error
		if id, err = entry(ctx, tx, identity, date); err != nil {
			return err
		}
		if err = line(ctx, tx, id, 1, b.Bank.ID, "SGD", amount, "1", amount); err != nil {
			return err
		}
		return line(ctx, tx, id, 2, b.Sales.ID, "SGD", "-"+amount, "1", "-"+amount)
	})
	if err != nil {
		t.Fatalf("post %s: %v", identity, err)
	}
	return id
}

func wantState(t *testing.T, what string, err error, states ...string) {
	t.Helper()
	if got := sqlstate(err); !slices.Contains(states, got) {
		t.Errorf("%s: want SQLSTATE %v, got %q (%v)", what, states, got, err)
	}
}

// count counts the rows of table visible to org, as the superuser's tenant-scoped view.
func count(t *testing.T, org uuid.UUID, table string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := asOrg(ctx, conn(t, "fyniq_readonly"), org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq."+table).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// req: INV-01
// req: ENG-22
// req: QA-ENG-05
// asvs: V2.2.3, V2.3.3
func TestAttackUnbalancedEntriesFailAtCommit(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack INV-01 Pte Ltd")
	c := conn(t, "fyniq_api")
	jpy := func(tx pgx.Tx, id uuid.UUID) error {
		// Base balances (0.10 − 0.10) but the yen do not (100 − 104): single-currency entries
		// balance in their own currency too.
		if err := line(ctx, tx, id, 1, b.Bank.ID, "JPY", "100", "0.001", "0.10"); err != nil {
			return err
		}
		return line(ctx, tx, id, 2, b.Sales.ID, "JPY", "-104", "0.001", "-0.10")
	}
	for name, lines := range map[string]func(pgx.Tx, uuid.UUID) error{
		"debits exceed credits": func(tx pgx.Tx, id uuid.UUID) error {
			if err := line(ctx, tx, id, 1, b.Bank.ID, "SGD", "100.00", "1", "100.00"); err != nil {
				return err
			}
			return line(ctx, tx, id, 2, b.Sales.ID, "SGD", "-99.99", "1", "-99.99")
		},
		"one line": func(tx pgx.Tx, id uuid.UUID) error {
			return line(ctx, tx, id, 1, b.Bank.ID, "SGD", "0.00", "1", "0.00")
		},
		"no lines":           func(pgx.Tx, uuid.UUID) error { return nil },
		"currency imbalance": jpy,
		"a line after an immediate check": func(tx pgx.Tx, id uuid.UUID) error {
			if err := line(ctx, tx, id, 1, b.Bank.ID, "SGD", "10.00", "1", "10.00"); err != nil {
				return err
			}
			if err := line(ctx, tx, id, 2, b.Sales.ID, "SGD", "-10.00", "1", "-10.00"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
				return err
			}
			return line(ctx, tx, id, 3, b.Bank.ID, "SGD", "5.00", "1", "5.00")
		},
	} {
		err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error {
			id, err := entry(ctx, tx, "unbalanced "+name, "2026-10-14")
			if err != nil {
				return err
			}
			return lines(tx, id)
		})
		wantState(t, name, err, "FY001")
	}
	if n := count(t, b.Org, "journal_entry"); n != 0 {
		t.Errorf("%d entries were stored; want none", n)
	}
}

// req: INV-02
// req: QA-ENG-05
// asvs: V13.2.2
// req: data/data-model Insert-only ledger records
func TestAttackPostedHistoryCannotChange(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack INV-02 Pte Ltd")
	id := postRaw(t, conn(t, "fyniq_api"), b, "posted", "2026-10-14", "100.00")
	for _, role := range allRoles {
		c := conn(t, role)
		for _, stmt := range []string{
			"UPDATE fyniq.journal_line SET amount = amount * 2",
			"DELETE FROM fyniq.journal_line",
			"UPDATE fyniq.journal_entry SET posting_date = '2026-10-01'",
			"DELETE FROM fyniq.journal_entry",
			"TRUNCATE fyniq.journal_line",
			"TRUNCATE fyniq.journal_entry CASCADE",
		} {
			err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			wantState(t, role+": "+stmt, err, "42501", "FY002")
		}
	}
	// No line joins a posted entry later, even a balanced pair.
	err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		if err := line(ctx, tx, id, 3, b.Bank.ID, "SGD", "1.00", "1", "1.00"); err != nil {
			return err
		}
		return line(ctx, tx, id, 4, b.Sales.ID, "SGD", "-1.00", "1", "-1.00")
	})
	wantState(t, "add lines to a posted entry", err, "FY002")
	if n := count(t, b.Org, "journal_line"); n != 2 {
		t.Errorf("%d lines after the attacks; want the original 2", n)
	}
}

// req: INV-03
// req: QA-ENG-05
// asvs: V2.3.2
func TestAttackPostingIntoAClosedPeriodFails(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack INV-03 Pte Ltd")
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, b.Oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"fyniq_api", "fyniq_worker", "fyniq_executor"} {
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := entry(ctx, tx, "closed "+role, "2026-10-31")
			return err
		})
		wantState(t, role+" posts into closed October", err, "FY003")
	}
	err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := entry(ctx, tx, "no period", "2027-01-15")
		return err
	})
	wantState(t, "post where no period exists", err, "FY030")
	// The period's own row cannot be moved to dodge the lock.
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.period SET status = 'open', closed_revision = NULL, starts_on = '2026-09-30' WHERE id = $1", b.Oct.ID)
		return err
	})
	wantState(t, "move a period's dates", err, "42501", "FY031")
}

// req: INV-04
// req: QA-ENG-05
// asvs: V2.2.3, V8.4.1
func TestAttackPostingToGroupInactiveOrForeignAccountsFails(t *testing.T) {
	ctx := context.Background()
	b, other := books(t, "Attack INV-04 Pte Ltd"), books(t, "Attack INV-04 Other Pte Ltd")
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().SetActive(ctx, b.Rent.ID, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for name, account := range map[string]uuid.UUID{
		"a group account":                b.Expenses.ID,
		"an inactive account":            b.Rent.ID,
		"another organisation's account": other.Bank.ID,
		"no account":                     uuid.New(),
	} {
		err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
			id, err := entry(ctx, tx, "to "+name, "2026-10-14")
			if err != nil {
				return err
			}
			if err := line(ctx, tx, id, 1, account, "SGD", "1.00", "1", "1.00"); err != nil {
				return err
			}
			return line(ctx, tx, id, 2, b.Sales.ID, "SGD", "-1.00", "1", "-1.00")
		})
		wantState(t, "post to "+name, err, "FY004")
	}
	// A group account cannot be posted to by turning a leaf with postings into a group either.
	postRaw(t, conn(t, "fyniq_api"), b, "leaf", "2026-10-14", "3.00")
	err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.account SET is_group = true WHERE id = $1", b.Bank.ID)
		return err
	})
	wantState(t, "turn an account with postings into a group", err, "FY041")
}

// A manual journal line to a share-capital account is refused by the posting-source check, however it
// is written; another source (the register of share issues, P1) may post there. The mark itself cannot
// change once the account exists.
// req: INV-13
// req: QA-ENG-05
// asvs: V2.2.3, V2.3.3
func TestAttackManualJournalToShareCapitalFails(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack INV-13 Pte Ltd")
	var capital storage.Account
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		capital, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3010", Name: "Share capital, ordinary", Type: storage.Equity,
			Role: storage.RoleShareCapital})
		return err
	}); err != nil || capital.Role != storage.RoleShareCapital {
		t.Fatalf("create the share-capital account: %v (%+v)", err, capital)
	}
	post := func(source string) error {
		return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `
INSERT INTO fyniq.journal_entry (posting_date, posting_identity, source_type, source_id, source_version, rule_version)
VALUES ('2026-10-14', $1, $2, $1, 1, 'attack@1') RETURNING id`, uuid.NewString(), source).Scan(&id); err != nil {
				return err
			}
			if err := auditRaw(ctx, tx, "journal_entry.posted", "journal_entry", id); err != nil {
				return err
			}
			if err := line(ctx, tx, id, 1, b.Bank.ID, "SGD", "1000.00", "1", "1000.00"); err != nil {
				return err
			}
			return line(ctx, tx, id, 2, capital.ID, "SGD", "-1000.00", "1", "-1000.00")
		})
	}
	wantState(t, "a manual journal line to share capital", post("manual_journal"), "FY007")
	// INV-13 is an allow list since 000054: only the register of share issues and the opening balances reach share
	// capital; any other source is refused, a new one included (rev 0.49).
	wantState(t, "an invoice line to share capital", post("invoice"), "FY007")
	for _, source := range []string{"share_issue", "opening_balance"} {
		if err := post(source); err != nil {
			t.Errorf("%s could not post to share capital: %v", source, err)
		}
	}
	err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.account SET role = NULL WHERE id = $1", capital.ID)
		return err
	})
	wantState(t, "unmark a share-capital account", err, "FY040")
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.account SET role = 'share-capital' WHERE id = $1", b.Sales.ID)
		return err
	})
	wantState(t, "mark an account as the API", err, "42501")
}

// The opening balances are one document per organisation, of their own type, never changed or removed (FR-GL-06,
// rev 0.49).
// req: FR-GL-06
func TestAttackOpeningBalancesAreOneAndOnlyTheyReachShareCapital(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack opening balances Pte Ltd")
	st := store(t, "fyniq_api")
	var first, journal storage.Document
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if first, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOpeningBalance, Payload: []byte(`{"lines":[]}`)}); err != nil {
			return err
		}
		journal, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[]}`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var found storage.Document
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		found, err = u.Documents().OpeningBalance(ctx)
		return err
	}); err != nil || found.ID != first.ID || found.Type != storage.DocumentOpeningBalance {
		t.Fatalf("the opening balances: %v %+v", err, found)
	}
	// A second one is refused, through the port or by hand.
	err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOpeningBalance, Payload: []byte(`{"lines":[]}`)})
		return err
	})
	if storage.CodeOf(err) != storage.CodeDuplicate {
		t.Errorf("a second opening balances document: %v", err)
	}
	other := books(t, "Attack opening balances B Pte Ltd")
	if err := st.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().OpeningBalance(ctx)
		return err
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organisation sees an opening balances document: %v", err)
	}
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "INSERT INTO fyniq.opening_balance (document_id) VALUES ('" + journal.ID.String() + "')", "FY054"},
		{"fyniq_migrator", "DELETE FROM fyniq.opening_balance", "FY054"},
		{"fyniq_migrator", "UPDATE fyniq.opening_balance SET document_id = '" + journal.ID.String() + "'", "FY054"},
		{"fyniq_api", "DELETE FROM fyniq.opening_balance", "42501"},
		{"fyniq_api", "UPDATE fyniq.document SET type = 'opening_balance' WHERE id = '" + journal.ID.String() + "'", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
	// A second opening balances document by hand: the organisation's key refuses it.
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.document (type) VALUES ('opening_balance') RETURNING id").Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.opening_balance (document_id) VALUES ($1)", id)
		return err
	})
	wantState(t, "a second opening balances row", err, "23505")
}

// The close's records never change, and the database refuses a close attested by the one who ran its checks, on checks
// that are not clean or not of that period, before them, or with a snapshot whose hash is not its own (FM-4, AUD-04).
// req: FM-4
// req: AUD-04
// req: FR-GL-07
func TestAttackTheCloseIsAttestedByAnotherOnItsOwnChecks(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack the close Pte Ltd")
	st := store(t, "fyniq_api")
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	var clean, dirty storage.CloseCheck
	if err := st.Do(as("preparer"), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if clean, err = u.Closes().RecordCheck(ctx, storage.NewCloseCheck{PeriodID: b.Oct.ID, Result: []byte(`[]`), Clean: true}); err != nil {
			return err
		}
		dirty, err = u.Closes().RecordCheck(ctx, storage.NewCloseCheck{PeriodID: b.Oct.ID, Result: []byte(`[]`), Clean: false})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if clean.CheckedBy != "user:preparer" {
		t.Fatalf("who ran the checks: %q", clean.CheckedBy)
	}
	insert := func(period, check uuid.UUID, by, at, snapshot, hash string) string {
		return fmt.Sprintf(`INSERT INTO fyniq.period_close (period_id, check_id, attested_by, attested_at, statement, closed_revision, snapshot, snapshot_hash)
VALUES ('%s', '%s', '%s', %s, 'Reviewed.', 0, '%s', %s)`, period, check, by, at, snapshot, hash)
	}
	good := "sha256(convert_to('{}', 'UTF8'))"
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "attested by the one who ran the checks", insert(b.Oct.ID, clean.ID, "user:preparer", "now()", "{}", good), "FY153"},
		{"fyniq_api", "on checks that refused the close", insert(b.Oct.ID, dirty.ID, "user:attester", "now()", "{}", good), "FY153"},
		{"fyniq_api", "on another period's checks", insert(b.Nov.ID, clean.ID, "user:attester", "now()", "{}", good), "FY153"},
		{"fyniq_api", "before the checks ran", insert(b.Oct.ID, clean.ID, "user:attester", "now() - interval '1 day'", "{}", good), "FY153"},
		{"fyniq_api", "a snapshot whose hash is not its own", insert(b.Oct.ID, clean.ID, "user:attester", "now()", "{}", "sha256('other')"), "23514"},
		{"fyniq_api", "checks that are not a list", "INSERT INTO fyniq.period_close_check (period_id, checked_by, checked_at, ledger_revision, result, clean) VALUES ('" +
			b.Oct.ID.String() + "', 'user:x', now(), 0, '{}', true)", "23514"},
		{"fyniq_migrator", "a run of the checks rewritten", "UPDATE fyniq.period_close_check SET clean = true", "FY152"},
		{"fyniq_migrator", "a run of the checks removed", "DELETE FROM fyniq.period_close_check", "FY152"},
		{"fyniq_api", "a run of the checks rewritten by the API", "UPDATE fyniq.period_close_check SET clean = true", "42501"},
		{"fyniq_ai", "checks run by the AI", "INSERT INTO fyniq.period_close_check (period_id, checked_by, checked_at, ledger_revision, result, clean) VALUES ('" +
			b.Oct.ID.String() + "', 'ai:x', now(), 0, '[]', true)", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.what, err, c.state)
	}
	// Another organisation sees none of it.
	other := books(t, "Attack the close B Pte Ltd")
	if err := st.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Closes().Check(ctx, clean.ID)
		return err
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organisation reads the checks: %v", err)
	}
}

// nanAttacks try NaN and an out-of-range value in each numeric column; TestEveryNumericColumn…
// fails when a numeric column has no entry here.
var nanAttacks = map[string]func(t *testing.T, b porttest.Books, value string) error{
	"bank_transfer.from_amount":             func(t *testing.T, b porttest.Books, v string) error { return rawTransfer(t, b, "from", v) },
	"bank_transfer.to_amount":               func(t *testing.T, b porttest.Books, v string) error { return rawTransfer(t, b, "to", v) },
	"bank_transfer.fee":                     func(t *testing.T, b porttest.Books, v string) error { return rawTransfer(t, b, "fee", v) },
	"bank_signal.amount":                    func(t *testing.T, b porttest.Books, v string) error { return rawSignal(t, b, v) },
	"bank_reconciliation.statement_balance": func(t *testing.T, b porttest.Books, v string) error { return rawReconciliation(t, b, "statement", v) },
	"bank_reconciliation.book_balance":      func(t *testing.T, b porttest.Books, v string) error { return rawReconciliation(t, b, "book", v) },
	"bank_reconciliation.unreconciled_total": func(t *testing.T, b porttest.Books, v string) error {
		return rawReconciliation(t, b, "unreconciled", v)
	},
	"bank_reconciliation.difference": func(t *testing.T, b porttest.Books, v string) error { return rawReconciliation(t, b, "difference", v) },
	"bank_statement.opening_balance": func(t *testing.T, b porttest.Books, v string) error { return rawBank(t, b, "opening", v) },
	"bank_statement.closing_balance": func(t *testing.T, b porttest.Books, v string) error { return rawBank(t, b, "closing", v) },
	"bank_line.amount":               func(t *testing.T, b porttest.Books, v string) error { return rawBank(t, b, "amount", v) },
	"bank_line.running_balance":      func(t *testing.T, b porttest.Books, v string) error { return rawBank(t, b, "running", v) },
	"bank_gap.expected_opening":      func(t *testing.T, b porttest.Books, v string) error { return rawBank(t, b, "expected", v) },
	"bank_gap.found_opening":         func(t *testing.T, b porttest.Books, v string) error { return rawBank(t, b, "found", v) },
	"journal_line.amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawLine(t, b, "SGD", v, "1", "1.00")
	},
	"journal_line.fx_rate": func(t *testing.T, b porttest.Books, v string) error {
		return rawLine(t, b, "USD", "1.00", v, "1.35")
	},
	"journal_line.base_amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawLine(t, b, "SGD", "1.00", "1", v)
	},
	"balance.amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawBalance(t, b, b.Bank.ID, v, "0")
	},
	"balance.base_amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawBalance(t, b, b.Bank.ID, "0", v)
	},
	"item_price.unit_price": func(t *testing.T, b porttest.Books, v string) error {
		return rawItemPrice(t, b, v)
	},
	"period_variance_explanation.current_amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawExplanation(t, b, v, "0")
	},
	"period_variance_explanation.previous_amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawExplanation(t, b, "0", v)
	},
	"contact.discount_percent": func(t *testing.T, b porttest.Books, v string) error {
		ctx := context.Background()
		return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO fyniq.contact (legal_name, is_vendor, discount_percent, discount_days) VALUES ('Attack', true, $1::numeric, 10)`, v)
			return err
		})
	},
	"withholding_decision.rate_percent": func(t *testing.T, b porttest.Books, v string) error {
		return rawWithholding(t, b, v)
	},
	"fx_rate.rate": func(t *testing.T, b porttest.Books, v string) error {
		return rawFXRate(t, b, v, "1.35")
	},
	"fx_rate.quote": func(t *testing.T, b porttest.Books, v string) error {
		return rawFXRate(t, b, "1.35", v)
	},
	"fx_feed_rate.rate": func(t *testing.T, b porttest.Books, v string) error {
		return rawFeedRate(t, v, "1.35")
	},
	"fx_feed_rate.quote": func(t *testing.T, b porttest.Books, v string) error {
		return rawFeedRate(t, "1.35", v)
	},
	"allocation.amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawAllocation(t, b, v, "1.00", "0.00", "0")
	},
	"allocation.base_amount": func(t *testing.T, b porttest.Books, v string) error {
		return rawAllocation(t, b, "1.00", v, "0.00", "0")
	},
	"allocation.realised_fx": func(t *testing.T, b porttest.Books, v string) error {
		return rawAllocation(t, b, "1.00", "1.00", v, "0")
	},
	"allocation.tolerance": func(t *testing.T, b porttest.Books, v string) error {
		return rawAllocation(t, b, "1.00", "1.00", "0.00", v)
	},
	"allocation.tax": func(t *testing.T, b porttest.Books, v string) error {
		return rawDepositUse(t, b, v)
	},
}

// rawDepositUse settles a customer deposit from the invoice that deducts it, with the output tax the use takes, as the
// owner, both made in the same transaction, so only the allocation's own checks can fail.
func rawDepositUse(t *testing.T, b porttest.Books, tax string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		var inv, dep uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.document (type) VALUES ('invoice') RETURNING id").Scan(&inv); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.document (type) VALUES ('customer_deposit') RETURNING id").Scan(&dep); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.allocation (source_id, source_version, document_id, on_date, currency, minor_units, amount,
  base_currency, base_minor_units, base_amount, realised_fx, tax, written_by, written_at)
VALUES ($1, 1, $2, '2026-10-14', 'SGD', 2, 1.00, 'SGD', 2, 1.00, 0, $3::numeric, 'user:attack-test', now())`, inv, dep, tax)
		return err
	})
}

// rawAllocation settles a bill from a bill payment as the owner, both made in the same transaction, so only the
// allocation's own checks can fail.
func rawAllocation(t *testing.T, b porttest.Books, amount, base, fx, tolerance string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		var payment, bill uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.document (type) VALUES ('bill_payment') RETURNING id").Scan(&payment); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.document (type) VALUES ('bill') RETURNING id").Scan(&bill); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.allocation (source_id, source_version, document_id, on_date, currency, minor_units, amount,
  base_currency, base_minor_units, base_amount, realised_fx, tolerance, written_by, written_at)
VALUES ($1, 1, $2, '2026-10-14', 'SGD', 2, $3::numeric, 'SGD', 2, $4::numeric, $5::numeric, $6::numeric, 'user:attack-test', now())`, payment, bill, amount, base, fx,
			tolerance)
		return err
	})
}

// rawFXRate enters a USD rate for a day as the API role, so only the rate's own checks can fail.
func rawFXRate(t *testing.T, b porttest.Books, rate, quote string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.fx_rate (kind, currency, on_date, rate, quote, quote_form, entered_by, entered_at)
VALUES ('day', 'USD', '2026-10-16', $1::numeric, $2::numeric, 'base-per-unit', 'user:attack-test', now())`, rate, quote)
		return err
	})
}

// rawWithholding records a decision to withhold at rate as the API role, for a supplier made in the same transaction, so
// only the rate's own checks can fail.
func rawWithholding(t *testing.T, b porttest.Books, rate string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		var supplier string
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.contact (legal_name, is_vendor, country) VALUES ('Attack Inc', true, 'US') RETURNING id`).Scan(&supplier); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.withholding_decision (supplier_id, nature, outcome, rate_percent, tax_agent, recorded_by)
VALUES ($1, 'interest', 'withhold', $2::numeric, 'Attack', 'user:attack-test')`, supplier, rate)
		return err
	})
}

// rawFeedRate stores a feed rate as the owner, as no application role writes the table but through record_feed_rate.
func rawFeedRate(t *testing.T, rate, quote string) error {
	t.Helper()
	ctx := context.Background()
	_, err := conn(t, "fyniq_migrator").Exec(ctx, `INSERT INTO fyniq.fx_feed_rate (base_currency, currency, on_date, rate, quote, quote_form, feed)
VALUES ('SGD', 'EUR', '1999-01-04', $1::numeric, $2::numeric, 'base-per-unit', 'Attack')`, rate, quote)
	return err
}

// rawExplanation explains rent's November variance as the API role, so only the amounts' own checks can fail.
func rawExplanation(t *testing.T, b porttest.Books, current, previous string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.period_variance_explanation
  (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at)
VALUES ($1, $2, $3::numeric, $4::numeric, 'Attack', 'user:attack-test', now())`, b.Nov.ID, b.Rent.ID, current, previous)
		return err
	})
}

// rawItemPrice adds an item with one SGD price as the API role, audited, so only the price's own checks can fail.
func rawItemPrice(t *testing.T, b porttest.Books, price string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.item (code, name, type) VALUES ($1, 'Attack', 'service') RETURNING id",
			"ATK-"+uuid.NewString()[:8]).Scan(&id); err != nil {
			return err
		}
		if err := auditRaw(ctx, tx, "item.created", "item", id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.item_price (item_id, currency, unit_price) VALUES ($1, 'SGD', $2::numeric)", id, price)
		return err
	})
}

// rawLine posts one attacking line with a balancing partner as the API role.
func rawLine(t *testing.T, b porttest.Books, currency, amount, rate, base string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		id, err := entry(ctx, tx, uuid.NewString(), "2026-10-14")
		if err != nil {
			return err
		}
		if err := line(ctx, tx, id, 1, b.Bank.ID, currency, amount, rate, base); err != nil {
			return err
		}
		return line(ctx, tx, id, 2, b.Sales.ID, "SGD", "-1.00", "1", "-1.00")
	})
}

// rawBalance writes a balance row as the owner: no application role may write the cache at all, so
// this proves the column constraints themselves.
func rawBalance(t *testing.T, b porttest.Books, account uuid.UUID, amount, base string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO fyniq.balance (organisation_id, account_id, period_id, currency, minor_units, amount, base_amount, line_count)
VALUES ($1, $2, $3, 'SGD', 2, $4::numeric, $5::numeric, 1)`, b.Org, account, b.Oct.ID, amount, base)
		return err
	})
}

// req: INV-05
// req: QA-ENG-05
// asvs: V2.2.1
// req: data/data-model Money columns
func TestAttackAmountsOffScaleNaNOrOutOfRangeFail(t *testing.T) {
	b := books(t, "Attack INV-05 Pte Ltd")
	for col, attack := range nanAttacks {
		wantState(t, col+" = NaN", attack(t, b, "NaN"), "23514")
		wantState(t, col+" = 1e17", attack(t, b, "100000000000000000"), "22003")
	}
	for name, err := range map[string]error{
		"SGD with three decimals":         rawLine(t, b, "SGD", "1.005", "1", "1.01"),
		"yen with decimals":               rawLine(t, b, "JPY", "1.5", "0.0091", "0.01"),
		"base not amount × rate (RND-FX)": rawLine(t, b, "USD", "100.00", "1.35", "135.01"),
		"a zero rate":                     rawLine(t, b, "USD", "1.00", "0", "0.00"),
		"a negative rate":                 rawLine(t, b, "USD", "1.00", "-1.35", "-1.35"),
		"a base-currency line at rate 2":  rawLine(t, b, "SGD", "1.00", "2", "2.00"),
		"infinity":                        rawLine(t, b, "SGD", "Infinity", "1", "1.00"),
	} {
		wantState(t, name, err, "23514", "22003")
	}
	wantState(t, "an unknown currency", rawLine(t, b, "XYZ", "1.00", "1", "1.00"), "FY005")
	// A base-only line passes the row check and is refused at commit: only the revaluation writes one (rev 0.72).
	wantState(t, "a base-only line from a manual journal", rawLine(t, b, "USD", "0.00", "1.35", "1.00"), "FY008")
	if n := count(t, b.Org, "journal_line"); n != 0 {
		t.Errorf("%d lines were stored; want none", n)
	}
}

// req: INV-06
// req: SEC-02
// req: QA-ENG-05
// asvs: V8.4.1
// req: data/data-model Keys
// req: data/data-model Tenancy and row-level security
func TestAttackCrossTenantReadsAndWritesFail(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack INV-06 A Pte Ltd"), books(t, "Attack INV-06 B Pte Ltd")
	postRaw(t, conn(t, "fyniq_api"), a, "a's sale", "2026-10-14", "500.00")
	tables := []string{}
	for _, r := range rows(t, conn(t, "fyniq"), tablesSQL) {
		if !slices.Contains(globalTables, r[0]) {
			tables = append(tables, r[0])
		}
	}
	for _, role := range []string{"fyniq_api", "fyniq_worker", "fyniq_executor", "fyniq_readonly", "fyniq_migrator"} {
		c := conn(t, role)
		for _, table := range tables {
			var n int
			err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq."+table+" WHERE organisation_id = $1", a.Org).Scan(&n)
			})
			if err != nil && sqlstate(err) != "42501" {
				t.Errorf("%s reads %s: %v", role, table, err)
			}
			if n != 0 {
				t.Errorf("%s acting for B sees %d of A's rows in %s", role, n, table)
			}
		}
	}
	// Writing a row for A while acting for B: refused by the column grants or by the policy.
	for _, role := range []string{"fyniq_api", "fyniq_migrator"} {
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO fyniq.account (organisation_id, code, name, type) VALUES ($1, '9999', 'Planted', 'asset')", a.Org)
			return err
		})
		wantState(t, role+" writes an account into A", err, "42501")
	}
	// Referencing A's existing UUIDs from B: composite foreign keys refuse it although foreign-key
	// checks bypass row-level security (§11.4).
	err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO fyniq.balance (account_id, period_id, currency, minor_units, amount, base_amount, line_count, organisation_id)
VALUES ($1, $2, 'SGD', 2, 0, 0, 1, $3)`, a.Bank.ID, a.Oct.ID, b.Org)
		return err
	})
	wantState(t, "a balance in B pointing at A's account and period", err, "23503")
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.account (code, name, type, parent_id) VALUES ('9998', 'Under A', 'expense', $1)", a.Expenses.ID)
		return err
	})
	wantState(t, "an account in B under A's group", err, "FY041", "23503")
}

// req: SEC-02
// req: QA-ENG-05
// asvs: V8.4.1
// req: data/data-model Tenancy and row-level security
func TestAttackPooledConnectionWithoutTenantSeesNothing(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack pooled A Pte Ltd"), books(t, "Attack pooled B Pte Ltd")
	p := pool(t, "fyniq_api") // one connection, reused by every transaction below
	// Write as A on the pooled connection.
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", a.Org.String()); err != nil {
		t.Fatal(err)
	}
	id, err := entry(ctx, tx, "pooled", "2026-10-14")
	if err == nil {
		err = line(ctx, tx, id, 1, a.Bank.ID, "SGD", "9.00", "1", "9.00")
	}
	if err == nil {
		err = line(ctx, tx, id, 2, a.Sales.ID, "SGD", "-9.00", "1", "-9.00")
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	read := func(org *uuid.UUID) (int, error) {
		tx, err := p.Begin(ctx)
		if err != nil {
			return 0, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if org != nil {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", org.String()); err != nil {
				return 0, err
			}
		}
		var n int
		err = tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM fyniq.journal_line) + (SELECT count(*) FROM fyniq.account) + (SELECT count(*) FROM fyniq.organisation)").Scan(&n)
		if err == nil {
			err = tx.Commit(ctx)
		}
		return n, err
	}
	if n, err := read(&a.Org); err != nil || n == 0 {
		t.Fatalf("as A: %d rows, %v; want A's", n, err)
	}
	var aLines int
	if err := asOrg(ctx, conn(t, "fyniq_readonly"), a.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.journal_line").Scan(&aLines)
	}); err != nil || aLines != 2 {
		t.Fatalf("A has %d lines, %v; want 2", aLines, err)
	}
	if n, err := read(&b.Org); err != nil || n != 4+1 {
		t.Fatalf("the same pooled connection as B: %d rows, %v; want only B's 4 accounts and 1 organisation", n, err)
	}
	if n, err := read(nil); err != nil || n != 0 {
		t.Fatalf("the same pooled connection, tenant not set: %d rows, %v; want none", n, err)
	}
	// Writing without a tenant fails too.
	tx, err = p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, "INSERT INTO fyniq.account (code, name, type) VALUES ('7777', 'Nobody''s', 'asset')")
	_ = tx.Rollback(ctx)
	wantState(t, "insert with no tenant", err, "23502", "42501", "FY006")
	err = asOrg(ctx, conn(t, "fyniq_api"), stringer("not-a-uuid"), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT count(*) FROM fyniq.account")
		return err
	})
	wantState(t, "a tenant that is not a UUID", err, "22P02")
}

type stringer string

func (s stringer) String() string { return string(s) }

// req: QA-ENG-05
// req: SEC-03
// asvs: V13.2.2
func TestAttackTheAIRoleCannotReadOrWrite(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack AI Pte Ltd")
	c := conn(t, "fyniq_ai")
	for _, stmt := range []string{
		"SELECT count(*) FROM fyniq.journal_line",
		"INSERT INTO fyniq.account (code, name, type) VALUES ('8888', 'AI', 'asset')",
		"INSERT INTO fyniq.journal_entry (posting_date, posting_identity, source_type, source_id, source_version, rule_version) VALUES ('2026-10-14', 'ai', 'ai', 'ai', 1, 'ai')",
		"UPDATE fyniq.period SET status = 'closed'",
		"UPDATE fyniq.organisation SET name = 'Taken over'",
		"DELETE FROM fyniq.idempotency_key",
		"SELECT fyniq.lock_ledger()",
		"SELECT fyniq.rebuild_balances()",
	} {
		err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		wantState(t, "fyniq_ai: "+stmt, err, "42501")
	}
}

// req: QA-ENG-05
// asvs: V13.2.2
func TestAttackGlobalTablesAndLedgerInternalsAreReadOnly(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack global Pte Ltd")
	for _, role := range serviceRoles {
		c := conn(t, role)
		for _, stmt := range []string{
			"INSERT INTO fyniq.currency (code, minor_units, name) VALUES ('ZZZ', 2, 'Planted')",
			"UPDATE fyniq.currency SET minor_units = 4 WHERE code = 'SGD'",
			"DELETE FROM fyniq.currency WHERE code = 'JPY'",
			"INSERT INTO fyniq.currencies (code, minor_units, name) VALUES ('ZZZ', 2, 'Planted')",
			"SELECT count(*) FROM fyniq.currency",
			"UPDATE fyniq.ledger_revision SET revision = revision + 5",
			"UPDATE fyniq.balance SET base_amount = 0",
			"INSERT INTO fyniq.balance (account_id, period_id, currency, minor_units, amount, base_amount, line_count) SELECT id, '" +
				b.Oct.ID.String() + "', 'SGD', 2, 1, 1, 1 FROM fyniq.account LIMIT 1",
		} {
			err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			wantState(t, role+": "+stmt, err, "42501")
		}
	}
	// Even the owner cannot skip or rewind a revision (ENG-46).
	for _, stmt := range []string{
		"UPDATE fyniq.ledger_revision SET revision = revision + 2",
		"UPDATE fyniq.ledger_revision SET revision = 0",
		"DELETE FROM fyniq.ledger_revision",
	} {
		err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		wantState(t, "fyniq_migrator: "+stmt, err, "FY046", "23503")
	}
}

// req: NFR-PORT-07
func TestAttackAFailedWriteMidCommandLeavesNoPartialRows(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack unit of work Pte Ltd")
	s := store(t, "fyniq_api")
	err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Ledger().Append(ctx, porttest.Sale("first write", porttest.Day(10, 14), b, "11.00")); err != nil {
			return err
		}
		if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "5000", Name: "New", Type: storage.Expense}); err != nil {
			return err
		}
		// The third write fails in the database: the account code is taken.
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1000", Name: "Bank again", Type: storage.Asset})
		return err
	})
	if storage.CodeOf(err) != storage.CodeDuplicate {
		t.Fatalf("want %s from the failing write, got %v", storage.CodeDuplicate, err)
	}
	if n := count(t, b.Org, "journal_entry"); n != 0 {
		t.Errorf("%d entries survived the failed command", n)
	}
	if n := count(t, b.Org, "account"); n != 4 {
		t.Errorf("%d accounts after the failed command; want the original 4", n)
	}
	var rev int64
	_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rev, err = u.Ledger().Revision(ctx)
		return err
	})
	if rev != 0 {
		t.Errorf("revision %d after the failed command; want 0", rev)
	}
}

// req: INV-02
// req: QA-ENG-05
// asvs: V2.3.1
func TestAttackDocumentVersionsAndStatusesCannotBeRewound(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack documents Pte Ltd")
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[]}`)})
		if err == nil {
			err = u.Documents().SetStatus(ctx, d.ID, storage.DocumentPosted)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_migrator", "UPDATE fyniq.document_version SET payload = '{}'", "FY002"},
		{"fyniq_migrator", "DELETE FROM fyniq.document_version", "FY002"},
		{"fyniq_api", "UPDATE fyniq.document_version SET payload = '{}'", "42501"},
		{"fyniq_api", "UPDATE fyniq.document SET status = 'draft'", "FY051"},
		{"fyniq_api", "UPDATE fyniq.document SET type = 'invoice'", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.document SET type = 'journal', created_at = now() - interval '1 day'", "FY050"},
		// A posted document's version moves only as an edit, which names who posted it (rev 0.54).
		{"fyniq_api", "UPDATE fyniq.document SET current_version = 2", "FY053"},
		{"fyniq_ai", "INSERT INTO fyniq.document (type) VALUES ('journal')", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// Every new version names who saved it, and a posting names who posted it and when, set with the move to posted and
// never changed after (FR-GL-01, rev 0.48).
// req: FR-GL-01
func TestAttackDocumentActorsAreRecordedOnce(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack document actors Pte Ltd")
	var draft, posted storage.Document
	if err := store(t, "fyniq_api").Do(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker-test"}}), b.Org,
		func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if draft, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[]}`)}); err != nil {
				return err
			}
			if posted, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[]}`)}); err != nil {
				return err
			}
			if err = u.Documents().SetStatus(ctx, posted.ID, storage.DocumentPosted); err != nil {
				return err
			}
			posted, err = u.Documents().Get(ctx, posted.ID)
			return err
		}); err != nil {
		t.Fatal(err)
	}
	if draft.SavedBy != "user:attacker-test" || posted.PostedBy != "user:attacker-test" || posted.PostedAt == nil {
		t.Fatalf("the actors recorded: %+v %+v", draft, posted)
	}
	d, p := draft.ID.String(), posted.ID.String()
	for _, c := range []struct{ role, stmt, state string }{
		// A version without who saved it, or with a malformed actor.
		{"fyniq_api", "INSERT INTO fyniq.document_version (document_id, version, payload) VALUES ('" + d + "', 2, '{}')", "FY053"},
		{"fyniq_api", "INSERT INTO fyniq.document_version (document_id, version, payload, created_by) VALUES ('" + d + "', 2, '{}', 'nobody')", "23514"},
		// Posting without who posted it; changing who posted it after.
		{"fyniq_api", "UPDATE fyniq.document SET status = 'posted' WHERE id = '" + d + "'", "FY053"},
		{"fyniq_api", "UPDATE fyniq.document SET posted_by = 'user:someone-else' WHERE id = '" + p + "'", "FY053"},
		{"fyniq_api", "UPDATE fyniq.document SET posted_at = now() - interval '1 day' WHERE id = '" + p + "'", "FY053"},
		{"fyniq_api", "UPDATE fyniq.document SET posted_by = 'user:early', posted_at = now() WHERE id = '" + d + "'", "FY053"},
		{"fyniq_migrator", "UPDATE fyniq.document SET posted_by = NULL, posted_at = NULL WHERE id = '" + p + "'", "FY053"},
		{"fyniq_api", "UPDATE fyniq.document_version SET created_by = 'user:x'", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// An edit of a posted document moves it to its next version only with who posted the edit, and only with the reversal
// of the entry it had and the entry of the new version in the same transaction; a reversed document never changes
// (FR-GL-12, rev 0.54).
// req: FR-GL-12
// req: ledger/posting-rules Edits of posted documents
func TestAttackAnEditReversesAndPostsTogether(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker-test"}})
	b := books(t, "Attack edits Pte Ltd")
	st := store(t, "fyniq_api")
	var posted storage.Document
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if posted, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[]}`)}); err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, posted.ID, storage.DocumentPosted)
	}); err != nil {
		t.Fatal(err)
	}
	// A reversed journal, posted and reversed through the journal service.
	svc := journal.Service{Store: st}
	reversed, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 5), "10.00"))
	if err == nil {
		_, err = svc.Post(ctx, b.Org, reversed.ID)
	}
	if err == nil {
		_, err = svc.Reverse(ctx, b.Org, reversed.ID, porttest.Day(10, 6), "attack")
	}
	if err != nil {
		t.Fatal(err)
	}
	// A posted document marked reversed without its reversal is refused at commit (rev 0.55).
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, posted.ID, storage.DocumentReversed)
	}); storage.CodeOf(err) != storage.CodeDocumentEdit {
		t.Errorf("a reversal without its entry: %v", err)
	}
	// Through the port: an edit with neither posting is refused at commit.
	err = st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().Edit(ctx, posted.ID, []byte(`{"lines":[]}`), "")
		return err
	})
	if storage.CodeOf(err) != storage.CodeDocumentEdit {
		t.Errorf("an edit without its postings: %v", err)
	}
	p, r := posted.ID.String(), reversed.ID.String()
	version := func(id string) string {
		return "INSERT INTO fyniq.document_version (document_id, version, payload, created_by) VALUES ('" + id + "', 2, '{}', 'user:x'); "
	}
	for _, c := range []struct{ role, stmt, state string }{
		// The version moves without who posted the edit.
		{"fyniq_api", version(p) + "UPDATE fyniq.document SET current_version = 2 WHERE id = '" + p + "'", "FY053"},
		// With who posted it, but with no reversal and no entry of the new version (checked now, before the audit check
		// at commit).
		{"fyniq_api", "SET CONSTRAINTS fyniq.document_edit_posted IMMEDIATE; " + version(p) +
			"UPDATE fyniq.document SET current_version = 2, posted_by = 'user:x', posted_at = now() WHERE id = '" + p + "'", "FY055"},
		// A reversed document never changes.
		{"fyniq_api", version(r) + "UPDATE fyniq.document SET current_version = 2 WHERE id = '" + r + "'", "FY055"},
		// The worker moves a status only with the reversal it needs, and saves no version.
		{"fyniq_worker", "SET CONSTRAINTS fyniq.document_reversed_posted IMMEDIATE; UPDATE fyniq.document SET status = 'reversed' WHERE id = '" + p + "'", "FY056"},
		{"fyniq_worker", version(p), "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// An organisation reads only its own members' names, and an invitation is withdrawn or accepted once, never
// changed again; the last Owner keeps the role (FR-ADM-01, rev 0.35).
// req: FR-ADM-01
// req: SEC-02
// asvs: V8.2.1, V8.4.1
func TestAttackMembersNamesInvitationsAndTheLastOwner(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack members A Pte Ltd"), books(t, "Attack members B Pte Ltd")
	st := store(t, "fyniq_api")
	// A member of A, through an invitation redeemed for a new sign-in.
	hash := make([]byte, 32)
	hash[0] = 7
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: "Only in A", Roles: []string{"owner"},
			ExpiresAt: clock.System{}.Now().Add(time.Hour), CreatedAt: clock.System{}.Now()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	j, err := st.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: "Only in A", Issuer: "urn:fyniq:attack", Subject: uuid.NewString(),
		Now: clock.System{}.Now()})
	if err != nil || j.OrganisationID != a.Org || len(j.Roles) != 1 {
		t.Fatalf("joining A: %v %+v", err, j)
	}
	names := func(org uuid.UUID) []string {
		var out []string
		if err := asOrg(ctx, conn(t, "fyniq_api"), org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT display_name FROM fyniq.member_names()")
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := names(a.Org); !slices.Contains(got, "Only in A") {
		t.Errorf("A's members' names: %v", got)
	}
	if got := names(b.Org); slices.Contains(got, "Only in A") {
		t.Errorf("B reads A's member: %v", got)
	}

	// The accepted invitation never changes again, even to be withdrawn.
	err = asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.member_invitation SET withdrawn_at = now() WHERE code_hash = $1", hash)
		return err
	})
	wantState(t, "withdraw an accepted invitation", err, "FY092", "23514")

	// The organisation's only Owner keeps the role.
	err = asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.role_assignment SET revoked_at = now() WHERE role = 'owner' AND revoked_at IS NULL")
		return err
	})
	wantState(t, "revoke the last Owner", err, "FY093")
}

// rawBank writes a money account, a statement, a line and a gap as the API in one transaction, with value in the
// column target names and valid values elsewhere, and returns the first refusal.
func rawBank(t *testing.T, b porttest.Books, target, value string) error {
	t.Helper()
	ctx := context.Background()
	pick := func(name, valid string) string {
		if name == target {
			return value
		}
		return valid
	}
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		var account, money, stmt uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.account (code, name, type, is_money_account, currency)
VALUES ('1019', 'Attack bank', 'asset', true, 'SGD') RETURNING id`).Scan(&account); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.money_account (account_id, kind, name, currency, created_by, created_at, changed_by, changed_at)
VALUES ($1, 'bank', 'Attack', 'SGD', 'user:x', now(), 'user:x', now()) RETURNING id`, account).Scan(&money); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.bank_statement (money_account_id, source, format, parser_version, period_from, period_to,
currency, minor_units, opening_balance, closing_balance, line_count, imported_by, imported_at)
VALUES ($1, 'file', 'csv', 'csv@1', '2026-10-01', '2026-10-31', 'SGD', 2, $2::numeric, $3::numeric, 1, 'user:x', now()) RETURNING id`,
			money, pick("opening", "1.00"), pick("closing", "2.00")).Scan(&stmt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fyniq.bank_line (statement_id, money_account_id, position, booking_date, amount, currency,
minor_units, running_balance, fingerprint, status, status_by, status_at)
VALUES ($1, $2, 1, '2026-10-02', $3::numeric, 'SGD', 2, $4::numeric, repeat('a', 64), 'unreconciled', 'user:x', now())`,
			stmt, money, pick("amount", "1.00"), pick("running", "2.00")); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.bank_gap (money_account_id, statement_id, before_date, currency, minor_units,
expected_opening, found_opening, found_at) VALUES ($1, $2, '2026-10-01', 'SGD', 2, $3::numeric, $4::numeric, now())`,
			money, stmt, pick("expected", "0.00"), pick("found", "1.00"))
		return err
	})
}

// rawTransfer, rawSignal and rawReconciliation insert one row with value in the named column; the row's checks run
// before its foreign keys, so the references need not exist.
func rawTransfer(t *testing.T, b porttest.Books, target, value string) error {
	t.Helper()
	ctx := context.Background()
	pick := func(name, valid string) string {
		if name == target {
			return value
		}
		return valid
	}
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.bank_transfer (from_account_id, to_account_id, from_line_id, on_date, from_currency,
from_minor_units, from_amount, to_currency, to_minor_units, to_amount, fee, made_by, made_at)
VALUES ($1, $2, $3, '2026-10-04', 'SGD', 2, $4::numeric, 'USD', 2, $5::numeric, $6::numeric, 'user:x', now())`,
			uuid.New(), uuid.New(), uuid.New(), pick("from", "1350.00"), pick("to", "1000.00"), pick("fee", "5.00"))
		return err
	})
}

func rawSignal(t *testing.T, b porttest.Books, value string) error {
	t.Helper()
	ctx := context.Background()
	return asOrg(ctx, conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.bank_signal (money_account_id, amount, currency, minor_units, received_at)
VALUES ($1, $2::numeric, 'SGD', 2, now())`, uuid.New(), value)
		return err
	})
}

func rawReconciliation(t *testing.T, b porttest.Books, target, value string) error {
	t.Helper()
	ctx := context.Background()
	pick := func(name, valid string) string {
		if name == target {
			return value
		}
		return valid
	}
	return asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.bank_reconciliation (money_account_id, period_id, currency, minor_units, statement_balance,
book_balance, unreconciled_total, unreconciled_count, difference, ledger_revision, prepared_by, prepared_at)
VALUES ($1, $2, 'SGD', 2, $3::numeric, $4::numeric, $5::numeric, 0, $6::numeric, 0, 'user:x', now())`,
			uuid.New(), b.Oct.ID, pick("statement", "0.00"), pick("book", "0.00"), pick("unreconciled", "0.00"), pick("difference", "0.00"))
		return err
	})
}
