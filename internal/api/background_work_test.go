//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// deadWork is a task of the background work whose attempts ran out, as the sweep leaves it, with why.
func (e *env) deadWork(why string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := workerStore(e.t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.work", Payload: []byte(`{}`)})
		if err != nil {
			return err
		}
		id = row.ID
		return u.Sweeping().MarkDead(ctx, row.ID, why)
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// workState is where a task of the background work stands.
func (e *env) workState(id uuid.UUID) storage.OutboxState {
	e.t.Helper()
	var state storage.OutboxState
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.Outbox().Get(ctx, id)
		state = row.State
		return err
	})
	return state
}

// A task whose attempts ran out shows on Checks, C.2 #12, until a person replays it (G4-073; §12.11, ADR-T7): its kind,
// its attempts, when it stopped and why. An Administrator or an Owner, who are told when a task stops, replays it: it
// is pending again with its attempts counted afresh and its last error kept, the replay is audited, and the check no
// longer lists it once it runs again. Nobody else replays one; a task not stopped is not found.
// req: G4-073
// req: FR-AUT-01
func TestAStoppedTaskShowsOnChecksAndReplays(t *testing.T) {
	e := newEnv(t)
	id := e.deadWork("the provider refused the connection")
	e.runCatalogue()
	c := e.call(http.MethodGet, "/v1/checks/scheduler.jobs-on-time", e.as(authz.Owner), nil)
	var listed map[string]any
	for _, it := range itemsOf(c) {
		if it["kind"] == "outbox" && it["id"] == id.String() {
			listed = it
		}
	}
	if listed == nil || listed["label"] != "Background work test.work" || !strings.Contains(listed["detail"].(string), "It stopped after 0 attempts on") ||
		!strings.Contains(listed["detail"].(string), "the provider refused the connection") {
		t.Fatalf("C.2 #12 with a stopped task: %s", c.Raw)
	}
	wantStatus(t, c, "warning") // to look at and run again: it does not hold the close

	path := "/v1/background-work/" + id.String() + "/replay"
	for _, r := range []authz.Role{authz.Accountant, authz.Controller, authz.Bookkeeper} {
		e.problemIs(e.call(http.MethodPost, path, e.as(r), nil), "replayBackgroundWork", 403, CodeForbidden)
	}
	if e.workState(id) != storage.OutboxDead {
		t.Fatal("a refused replay moved the task")
	}
	admin := e.as(authz.Administrator)
	r := e.call(http.MethodPost, path, admin, nil, "Idempotency-Key", "replay-"+id.String())
	if r.Status != http.StatusOK || r.str("state") != "pending" || r.Body["attempts"] != float64(0) || r.str("last_error") != "the provider refused the connection" {
		t.Fatalf("an Administrator's replay: %d %s", r.Status, r.Raw)
	}
	if again := e.call(http.MethodPost, path, admin, nil, "Idempotency-Key", "replay-"+id.String()); again.Status != http.StatusOK ||
		again.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("the same replay again: %d %s", again.Status, again.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path, admin, nil), "replayBackgroundWork", 404, CodeNotFound)
	var audited bool
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Find(ctx, storage.AuditQuery{Action: "outbox.row.replayed", ObjectType: "outbox", ObjectID: &id, Limit: 5})
		for _, ev := range events {
			audited = audited || strings.Contains(ev.Body, `"state":["dead","pending"]`)
		}
		return err
	})
	if !audited {
		t.Error("the replay is not in the trail")
	}

	// The Owner, told too, replays another.
	other := e.deadWork("the render service did not answer")
	if r := e.call(http.MethodPost, "/v1/background-work/"+other.String()+"/replay", e.as(authz.Owner), nil); r.Status != http.StatusOK {
		t.Fatalf("an Owner's replay: %d %s", r.Status, r.Raw)
	}
	e.runCatalogue()
	if c := e.call(http.MethodGet, "/v1/checks/scheduler.jobs-on-time", e.as(authz.Owner), nil); strings.Contains(c.Raw, id.String()) || strings.Contains(c.Raw, other.String()) {
		t.Errorf("C.2 #12 after the replays: %s", c.Raw)
	}
}
