//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The database keeps each agent's route whoever writes (golden rule 2; AI-74): the agents are the release's, written
// only by the migrations; a route's version is the next for its agent, with its steps written with it, never changed or
// removed; a step names only a model on its connection's allow-list; a route names only classes its agent handles; the
// worker and the AI write none of it, and another organisation sees none of it. The call log is written only through
// record_ai_call, by the API and the worker in their own organisation, never by the AI (CMP-AI-01), and never changed
// (AI-12, AI-76).
// req: AI-74
// req: AI-76
func TestAttackRoutesKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Routes A Pte Ltd"), books(t, "Routes B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	approval := uuid.Must(uuid.NewV7())
	var c storage.AIConnection
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "OpenAI", Connector: "openai", Auth: "api-key", Configuration: json.RawMessage(`{}`),
			Host: "api.openai.com", Billing: "usage", CreatedBy: "user:x", CreatedAt: now}, approval); err != nil {
			return err
		}
		if _, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "refresh", Outcome: "complete", Pages: 1, Added: 2,
			ReadBy: "user:x", ReadAt: now}, []storage.AIModel{{ConnectionID: c.ID, Model: "gpt-6", FirstListed: now, LastListed: now, Listed: true},
			{ConnectionID: c.ID, Model: "gpt-5", FirstListed: now, LastListed: now, Listed: true}}); err != nil {
			return err
		}
		if _, err := u.AIModels().Probe(ctx, c.ID, "gpt-6", true, false, "answered", now); err != nil {
			return err
		}
		_, err = u.AIModels().Allow(ctx, c.ID, "gpt-6", true, &approval)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	save := func(org uuid.UUID, r storage.AIRoute) (storage.AIRoute, error) {
		var out storage.AIRoute
		err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIRoutes().Save(ctx, r)
			return err
		})
		return out, err
	}
	route := func(version int, classes []string, models ...string) storage.AIRoute {
		r := storage.AIRoute{Agent: "chat", Version: version, Classes: classes, MadeBy: "user:x", MadeAt: now}
		for _, m := range models {
			r.Steps = append(r.Steps, storage.AIRouteStep{ConnectionID: c.ID, Model: m, Effort: "high", Cache: true})
		}
		return r
	}
	v1, err := save(a.Org, route(1, []string{"know-how", "business-figures"}, "gpt-6"))
	if err != nil || v1.Version != 1 || len(v1.Steps) != 1 || v1.Steps[0].Position != 1 {
		t.Fatalf("the first version: %v %+v", err, v1)
	}
	if _, err := save(a.Org, route(1, []string{"know-how"}, "gpt-6")); !storageCode(err, storage.CodeAIRoute) && !storageCode(err, storage.CodeDuplicate) {
		t.Errorf("a version twice: %v", err)
	}
	if _, err := save(a.Org, route(3, []string{"know-how"}, "gpt-6")); !storageCode(err, storage.CodeAIRoute) {
		t.Errorf("a version skipped: %v", err)
	}
	if _, err := save(a.Org, route(2, []string{"know-how"}, "gpt-5")); !storageCode(err, storage.CodeAIRoute) {
		t.Errorf("a model not on the allow-list: %v", err)
	}
	if _, err := save(a.Org, route(2, []string{"payroll"}, "gpt-6")); err != nil {
		t.Errorf("the assistant handles payroll: %v", err)
	}
	report := route(1, []string{"personal-data"}, "gpt-6")
	report.Agent = "report_commentary"
	if _, err := save(a.Org, report); !storageCode(err, storage.CodeAIRoute) {
		t.Errorf("a class the agent never handles: %v", err)
	}
	unknown := route(1, []string{"know-how"}, "gpt-6")
	unknown.Agent = "dreamer"
	if _, err := save(a.Org, unknown); err == nil {
		t.Error("a route of an agent the release does not have")
	}

	// The API records an attempt through the function, in its own organisation; the AI may not (CMP-AI-01).
	record := `SELECT fyniq.record_ai_call(gen_random_uuid(), 'chat', 1, 1, NULL, $1, 'gpt-6', 'gpt-6', 'gpt-6-20261001', 'user:x',
	  ARRAY['business-figures'], 'ok', NULL, 1000, 500, 0, 0, 100, NULL, NULL, NULL, NULL, 12, now())`
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, record, c.ID); return err }); err != nil {
		t.Errorf("the API recording a call: %v", err)
	}
	wantState(t, "the AI recording a call", asOrg(ctx, conn(t, "fyniq_ai"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, record, c.ID); return err }),
		"42501")
	if err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, record, c.ID); return err }); err == nil {
		t.Error("the API recorded a call on another organisation's connection")
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner changing a route", "fyniq_migrator", `UPDATE fyniq.ai_route SET classes = ARRAY['know-how']`, []string{"FY334"}},
		{"the owner removing a route", "fyniq_migrator", `DELETE FROM fyniq.ai_route`, []string{"FY334"}},
		{"the owner changing a step", "fyniq_migrator", `UPDATE fyniq.ai_route_step SET model = 'gpt-5'`, []string{"FY334"}},
		{"the owner removing a step", "fyniq_migrator", `DELETE FROM fyniq.ai_route_step`, []string{"FY334"}},
		{"the API adding a step to a saved version", "fyniq_api", `INSERT INTO fyniq.ai_route_step (route_id, position, connection_id, model)
		  SELECT r.id, 5, s.connection_id, s.model FROM fyniq.ai_route r JOIN fyniq.ai_route_step s ON s.route_id = r.id WHERE r.version = 1`, []string{"FY334"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_route_step, fyniq.ai_route`, []string{"FY060", "0A000"}},
		{"a route of restricted data", "fyniq_api", `INSERT INTO fyniq.ai_route (agent, version, classes, made_by, made_at)
		  VALUES ('chat', 3, ARRAY['restricted'], 'user:x', now())`, []string{"23514", "FY334"}},
		{"a sixth step", "fyniq_api", `INSERT INTO fyniq.ai_route_step (route_id, position, connection_id, model)
		  SELECT gen_random_uuid(), 6, gen_random_uuid(), 'gpt-6'`, []string{"23514", "FY334"}},
		{"the API adding an agent", "fyniq_api", `INSERT INTO fyniq.ai_agent (id, name, description, classes, line, position)
		  VALUES ('dreamer', 'Dreamer', 'Dreams.', ARRAY['know-how'], 'main', 999)`, []string{"42501"}},
		{"the worker setting a route", "fyniq_worker", `INSERT INTO fyniq.ai_route (agent, version, classes, made_by, made_at)
		  VALUES ('chat', 3, ARRAY['know-how'], 'system:x', now())`, []string{"42501"}},
		{"the AI reading the routes", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_route`, []string{"42501"}},
		{"the API writing the call log", "fyniq_api", `INSERT INTO fyniq.ai_call (run_id, agent, route_version, step, connection_id, model_requested,
		  actor, classes, outcome, detail, started_at) VALUES (gen_random_uuid(), 'chat', 1, 1, gen_random_uuid(), 'gpt-6', 'user:x',
		  ARRAY['know-how'], 'skipped', 'x', now())`, []string{"42501"}},
		{"the AI writing the call log", "fyniq_ai", `INSERT INTO fyniq.ai_call (run_id, agent, route_version, step, connection_id, model_requested,
		  actor, classes, outcome, detail, started_at) VALUES (gen_random_uuid(), 'chat', 1, 1, gen_random_uuid(), 'gpt-6', 'user:x',
		  ARRAY['know-how'], 'skipped', 'x', now())`, []string{"42501"}},
		{"the owner changing a call", "fyniq_migrator", `UPDATE fyniq.ai_call SET outcome = 'ok'`, []string{"FY335"}},
		{"the owner removing a call", "fyniq_migrator", `DELETE FROM fyniq.ai_call`, []string{"FY335"}},
		{"a call that served with no model reported", "fyniq_api", `SELECT fyniq.record_ai_call(gen_random_uuid(), 'chat', 1, 1, NULL, '` + c.ID.String() +
			`', 'gpt-6', NULL, NULL, 'user:x', ARRAY['know-how'], 'ok', NULL, 0, 0, 0, 0, 0, NULL, NULL, NULL, NULL, 0, now())`, []string{"23514"}},
		{"a call carrying restricted data", "fyniq_api", `SELECT fyniq.record_ai_call(gen_random_uuid(), 'chat', 1, 1, NULL, '` + c.ID.String() +
			`', 'gpt-6', NULL, NULL, 'user:x', ARRAY['restricted'], 'skipped', 'x', 0, 0, 0, 0, 0, NULL, NULL, NULL, NULL, 0, now())`, []string{"23514"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}

	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		routes, err := u.AIRoutes().Current(ctx)
		if err == nil && len(routes) != 0 {
			t.Errorf("another organisation sees %d routes", len(routes))
		}
		agents, err := u.AIRoutes().Agents(ctx)
		if err == nil && (len(agents) < 8 || agents[0].ID != "chat") {
			t.Errorf("the agents are the release's: %+v", agents)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
