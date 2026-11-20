//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// taskClient is an integration API client of e's organisation acting for the member user, holding scopes.
func (f founders) taskClient(client, user string, class string, scopes ...string) string {
	return f.e.principal(authn.Principal{Via: authn.ViaToken, Subject: client, Client: client, Class: class, UserID: uuid.MustParse(user), Organisation: f.e.books.Org,
		Roles: []authz.Role{authz.Administrator}, Scopes: scopes, Scoped: true})
}

// FR-INT-24 (rev 0.610): an integration client with fyniq.write.tasks makes tasks of its own and updates only those, finishing one once; a retried create finds the task;
// another client sees and changes nothing of it; a person who may view the row reads every client's.
// req: FR-INT-24
// asvs: V4.1.1, V4.2.1
func TestAnIntegrationClientMakesAndUpdatesItsOwnTasks(t *testing.T) {
	f := newFounders(t)
	e := f.e
	jira := f.taskClient("jira", f.ahkow, authn.ClassIntegration, "fyniq.write.tasks")
	linear := f.taskClient("linear", f.ahkow, authn.ClassIntegration, "fyniq.write.tasks")
	body := map[string]any{"external_ref": "OPS-142", "title": "Chase the signed engagement letter", "due_on": "2026-12-15", "link_url": "https://example.atlassian.net/browse/OPS-142"}

	made := e.call(http.MethodPost, "/v1/tasks", jira, body)
	if made.Status != http.StatusCreated || made.str("status") != "open" || made.str("client_id") != "jira" || made.str("due_on") != "2026-12-15" {
		t.Fatalf("making a task: %d %s", made.Status, made.Raw)
	}
	again := e.call(http.MethodPost, "/v1/tasks", jira, map[string]any{"external_ref": "OPS-142", "title": "A different title"})
	if again.Status != http.StatusOK || again.str("id") != made.str("id") || again.str("title") != "Chase the signed engagement letter" {
		t.Errorf("a retried create: %d %s", again.Status, again.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/tasks", jira, map[string]any{"external_ref": "OPS-143", "title": "x", "link_url": "http://insecure.example.com"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("an http link: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/tasks", jira, map[string]any{"external_ref": "OPS 144", "title": "x"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a reference with a space: %d %s", r.Status, r.Raw)
	}

	path := "/v1/tasks/" + made.str("id")
	if r := e.call(http.MethodGet, path, linear, nil); r.Status != http.StatusNotFound {
		t.Errorf("another client reads the task: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPatch, path, linear, map[string]any{"status": "done"}, "If-Match", `"1"`); r.Status != http.StatusNotFound {
		t.Errorf("another client updates the task: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/tasks", linear, nil); len(listOf(r)) != 0 {
		t.Errorf("another client lists the task: %s", r.Raw)
	}
	if r := e.call(http.MethodPatch, path, jira, map[string]any{"title": "x"}); r.Status != http.StatusPreconditionRequired {
		t.Errorf("an update without If-Match: %d %s", r.Status, r.Raw)
	}
	updated := e.call(http.MethodPatch, path, jira, map[string]any{"title": "Chase the letter again", "due_on": ""}, "If-Match", `"1"`)
	if updated.Status != http.StatusOK || updated.str("title") != "Chase the letter again" || updated.str("due_on") != "" {
		t.Fatalf("an update of its own task: %d %s", updated.Status, updated.Raw)
	}
	if r := e.call(http.MethodPatch, path, jira, map[string]any{"title": "stale"}, "If-Match", `"1"`); r.Status != http.StatusPreconditionFailed {
		t.Errorf("an update over a stale version: %d %s", r.Status, r.Raw)
	}
	done := e.call(http.MethodPatch, path, jira, map[string]any{"status": "done"}, "If-Match", `"2"`)
	if done.Status != http.StatusOK || done.str("status") != "done" || done.str("closed_at") == "" {
		t.Fatalf("finishing it: %d %s", done.Status, done.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, path, jira, map[string]any{"status": "cancelled"}, "If-Match", `"3"`), "updateIntegrationTask", http.StatusConflict, CodeIntegrationTaskFinished)

	// a person who may view the row reads every client's tasks; the Owner and the Administrator do, the Bookkeeper does not
	e.call(http.MethodPost, "/v1/tasks", linear, map[string]any{"external_ref": "L-1", "title": "From Linear"})
	if r := e.call(http.MethodGet, "/v1/tasks", e.as(authz.Controller), nil); r.Status != http.StatusOK || len(listOf(r)) != 2 {
		t.Errorf("a Controller lists every client's tasks: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/tasks", e.as(authz.Bookkeeper), nil); r.Status != http.StatusForbidden {
		t.Errorf("a Bookkeeper lists the tasks: %d", r.Status)
	}
}

// The attack tests of FR-INT-24 (rev 0.610): the narrow scope is the only way in, a compliance task is out of reach however it is asked, and no person in the browser, agent,
// the AI or the link's client makes or edits these tasks.
// req: FR-INT-24
// req: FR-INT-02
// asvs: V4.1.1, V8.2.1, V8.2.2
func TestNoOneButAScopedIntegrationClientTouchesAnIntegrationTask(t *testing.T) {
	f := newFounders(t)
	e := f.e
	body := map[string]any{"external_ref": "A-1", "title": "x"}
	for name, tok := range map[string]string{
		"a client without the scope":       f.taskClient("c1", f.ahkow, authn.ClassIntegration, "fyniq.write.sales"),
		"a read-only scope on another":     f.taskClient("c2", f.ahkow, authn.ClassIntegration, "fyniq.read.reports"),
		"an agent-class client":            f.taskClient("c3", f.ahkow, authn.ClassAgent, "fyniq.write.tasks"),
		"an Owner in the browser":          f.as(f.ahkow),
		"the Administrator role":           e.as(authz.Administrator),
		"the AI":                           e.principal(authn.Principal{Client: "assistant", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}}),
		"the link's client":                e.principal(authn.Principal{Via: authn.ViaToken, Subject: "nemura", Client: "nemura", Class: authn.ClassIntegration, Organisation: e.books.Org, Scoped: true, Link: true, Scopes: []string{"fyniq.write.tasks"}}),
		"a client acting for no member":    e.principal(authn.Principal{Via: authn.ViaToken, Subject: "orphan", Client: "orphan", Class: authn.ClassIntegration, Organisation: e.books.Org, Roles: []authz.Role{authz.Administrator}, Scoped: true, Scopes: []string{"fyniq.write.tasks"}}),
		"a client acting for a Bookkeeper": e.principal(authn.Principal{Via: authn.ViaToken, Subject: "bk", Client: "bk", Class: authn.ClassIntegration, UserID: uuid.MustParse(f.ahkow), Organisation: e.books.Org, Roles: []authz.Role{authz.Bookkeeper}, Scoped: true, Scopes: []string{"fyniq.write.tasks"}}),
	} {
		if r := e.call(http.MethodPost, "/v1/tasks", tok, body); r.Status != http.StatusForbidden {
			t.Errorf("%s made a task: %d %s", name, r.Status, r.Raw)
		}
	}

	// a compliance task is not reachable through the integration operations, with the right scope: not read, not updated, not closed, not dismissed
	jira := f.taskClient("jira", f.ahkow, authn.ClassIntegration, "fyniq.write.tasks")
	task := raiseTasks(e, f.ahkow, "calendar:gst_f5:2027-01-31")[0]
	id := task.ID.String()
	e.problemIs(e.call(http.MethodGet, "/v1/tasks/"+id, jira, nil), "getIntegrationTask", http.StatusNotFound, CodeNotFound)
	e.problemIs(e.call(http.MethodPatch, "/v1/tasks/"+id, jira, map[string]any{"status": "done", "title": "Done"}, "If-Match", `"1"`), "updateIntegrationTask", http.StatusNotFound, CodeNotFound)
	for _, op := range []string{"done", "dismiss"} {
		if r := e.call(http.MethodPost, "/v1/compliance/tasks/"+id+"/"+op, jira, map[string]any{"reason": "x"}, "If-Match", `"1"`); r.Status != http.StatusForbidden {
			t.Errorf("an integration client %ss a compliance task: %d %s", op, r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodGet, "/v1/compliance/tasks", jira, nil); r.Status != http.StatusForbidden {
		t.Errorf("an integration client lists the compliance tasks: %d %s", r.Status, r.Raw)
	}
	got := e.call(http.MethodGet, "/v1/compliance/tasks/"+id, e.as(authz.Owner), nil)
	if got.str("status") != "open" || got.str("title") != "File the GST return" {
		t.Errorf("the compliance task changed: %s", got.Raw)
	}
	// and the scope reaches no financial record
	for _, p := range []string{"/v1/accounts", "/v1/journals", "/v1/invoices", "/v1/contacts"} {
		if r := e.call(http.MethodGet, p, jira, nil); r.Status != http.StatusForbidden {
			t.Errorf("the tasks scope reads %s: %d", p, r.Status)
		}
	}
	if r := e.call(http.MethodPost, "/v1/journals", jira, map[string]any{}); r.Status != http.StatusForbidden {
		t.Errorf("the tasks scope makes a journal: %d", r.Status)
	}
}

// listOf is a page's items.
func listOf(r response) []any { items, _ := r.Body["items"].([]any); return items }

func init() {
	// The matrix calls as a person in the browser, and only an integration client makes or updates these tasks: a role the policy allows is answered past it, by that rule.
	pastPolicy["createIntegrationTask"] = CodeIntegrationTaskClient
	pastPolicy["updateIntegrationTask"] = CodeIntegrationTaskClient
	pastPolicy["getIntegrationTask"] = CodeNotFound // the matrix has no task of an integration
	pastPolicy["closeIntegrationTask"] = CodeNotFound
}

var matrixIntegrationTasks = map[string]func(e *env, tok string) (response, func() bool){
	"listIntegrationTasks": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tasks", tok, nil), nil
	},
	"getIntegrationTask": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tasks/"+uuid.NewString(), tok, nil), nil
	},
	"createIntegrationTask": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tasks", tok, map[string]any{"external_ref": "M-" + uuid.NewString(), "title": "Matrix"}), nil
	},
	"updateIntegrationTask": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPatch, "/v1/tasks/"+uuid.NewString(), tok, map[string]any{"title": "Matrix"}, "If-Match", `"1"`), nil
	},
	"closeIntegrationTask": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tasks/"+uuid.NewString()+"/close", tok, map[string]any{"status": "done"}, "If-Match", `"1"`), nil
	},
}

