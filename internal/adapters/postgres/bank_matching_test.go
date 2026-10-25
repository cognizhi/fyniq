//go:build local

package postgres_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// bankedBooks are Books with two money accounts (SGD and USD) and an October statement of three SGD lines.
type bankedBooks struct {
	porttest.Books
	sgd, usd storage.MoneyAccount
	lines    []storage.BankLine
}

func newBankedBooks(t *testing.T, name string, now time.Time) bankedBooks {
	t.Helper()
	ctx := context.Background()
	out := bankedBooks{Books: books(t, name)}
	if err := store(t, "fyniq_api").Do(ctx, out.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var accounts [2]storage.Account
		for i, a := range []storage.NewAccount{
			{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}},
			{Code: "1021", Name: "Wise USD", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}},
		} {
			var err error
			if accounts[i], err = u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		var err error
		if out.sgd, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: accounts[0].ID, Kind: storage.MoneyBank,
			Name: "DBS current", Currency: "SGD", At: now}); err != nil {
			return err
		}
		if out.usd, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: accounts[1].ID, Kind: storage.MoneyWallet,
			Name: "Wise USD", Currency: "USD", At: now}); err != nil {
			return err
		}
		_, out.lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: out.sgd.ID, Source: storage.MoneySourceFile,
			Format: storage.FormatCSV, ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 1), PeriodTo: porttest.Day(10, 31), Currency: "SGD",
			Lines: []storage.NewBankLine{
				{Position: 1, BookingDate: porttest.Day(10, 3), Amount: "109.00", Reference: "INV-0001", Fingerprint: fingerprint(name + "1")},
				{Position: 2, BookingDate: porttest.Day(10, 4), Amount: "-1350.00", Reference: "TO WISE", Fingerprint: fingerprint(name + "2")},
				{Position: 3, BookingDate: porttest.Day(10, 5), Amount: "-20.00", Fingerprint: fingerprint(name + "3")},
			}, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// Rules, matches, transfers, signals and reconciliations keep what they recorded (migration 000145): a rule auto-applies
