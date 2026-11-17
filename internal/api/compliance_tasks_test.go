//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixComplianceTasks are the authorisation matrix's rows for the compliance tasks: each on a task of its own.
var matrixComplianceTasks = map[string]func(e *env, tok string) (response, func() bool){
	"listComplianceTasks": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/tasks", tok, nil), nil
	},
	"getComplianceTask": func(e *env, tok string) (response, func() bool) {
		task := raiseTasks(e, matrixApprover(e), "calendar:matrix:"+uuid.NewString())[0]
		return e.call(http.MethodGet, "/v1/compliance/tasks/"+task.ID.String(), tok, nil), nil
	},
	"completeComplianceTask": func(e *env, tok string) (response, func() bool) {
		task := raiseTasks(e, matrixApprover(e), "calendar:matrix:"+uuid.NewString())[0]
		return e.call(http.MethodPost, "/v1/compliance/tasks/"+task.ID.String()+"/done", steppedUp(e, tok), nil, "If-Match", `"1"`), nil
	},
	"dismissComplianceTask": func(e *env, tok string) (response, func() bool) {
		task := raiseTasks(e, matrixApprover(e), "calendar:matrix:"+uuid.NewString())[0]
		return e.call(http.MethodPost, "/v1/compliance/tasks/"+task.ID.String()+"/dismiss", steppedUp(e, tok), map[string]any{"reason": "Not needed"},
			"If-Match", `"1"`), nil
	},
}

// raiseTasks raises a run's tasks for keys in e's organisation, as the worker does, acting for user.
func raiseTasks(e *env, user string, keys ...string) []storage.ComplianceTask {
	e.t.Helper()
	var out []storage.ComplianceTask
	now := e.clock.Now()
	due := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "agent", ID: "company-secretary"}})
	if err := workerStore(e.t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		run, _, err := u.AgentRuns().Queue(ctx, storage.NewAgentRun{Agent: "company-secretary", SkillVersion: 1, SkillSHA256: strings.Repeat("ab", 32),
			Trigger: "event:test:" + uuid.NewString(), SlotAt: now, ActsFor: uuid.MustParse(user), QueuedAt: now})
		if err == nil {
			run, err = u.AgentRuns().Start(ctx, run.ID, run.Version, now)
		}
		for _, k := range keys {
			if err != nil {
				return err
			}
			var t storage.ComplianceTask
			t, err = u.ComplianceTasks().RaiseForRun(ctx, run.ID, storage.NewComplianceTask{FindingKey: k, Title: "File the GST return",
				OwnerRole: "owner", DueOn: &due, CreatedAt: now})
			out = append(out, t)
		}
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// The compliance tasks as people close them (AI-65; design D7): everyone with V on the AI company secretary's row sees
// them, the Owner and the Administrator mark one done or dismiss it with a reason over the version they read, a
// closed task stays closed, and the AI reaches none.
// req: AI-65
// asvs: V8.2.1, V8.2.2
func TestPeopleCloseTheirComplianceTasks(t *testing.T) {
	e := newEnv(t)
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Books", "roles": []string{"accountant"}}).str("code")
	f := foundersIn(e)
	accountantID := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "books-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Books"}).str("user_id")
	accountant := e.principal(authn.Principal{Subject: "books", UserID: uuid.MustParse(accountantID), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant}})
	ahkow := f.as(f.ahkow)
	tasks := raiseTasks(e, f.ahkow, "calendar:gst_f5:2027-01-31", "calendar:annual_return:2027-07-31")

	list := e.call(http.MethodGet, "/v1/compliance/tasks?status=open", accountant, nil)
	if items, _ := list.Body["items"].([]any); list.Status != http.StatusOK || len(items) != 2 {
		t.Fatalf("the open tasks as the Accountant: %d %s", list.Status, list.Raw)
	}
	got := e.call(http.MethodGet, "/v1/compliance/tasks/"+tasks[0].ID.String(), ahkow, nil)
	if got.Status != http.StatusOK || got.str("due_on") != "2027-01-31" || got.str("source") != "agent_run" || got.Header.Get("ETag") != `"1"` {
		t.Fatalf("a task: %d %s", got.Status, got.Raw)
	}
	done := "/v1/compliance/tasks/" + tasks[0].ID.String() + "/done"
	if r := e.call(http.MethodPost, done, accountant, nil, "If-Match", `"1"`); r.Status != http.StatusForbidden {
		t.Errorf("the Accountant marking a task done: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, done, ahkow, nil), "completeComplianceTask", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, done, ahkow, nil, "If-Match", `"7"`), "completeComplianceTask", 412, CodeStale)
	if pv := e.call(http.MethodPost, done, ahkow, nil, "If-Match", `"1"`, "Prefer", "return=preview"); pv.Status != http.StatusOK ||
		pv.Header.Get("Preference-Applied") != "return=preview" || pv.str("status") != storage.TaskDone {
		t.Fatalf("previewing marking a task done: %d %s", pv.Status, pv.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/compliance/tasks/"+tasks[0].ID.String(), ahkow, nil); r.str("status") != storage.TaskOpen {
		t.Fatalf("a preview saved the task: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, done, ahkow, nil, "If-Match", `"1"`); r.Status != http.StatusOK || r.str("status") != storage.TaskDone ||
		r.str("closed_by") != f.ahkow {
		t.Fatalf("marking a task done: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, done, ahkow, nil, "If-Match", `"2"`), "completeComplianceTask", 409, storage.CodeComplianceTask)

	dismiss := "/v1/compliance/tasks/" + tasks[1].ID.String() + "/dismiss"
	e.problemIs(e.call(http.MethodPost, dismiss, ahkow, map[string]any{"reason": "   "}, "If-Match", `"1"`), "dismissComplianceTask", 422, CodeTaskReason)
	if r := e.call(http.MethodPost, dismiss, ahkow, map[string]any{"reason": "Filed by the secretarial firm"}, "If-Match", `"1"`); r.Status != http.StatusOK ||
		r.str("status") != storage.TaskDismissed || r.str("reason") != "Filed by the secretarial firm" {
		t.Fatalf("dismissing a task: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/compliance/tasks/"+uuid.NewString(), ahkow, nil); r.Status != http.StatusNotFound {
		t.Errorf("a task that is none: %d", r.Status)
	}
	// The AI reaches none of them.
	aiSwitchedOn(e)
	if r := e.call(http.MethodGet, "/v1/compliance/tasks", proposeTurn(e, f.ahkow), nil); r.Status < 400 {
		t.Errorf("the AI listing the tasks: %d %s", r.Status, r.Raw)
	}
}
