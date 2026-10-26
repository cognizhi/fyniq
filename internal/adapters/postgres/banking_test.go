//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func dec(s string) *storage.Decimal {
	d := storage.Decimal(s)
	return &d
}

// Money accounts, statements and lines are the organisation's own rows (migration 000144): a money account sits on a
// money chart account in its currency and is never deleted; its sources are added from later dates, never changed; a
// statement and its lines arrive together and never change; a line moves only along the queue, back to unreconciled
// only while its period is open; a gap is resolved once. Every change is audited, and another organisation sees none.
// req: FR-BNK-01
// req: FR-BNK-04
// req: FR-BNK-16
// req: FR-AP-18
// req: FR-AP-18
// req: INV-06
// req: QA-ENG-05
// asvs: V8.4.1, V16.3.3, V2.3.2
func TestBankingStoreKeepsWhatTheBankSaid(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Banking organisation A Pte Ltd"), books(t, "Banking organisation B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var chart, plain storage.Account
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if chart, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		plain, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1500", Name: "Equipment", Type: storage.Asset})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	do := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return st.Do(ctx, a.Org, fn) }

	// A money account sits on a money chart account, in its currency.
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: plain.ID, Kind: storage.MoneyBank, Name: "Equipment", Currency: "SGD", At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a money account on a plain account: %v", err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: chart.ID, Kind: storage.MoneyBank, Name: "DBS", Currency: "USD", At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a money account in another currency: %v", err)
	}
	var acct storage.MoneyAccount
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		acct, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: chart.ID, Kind: storage.MoneyBank, Name: "DBS current",
			Institution: "DBS Bank", Identifier: "072-123456-7", Currency: "SGD", At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: chart.ID, Kind: storage.MoneyBank, Name: "Again", Currency: "SGD", At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeDuplicate {
		t.Fatalf("a second money account on one chart account: %v", err)
	}
	renamed := acct
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		renamed, err = u.Banking().ChangeMoneyAccount(ctx, acct.ID, acct.Version, storage.MoneyAccountChange{Kind: storage.MoneyBank, Name: "DBS operating",
			Institution: "DBS Bank", Identifier: "072-123456-7", At: now})
		return err
	}); err != nil || renamed.Version != 2 || renamed.Name != "DBS operating" {
		t.Fatalf("rename: %+v, %v", renamed, err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().ChangeMoneyAccount(ctx, acct.ID, acct.Version, storage.MoneyAccountChange{Kind: storage.MoneyBank, Name: "Stale", At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeVersion {
		t.Fatalf("a stale change: %v", err)
	}
	// Only a card names a holder (FR-AP-18): a bank account does not take one.
	holder := founder(t, st, a.Org, 7, now)
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().ChangeMoneyAccount(ctx, acct.ID, renamed.Version, storage.MoneyAccountChange{Kind: storage.MoneyBank, Name: "DBS operating",
			Institution: "DBS Bank", Identifier: "072-123456-7", CardholderID: &holder, At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeInvalid {
		t.Fatalf("a holder on a bank account: %v", err)
	}

	// Sources are added from later dates.
	for i, src := range []storage.NewMoneyAccountSource{
		{MoneyAccountID: acct.ID, Kind: storage.MoneySourceFile, ReportFormat: storage.FormatMT940, FromDate: porttest.Day(10, 1), At: now},
		{MoneyAccountID: acct.ID, Kind: storage.MoneySourceEmail, Senders: []string{"ideal@dbs.example"}, ReportFormat: storage.FormatCSV, FromDate: porttest.Day(10, 15), At: now},
	} {
		if err := do(func(ctx context.Context, u storage.UnitOfWork) error { _, err := u.Banking().AddSource(ctx, src); return err }); err != nil {
			t.Fatalf("source %d: %v", i, err)
		}
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: acct.ID, Kind: storage.MoneySourceFile,
			ReportFormat: storage.FormatCSV, FromDate: porttest.Day(10, 10), At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a source before the current one: %v", err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: acct.ID, Kind: storage.MoneySourceEmail,
			FromDate: porttest.Day(10, 20), At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("reports by email without the bank's senders: %v", err)
	}

	// A statement arrives with its lines; a gap is found with it.
	var stmt storage.BankStatement
	var lines []storage.BankLine
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		stmt, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: acct.ID, Source: storage.MoneySourceFile,
			Format: storage.FormatMT940, ParserVersion: "mt940@1", PeriodFrom: porttest.Day(10, 1), PeriodTo: porttest.Day(10, 31),
			Currency: "SGD", Opening: dec("1000.00"), Closing: dec("1500.00"), FileRef: "staging/ab12",
			Lines: []storage.NewBankLine{
				{Position: 1, BookingDate: porttest.Day(10, 3), Amount: "500.00", Reference: "INV-0138", Counterparty: "ACME ROBOTICS",
					RunningBalance: dec("1500.00"), Fingerprint: fingerprint("1")},
				{Position: 2, BookingDate: porttest.Day(10, 3), Amount: "500.00", Reference: "INV-0138", Counterparty: "ACME ROBOTICS",
					RunningBalance: dec("2000.00"), Fingerprint: fingerprint("2")},
			},
			Gap: &storage.NewBankGap{BeforeDate: porttest.Day(10, 1), FoundOpening: dec("1000.00")}, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Status != storage.LineUnreconciled || stmt.LineCount != 2 {
		t.Fatalf("the import: %+v %+v", stmt, lines)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: acct.ID, Source: storage.MoneySourceFile,
			Format: storage.FormatCSV, ParserVersion: "csv@1", PeriodFrom: porttest.Day(11, 1), PeriodTo: porttest.Day(11, 2), Currency: "USD", At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a statement in another currency: %v", err)
	}

	// A line moves along the queue; back to unreconciled only while its period is open.
	line := lines[0]
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineSuspense, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		line, err = u.Banking().Line(ctx, line.ID)
		if err != nil {
			return err
		}
		_, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineExcluded, now)
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("suspense to excluded: %v", err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		line, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineCategorised, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Do(ctx, a.Org, closePeriod(a.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineUnreconciled, now)
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("an undo in a closed period: %v", err)
	}

	// A gap is resolved once.
	var gaps []storage.BankGap
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		gaps, err = u.Banking().Gaps(ctx, acct.ID, true)
		if err != nil || len(gaps) != 1 {
			t.Fatalf("open gaps: %+v, %v", gaps, err)
		}
		_, err = u.Banking().ResolveGap(ctx, gaps[0].ID, "The account opened on 1 October.", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().ResolveGap(ctx, gaps[0].ID, "Again.", now)
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("resolving a gap twice: %v", err)
	}

	for want, id := range map[string]uuid.UUID{"money_account.opened": acct.ID, "money_account.source_set": acct.ID, "bank_statement.imported": stmt.ID,
		"bank_line.categorised": line.ID, "bank_gap.resolved": gaps[0].ID} {
		if got := actions(t, a.Org, id); !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}

	// What the database refuses, whoever asks.
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "change a line's amount", "UPDATE fyniq.bank_line SET amount = 1 WHERE statement_id = $1", "42501"},
		{"fyniq_migrator", "change a line's amount as the migrator", "UPDATE fyniq.bank_line SET amount = 1, version = version + 1 WHERE statement_id = $1", "FY220"},
		{"fyniq_api", "delete a line", "DELETE FROM fyniq.bank_line WHERE statement_id = $1", "42501"},
		{"fyniq_migrator", "delete a line as the migrator", "DELETE FROM fyniq.bank_line WHERE statement_id = $1", "FY220"},
		{"fyniq_api", "add a line to an imported statement", "INSERT INTO fyniq.bank_line (statement_id, money_account_id, position, booking_date, amount, currency, minor_units, fingerprint, status, status_by, status_at) SELECT id, money_account_id, 9, '2026-10-04', 1, currency, minor_units, repeat('a', 64), 'unreconciled', 'user:x', now() FROM fyniq.bank_statement WHERE id = $1", "FY220"},
		{"fyniq_migrator", "change a statement", "UPDATE fyniq.bank_statement SET closing_balance = 0 WHERE id = $1", "FY219"},
		{"fyniq_migrator", "delete a statement", "DELETE FROM fyniq.bank_statement WHERE id = $1", "FY219"},
		{"fyniq_migrator", "change a source", "UPDATE fyniq.money_account_source SET from_date = '2026-01-01' WHERE money_account_id = (SELECT money_account_id FROM fyniq.bank_statement WHERE id = $1)", "FY217"},
		{"fyniq_migrator", "delete a money account", "DELETE FROM fyniq.money_account WHERE id = (SELECT money_account_id FROM fyniq.bank_statement WHERE id = $1)", "FY216"},
		{"fyniq_worker", "rename a money account as the worker", "UPDATE fyniq.money_account SET name = 'x', version = version + 1 WHERE id = (SELECT money_account_id FROM fyniq.bank_statement WHERE id = $1)", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, stmt.ID); return err })
		wantState(t, x.what, err, x.state)
	}
	for _, table := range []string{"money_account", "money_account_source", "bank_statement", "bank_line", "bank_gap"} {
		if n := count(t, b.Org, table); n != 0 {
			t.Errorf("organisation B counts %d rows of %s", n, table)
		}
	}
	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		mine, err := u.Banking().MoneyAccounts(ctx)
		if err == nil && len(mine) != 0 {
			t.Errorf("organisation B sees %d money accounts", len(mine))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
