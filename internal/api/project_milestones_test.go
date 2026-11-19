//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-PRJ-10: a fixed-fee project bills by milestone: an accepted milestone is invoiced once, an unaccepted one is refused, posting marks it billed
// and voiding releases it, an amount and name never change, and a project of another billing method has none.
// req: FR-PRJ-10
func TestAFixedFeeProjectBillsByAcceptedMilestone(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Mile", "approver")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "FIX-1", "name": "Fixed", "currency": "SGD", "billing_method": "fixed_fee", "customer_id": e.customer(),
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	mk := func(name, amount string) response {
		return e.call(http.MethodPost, "/v1/project-milestones", acc, map[string]any{"project_id": pid, "name": name, "amount": amount, "due_on": "2026-12-15"}, "Idempotency-Key", uuid.NewString())
	}
	m := mk("Design sign-off", "1500.00")
	if m.Status != 201 || m.str("kind") != "milestone" || m.str("amount", "amount") != "1500.00" || m.Body["can_invoice"] != false {
		t.Fatalf("the milestone: %d %s", m.Status, m.Raw)
	}
	if r := mk("Zero", "0"); r.Status != 422 {
		t.Errorf("a zero amount: %d %s", r.Status, r.Raw)
	}
	draft := func() response {
		return e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, map[string]any{"project_id": pid, "revenue_account_id": e.books.Sales.ID.String(), "milestone_id": m.str("id")},
			"Idempotency-Key", uuid.NewString())
	}
	if r := draft(); r.Status != 409 || !strings.Contains(r.Raw, "PRJ-15") {
		t.Errorf("an unaccepted milestone: %d %s", r.Status, r.Raw)
	}
	// The customer accepts it (not in the future), once.
	acceptURL := "/v1/project-milestones/" + m.str("id") + "/accept"
	if r := e.call(http.MethodPost, acceptURL, acc, map[string]any{"accepted_on": "2090-01-01"}, "If-Match", m.Header.Get("ETag"), "Idempotency-Key", uuid.NewString()); r.Status != 422 {
		t.Errorf("accepted in the future: %d %s", r.Status, r.Raw)
	}
	a := e.call(http.MethodPost, acceptURL, acc, map[string]any{"accepted_on": "2026-10-14"}, "If-Match", m.Header.Get("ETag"), "Idempotency-Key", uuid.NewString())
	if a.Status != 200 || a.str("accepted_on") != "2026-10-14" || a.Body["can_invoice"] != true {
		t.Fatalf("accepting: %d %s", a.Status, a.Raw)
	}
	if r := e.call(http.MethodPost, acceptURL, acc, map[string]any{"accepted_on": "2026-10-14"}, "If-Match", a.Header.Get("ETag"), "Idempotency-Key", uuid.NewString()); r.Status != 409 {
		t.Errorf("accepted twice: %d %s", r.Status, r.Raw)
	}
	d := draft()
	if d.Status != 201 || len(d.Body["lines"].([]any)) != 1 {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	if i := e.call(http.MethodPost, "/v1/invoices/"+d.str("id")+"/issue", acc, nil, "If-Match", d.Header.Get("ETag")); i.Status != 200 {
		t.Fatalf("issuing: %d %s", i.Status, i.Raw)
	}
	list := func() response { return e.call(http.MethodGet, "/v1/projects/"+pid+"/milestones", acc, nil) }
	if l := list(); l.Body["milestones"].([]any)[0].(map[string]any)["invoice_id"] != d.str("id") || l.Body["retainer"] != nil {
		t.Errorf("billed: %s", l.Raw)
	}
	if r := draft(); r.Status != 409 || !strings.Contains(r.Raw, "PRJ-15") {
		t.Errorf("billed twice: %d %s", r.Status, r.Raw)
	}
	vctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture-approver"}})
	if _, err := (journal.Invoices{Store: e.store}).Void(vctx, e.books.Org, uuid.MustParse(d.str("id")), time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), "Billed in error"); err != nil {
		t.Fatalf("voiding: %v", err)
	}
	if l := list(); l.Body["milestones"].([]any)[0].(map[string]any)["invoice_id"] != nil {
		t.Errorf("a voided invoice releases the milestone: %s", l.Raw)
	}
	// A project of another billing method has no milestones.
	tm := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "TM-9", "name": "Hourly", "currency": "SGD", "billing_method": "time_and_materials",
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}}})
	if r := e.call(http.MethodPost, "/v1/project-milestones", acc, map[string]any{"project_id": tm.str("id"), "name": "x", "amount": "10"}, "Idempotency-Key", uuid.NewString()); r.Status != 409 {
		t.Errorf("a milestone on a time-and-materials project: %d %s", r.Status, r.Raw)
	}
}

