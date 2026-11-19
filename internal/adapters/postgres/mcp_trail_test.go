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

// The trail of AI apps' calls over MCP goes only forward and stays in its organisation: the API's role records and reads it and
// can neither change nor erase it, the AI's role reads none of it, another organisation sees none of it, a record that claims a
// read with a refusal's code is refused, and the first-seen marks are once-only (AI-44, AI-45, QA-ENG-05).
// req: AI-44
// req: AI-45
// req: QA-ENG-05
// asvs: V8.1.1, V8.2.2, V16.4.1
func TestAttackTheMCPTrailGoesOnlyForward(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Trail A Pte Ltd"), books(t, "Trail B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person := founder(t, api, a.Org, 1, now)
	record := func(c storage.MCPCall) error {
		return api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.MCPCalls().Record(ctx, c)
			return err
		})
	}
	good := storage.MCPCall{UserID: person, App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRead, At: now}
	if err := record(good); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]storage.MCPCall{
		"a read that carries a refusal's code": {UserID: person, App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRead, Code: "MCP-SCOPE-01", At: now},
		"a refusal with no code":               {UserID: person, App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRefused, At: now},
		"an app that is not a name":            {UserID: person, App: "Claude Code; DROP", Tool: "metrics.query", Outcome: storage.MCPRead, At: now},
		"a tool that is not a tool":            {UserID: person, App: "claude-code", Tool: "x y", Outcome: storage.MCPRead, At: now},
		"an input hash that is not a hash":     {UserID: person, App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRead, InputSHA256: "the whole input", At: now},
		"a token as its identity":              {UserID: person, App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRead, TokenID: "ory_at_secretsecretsecret", At: now},
	} {
		if err := record(bad); err == nil {
			t.Errorf("%s was recorded", name)
		}
	}
	// Another organisation sees none of it, and counts none of it.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rows, err := u.MCPCalls().List(ctx, uuid.Nil, 10)
		if err == nil && len(rows) != 0 {
			t.Errorf("another organisation reads %d calls", len(rows))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Once only: the first time is first, the second is not.
	for i, want := range []bool{true, false} {
		err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			first, err := u.MCPCalls().FirstSeen(ctx, person, "app", "claude-code", now)
			if err == nil && first != want {
				t.Errorf("the %d time: first %v, want %v", i+1, first, want)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The API's role cannot change or erase either, and the AI's role cannot read them.
	exec := func(role, stmt string) error {
		c, err := pgx.Connect(ctx, db.URL(role))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(ctx)
		tx, err := c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", a.Org.String()); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, stmt)
		return err
	}
	for _, stmt := range []string{"UPDATE fyniq.mcp_call SET outcome = 'refused'", "DELETE FROM fyniq.mcp_call", "TRUNCATE fyniq.mcp_call",
		"UPDATE fyniq.mcp_seen SET value = 'x'", "DELETE FROM fyniq.mcp_seen", "UPDATE fyniq.mcp_user_optin SET opted_in = true", "DELETE FROM fyniq.mcp_user_optin"} {
		if err := exec("fyniq_api", stmt); err == nil {
			t.Errorf("the API's role was allowed: %s", stmt)
		}
	}
	for _, stmt := range []string{"SELECT * FROM fyniq.mcp_call", "SELECT * FROM fyniq.mcp_seen", "SELECT * FROM fyniq.mcp_user_optin",
		"INSERT INTO fyniq.mcp_call (user_id, app, tool, outcome, at) VALUES (gen_random_uuid(), 'x', 'y', 'read', now())"} {
		if err := exec("fyniq_ai", stmt); err == nil {
			t.Errorf("the AI's role was allowed: %s", stmt)
		}
	}
}

// The trail of calls is purged after its retention, by the worker alone, only the calls made before the time asked, only in its
// organisation: the API's role cannot run the purge, the guard still refuses a delete or an update from anyone else, and the purge
// leaves the setting of the flag off (AI-44, §11.9).
// req: AI-44
// req: QA-ENG-05
// asvs: V8.1.1, V16.4.1
func TestTheMCPTrailIsPurgedAfterItsRetentionByTheWorkerAlone(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Purge A Pte Ltd"), books(t, "Purge B Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person, other := founder(t, api, a.Org, 1, now), founder(t, api, b.Org, 2, now)
	record := func(org uuid.UUID, who uuid.UUID, at time.Time) {
		t.Helper()
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.MCPCalls().Record(ctx, storage.MCPCall{UserID: who, App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRead, At: at})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	old, recent := now.AddDate(0, 0, -400), now.AddDate(0, 0, -10)
	record(a.Org, person, old)
	record(a.Org, person, old.Add(time.Hour))
	record(a.Org, person, recent)
	record(b.Org, other, old)
	count := func(org uuid.UUID) (n int) {
		t.Helper()
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			rows, err := u.MCPCalls().List(ctx, uuid.Nil, 100)
			n = len(rows)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// The API's role cannot run it.
	err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MCPCalls().Purge(ctx, now.AddDate(0, 0, -365))
		return err
	})
	if err == nil {
		t.Error("the API's role ran the purge")
	}
	if count(a.Org) != 3 {
		t.Fatalf("the trail after a refused purge: %d", count(a.Org))
	}
	// The worker's purge removes the old calls of its organisation and leaves the recent ones and the other organisation's.
	var purged int
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		purged, err = u.MCPCalls().Purge(ctx, now.AddDate(0, 0, -365))
		return err
	}); err != nil || purged != 2 {
		t.Fatalf("the purge: %d, %v", purged, err)
	}
	if count(a.Org) != 1 || count(b.Org) != 1 {
		t.Errorf("after the purge: %d and %d calls, want 1 and 1", count(a.Org), count(b.Org))
	}
	// A delete outside the purge is refused as before, from the worker's role too, which holds no DELETE.
	for _, role := range []string{"fyniq_api", "fyniq_worker"} {
		c, err := pgx.Connect(ctx, db.URL(role))
		if err != nil {
			t.Fatal(err)
		}
		tx, _ := c.Begin(ctx)
		_, _ = tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", a.Org.String())
		if _, err := tx.Exec(ctx, "DELETE FROM fyniq.mcp_call"); err == nil {
			t.Errorf("%s deleted the trail", role)
		}
		_ = tx.Rollback(ctx)
		c.Close(ctx)
	}
}
