//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	if err := post("share_issue"); err != nil {
		t.Errorf("another source could not post to share capital: %v", err)
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

// nanAttacks try NaN and an out-of-range value in each numeric column; TestEveryNumericColumn…
// fails when a numeric column has no entry here.
var nanAttacks = map[string]func(t *testing.T, b porttest.Books, value string) error{
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
		"SGD with three decimals":                 rawLine(t, b, "SGD", "1.005", "1", "1.01"),
		"yen with decimals":                       rawLine(t, b, "JPY", "1.5", "0.0091", "0.01"),
		"base not amount × rate (RND-FX)":         rawLine(t, b, "USD", "100.00", "1.35", "135.01"),
		"a base-only line (FX revaluation is P1)": rawLine(t, b, "USD", "0.00", "1.35", "1.00"),
		"a zero rate":                             rawLine(t, b, "USD", "1.00", "0", "0.00"),
		"a negative rate":                         rawLine(t, b, "USD", "1.00", "-1.35", "-1.35"),
		"a base-currency line at rate 2":          rawLine(t, b, "SGD", "1.00", "2", "2.00"),
		"infinity":                                rawLine(t, b, "SGD", "Infinity", "1", "1.00"),
	} {
		wantState(t, name, err, "23514", "22003")
	}
	wantState(t, "an unknown currency", rawLine(t, b, "XYZ", "1.00", "1", "1.00"), "FY005")
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
		{"fyniq_api", "UPDATE fyniq.document SET current_version = 2", "23503"},
		{"fyniq_ai", "INSERT INTO fyniq.document (type) VALUES ('journal')", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
