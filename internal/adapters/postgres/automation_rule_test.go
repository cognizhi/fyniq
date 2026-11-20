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

// A workflow rule can only be what the database allows (migration 001202): its trigger and its one action are from fixed lists, so no rule can name an action that posts, approves,
// pays or files; it is changed only as its next version, an archived rule not at all, never deleted; and the worker holds no write on rules.
// req: FR-AUT-03
// asvs: V8.2.2, V8.3.1, V16.3.3
func TestAttackAWorkflowRuleHasOnlyTheTriggersAndActionsTheDatabaseAllows(t *testing.T) {
	ctx := context.Background()
	a := books(t, "Attack workflow rules Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	maker, told := founder(t, st, a.Org, 21, now), founder(t, st, a.Org, 22, now)
	var rule storage.AutomationRule
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		rule, err = u.AutomationRules().Create(ctx, storage.NewAutomationRule{Name: "Large bills", DocumentType: "bill", AmountAtLeast: "5000.0000", Recipients: []uuid.UUID{told}, By: maker, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	api := func(stmt string, args ...any) error {
		return asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	insert := `INSERT INTO fyniq.automation_rule (name, trigger, document_type, amount_at_least, action, recipients, created_by, created_at, updated_at)
	           VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $7)`
	who := []uuid.UUID{told}
	wantState(t, "an action that posts", api(insert, "x", "document.posted", nil, "post", who, maker, now), "23514")
	wantState(t, "an action that approves", api(insert, "x", "document.posted", nil, "approve", who, maker, now), "23514")
	wantState(t, "an action that pays or files", api(insert, "x", "document.posted", nil, "pay", who, maker, now), "23514")
	wantState(t, "a trigger off the list", api(insert, "x", "bank.line", nil, "notify", who, maker, now), "23514")
	wantState(t, "a rule with no recipient", api(insert, "x", "document.posted", nil, "notify", []uuid.UUID{}, maker, now), "23514")
	wantState(t, "a rule with too many recipients", api(insert, "x", "document.posted", nil, "notify", func() []uuid.UUID {
		out := make([]uuid.UUID, 21)
		for i := range out {
			out[i] = uuid.New()
		}
		return out
	}(), maker, now), "23514")
	wantState(t, "a negative amount", api(insert, "x", "document.posted", "-1", "notify", who, maker, now), "23514")
	wantState(t, "an amount that is not a number", api(insert, "x", "document.posted", "NaN", "notify", who, maker, now), "23514")
	wantState(t, "a rule's action changed", api(`UPDATE fyniq.automation_rule SET action = 'post', version = version + 1 WHERE id = $1`, rule.ID), "42501")
	wantState(t, "a rule's trigger changed", api(`UPDATE fyniq.automation_rule SET trigger = 'bank.line', version = version + 1 WHERE id = $1`, rule.ID), "42501")
	wantState(t, "a rule's maker changed", api(`UPDATE fyniq.automation_rule SET created_by = $2, version = version + 1 WHERE id = $1`, rule.ID, told), "42501")
	wantState(t, "a change that skips a version", api(`UPDATE fyniq.automation_rule SET name = 'y', version = version + 2 WHERE id = $1`, rule.ID), "FYA01")
	wantState(t, "a rule deleted", api(`DELETE FROM fyniq.automation_rule WHERE id = $1`, rule.ID), "FYA01", "42501")

	// the worker reads the rules and changes none
	wantState(t, "the worker changing a rule", asOrg(ctx, conn(t, "fyniq_worker"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.automation_rule SET state = 'paused'`)
		return err
	}), "42501")
	wantState(t, "the worker making a rule", asOrg(ctx, conn(t, "fyniq_worker"), a.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insert, "x", "document.posted", nil, "notify", who, maker, now)
		return err
	}), "42501")

	// archived is final
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AutomationRules().Change(ctx, rule.ID, rule.Version, storage.AutomationRuleChange{Name: rule.Name, DocumentType: rule.DocumentType, State: "archived",
			AmountAtLeast: rule.AmountAtLeast, Recipients: rule.Recipients, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantState(t, "an archived rule resumed", api(`UPDATE fyniq.automation_rule SET state = 'active', version = version + 1 WHERE id = $1`, rule.ID), "FYA01")
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		active, err := u.AutomationRules().Active(ctx, "document.posted")
		if err == nil && len(active) != 0 {
			t.Errorf("an archived rule is active: %v", active)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