// FR-PRJ-10: a retainer bills a period without acceptance, and its approved billable time is drawn down at billing rates: the balance is what
// was billed less what was drawn.
// req: FR-PRJ-10
func TestARetainerIsBilledByPeriodAndDrawnDownByTime(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Ret", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Ret", "employee")
	una := e.joinedIn(e.books.Org, "Una Ret", "controller")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "RET-1", "name": "Retainer", "currency": "SGD", "billing_method": "retainer", "customer_id": e.customer(),
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	e.call(http.MethodPost, "/v1/billing-rates", acc, map[string]any{"project_id": pid, "effective_from": "2026-01-01", "hourly_rate": "100"}, "Idempotency-Key", uuid.NewString())
	m := e.call(http.MethodPost, "/v1/project-milestones", acc, map[string]any{"project_id": pid, "name": "November", "amount": "1000.00"}, "Idempotency-Key", uuid.NewString())
	if m.Status != 201 || m.str("kind") != "retainer" || m.Body["can_invoice"] != true {
		t.Fatalf("the retainer period: %d %s", m.Status, m.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/project-milestones/"+m.str("id")+"/accept", acc, map[string]any{"accepted_on": "2026-10-14"}, "If-Match", m.Header.Get("ETag"), "Idempotency-Key", uuid.NewString()); r.Status != 409 {
		t.Errorf("accepting a retainer period: %d %s", r.Status, r.Raw)
	}
	// 3 hours of approved billable time draw 300.00 down; the retainer's time is never invoiced as time.
	tok := e.person(priya, authz.Employee)
	e.saveWeek(tok, "2090-01-02", map[string]any{"project_id": pid, "task_id": task, "work_date": "2090-01-03", "minutes": 180})
	_, tag := e.weekOf(tok, "2090-01-02")
	s := e.call(http.MethodPost, "/v1/timesheet/2090-01-02/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	l := e.call(http.MethodGet, "/v1/projects/"+pid+"/milestones", acc, nil)
	if r := l.Body["retainer"].(map[string]any); r["billed"].(map[string]any)["amount"] != "0.00" || r["drawn"].(map[string]any)["amount"] != "300.00" || r["left"].(map[string]any)["amount"] != "-300.00" {
		t.Fatalf("before billing: %s", l.Raw)
	}
	d := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, map[string]any{"project_id": pid, "revenue_account_id": e.books.Sales.ID.String(), "milestone_id": m.str("id")}, "Idempotency-Key", uuid.NewString())
	if d.Status != 201 {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	if i := e.call(http.MethodPost, "/v1/invoices/"+d.str("id")+"/issue", acc, nil, "If-Match", d.Header.Get("ETag")); i.Status != 200 {
		t.Fatalf("issuing: %d %s", i.Status, i.Raw)
	}
	l = e.call(http.MethodGet, "/v1/projects/"+pid+"/milestones", acc, nil)
	if r := l.Body["retainer"].(map[string]any); r["billed"].(map[string]any)["amount"] != "1000.00" || r["left"].(map[string]any)["amount"] != "700.00" {
		t.Errorf("after billing: %s", l.Raw)
	}
	u := e.call(http.MethodGet, "/v1/projects/"+pid+"/unbilled", acc, nil)
	if len(u.Body["time"].([]any)) != 0 {
		t.Errorf("a retainer's time is not billed as time: %s", u.Raw)
	}
}
