//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The executor's grants for recur.create_template and plan.propose_budget are the narrowest their domain functions need (migrations 000961, 000962):
// it adds a recurring journal and its first content version, and drafts a budget, and nothing more of either: it cannot change, pause, resume or delete a
// recurring journal or its content, make a draft of one (the worker's), add a recurring bill or invoice, or change or delete a budget draft; and the
// budget's own guard still refuses a malformed budget whoever writes it.
// req: AI-10
// req: ai/task-catalogue recur.create_template Create a recurring template
// req: ai/task-catalogue plan.propose_budget Create a budget version
// asvs: V8.2.2, V8.3.1
func TestAttackTheExecutorAddsARecurringJournalAndDraftsABudgetAndNothingMore(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Executor master data Pte Ltd")
	start := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	next := start
	var rj storage.RecurringJournal
	var budgetID uuid.UUID
	exec := store(t, "fyniq_executor")
	if err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rj, err = u.RecurringJournals().Create(ctx, storage.RecurringJournalDetails{Name: "Rent accrual", Status: storage.RecurringActive,
			Schedule: storage.Schedule{EveryMonths: 1, DayOfMonth: 1, StartsOn: start}, NextOn: &next, Content: []byte(`{"lines":[]}`)})
		if err != nil {
			return err
		}
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBudget, Payload: []byte(`{"name":"FY2099","kind":"original","financial_year":"2099-01-01","lines":[]}`)})
		budgetID = d.ID
		return err
	}); err != nil {
		t.Fatalf("what the executor may do: %v", err)
	}
	// A budget that breaks its guard is refused whoever writes it.
	err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBudget, Payload: []byte(`{"name":"x","kind":"original","financial_year":"2099-01-01","lines":[{"account_id":"nope","amounts":["1"]}],"extra":1}`)})
		return err
	})
	if err == nil {
		t.Error("the executor drafted a malformed budget")
	}
	for _, c := range []struct{ what, stmt string }{
		{"pausing a recurring journal", `UPDATE fyniq.recurring_journal SET status = 'paused' WHERE id = $1`},
		{"moving its next occurrence", `UPDATE fyniq.recurring_journal SET next_on = '2099-06-01' WHERE id = $1`},
		{"deleting it", `DELETE FROM fyniq.recurring_journal WHERE id = $1`},
		{"changing its content", `UPDATE fyniq.recurring_journal_version SET content = '{}' WHERE recurring_journal_id = $1`},
		{"making its draft", `SELECT fyniq.create_scheduled_draft($1, '2099-01-01')`},
		{"deleting a budget draft", `DELETE FROM fyniq.document WHERE id = $2`},
		{"adding a recurring bill", `INSERT INTO fyniq.recurring_bill (name, status, every_months, day_of_month, starts_on, next_on) VALUES ('x', 'active', 1, 1, '2099-01-01', '2099-01-01')`},
		{"adding a recurring invoice", `INSERT INTO fyniq.recurring_invoice (name, status, every_months, day_of_month, starts_on, next_on) VALUES ('x', 'active', 1, 1, '2099-01-01', '2099-01-01')`},
		{"approving a budget", `UPDATE fyniq.document SET status = 'posted' WHERE id = $2`},
		{"revising a budget", `UPDATE fyniq.document SET current_version = 2 WHERE id = $2`},
	} {
		err := asOrg(ctx, conn(t, "fyniq_executor"), b.Org, func(tx pgx.Tx) error {
			if c.what == "adding a recurring bill" || c.what == "adding a recurring invoice" {
				_, err := tx.Exec(ctx, c.stmt)
				return err
			}
			if strings.Contains(c.stmt, "$2") {
				_, err := tx.Exec(ctx, strings.Replace(c.stmt, "$2", "$1", 1), budgetID)
				return err
			}
			_, err := tx.Exec(ctx, c.stmt, rj.ID)
			return err
		})
		if err == nil {
			t.Errorf("the executor was allowed %s", c.what)
		}
	}
	_ = uuid.Nil
}