// only under its approved request; a line has one live match, undone once and only while its period is open; a
// transfer's legs are its money out and money in; a signal closes once; a reconciliation is signed once, by another
// person, and stays as signed, as it does once its period is closed. Every change is audited; another organisation
// sees none of it.
// req: FR-BNK-05
// req: FR-BNK-07
// req: FR-BNK-09
// req: FR-BNK-10
// req: FR-BNK-14
// req: INV-06
// req: QA-ENG-05
// asvs: V8.4.1, V2.3.2, V2.3.5
func TestBankMatchingKeepsWhatItRecorded(t *testing.T) {
	ctx := context.Background()
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	a := newBankedBooks(t, "Matching organisation A Pte Ltd", now)
	b := books(t, "Matching organisation B Pte Ltd")
	st := store(t, "fyniq_api")
	do := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return st.Do(ctx, a.Org, fn) }

	// A rule, changed one version at a time; it cannot auto-apply without its approved request.
	var rule storage.BankRule
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rule, err = u.BankMatching().CreateRule(ctx, storage.NewBankRule{Name: "Bank fees", Position: 1, Conditions: []byte(`{"description":"FEE"}`),
			Action: []byte(`{"category":"6460"}`), Active: true, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rule, err = u.BankMatching().ChangeRule(ctx, rule.ID, rule.Version, storage.NewBankRule{Name: "Bank charges", Position: 2,
			Conditions: rule.Conditions, Action: rule.Action, Active: true, At: now})
		return err
	}); err != nil || rule.Version != 2 {
		t.Fatalf("change a rule: %+v, %v", rule, err)
	}

	// A categorised line: its match names the entry posted.
	var entry storage.Entry
	line := a.lines[2]
	var match storage.BankMatch
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if entry, err = u.Ledger().Append(ctx, porttest.Sale("bank-line-3", porttest.Day(10, 5), a.Books, "20.00")); err != nil {
			return err
		}
		if match, err = u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: line.ID, Kind: storage.MatchCategorised, EntryID: &entry.ID,
			Basis: storage.BasisPerson, At: now}); err != nil {
			return err
		}
		line, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineCategorised, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: line.ID, Kind: storage.MatchSuspense, EntryID: &entry.ID,
			Basis: storage.BasisPerson, At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeDuplicate && storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a second live match of a line: %v", err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: a.lines[0].ID, Kind: storage.MatchCategorised, EntryID: &entry.ID,
			Basis: storage.BasisRule, RuleID: &rule.ID, At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a match by a rule that does not auto-apply: %v", err)
	}

	// A transfer in transit: its paying leg is the line's money out.
	var tr storage.BankTransfer
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().RecordTransfer(ctx, storage.NewBankTransfer{FromAccountID: a.sgd.ID, ToAccountID: a.usd.ID, FromLineID: &a.lines[0].ID,
			OnDate: porttest.Day(10, 4), FromCurrency: "SGD", FromAmount: "1350.00", ToCurrency: "USD", ToAmount: "1000.00", At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("a transfer whose paying line is money in: %v", err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		tr, err = u.BankMatching().RecordTransfer(ctx, storage.NewBankTransfer{FromAccountID: a.sgd.ID, ToAccountID: a.usd.ID, FromLineID: &a.lines[1].ID,
			OnDate: porttest.Day(10, 4), FromCurrency: "SGD", FromAmount: "1350.00", ToCurrency: "USD", ToAmount: "1000.00", Fee: dec("5.00"), At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		transit, err := u.BankMatching().InTransit(ctx)
		if err == nil && !slices.ContainsFunc(transit, func(x storage.BankTransfer) bool { return x.ID == tr.ID }) {
			t.Errorf("in transit: %+v", transit)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A signal closes once.
	var sig storage.BankSignal
	worker := store(t, "fyniq_worker")
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		sig, err = u.BankMatching().RecordSignal(ctx, storage.NewBankSignal{MoneyAccountID: a.sgd.ID, Amount: "109.00", Currency: "SGD",
			Payer: "ACME ROBOTICS", ReceivedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"", storage.CodeBanking} {
		err := do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.BankMatching().CloseSignal(ctx, sig.ID, storage.SignalClosed, &a.lines[0].ID, now)
			return err
		})
		if storage.CodeOf(err) != want && !(want == "" && err == nil) {
			t.Fatalf("close the signal, time %d: %v", i+1, err)
		}
	}

	// A reconciliation: prepared, prepared again, never signed by its preparer, signed once by another person.
	var rec storage.BankReconciliation
	prepare := func() error {
		return do(func(ctx context.Context, u storage.UnitOfWork) error {
			got, err := u.BankMatching().PrepareReconciliation(ctx, storage.NewBankReconciliation{MoneyAccountID: a.sgd.ID, PeriodID: a.Oct.ID,
				Currency: "SGD", StatementBalance: "1000.00", BookBalance: "1109.00", UnreconciledTotal: "-109.00", UnreconciledCount: 1,
				Difference: "0.00", LedgerRevision: entry.LedgerRevision, At: now})
			if err == nil {
				rec = got
			}
			return err
		})
	}
	if err := prepare(); err != nil {
		t.Fatal(err)
	}
	if err := prepare(); err != nil || rec.Version != 2 {
		t.Fatalf("prepare again: %+v, %v", rec, err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().SignReconciliation(ctx, rec.ID, rec.Version, now, now)
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("signed by its preparer: %v", err)
	}
	other := storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: uuid.NewString()}})
	if err := st.Do(other, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rec, err = u.BankMatching().SignReconciliation(ctx, rec.ID, rec.Version, now, now)
		return err
	}); err != nil || rec.SignedBy == "" {
		t.Fatalf("signed by another person: %+v, %v", rec, err)
	}
	if err := prepare(); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("prepared again once signed: %v", err)
	}

	// An undo while the period is open; none once it is closed.
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().UndoMatch(ctx, match.ID, "Coded to the wrong account.", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().UndoMatch(ctx, match.ID, "Again.", now)
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("an undo twice: %v", err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		again, err := u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: a.lines[2].ID, Kind: storage.MatchCategorised, EntryID: &entry.ID,
			Basis: storage.BasisPerson, At: now})
		match = again
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Do(ctx, a.Org, closePeriod(a.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().UndoMatch(ctx, match.ID, "Too late.", now)
		return err
	}); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("an undo in a closed period: %v", err)
	}

	for want, id := range map[string]uuid.UUID{"bank_rule.changed": rule.ID, "bank_line.match_made": line.ID, "bank_line.match_undone": line.ID,
		"bank_transfer.recorded": tr.ID, "bank_signal.closed": sig.ID, "bank_reconciliation.signed": rec.ID} {
		if got := actions(t, a.Org, id); !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}

	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "auto-apply without an approved request", "UPDATE fyniq.bank_rule SET auto_apply = true, version = version + 1 WHERE id = $1", "FY222"},
		{"fyniq_migrator", "delete a rule", "DELETE FROM fyniq.bank_rule WHERE id = $1", "FY222"},
		{"fyniq_api", "delete a match", "DELETE FROM fyniq.bank_match WHERE rule_id IS NULL AND line_id IN (SELECT line_id FROM fyniq.bank_match) AND $1::uuid IS NOT NULL", "42501"},
		{"fyniq_migrator", "change a match's entry", "UPDATE fyniq.bank_match SET entry_id = NULL WHERE $1::uuid IS NOT NULL", "FY224"},
		{"fyniq_migrator", "change a transfer's amount", "UPDATE fyniq.bank_transfer SET to_amount = 999, version = version + 1 WHERE $1::uuid IS NOT NULL", "FY223"},
		{"fyniq_migrator", "reopen a signal", "UPDATE fyniq.bank_signal SET status = 'pending', closed_at = NULL, closed_line_id = NULL WHERE $1::uuid IS NOT NULL", "FY225"},
		{"fyniq_migrator", "change a signed reconciliation", "UPDATE fyniq.bank_reconciliation SET book_balance = 0, version = version + 1 WHERE $1::uuid IS NOT NULL", "FY226"},
		{"fyniq_api", "add a signal as the API", "INSERT INTO fyniq.bank_signal (money_account_id, amount, currency, minor_units, received_at) SELECT money_account_id, 1, currency, minor_units, now() FROM fyniq.bank_signal WHERE $1::uuid IS NOT NULL", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, rule.ID); return err })
		wantState(t, x.what, err, x.state)
	}
	for _, table := range []string{"bank_rule", "bank_match", "bank_transfer", "bank_signal", "bank_reconciliation"} {
		if n := count(t, b.Org, table); n != 0 {
			t.Errorf("organisation B counts %d rows of %s", n, table)
		}
	}
}
