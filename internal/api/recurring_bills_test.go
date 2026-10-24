//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
)

// makeBillDrafts runs the scheduler's recurring bills for e's organisation up to day, as the worker does.
func (e *env) makeBillDrafts(day time.Time) journal.RecurringRun {
	e.t.Helper()
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(worker.Close)
	run, err := journal.Bills{Store: worker, AfterMaking: notify.RecurringMade(outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock})}.MakeDue(
		context.Background(), e.books.Org, day)
	if err != nil {
		e.t.Fatal(err)
	}
	return run
}

// recurringBill is a recurring bill of the supplier for rent, net and no GST, on schedule.
func (e *env) recurringBill(name, net string, schedule map[string]any) map[string]any {
	d := e.billDraft(net, "0.00")
	return map[string]any{"name": name, "schedule": schedule, "supplier_id": d["supplier_id"], "currency": "SGD", "lines": d["lines"],
		"notes": "Unit 05-12"}
}

// FR-AP-09, FR-AUT-01 (the owner's decision of 22 Oct 2026, built in rev 0.84): a recurring bill is a bill's supplier,
// currency, lines and notes and a schedule; the scheduler makes the draft bill of each occurrence, dated on it, saved by
// scheduler:bills.recurring and naming the recurring bill's version, which a person completes and posts. Its statuses
// are those of recurring journals: content the bill rules would refuse (C.1 #29), and an occurrence due but not made
// (C.1 #28).
// req: FR-AP-09
// req: FR-NTF-01
// req: FR-AUT-01
// req: C.1 #28
// req: C.1 #29
func TestRecurringBillsThroughTheAPI(t *testing.T) {
	e := newEnv(t) // its day is 15 October 2026
	accountant := e.as(authz.Accountant)
	check := func(r response, i int) map[string]any {
		t.Helper()
		c, _ := r.Body["checks"].([]any)
		if len(c) <= i {
			t.Fatalf("no status %d: %s", i, r.Raw)
		}
		return c[i].(map[string]any)
	}

	// A preview checks and saves nothing.
	rent := e.recurringBill("Office rent", "1500.00", monthly(31, "2026-10-01"))
	if p := e.call(http.MethodPost, "/v1/recurring-bills", accountant, rent, "Prefer", "return=preview"); p.Status != http.StatusOK ||
		p.Header.Get("Preference-Applied") != "return=preview" || p.str("next_on") != "2026-10-31" {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/recurring-bills", accountant, nil); len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("a preview saved something: %s", l.Raw)
	}
	c := e.call(http.MethodPost, "/v1/recurring-bills", accountant, rent, "Idempotency-Key", "recurring-bill-rent")
	if c.Status != http.StatusCreated || c.str("status") != "active" || c.str("next_on") != "2026-10-31" || c.Header.Get("ETag") != `"1"` ||
		c.str("total", "amount") != "1500.00" || c.str("supplier", "name") != "Atlassian Pty Ltd" || c.str("notes") != "Unit 05-12" ||
		check(c, 0)["code"] != journal.StatusRecurringReady || c.Header.Get("Location") != "/v1/recurring-bills/"+c.str("id") {
		t.Fatalf("adding a recurring bill: %d %s", c.Status, c.Raw)
	}
	id := c.str("id")
	if again := e.call(http.MethodPost, "/v1/recurring-bills", accountant, rent, "Idempotency-Key", "recurring-bill-rent"); again.str("id") != id {
		t.Errorf("a replay made another: %s", again.Raw)
	}

	// Every invalid field at once; content the bill rules refuse is kept, and says so.
	bad := e.recurringBill(" ", "1.00", map[string]any{"every_months": 1, "day_of_month": 1, "starts_on": "2026-10-15", "ends_on": "2026-10-01"})
	r := e.call(http.MethodPost, "/v1/recurring-bills", accountant, bad)
	e.problemIs(r, "createRecurringBill", 422, CodeInvalid)
	if !strings.Contains(r.Raw, `"field":"body.name"`) || !strings.Contains(r.Raw, `"field":"body.schedule.ends_on"`) {
		t.Errorf("the invalid fields: %s", r.Raw)
	}
	wrong := e.recurringBill("Cleaning", "200.00", monthly(1, "2026-11-01"))
	wrong["lines"].([]map[string]any)[0]["gst_code"] = "SR"
	if w := e.call(http.MethodPost, "/v1/recurring-bills", accountant, wrong); w.Status != http.StatusCreated ||
		check(w, 0)["severity"] != "error" || check(w, 0)["code"] != bill.CodeGSTCode {
		t.Errorf("an incomplete recurring bill: %d %s", w.Status, w.Raw)
	}

	// A change at its version: the content is a new version; paused, then resumed.
	rent["notes"] = "Unit 05-12, from November with service charge"
	e.problemIs(e.call(http.MethodPut, "/v1/recurring-bills/"+id, accountant, rent, "If-Match", `"7"`), "replaceRecurringBill", 412, CodeStale)
	p := e.call(http.MethodPut, "/v1/recurring-bills/"+id, accountant, rent, "If-Match", `"1"`)
	if p.Status != http.StatusOK || p.Body["version"] != float64(2) || p.Body["content_version"] != float64(2) {
		t.Fatalf("changing it: %d %s", p.Status, p.Raw)
	}
	s := e.call(http.MethodPost, "/v1/recurring-bills/"+id+"/pause", accountant, nil, "If-Match", `"2"`)
	if s.Status != http.StatusOK || s.str("status") != "paused" || check(s, 0)["code"] != journal.StatusRecurringPaused {
		t.Fatalf("pausing: %d %s", s.Status, s.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/recurring-bills/"+id+"/pause", accountant, nil, "If-Match", `"3"`), "pauseRecurringBill", 409, journal.CodeRecurringPaused)
	if s = e.call(http.MethodPost, "/v1/recurring-bills/"+id+"/resume", accountant, nil, "If-Match", `"3"`); s.Status != http.StatusOK ||
		s.str("status") != "active" || s.str("next_on") != "2026-10-31" {
		t.Fatalf("resuming: %d %s", s.Status, s.Raw)
	}

	// One that started in September is late, so October's close refuses (C.1 #28); the incomplete one is warned of
	// (C.1 #29).
	late := e.call(http.MethodPost, "/v1/recurring-bills", accountant, e.recurringBill("Software", "50.00", monthly(1, "2026-09-01")))
	if late.str("next_on") != "2026-09-01" || check(late, 0)["code"] != journal.StatusRecurringDue {
		t.Fatalf("a recurring bill due but not made: %s", late.Raw)
	}
	controller := e.as(authz.Controller)
	closeCheck := func(key string) map[string]any {
		t.Helper()
		oct := e.books.Oct.ID.String()
		st := e.call(http.MethodGet, "/v1/periods/"+oct+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+oct+"/close-checks", controller, nil, "If-Match", st.Header.Get("ETag"))
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == key {
				return c.(map[string]any)
			}
		}
		t.Fatalf("no %s check: %s", key, r.Raw)
		return nil
	}
	if c := closeCheck("recurring-bills"); c["severity"] != "error" || !strings.Contains(c["detail"].(string), "1 recurring bill(s) have an occurrence more than a day ago") {
		t.Errorf("October's close, with a recurring bill the scheduler did not make: %v", c)
	}
	if c := closeCheck("recurring-bills-content"); c["severity"] != "warning" || !strings.Contains(c["detail"].(string), "1 active recurring bill(s)") {
		t.Errorf("October's close, with an incomplete recurring bill: %v", c)
	}
	e.call(http.MethodPost, "/v1/recurring-bills/"+late.str("id")+"/pause", accountant, nil, "If-Match", `"1"`)

	// The scheduler makes the draft of 31 October: dated on it, saved by it, naming the recurring bill's version, with no
	// supplier's number and no number of its own until it posts.
	if run := e.makeBillDrafts(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)); run.Made != 1 {
		t.Fatalf("the run to 31 October made %d drafts", run.Made)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `made a draft dated 31 Oct 2026: check it and post it`) || !strings.Contains(trail, `"recurring.made"`) {
		t.Error("those who post bills were not told of the draft")
	}
	g := e.call(http.MethodGet, "/v1/recurring-bills/"+id, accountant, nil)
	draft := e.call(http.MethodGet, "/v1/bills/"+g.str("last_bill_id"), accountant, nil)
	if g.str("last_on") != "2026-10-31" || g.str("next_on") != "2026-11-30" || draft.str("status") != "draft" || draft.str("date") != "2026-10-31" ||
		draft.str("notes") != "Unit 05-12, from November with service charge" || draft.str("prepared_by", "id") != "scheduler:bills.recurring" ||
		draft.str("supplier", "name") != "Atlassian Pty Ltd" || draft.Body["number"] != nil || draft.Body["supplier_ref"] != nil {
		t.Fatalf("the October draft: %s %s", g.Raw, draft.Raw)
	}
	// Completed with the supplier's number, it posts as any bill.
	body := e.billDraft("1500.00", "0.00")
	body["date"], body["supplier_ref"] = "2026-10-31", "RENT-2610"
	sv := e.call(http.MethodPut, "/v1/bills/"+draft.str("id"), accountant, body, "If-Match", draft.Header.Get("ETag"))
	if sv.Status != http.StatusOK || sv.str("prepared_by", "kind") != "user" {
		t.Fatalf("completing the scheduler's draft: %d %s", sv.Status, sv.Raw)
	}
	if posted := e.call(http.MethodPost, "/v1/bills/"+draft.str("id")+"/post", accountant, nil, "If-Match", sv.Header.Get("ETag")); posted.Status != http.StatusOK ||
		posted.str("bill", "number") == "" {
		t.Fatalf("posting the scheduler's draft: %d %s", posted.Status, posted.Raw)
	}
}

