//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// auditActionsAfter are the actions of the organisation's audit events after sequence number seq, in order, read as
// the read-only role; and the last sequence number.
func auditActionsAfter(t *testing.T, e *env, seq int64) ([]string, int64) {
	t.Helper()
	reader, err := postgres.Open(context.Background(), db.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var actions []string
	last := seq
	if err := reader.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, seq, 10000)
		for _, ev := range events {
			var body struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal([]byte(ev.Body), &body); err != nil {
				return err
			}
			actions = append(actions, body.Action)
			last = ev.Seq
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return actions, last
}

// The first-run tour's state (FR-UX-34): offered until the person answers, offered again to resume where it stopped,
// their own, and the tour's only write: every save is the state and its audit event, nothing else (G4-104, QA-UI-06).
// req: FR-UX-34
// req: G4-104
func TestTheTourSavesOnlyItsOwnState(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	drainWork(t, e)

	got := e.call(http.MethodGet, "/v1/help-state", ahkow, nil)
	if got.Status != 200 || got.Body["tour"] != "not_started" || got.Body["offer"] != true || got.Body["step"] != float64(0) || got.Header.Get("ETag") != got.str("etag") {
		t.Fatalf("before the tour: %d %s", got.Status, got.Raw)
	}
	_, seq := auditActionsAfter(t, e, 0)

	// Started, moved on and stopped part-way: offered again, to resume at the step shown last.
	started := e.call(http.MethodPut, "/v1/help-state/tour", ahkow, map[string]any{"tour": "started", "step": 0}, "If-Match", got.str("etag"))
	if started.Status != 200 || started.Body["tour"] != "started" || started.Body["offer"] != true {
		t.Fatalf("starting: %d %s", started.Status, started.Raw)
	}
	moved := e.call(http.MethodPut, "/v1/help-state/tour", ahkow, map[string]any{"tour": "started", "step": 2}, "If-Match", started.Header.Get("ETag"))
	if moved.Status != 200 || moved.Body["step"] != float64(2) {
		t.Fatalf("moving on: %d %s", moved.Status, moved.Raw)
	}
	if stale := e.call(http.MethodPut, "/v1/help-state/tour", ahkow, map[string]any{"tour": "started", "step": 3}, "If-Match", started.Header.Get("ETag")); stale.Status != 412 {
		t.Fatalf("a stale step was saved: %d %s", stale.Status, stale.Raw)
	}
	if again := e.call(http.MethodGet, "/v1/help-state", ahkow, nil); again.Body["tour"] != "started" || again.Body["step"] != float64(2) || again.Body["offer"] != true {
		t.Fatalf("coming back: %s", again.Raw)
	}
	// A step outside the seven, or a state the tour has not, is refused.
	for _, bad := range []map[string]any{{"tour": "started", "step": 7}, {"tour": "paused", "step": 1}} {
		if r := e.call(http.MethodPut, "/v1/help-state/tour", ahkow, bad, "If-Match", moved.Header.Get("ETag")); r.Status != 422 {
			t.Fatalf("%v: %d %s", bad, r.Status, r.Raw)
		}
	}
	done := e.call(http.MethodPut, "/v1/help-state/tour", ahkow, map[string]any{"tour": "finished", "step": 6}, "If-Match", moved.Header.Get("ETag"))
	if done.Status != 200 || done.Body["tour"] != "finished" || done.Body["offer"] != false {
		t.Fatalf("finishing: %d %s", done.Status, done.Raw)
	}

	// Wei Ling's is her own: not started, and declining the offer is saved as skipped.
	theirs := e.call(http.MethodGet, "/v1/help-state", weiling, nil)
	if theirs.Body["tour"] != "not_started" || theirs.Body["offer"] != true {
		t.Fatalf("another person's state: %s", theirs.Raw)
	}
	if declined := e.call(http.MethodPut, "/v1/help-state/tour", weiling, map[string]any{"tour": "skipped", "step": 0}, "If-Match", theirs.str("etag")); declined.Body["offer"] != false {
		t.Fatalf("declining: %d %s", declined.Status, declined.Raw)
	}

	// Each save wrote its state and its audit event, and nothing else did.
	actions, _ := auditActionsAfter(t, e, seq)
	if len(actions) != 4 {
		t.Fatalf("the tour's writes: %v", actions)
	}
	for _, a := range actions {
		if a != "user_help_state.set" {
			t.Fatalf("the tour wrote %q: %v", a, actions)
		}
	}

	// A caller that is not a person has no tour.
	client := e.principal(authn.Principal{Subject: "client-1", Client: "integration", Class: authn.ClassIntegration, Organisation: e.books.Org,
		Roles: []authz.Role{authz.Owner}})
	if r := e.call(http.MethodGet, "/v1/help-state", client, nil); r.Status != 200 || r.Body["offer"] != false {
		t.Fatalf("a client's state: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPut, "/v1/help-state/tour", client, map[string]any{"tour": "started", "step": 0}, "If-Match", "*"); r.Status != 422 || r.str("code") != CodeHelpNoUser {
		t.Fatalf("a client's tour: %d %s", r.Status, r.Raw)
	}
}

// A person's opt-in to AI apps over MCP (AI-35): off until they choose, their own, audited as the save and nothing else, and
// never a client's, an integration's or an AI's to make. With the organisation's switch and allowlist it is what lets an
// AI app connect, so opting out is the person's own kill switch.
// req: AI-35
// req: CTL-MCP-03
// asvs: V8.2.2, V4.1.1
func TestAPersonOptsInToAIAppsAndNobodyElseDoes(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	drainWork(t, e)

	got := e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil)
	if got.Status != 200 || got.Body["opted_in"] != false || got.Body["organisation_on"] != false || got.Body["can_connect"] != false || got.Header.Get("ETag") != got.str("etag") {
		t.Fatalf("before choosing: %d %s", got.Status, got.Raw)
	}
	_, seq := auditActionsAfter(t, e, 0)

	// Opting in does not let an app connect while the organisation's switch is off.
	in := e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": true}, "If-Match", got.str("etag"))
	if in.Status != 200 || in.Body["opted_in"] != true || in.Body["can_connect"] != false {
		t.Fatalf("opting in: %d %s", in.Status, in.Raw)
	}
	if stale := e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": false}, "If-Match", got.str("etag")); stale.Status != 412 {
		t.Fatalf("a stale choice was saved: %d %s", stale.Status, stale.Raw)
	}

	// With an Owner's switch on and an app allowed, it is what lets one connect; and opting out stops it again.
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Settings().Record(ctx, "features.mcp", "false", "true", from, 1); err != nil {
			return err
		}
		_, err := u.Settings().Record(ctx, "mcp.clients", "", "claude-code", from, 1)
		return err
	})
	if on := e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil); on.Body["can_connect"] != true || on.Body["organisation_on"] != true || len(on.Body["apps"].([]any)) != 1 {
		t.Fatalf("the organisation on and the person in: %s", on.Raw)
	}
	out := e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": false}, "If-Match", in.Header.Get("ETag"))
	if out.Status != 200 || out.Body["opted_in"] != false || out.Body["can_connect"] != false {
		t.Fatalf("opting out: %d %s", out.Status, out.Raw)
	}

	// Wei Ling's choice is her own: not made, and Ah Kow's does not carry over.
	theirs := e.call(http.MethodGet, "/v1/me/mcp", weiling, nil)
	if theirs.Body["opted_in"] != false || theirs.Body["can_connect"] != false || theirs.Body["set_at"] != nil {
		t.Fatalf("another person's choice: %s", theirs.Raw)
	}

	// Each save wrote its choice and its audit event, and nothing else did.
	actions, _ := auditActionsAfter(t, e, seq)
	saves := 0
	for _, a := range actions {
		if a == "mcp_user_optin.set" {
			saves++
		}
	}
	if saves != 2 {
		t.Fatalf("the choices' audit events: %v", actions)
	}

	// Only a person chooses: not a client, an integration or an AI, and not an Employee, an Auditor or the Board.
	for name, p := range map[string]authn.Principal{
		"an integration": {Subject: "client-1", Client: "integration", Class: authn.ClassIntegration, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}},
		"an AI app":      {Subject: "agent-1", Client: "claude-code", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}},
	} {
		tok := e.principal(p)
		if r := e.call(http.MethodPut, "/v1/me/mcp", tok, map[string]any{"opted_in": true}, "If-Match", "*"); r.Status != 403 && (r.Status != 422 || r.str("code") != CodeMCPNoUser) {
			t.Errorf("%s opted a person in: %d %s", name, r.Status, r.Raw)
		}
	}
	for _, role := range []authz.Role{authz.Employee, authz.Auditor, authz.BoardViewer} {
		tok := e.as(role)
		if r := e.call(http.MethodPut, "/v1/me/mcp", tok, map[string]any{"opted_in": true}, "If-Match", "*"); r.Status != 403 {
			t.Errorf("%s: %d %s", role, r.Status, r.Raw)
		}
	}
}
