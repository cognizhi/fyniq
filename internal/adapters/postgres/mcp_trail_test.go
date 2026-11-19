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
