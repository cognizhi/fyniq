//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Budgets are admission control the database keeps (AI-86): ten calls that could each cost 1 against 5 left, started
// together on their own connections, admit at most five; the reservation that reaches 80% raises the alert once; a
// settled reservation frees what it did not cost, and an unknown one stays whole; a connection's spent budget and the
// agent's are told apart; a cost that could not be put in the base currency is refused where a budget applies.
// Budgets keep their scope and versions, reservations move only forward, neither is removed, the AI runs none of it
// (CMP-AI-01), and another organisation sees none of it.
// req: AI-86
// req: AI-77
// req: QA-ENG-05
// asvs: V2.3.2, V8.2.2
func TestAttackAIBudgetsAdmitOnlyWhatTheyCover(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Budgets A Pte Ltd"), books(t, "Budgets B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var c storage.AIConnection
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		c, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "OpenAI", Connector: "openai", Auth: "api-key", Configuration: json.RawMessage(`{}`),
			Host: "api.openai.com", Billing: "usage", CreatedBy: "user:x", CreatedAt: now}, uuid.Must(uuid.NewV7()))
		if err != nil {
			return err
		}
		_, err = u.AIBudgets().Set(ctx, storage.AIBudget{Scope: "agent", Subject: "chat", Amount: "5", Version: 1, SetBy: "user:owner", SetAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const month = "2099-01"
	reserve := func(org uuid.UUID, connection uuid.UUID, amount string) (uuid.UUID, []storage.AIBudgetAlert, error) {
		var id uuid.UUID
		var alerts []storage.AIBudgetAlert
		err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			id, alerts, err = u.AIBudgets().Reserve(ctx, storage.AIReservation{Run: uuid.Must(uuid.NewV7()), Step: 1, Agent: "chat", Connection: connection,
				Person: "user:x", Month: month, Amount: amount})
			return err
		})
		return id, alerts, err
	}

	// Ten calls of 1 against 5, together.
	type result struct {
		id     uuid.UUID
		alerts int
		err    error
	}
	results := make(chan result, 10)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id, alerts, err := reserve(a.Org, c.ID, "1")
			results <- result{id, len(alerts), err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var admitted []uuid.UUID
	alerted := 0
	for r := range results {
		switch {
		case r.err == nil:
			admitted, alerted = append(admitted, r.id), alerted+r.alerts
		case !storageCode(r.err, storage.CodeAIBudgetSpent):
			t.Errorf("a refusal: %v", r.err)
		}
	}
	if len(admitted) != 5 || alerted != 1 {
		t.Fatalf("%d admitted of ten, %d alerts", len(admitted), alerted)
	}

	// Settling frees what an attempt did not cost; an unknown outcome keeps its reservation whole.
	settle := func(id uuid.UUID, actual string, known bool) error {
		return api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.AIBudgets().Settle(ctx, id, actual, known)
		})
	}
	if err := settle(admitted[0], "0.5", true); err != nil {
		t.Fatal(err)
	}
	if err := settle(admitted[1], "", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reserve(a.Org, c.ID, "1"); !storageCode(err, storage.CodeAIBudgetSpent) {
		t.Errorf("4.5 used, 1 more: %v", err)
	}
	if err := settle(admitted[1], "0.5", true); err != nil {
		t.Fatalf("an unknown outcome reconciled: %v", err)
	}
	if _, _, err := reserve(a.Org, c.ID, "1"); err != nil {
		t.Errorf("4 used, 1 more: %v", err)
	}
	if err := settle(admitted[0], "0.1", true); !storageCode(err, storage.CodeAIBudget) {
		t.Errorf("settling twice: %v", err)
	}
	var used string
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIBudgets().List(ctx, month)
		if err == nil && len(all) == 1 {
			used = all[0].Used
		}
		return err
	}); err != nil || used != "5" {
		t.Errorf("used this month: %q %v", used, err)
	}

	// A connection's budget spent is told apart, so the next step may serve; a cost with no rate is refused where a
	// budget applies, and admitted where none does.
	other := uuid.Must(uuid.NewV7())
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.AIBudgets().Set(ctx, storage.AIBudget{Scope: "connection", Subject: c.ID.String(), Amount: "0.1", Version: 1, SetBy: "user:owner",
			SetAt: now}); err != nil {
			return err
		}
		_, err := u.AIBudgets().Set(ctx, storage.AIBudget{Scope: "agent", Subject: "chat", Amount: "100", Version: 2, SetBy: "user:owner", SetAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reserve(a.Org, c.ID, "1"); !storageCode(err, storage.CodeAIConnectionSpent) {
		t.Errorf("the connection's budget: %v", err)
	}
	if _, _, err := reserve(a.Org, c.ID, ""); !storageCode(err, storage.CodeAIBudgetSpent) {
		t.Errorf("no rate where a budget applies: %v", err)
	}
	if _, _, err := reserve(b.Org, other, ""); err == nil {
		t.Error("a reservation on a connection the organisation does not have")
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIBudgets().Set(ctx, storage.AIBudget{Scope: "agent", Subject: "chat", Amount: "1", Version: 2, SetBy: "user:owner", SetAt: now})
		return err
	}); !storageCode(err, storage.CodeVersion) {
		t.Errorf("a budget from an old version: %v", err)
	}

	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner moving a budget", "fyniq_migrator", `UPDATE fyniq.ai_budget SET subject = 'kb_compiler' WHERE scope = 'agent'`, []string{"FY336"}},
		{"the owner removing a budget", "fyniq_migrator", `DELETE FROM fyniq.ai_budget`, []string{"FY336"}},
		{"the owner shrinking a reservation", "fyniq_migrator", `UPDATE fyniq.ai_reservation SET amount = 0`, []string{"FY336"}},
		{"the owner putting a settled reservation back", "fyniq_migrator", `UPDATE fyniq.ai_reservation SET state = 'held', actual = NULL,
		  settled_at = NULL WHERE state = 'settled'`, []string{"FY336", "23514"}},
		{"the owner removing a reservation", "fyniq_migrator", `DELETE FROM fyniq.ai_reservation`, []string{"FY336"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_reservation, fyniq.ai_budget`, []string{"FY060", "0A000"}},
		{"the API settling by hand", "fyniq_api", `UPDATE fyniq.ai_reservation SET actual = 0`, []string{"42501"}},
		{"the API reserving by hand", "fyniq_api", `INSERT INTO fyniq.ai_reservation (run_id, step, agent, connection_id, person, month, amount)
		  VALUES (gen_random_uuid(), 1, 'chat', gen_random_uuid(), 'user:x', '2099-01', 0)`, []string{"42501"}},
		{"the worker setting a budget", "fyniq_worker", `INSERT INTO fyniq.ai_budget (scope, subject, amount, set_by, set_at)
		  VALUES ('organisation', '', 1, 'system:x', now())`, []string{"42501"}},
		{"a budget of zero", "fyniq_api", `UPDATE fyniq.ai_budget SET amount = 0, version = version + 1`, []string{"23514"}},
		{"the AI reserving", "fyniq_ai", `SELECT fyniq.ai_reserve(gen_random_uuid(), 1, 'chat', gen_random_uuid(), 'user:x', '2099-01', 0)`, []string{"42501"}},
		{"the AI settling", "fyniq_ai", `SELECT fyniq.ai_settle(gen_random_uuid(), 0, true)`, []string{"42501"}},
		{"the AI reading the budgets", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_budget`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
	_, err := conn(t, "fyniq_api").Exec(ctx, `SELECT fyniq.ai_reserve(gen_random_uuid(), 1, 'chat', gen_random_uuid(), 'user:x', '2099-01', 0)`)
	wantState(t, "a reservation outside any organisation", err, "FY336")
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIBudgets().List(ctx, month)
		if err == nil && len(all) != 0 {
			t.Errorf("another organisation sees %d budgets", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
