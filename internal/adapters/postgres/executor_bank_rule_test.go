//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The executor's grants for bank.rule_upsert (migration 000963) are the API's own columns minus the two that make a rule apply by itself: it creates and
// changes a rule, a rule it creates says it is an AI proposal's, and it can never set auto_apply or its approval, delete a rule or touch a bank line, a match or
// a transfer, whatever SQL it sends.
// req: AI-10
// req: FR-BNK-14
// req: ai/task-catalogue bank.rule_upsert Create or edit a bank rule
// asvs: V8.2.2, V8.3.1
func TestAttackTheExecutorKeepsBankRulesAndCanNeverMakeOneApplyByItself(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Executor bank rules Pte Ltd")
	exec := store(t, "fyniq_executor")
	at := clock.System{}.Now().UTC()
	var made storage.BankRule
	if err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		made, err = u.BankMatching().CreateRule(ctx, storage.NewBankRule{Name: "AWS", Position: 1, Conditions: []byte(`{"description_contains":"aws"}`),
			Action: []byte(`{"account_id":"` + b.Rent.ID.String() + `"}`), Active: true, FromAIProposal: true, At: at})
		if err != nil {
			return err
		}
		made, err = u.BankMatching().ChangeRule(ctx, made.ID, made.Version, storage.NewBankRule{Name: "AWS 2", Position: 1, Conditions: []byte(`{"description_contains":"aws"}`),
			Action: []byte(`{"account_id":"` + b.Rent.ID.String() + `"}`), Active: false, At: at})
		return err
	}); err != nil {
		t.Fatalf("what the executor may do: %v", err)
	}
	if !made.FromAIProposal || made.AutoApply {
		t.Errorf("the rule: %+v", made)
	}
	// A rule it makes that does not say so is refused by the database.
	err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().CreateRule(ctx, storage.NewBankRule{Name: "Pass as a person's", Position: 2, Conditions: []byte(`{"description_contains":"x"}`),
			Action: []byte(`{"account_id":"` + b.Rent.ID.String() + `"}`), Active: true, FromAIProposal: false, At: at})
		return err
	})
	if err == nil {
		t.Error("the executor made a rule that passes for a person's")
	}
	for _, c := range []struct{ what, stmt string }{
		{"making a rule apply by itself", `UPDATE fyniq.bank_rule SET auto_apply = true WHERE id = $1`},
		{"naming an approval", `UPDATE fyniq.bank_rule SET auto_apply_approval_id = gen_random_uuid() WHERE id = $1`},
		{"deleting a rule", `DELETE FROM fyniq.bank_rule WHERE id = $1`},
		{"changing who made it", `UPDATE fyniq.bank_rule SET from_ai_proposal = false WHERE id = $1`},
		{"matching a line", `INSERT INTO fyniq.bank_match (line_id, kind, made_by) SELECT gen_random_uuid(), 'person', 'x' WHERE $1::uuid IS NOT NULL`},
		{"changing a bank line", `UPDATE fyniq.bank_line SET status = 'categorised' WHERE id = $1`},
		{"adding a transfer", `INSERT INTO fyniq.bank_transfer (on_date) SELECT now() WHERE $1::uuid IS NOT NULL`},
	} {
		err := asOrg(ctx, conn(t, "fyniq_executor"), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, c.stmt, made.ID)
			return err
		})
		if err == nil {
			t.Errorf("the executor was allowed %s", c.what)
		}
	}
	_ = uuid.Nil
}
