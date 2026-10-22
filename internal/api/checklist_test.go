//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The close checklist (the founders' decisions of 22 Oct 2026): every month has the template's tasks, due days after the
// month's end; a task is done, or skipped for the month with a reason, and the close checks refuse the close while one
// is neither. The Accountant or the Controller marks tasks and changes the template, as a new version that keeps the
// marks of the tasks it keeps.
// req: FR-GL-07
// req: FM-4
// req: C.2 #58
func TestTheCloseChecklist(t *testing.T) {
	e := newEnv(t)
	accountant, controller := e.as(authz.Accountant), e.as(authz.Controller)
	oct := e.books.Oct.ID.String()
	mark := func(tok, key string, body map[string]any, headers ...string) response {
		return e.call(http.MethodPost, "/v1/periods/"+oct+"/checklist/"+key, tok, body, headers...)
	}
	checks := func() response {
		t.Helper()
		s := e.call(http.MethodGet, "/v1/periods/"+oct+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+oct+"/close-checks", controller, nil, "If-Match", s.Header.Get("ETag"))
		if r.Status != 200 {
			t.Fatalf("runCloseChecks: %d %s", r.Status, r.Raw)
		}
		return r
	}
	checklist := func(r response) map[string]any {
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == "checklist" {
				return c.(map[string]any)
			}
		}
		return nil
	}
	task := func(r response, key string) map[string]any {
		for _, x := range r.Body["tasks"].([]any) {
			if x.(map[string]any)["key"] == key {
				return x.(map[string]any)
			}
		}
		return nil
	}

	// The recommended template is in force until the organisation saves its own.
	tpl := e.call(http.MethodGet, "/v1/close-checklist-template", accountant, nil)
	if tpl.Status != 200 || tpl.Header.Get("ETag") != `"0"` || tpl.Body["recommended"] != true || len(tpl.Body["tasks"].([]any)) != len(closing.Recommended) {
		t.Fatalf("the template: %d %s", tpl.Status, tpl.Raw)
	}
	r := e.call(http.MethodGet, "/v1/periods/"+oct+"/checklist", controller, nil)
	if r.Status != 200 || r.Body["open"] != float64(6) || r.Body["template_version"] != float64(0) || task(r, "bank")["due_on"] != "2026-11-07" ||
		task(r, "bank")["state"] != "open" {
		t.Fatalf("October's checklist: %d %s", r.Status, r.Raw)
	}
	if c := checklist(checks()); c["severity"] != "error" || !strings.HasPrefix(c["detail"].(string), "6 of 6 checklist task(s)") {
		t.Fatalf("the checks with the checklist open: %v", c)
	}

	// Marked by the members who run the checks: done with a note, or skipped with a reason; never by the Owner or the AI.
	e.problemIs(mark(accountant, "payroll", map[string]any{"state": "skipped"}), "markChecklistTask", 422, closing.CodeSkipReason)
	e.problemIs(mark(accountant, "stock-count", map[string]any{"state": "done"}), "markChecklistTask", 409, closing.CodeNoTask)
	e.problemIs(mark(e.as(authz.Owner), "bank", map[string]any{"state": "done"}), "markChecklistTask", 403, CodeForbidden)
	pv := mark(accountant, "bank", map[string]any{"state": "done"}, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" ||
		e.call(http.MethodGet, "/v1/periods/"+oct+"/checklist", controller, nil).Body["open"] != float64(6) {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	r = mark(accountant, "bank", map[string]any{"state": "done", "note": "4 statements agreed"}, "Idempotency-Key", "bank-done")
	if r.Status != 200 || task(r, "bank")["state"] != "done" || task(r, "bank")["note"] != "4 statements agreed" || task(r, "bank")["by"] == nil ||
		r.Body["done"] != float64(1) || r.Body["open"] != float64(5) {
		t.Fatalf("marking bank done: %d %s", r.Status, r.Raw)
	}
	if again := mark(accountant, "bank", map[string]any{"state": "done", "note": "4 statements agreed"}, "Idempotency-Key", "bank-done"); again.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("the repeat: %s", again.Raw)
	}
	if r = mark(controller, "payroll", map[string]any{"state": "skipped", "note": "No employees yet"}); r.Status != 200 || task(r, "payroll")["state"] != "skipped" {
		t.Fatalf("skipping payroll: %d %s", r.Status, r.Raw)
	}
	for _, key := range []string{"documents", "accruals", "fx", "review"} {
		if r = mark(controller, key, map[string]any{"state": "done"}); r.Status != 200 {
			t.Fatalf("marking %s: %d %s", key, r.Status, r.Raw)
		}
	}
	if c := checklist(checks()); c["severity"] != "ok" || c["detail"] != "5 done, 1 skipped, on version 0 of the template" {
		t.Fatalf("the checks with the checklist done: %v", c)
	}
	// Opened again, the task counts as open.
	if r = mark(accountant, "review", map[string]any{"state": "open", "note": "A late bill arrived"}); r.Body["open"] != float64(1) {
		t.Errorf("opening review again: %s", r.Raw)
	}

	// The template: a new version at the version read, every problem listed; the tasks it keeps keep their marks.
	put := func(tok, tag string, tasks ...map[string]any) response {
		return e.call(http.MethodPut, "/v1/close-checklist-template", tok, map[string]any{"tasks": tasks}, "If-Match", tag)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/close-checklist-template", controller, map[string]any{"tasks": []any{map[string]any{"title": "x"}}}),
		"replaceCloseChecklistTemplate", 428, CodeIfMatchRequired)
	e.problemIs(put(controller, `"7"`, map[string]any{"title": "x"}), "replaceCloseChecklistTemplate", 412, CodeStale)
	bad := put(controller, `"0"`, map[string]any{"key": "bank", "title": "Bank"}, map[string]any{"key": "bank", "title": "Bank again"})
	e.problemIs(bad, "replaceCloseChecklistTemplate", 422, CodeInvalid)
	if !strings.Contains(bad.Raw, `"field":"body.tasks[1].key"`) || !strings.Contains(bad.Raw, closing.CodeTemplate) {
		t.Errorf("a key used twice: %s", bad.Raw)
	}
	e.problemIs(put(e.as(authz.Owner), `"0"`, map[string]any{"title": "x"}), "replaceCloseChecklistTemplate", 403, CodeForbidden)
	saved := put(accountant, `"0"`, map[string]any{"key": "bank", "title": "Bank accounts reconciled", "owner_role": "accountant", "due_days": 5},
		map[string]any{"key": "review", "title": "The month reviewed", "owner_role": "controller", "due_days": 12},
		map[string]any{"title": "Board pack sent", "due_days": 15})
	if saved.Status != 200 || saved.Header.Get("ETag") != `"1"` || saved.Body["recommended"] != false || saved.str("written_by", "kind") != "user" ||
		saved.Body["tasks"].([]any)[2].(map[string]any)["key"] != "board-pack-sent" {
		t.Fatalf("saving the template: %d %s", saved.Status, saved.Raw)
	}
	r = e.call(http.MethodGet, "/v1/periods/"+oct+"/checklist", controller, nil)
	if r.Body["template_version"] != float64(1) || len(r.Body["tasks"].([]any)) != 3 || task(r, "bank")["state"] != "done" ||
		task(r, "bank")["due_on"] != "2026-11-05" || task(r, "board-pack-sent")["state"] != "open" || r.Body["open"] != float64(2) {
		t.Fatalf("October's checklist on the new template: %s", r.Raw)
	}

	// A closed month's checklist does not change.
	checklistDone(t, e.store, e.books.Org, e.books.Oct.ID)
	ctx := context.Background()
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	c, err := e.api.closing.RunChecks(as("preparer"), e.books.Org, e.books.Oct.ID)
	if err == nil {
		_, _, err = e.api.closing.Close(as("attester"), e.books.Org, e.books.Oct.ID, c.ID, "Reviewed.")
	}
	if err != nil {
		t.Fatal(err)
	}
	e.problemIs(mark(accountant, "bank", map[string]any{"state": "open"}), "markChecklistTask", 409, closing.CodeNotOpen)

	trail := auditTrail(t, e)
	for _, want := range []string{`"action":"close_checklist.task_marked"`, `"action":"close_checklist.template_saved"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
}
