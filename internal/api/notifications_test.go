//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fanOutAll runs the worker's fan-out of every queued notification event, in its own organisation, and drops other
// work.
func fanOutAll(t *testing.T, e *env) {
	t.Helper()
	fanOutAllIn(t, e, db.URL("fyniq_worker"))
}

// fanOutAllIn is fanOutAll as the worker of the database at workerURL, such as a fresh installation's.
func fanOutAllIn(t *testing.T, e *env, workerURL string) {
	t.Helper()
	ctx := context.Background()
	worker, err := postgres.Open(ctx, workerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	f := notify.Fanout{Store: worker, Dispatcher: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock,
		Origin: "https://fyniq.test"}
	relays := notify.Relays{Store: worker, Operators: worker, Dispatcher: outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock}} // their fan-outs come round again
	for {
		task, err := e.queue.Next(ctx, "worker")
		if err != nil {
			return
		}
		var m outbox.Message
		if err := json.Unmarshal(task.Payload, &m); err != nil {
			t.Fatal(err)
		}
		var row storage.OutboxRow
		if err := e.store.Do(ctx, m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) error {
			row, err = u.Outbox().Get(ctx, m.OutboxID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		switch row.Kind {
		case notify.FanoutKind:
			if err := f.Work()(ctx, m.OrganisationID, row); err != nil {
				t.Fatal(err)
			}
		case notify.RelayKind:
			if err := relays.Work()(ctx, m.OrganisationID, row); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// An approval asked for reaches those who may approve it, never the person who asked; a role change reaches its member
// as a security notice. Each person reads, unreads, archives and reads all of their own notifications; preferences
// list every type with its recommendation, refuse to turn a security notice off or name no type, choose a digest over
// the type's ETag, and set and clear quiet hours.
// req: FR-NTF-01
// req: FR-NTF-02
// req: FR-NTF-05
func TestTheNotificationCentreAndPreferences(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	drainWork(t, e)
	d := e.call(http.MethodPost, "/v1/journals", ahkow, e.draft("2000.01"))
	if asked := e.call(http.MethodPost, "/v1/journals/"+d.str("id")+"/post-approval", ahkow, nil, "If-Match", d.Header.Get("ETag")); asked.Status != 202 {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	fanOutAll(t, e)
	got := e.call(http.MethodGet, "/v1/notifications", weiling, nil)
	items, _ := got.Body["items"].([]any)
	if got.Status != 200 || got.Body["unread"] != float64(1) || len(items) != 1 {
		t.Fatalf("Wei Ling's notifications: %d %s", got.Status, got.Raw)
	}
	n := items[0].(map[string]any)
	if n["type"] != "approval.requested" || n["actionable"] != true || n["category"] != "approvals" || n["read"] != false ||
		n["title"] != "Post the draft journal (October rent), debits 2000.01: waits for your approval" {
		t.Fatalf("the notification: %v", n)
	}
	if r := e.call(http.MethodGet, "/v1/notifications", ahkow, nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("the requester was told: %s", r.Raw)
	}
	id := n["id"].(string)
	if r := e.call(http.MethodPost, "/v1/notifications/"+id+"/read", weiling, nil, "Idempotency-Key", uuid.NewString()); r.Status != 200 || r.Body["read"] != true {
		t.Fatalf("reading: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/"+id+"/unread", weiling, nil); r.Body["read"] != false {
		t.Fatalf("unreading: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/"+id+"/read", ahkow, nil); r.Status != http.StatusNotFound {
		t.Fatalf("someone else reading it: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/read-all", weiling, nil); r.Body["marked"] != float64(1) {
		t.Fatalf("reading all: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/"+id+"/archive", weiling, nil); r.Body["archived"] != true {
		t.Fatalf("archiving: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/notifications", weiling, nil); len(r.Body["items"].([]any)) != 0 || r.Body["unread"] != float64(0) {
		t.Fatalf("an archived one still listed: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/notifications?archived=true", weiling, nil); len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("the archive: %s", r.Raw)
	}

	// A role change is a security notice to its member.
	members := e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil).Body["items"].([]any)
	var hers string
	for _, m := range members {
		if m.(map[string]any)["user_id"] == f.weiling {
			hers = m.(map[string]any)["id"].(string)
		}
	}
	asked := e.call(http.MethodPost, "/v1/members/"+hers+"/roles", f.as(f.ahkow), map[string]any{"role": "auditor"}, "If-Match", "*")
	if asked.Status != 202 {
		t.Fatalf("asking to assign a role: %d %s", asked.Status, asked.Raw)
	}
	if r := f.decide(f.as(f.weiling), asked.str("id"), "approve", hashOf(asked)); r.Status != 200 {
		t.Fatalf("approving the role: %d %s", r.Status, r.Raw)
	}
	fanOutAll(t, e)
	if r := e.call(http.MethodGet, "/v1/notifications?unread=true", ahkow, nil); !strings.Contains(r.Raw, `"type":"approval.decided"`) {
		t.Fatalf("the requester was not told of the decision: %s", r.Raw)
	}
	sec := e.call(http.MethodGet, "/v1/notifications?unread=true", weiling, nil).Body["items"].([]any)
	if len(sec) == 0 || sec[0].(map[string]any)["type"] != "security.roles" || sec[0].(map[string]any)["severity"] != "warning" {
		t.Fatalf("the security notice: %v", sec)
	}

	// Preferences.
	p := e.call(http.MethodGet, "/v1/notification-preferences", weiling, nil)
	types := p.Body["types"].([]any)
	if p.Status != 200 || len(types) != len(notify.Catalogue) || p.Body["quiet_hours"] != nil {
		t.Fatalf("the preferences: %d %s", p.Status, p.Raw)
	}
	var tag string
	for _, x := range types {
		if m := x.(map[string]any); m["type"] == "approval.decided" {
			tag = m["etag"].(string)
			if m["email"] != "immediate" || m["recommended"] != "immediate" || m["locked"] != false {
				t.Fatalf("approval.decided: %v", m)
			}
		}
	}
	if r := e.call(http.MethodPut, "/v1/notification-preferences/security.roles", weiling, map[string]any{"email": "off"}, "If-Match", "*"); r.problemCode() != CodeNotifyLocked {
		t.Fatalf("turning a security notice off: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPut, "/v1/notification-preferences/no.such", weiling, map[string]any{"email": "off"}, "If-Match", "*"); r.problemCode() != CodeNotifyType {
		t.Fatalf("an unknown type: %d %s", r.Status, r.Raw)
	}
	set := e.call(http.MethodPut, "/v1/notification-preferences/approval.decided", weiling, map[string]any{"email": "daily"}, "If-Match", tag)
	if set.Status != 200 {
		t.Fatalf("choosing the digest: %d %s", set.Status, set.Raw)
	}
	for _, x := range set.Body["types"].([]any) {
		if m := x.(map[string]any); m["type"] == "approval.decided" && m["email"] != "daily" {
			t.Fatalf("after choosing: %v", m)
		}
	}
	if r := e.call(http.MethodPut, "/v1/notification-preferences/approval.decided", weiling, map[string]any{"email": "weekly"}, "If-Match", tag); r.Status != http.StatusPreconditionFailed {
		t.Fatalf("over a stale ETag: %d %s", r.Status, r.Raw)
	}
	qtag := set.str("quiet_hours_etag")
	if r := e.call(http.MethodPut, "/v1/notification-quiet-hours", weiling, map[string]any{"from": "25:00", "to": "07:00"}, "If-Match", qtag); r.problemCode() != CodeNotifyTime {
		t.Fatalf("a time that is not one: %d %s", r.Status, r.Raw)
	}
	q := e.call(http.MethodPut, "/v1/notification-quiet-hours", weiling, map[string]any{"from": "22:00", "to": "07:00"}, "If-Match", qtag)
	if qh, _ := q.Body["quiet_hours"].(map[string]any); q.Status != 200 || qh["from"] != "22:00" || qh["to"] != "07:00" {
		t.Fatalf("setting quiet hours: %d %s", q.Status, q.Raw)
	}
	if c := e.call(http.MethodPut, "/v1/notification-quiet-hours", weiling, map[string]any{}, "If-Match", q.str("quiet_hours_etag")); c.Body["quiet_hours"] != nil {
		t.Fatalf("clearing quiet hours: %s", c.Raw)
	}
}

// matrixNotification fans out a notification to the matrix member and returns its ID.
func matrixNotification(e *env) string {
	user := matrixMemberUser(e)
	fixture := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix-fixture"}})
	if err := e.store.Do(fixture, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := notify.Emit(ctx, outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}, u, e.books.Org, notify.Event{Type: "approval.decided",
			Title: "Approved: matrix", Link: "/approvals/x", Audience: notify.Audience{Users: []uuid.UUID{user}}, At: e.clock.Now()})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	fanOutAll(e.t, e)
	var id uuid.UUID
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ns, err := u.Notifications().List(ctx, storage.NotificationFilter{UserID: user, Limit: 1})
		if err == nil && len(ns) > 0 {
			id = ns[0].ID
		}
		return err
	}); err != nil || id == uuid.Nil {
		e.t.Fatalf("the matrix notification: %v", err)
	}
	return id.String()
}

// asMatrixMember is tok's principal as the matrix member, a person.
func asMatrixMember(e *env, tok string) string {
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	p.UserID = matrixMemberUser(e)
	return e.singleFactor(p)
}
