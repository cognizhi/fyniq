//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
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
