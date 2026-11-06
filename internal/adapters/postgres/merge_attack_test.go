//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A merge is the database's to keep (FR-COA-04, PRD-001 rev 0.174; FY260): an account names the account it was merged
// into only as it becomes inactive, never as a money, group or system account, never into itself, into an account of
// another type or into an inactive one, and never while it is a balance-sheet account still holding a balance. Once
// merged it stays inactive and merged into the same account, for the API role and the owner alike, and a new account is
// never merged already.
// req: FR-COA-04
// req: QA-ENG-05
// asvs: V2.3.1, V2.3.2
func TestAttackAMergedAccountStaysMerged(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	b := books(t, "Attack account merges Pte Ltd")
	s := store(t, "fyniq_api")
	var deposits, other, hosting, closed storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		create := func(code, name string, typ storage.AccountType) storage.Account {
			if err != nil {
				return storage.Account{}
			}
			var a storage.Account
			a, err = u.Accounts().Create(ctx, storage.NewAccount{Code: code, Name: name, Type: typ})
			return a
		}
		deposits, other = create("1260", "Deposits paid", "asset"), create("1270", "Other deposits", "asset")
		hosting, closed = create("6150", "Hosting", "expense"), create("6990", "Closed", "expense")
		if err != nil {
			return err
		}
		_, err = u.Accounts().SetActive(ctx, closed.ID, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc := journal.Service{Store: s}
	d, err := svc.Create(ctx, b.Org, journal.Draft{Date: porttest.Day(10, 10), Memo: "Deposit", Lines: []ledger.JournalLine{
		{AccountID: deposits.ID, Amount: amount(t, "500.00", sgd)},
		{AccountID: b.Bank.ID, Amount: amount(t, "-500.00", sgd)}}})
	if err == nil {
		_, err = svc.Post(ctx, b.Org, d.ID)
	}
	if err != nil {
		t.Fatal(err)
	}

	exec := func(role, sql string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			if err := auditRaw(ctx, tx, "account.updated", "account", args[0].(uuid.UUID)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
	}
	const merge = "UPDATE fyniq.account SET is_active = false, merged_into_id = $2, version = version + 1 WHERE id = $1"
	for _, x := range []struct {
		name     string
		sql      string
		from, to uuid.UUID
	}{
		{"merging an account that stays active", "UPDATE fyniq.account SET merged_into_id = $2, version = version + 1 WHERE id = $1", b.Rent.ID, hosting.ID},
		{"merging a money account", merge, b.Bank.ID, deposits.ID},
		{"merging a group", merge, b.Expenses.ID, hosting.ID},
		{"merging an account into itself", merge, hosting.ID, hosting.ID},
		{"merging into an account of another type", merge, b.Rent.ID, deposits.ID},
		{"merging into an inactive account", merge, b.Rent.ID, closed.ID},
		{"merging a balance-sheet account that holds a balance", merge, deposits.ID, other.ID},
	} {
		wantState(t, "the API role "+x.name, exec("fyniq_api", x.sql, x.from, x.to), "FY260")
		wantState(t, "the owner "+x.name, exec("fyniq_migrator", x.sql, x.from, x.to), "FY260")
	}

	if err := exec("fyniq_api", merge, b.Rent.ID, hosting.ID); err != nil {
		t.Fatalf("merging Rent, which holds nothing, into Hosting: %v", err)
	}
	for _, x := range []struct{ name, sql string }{
		{"reactivating a merged account", "UPDATE fyniq.account SET is_active = true, version = version + 1 WHERE id = $1"},
		{"clearing a merge", "UPDATE fyniq.account SET merged_into_id = NULL, version = version + 1 WHERE id = $1"},
		{"merging it again elsewhere", "UPDATE fyniq.account SET merged_into_id = $2, version = version + 1 WHERE id = $1"},
	} {
		args := []any{b.Rent.ID}
		if x.name == "merging it again elsewhere" {
			args = append(args, other.ID)
		}
		wantState(t, "the API role "+x.name, exec("fyniq_api", x.sql, args...), "FY260")
		wantState(t, "the owner "+x.name, exec("fyniq_migrator", x.sql, args...), "FY260")
	}
	err = asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.account (code, name, type, merged_into_id) VALUES ('6151', 'Born merged', 'expense', $1) RETURNING id",
			hosting.ID).Scan(&id); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "account.created", "account", id)
	})
	wantState(t, "a new account merged already", err, "FY260")
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.account (code, name, type, merged_into_id) VALUES ('6152', 'Born merged', 'expense', $1)", hosting.ID)
		return err
	})
	wantState(t, "the API role inserting a merged account", err, "42501")
}