// SOD-3 (rev 0.84): whoever wrote the version of a recurring bill a draft was made from prepared that draft, so they do
// not approve its posting, whoever asks.
// req: SOD-3
// req: FR-AP-09
func TestTheAuthorOfARecurringBillDoesNotApproveItsDrafts(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Accountant, authz.Controller), f.as(f.weiling, authz.Accountant, authz.Controller)
	c := e.call(http.MethodPost, "/v1/recurring-bills", weiling, e.recurringBill("Office rent", "5000.00", monthly(31, "2026-10-01")))
	if c.Status != http.StatusCreated {
		t.Fatalf("Wei Ling's recurring bill: %d %s", c.Status, c.Raw)
	}
	e.makeBillDrafts(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	id := e.call(http.MethodGet, "/v1/recurring-bills/"+c.str("id"), ahkow, nil).str("last_bill_id")
	draft := e.call(http.MethodGet, "/v1/bills/"+id, ahkow, nil)
	asked := e.call(http.MethodPost, "/v1/bills/"+id+"/post-approval", ahkow, nil, "If-Match", draft.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted {
		t.Fatalf("Ah Kow asks to post the scheduler's draft: %d %s", asked.Status, asked.Raw)
	}
	r := f.decide(weiling, asked.str("id"), "approve", hashOf(asked))
	e.problemIs(r, "approveApproval", 403, CodeOwnBill)
	if !strings.Contains(r.Raw, "You wrote the recurring bill this draft was made from") {
		t.Errorf("the refusal does not say why: %s", r.Raw)
	}
}
