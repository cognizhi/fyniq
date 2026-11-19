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

// UC-PRJ-02, FR-PRJ-10: a time-and-materials project bills its approved hours at billing rates (RND-TIME per entry) and its billable costs at
// cost plus markup (RND-MARKUP) through an ordinary draft invoice; posting the invoice marks the work billed, so it is not billed twice;
// voiding the invoice releases it; an internal project is never billed; time without a billing rate is listed apart.
// req: FR-PRJ-10
// req: RND-MARKUP
// req: UC-PRJ-02
func TestAProjectBillsItsUnbilledWorkOnceThroughADraftInvoice(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Bill", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Bill", "employee")
	una := e.joinedIn(e.books.Org, "Una Bill", "controller")
	customer := e.customer()
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "BILL-1", "name": "Rollout", "currency": "SGD", "billing_method": "time_and_materials", "customer_id": customer,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")

	// 62.5 approved billable hours in a week: five days of 12 h 30 min.
	tok := e.person(priya, authz.Employee)
	var entries []map[string]any
	for _, d := range []string{"2090-01-02", "2090-01-03", "2090-01-04", "2090-01-05", "2090-01-06"} {
		entries = append(entries, map[string]any{"project_id": pid, "task_id": task, "work_date": d, "minutes": 750})
	}
	if r := e.saveWeek(tok, "2090-01-02", entries...); r.Status != 200 {
		t.Fatalf("saving the week: %d %s", r.Status, r.Raw)
	}
	_, tag := e.weekOf(tok, "2090-01-02")
	s := e.call(http.MethodPost, "/v1/timesheet/2090-01-02/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	unbilled := func() response { return e.call(http.MethodGet, "/v1/projects/"+pid+"/unbilled", acc, nil) }
	// With no billing rate the time is listed apart, and nothing can be invoiced.
	u := unbilled()
	if u.Status != 200 || len(u.Body["unpriced_time"].([]any)) != 5 || len(u.Body["time"].([]any)) != 0 || u.str("total", "amount") != "0.00" {
		t.Fatalf("without a rate: %d %s", u.Status, u.Raw)
	}
	body := map[string]any{"project_id": pid, "revenue_account_id": e.books.Sales.ID.String()}
	if r := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, body, "Idempotency-Key", uuid.NewString()); r.Status != 409 || !strings.Contains(r.Raw, "PRJ-13") {
		t.Errorf("nothing to bill: %d %s", r.Status, r.Raw)
	}
	e.call(http.MethodPost, "/v1/billing-rates", acc, map[string]any{"project_id": pid, "effective_from": "2026-01-01", "hourly_rate": "20"}, "Idempotency-Key", uuid.NewString())

	// A billable cost: a posted bill line of S$420.00 at 10% markup.
	bd := e.billDraft("420.00", "0.00")
	bl := bd["lines"].([]map[string]any)[0]
	bl["billable"], bl["markup_percent"], bl["dimension_value_ids"] = true, "10", []string{pid}
	bc := e.call(http.MethodPost, "/v1/bills", acc, bd)
	if bc.Status != 201 {
		t.Fatalf("the bill: %d %s", bc.Status, bc.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/bills/"+bc.str("id")+"/post", acc, nil, "If-Match", bc.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("posting the bill: %d %s", r.Status, r.Raw)
	}

	u = unbilled()
	if u.str("time_value", "amount") != "1250.00" || u.str("expense_value", "amount") != "462.00" || u.str("total", "amount") != "1712.00" ||
		len(u.Body["time"].([]any)) != 5 || len(u.Body["expenses"].([]any)) != 1 {
		t.Fatalf("unbilled: %s", u.Raw)
	}
	if x := u.Body["expenses"].([]any)[0].(map[string]any); x["markup"].(map[string]any)["amount"] != "42.00" || x["source"] != "bill" {
		t.Errorf("the cost: %v", x)
	}

	// The draft invoice: one line for the person's time, one for the cost, each carrying the project; it is billed only when posted.
	d := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, body, "Idempotency-Key", uuid.NewString())
	if d.Status != 201 || len(d.Body["lines"].([]any)) != 2 || d.str("total", "amount") != "1712.00" && d.str("total", "amount") != "1866.08" {
		t.Fatalf("the draft invoice: %d %s", d.Status, d.Raw)
	}
	if u := unbilled(); u.str("total", "amount") != "1712.00" {
		t.Errorf("a draft bills nothing yet: %s", u.Raw)
	}
	iss := e.call(http.MethodPost, "/v1/invoices/"+d.str("id")+"/issue", acc, nil, "If-Match", d.Header.Get("ETag"))
	if iss.Status != 200 {
		t.Fatalf("issuing: %d %s", iss.Status, iss.Raw)
	}
	if u := unbilled(); u.str("total", "amount") != "0.00" || len(u.Body["time"].([]any)) != 0 || len(u.Body["expenses"].([]any)) != 0 {
		t.Errorf("after posting, the work is billed: %s", u.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, body, "Idempotency-Key", uuid.NewString()); r.Status != 409 {
		t.Errorf("nothing is left to bill: %d %s", r.Status, r.Raw)
	}
	// Voiding the invoice releases the work.
	// In Founder mode a void is asked for and approved by a second person; the posting service does what that approval does.
	vctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture-approver"}})
	if _, err := (journal.Invoices{Store: e.store}).Void(vctx, e.books.Org, uuid.MustParse(d.str("id")), time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), "Billed in error"); err != nil {
		t.Fatalf("voiding: %v", err)
	}
	if u := unbilled(); u.str("total", "amount") != "1712.00" {
		t.Errorf("a voided invoice releases the work: %s", u.Raw)
	}

	// An internal project is never billed; the AI-facing readers see it as nothing to bill.
	in := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "INT-1", "name": "Internal", "currency": "SGD", "billing_method": "internal",
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}}})
	if in.Status != 201 {
		t.Fatalf("the internal project: %d %s", in.Status, in.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, map[string]any{"project_id": in.str("id"), "revenue_account_id": e.books.Sales.ID.String()}, "Idempotency-Key", uuid.NewString()); r.Status != 409 || !strings.Contains(r.Raw, "PRJ-12") {
		t.Errorf("an internal project: %d %s", r.Status, r.Raw)
	}
	// A manager reads the unbilled view of their own project but may not draft its invoice (no right to create invoices).
	m := e.call(http.MethodGet, "/v1/projects/"+pid+"/unbilled", e.person(wei, authz.Approver), nil)
	if m.Status != 200 {
		t.Errorf("the manager's unbilled view: %d %s", m.Status, m.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/project-invoice-drafts", e.person(wei, authz.Approver), body, "Idempotency-Key", uuid.NewString()), "draftProjectInvoice", 403, CodeForbidden)
}

// Two drafts may name the same work, but only one invoice bills it: the database refuses the second one's posting (FY606), so a piece of
// work is billed once whichever way invoices are issued (FR-PRJ-10).
// req: FR-PRJ-10
func TestTwoInvoicesNeverBillTheSameWork(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Twice", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Twice", "employee")
	una := e.joinedIn(e.books.Org, "Una Twice", "controller")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "TWO-1", "name": "Twice", "currency": "SGD", "billing_method": "time_and_materials", "customer_id": e.customer(),
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	e.call(http.MethodPost, "/v1/billing-rates", acc, map[string]any{"project_id": pid, "effective_from": "2026-01-01", "hourly_rate": "20"}, "Idempotency-Key", uuid.NewString())
	tok := e.person(priya, authz.Employee)
	e.saveWeek(tok, "2090-01-09", map[string]any{"project_id": pid, "task_id": task, "work_date": "2090-01-10", "minutes": 60})
	_, tag := e.weekOf(tok, "2090-01-09")
	s := e.call(http.MethodPost, "/v1/timesheet/2090-01-09/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	body := map[string]any{"project_id": pid, "revenue_account_id": e.books.Sales.ID.String()}
	first := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, body, "Idempotency-Key", uuid.NewString())
	second := e.call(http.MethodPost, "/v1/project-invoice-drafts", acc, body, "Idempotency-Key", uuid.NewString())
	if first.Status != 201 || second.Status != 201 {
		t.Fatalf("two drafts: %d %d", first.Status, second.Status)
	}
	if r := e.call(http.MethodPost, "/v1/invoices/"+first.str("id")+"/issue", acc, nil, "If-Match", first.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("issuing the first: %d %s", r.Status, r.Raw)
	}
	r := e.call(http.MethodPost, "/v1/invoices/"+second.str("id")+"/issue", acc, nil, "If-Match", second.Header.Get("ETag"))
	if r.Status != 409 || !strings.Contains(r.Raw, "STORAGE-BILL-01") {
		t.Errorf("issuing the second: %d %s", r.Status, r.Raw)
	}
	// The second stays a draft: nothing posted.
	if g := e.call(http.MethodGet, "/v1/invoices/"+second.str("id"), acc, nil); g.str("status") != "draft" {
		t.Errorf("the second invoice: %s", g.Raw)
	}
}
