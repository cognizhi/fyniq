//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-14 (rev 0.65): an explanation of a variance is of an income or expense account, in an open period, and once
// written it never changes or goes, whatever the API does; a later one replaces it as the account's latest.
// req: FR-GL-14
func TestAttackVarianceExplanationsAreAppendOnlyAndOnlyInAnOpenPeriod(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	b := books(t, "Flux Pte Ltd")
	s := store(t, "fyniq_api")
	explain := func(period storage.Period, acc storage.Account, words string) (storage.VarianceExplanation, error) {
		var out storage.VarianceExplanation
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out, err = u.Closes().ExplainVariance(ctx, storage.NewVarianceExplanation{PeriodID: period.ID, AccountID: acc.ID,
				Current: "8000.00", Previous: "5000.00", Explanation: words})
			return err
		})
		return out, err
	}
	first, err := explain(b.Nov, b.Rent, "The office moved.")
	if err != nil {
		t.Fatal(err)
	}
	if first.WrittenBy != "user:accountant" || first.WrittenAt.IsZero() {
		t.Errorf("the explanation: %+v", first)
	}
	second, err := explain(b.Nov, b.Rent, "The office moved, and the deposit was topped up.")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		xs, err := u.Closes().VarianceExplanations(ctx, b.Nov.ID)
		if err == nil && (len(xs) != 1 || xs[0].ID != second.ID || xs[0].Current != "8000.0000" || xs[0].Previous != "5000.0000") {
			t.Errorf("the latest explanation: %+v", xs)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, b.Org, "period_variance_explanation"); n != 2 {
		t.Errorf("%d explanations kept, want both", n)
	}

	// A balance-sheet account's variance is not explained; nor is a variance in a closed period.
	if _, err := explain(b.Nov, b.Bank, "Cash moved."); !refusedAs(err, storage.CodeVariance) {
		t.Errorf("a bank account's variance: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, b.Oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := explain(b.Oct, b.Rent, "Too late."); !refusedAs(err, storage.CodeVariance) {
		t.Errorf("a closed period's variance: %v", err)
	}

	for _, c := range []struct{ role, stmt, state string }{
		// The API's role only adds explanations.
		{"fyniq_api", "UPDATE fyniq.period_variance_explanation SET explanation = 'x'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.period_variance_explanation", "42501"},
		// Not even the owner of the table rewrites or removes one.
		{"fyniq_migrator", "UPDATE fyniq.period_variance_explanation SET explanation = 'x'", "FY163"},
		{"fyniq_migrator", "UPDATE fyniq.period_variance_explanation SET current_amount = 0", "FY163"},
		{"fyniq_migrator", "DELETE FROM fyniq.period_variance_explanation", "FY163"},
		{"fyniq_migrator", "TRUNCATE fyniq.period_variance_explanation", "FY002"},
		// The words are there, and the writer is named as the audit trail names actors.
		{"fyniq_api", "INSERT INTO fyniq.period_variance_explanation (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at) " +
			"VALUES ('" + b.Nov.ID.String() + "', '" + b.Rent.ID.String() + "', 1, 0, '  ', 'user:a', now())", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.period_variance_explanation (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at) " +
			"VALUES ('" + b.Nov.ID.String() + "', '" + b.Rent.ID.String() + "', 1, 0, 'x', 'nobody', now())", "23514"},
		// The database itself refuses a closed period and a balance-sheet account.
		{"fyniq_api", "INSERT INTO fyniq.period_variance_explanation (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at) " +
			"VALUES ('" + b.Oct.ID.String() + "', '" + b.Rent.ID.String() + "', 1, 0, 'x', 'user:a', now())", "FY162"},
		{"fyniq_api", "INSERT INTO fyniq.period_variance_explanation (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at) " +
			"VALUES ('" + b.Nov.ID.String() + "', '" + b.Bank.ID.String() + "', 1, 0, 'x', 'user:a', now())", "FY162"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
