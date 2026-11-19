//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// C.1 #26, C.1 #27 and C.2 #41 (FR-PRJ-09, FR-PRJ-05, FR-PRJ-07): a project over its hours budget, a week left waiting past 7 days
// and closed weeks with no time or time not approved are each warned about, with the fix named; with nothing wrong each is fine,
// and they appear only while Projects and time are on.
// req: FR-CTL-01
// req: FR-PRJ-09
// req: FR-PRJ-05
// req: FR-PRJ-07
func TestTheProjectChecksWarnOfBudgetsWaitingWeeksAndClosedWeeks(t *testing.T) {
	e := newEnv(t)
	if l := e.call(http.MethodGet, "/v1/checks", e.as(authz.Controller), nil); strings.Contains(l.Raw, "projects.budget-and-margin") {
		t.Fatalf("the project checks show while Projects and time are off: %s", l.Raw)
	}
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Chk", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Chk", "employee")
	una := e.joinedIn(e.books.Org, "Una Chk", "controller")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "CHK-1", "name": "Checked", "currency": "SGD", "billing_method": "time_and_materials", "budget_minutes": 60,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	result := func(key string) response {
		e.runCatalogue()
		return e.call(http.MethodGet, "/v1/checks/"+key, e.as(authz.Controller), nil)
	}
	if r := result("projects.budget-and-margin"); r.str("status") != "ok" {
		t.Errorf("nothing over budget yet: %s", r.Raw)
	}
	tok := e.person(priya, authz.Employee)
	e.saveWeek(tok, "2026-10-05", map[string]any{"project_id": pid, "task_id": task, "work_date": "2026-10-06", "minutes": 180})
	_, tag := e.weekOf(tok, "2026-10-05")
	s := e.call(http.MethodPost, "/v1/timesheet/2026-10-05/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	if r := result("projects.timesheet-problems"); r.str("status") != "ok" {
		t.Errorf("a week just submitted is not late: %s", r.Raw)
	}
	e.clock.Advance(8 * 24 * time.Hour)
	if r := result("projects.timesheet-problems"); r.str("status") != "warning" || !strings.Contains(r.Raw, "waits for its manager") {
		t.Errorf("a week waiting 8 days: %s", r.Raw)
	}
	r := result("projects.closed-weeks")
	if r.str("status") != "warning" || !strings.Contains(r.Raw, "time not approved yet") || !strings.Contains(r.Raw, "no time entered") {
		t.Errorf("closed weeks: %s", r.Raw)
	}
	// Approved, the week is fine, and the project is over its 60-minute budget.
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if a := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); a.Status != 200 {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	if r := result("projects.timesheet-problems"); r.str("status") != "ok" {
		t.Errorf("after approval: %s", r.Raw)
	}
	if r := result("projects.budget-and-margin"); r.str("status") != "warning" || !strings.Contains(r.Raw, "180 of 60 budgeted minutes used") {
		t.Errorf("over the hours budget: %s", r.Raw)
	}
}

// C.2 #40 (FR-PRJ-10): approved time on a time-and-materials project that is on no posted invoice 30 days after the end of its month is
// listed with the project; invoicing it clears the check, and a month that ended less than 30 days ago is not late.
// req: FR-PRJ-10
// req: FR-CTL-01
func TestTheCheckForApprovedTimeNotInvoiced(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Inv", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Inv", "employee")
	una := e.joinedIn(e.books.Org, "Una Inv", "controller")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "INV-1", "name": "Invoiced late", "currency": "SGD", "billing_method": "time_and_materials", "customer_id": e.customer(),
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	e.call(http.MethodPost, "/v1/billing-rates", acc, map[string]any{"project_id": pid, "effective_from": "2026-01-01", "hourly_rate": "20"}, "Idempotency-Key", uuid.NewString())
	result := func() response {
		e.runCatalogue()
		return e.call(http.MethodGet, "/v1/checks/projects.time-not-invoiced", e.as(authz.Controller), nil)
	}
	if r := result(); r.str("status") != "ok" {
		t.Fatalf("nothing approved yet: %s", r.Raw)
	}
	tok := e.person(priya, authz.Employee)
	// 3 August 2026 week: its month ended 31 August, 30 days later is 30 September, before today (15 October 2026).
	if r := e.saveWeek(tok, "2026-08-03", map[string]any{"project_id": pid, "task_id": task, "work_date": "2026-08-04", "minutes": 120}); r.Status != 200 {
		t.Fatalf("saving the week: %d %s", r.Status, r.Raw)
	}
	_, tag := e.weekOf(tok, "2026-08-03")
	s := e.call(http.MethodPost, "/v1/timesheet/2026-08-03/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if a := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); a.Status != 200 {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	if r := result(); r.str("status") != "warning" || !strings.Contains(r.Raw, "1 approved entry not invoiced, the oldest from 2026-08-04") {
		t.Errorf("late time: %s", r.Raw)
	}
	d := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, map[string]any{"project_id": pid, "revenue_account_id": e.books.Sales.ID.String()}, "Idempotency-Key", uuid.NewString())
	if d.Status != 201 {
		t.Fatalf("drafting: %d %s", d.Status, d.Raw)
	}
	if r := result(); r.str("status") != "warning" {
		t.Errorf("a draft invoice bills nothing: %s", r.Raw)
	}
	if i := e.call(http.MethodPost, "/v1/invoices/"+d.str("id")+"/issue", acc, nil, "If-Match", d.Header.Get("ETag")); i.Status != 200 {
		t.Fatalf("issuing: %d %s", i.Status, i.Raw)
	}
	if r := result(); r.str("status") != "ok" {
		t.Errorf("invoiced time clears the check: %s", r.Raw)
	}
}