// FR-INT-24: a person who holds the right on the row, an Owner or an Administrator, finishes a task an integration made from the Inbox, over the version they read, and
// the audit trail names them; the Controller who only views is refused, and so is every client, token and agent, including the one that made it; a finished task stays finished.
// req: FR-INT-24
// asvs: V4.1.1, V4.2.1, V8.2.1
func TestAPersonFinishesATaskAnIntegrationMade(t *testing.T) {
	f := newFounders(t)
	e := f.e
	jira := f.taskClient("jira", f.ahkow, authn.ClassIntegration, "fyniq.write.tasks")
	made := e.call(http.MethodPost, "/v1/tasks", jira, map[string]any{"external_ref": "OPS-9", "title": "Chase the engagement letter"})
	path := "/v1/tasks/" + made.str("id") + "/close"
	body := map[string]any{"status": "done"}
	if r := e.call(http.MethodPost, path, e.as(authz.Controller), body, "If-Match", `"1"`); r.Status != http.StatusForbidden {
		t.Errorf("a Controller, who only views, finishes it: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, path, e.as(authz.Bookkeeper), body, "If-Match", `"1"`); r.Status != http.StatusForbidden {
		t.Errorf("a Bookkeeper finishes it: %d %s", r.Status, r.Raw)
	}
	for name, who := range map[string]string{"the client that made it": jira, "another client": f.taskClient("linear", f.ahkow, authn.ClassIntegration, "fyniq.write.tasks"),
		"an agent client": f.taskClient("agent", f.ahkow, authn.ClassAgent, "fyniq.write.tasks")} {
		if r := e.call(http.MethodPost, path, who, body, "If-Match", `"1"`); r.Status != http.StatusForbidden && r.Status != http.StatusUnauthorized {
			t.Errorf("%s finishes it through the person's operation: %d %s", name, r.Status, r.Raw)
		}
	}
	e.problemIs(e.call(http.MethodPost, path, e.as(authz.Administrator), body), "closeIntegrationTask", http.StatusPreconditionRequired, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, path, e.as(authz.Administrator), body, "If-Match", `"7"`), "closeIntegrationTask", http.StatusPreconditionFailed, CodeStale)
	e.problemIs(e.call(http.MethodPost, "/v1/tasks/"+uuid.NewString()+"/close", e.as(authz.Administrator), body, "If-Match", `"1"`), "closeIntegrationTask", http.StatusNotFound, CodeNotFound)
	if r := e.call(http.MethodPost, path, e.as(authz.Administrator), map[string]any{"status": "open"}, "If-Match", `"1"`); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("reopening it: %d %s", r.Status, r.Raw)
	}
	done := e.call(http.MethodPost, path, e.as(authz.Administrator), body, "If-Match", `"1"`)
	if done.Status != http.StatusOK || done.str("status") != "done" || done.str("closed_at") == "" || done.str("client_id") != "jira" || done.str("title") != "Chase the engagement letter" {
		t.Fatalf("an Administrator finishes it: %d %s", done.Status, done.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path, e.as(authz.Owner), body, "If-Match", `"2"`), "closeIntegrationTask", http.StatusConflict, CodeIntegrationTaskFinished)
	e.problemIs(e.call(http.MethodPatch, "/v1/tasks/"+made.str("id"), jira, map[string]any{"status": "cancelled"}, "If-Match", `"2"`), "updateIntegrationTask", http.StatusConflict, CodeIntegrationTaskFinished)
}
