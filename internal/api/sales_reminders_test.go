//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/salesreminder"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// invoiceDue issues an invoice of 100.00 + GST dated date and due on due as an Accountant, and returns its ID and ETag.
func (e *env) invoiceDue(date, due string) (id, etag string) {
	e.t.Helper()
	d := e.invoiceDraft("1", "100.00")
	d["date"], d["due_date"] = date, due
	c := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), d)
	if c.Status != http.StatusCreated {
		e.t.Fatalf("createInvoice = %d: %s", c.Status, c.Raw)
	}
	r := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", e.as(authz.Accountant), nil, "If-Match", c.Header.Get("ETag"))
	if r.Status != http.StatusOK {
		e.t.Fatalf("issueInvoice = %d: %s", r.Status, r.Raw)
	}
	return c.str("id"), r.Header.Get("ETag")
}

// remindersDue are the reminders listed, by document ID.
func (e *env) remindersDue() map[string]map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, "/v1/sales-reminders", e.as(authz.Accountant), nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("listSalesReminders = %d: %s", r.Status, r.Raw)
	}
	out := map[string]map[string]any{}
	for _, x := range r.Body["items"].([]any) {
		m := x.(map[string]any)
		out[m["document_id"].(string)] = m
	}
	return out
}

// setCustomerGroup puts the customer in group (empty: none) through the contacts API.
func (e *env) setCustomerGroup(group string) response {
	e.t.Helper()
	return e.call(http.MethodPatch, "/v1/contacts/"+e.customer(), e.as(authz.Accountant), map[string]any{"customer_group_id": group}, "If-Match", "*")
}

// FR-AR-08 through the API (p1-sales task 7.1; the owner's decision of 24 Oct 2026, reminders through the Inbox until
// bank import). On the matrix's today, 15 October 2026, with the standard schedule (3 days before the due date, on it, 7
// and 30 days after): an invoice due on 1 October is due its reminder 7 days after, worded as a payment reminder; one due
// on 17 October its reminder 3 days before, worded as the invoice; one due on 30 November none yet; a quote valid until 17
// October is reminded of 3 days before. A customer group of no days stops the invoices' reminders; one of 0 and 14 days
// lists the first invoice's 14th day and not yet the second's. Sending the overdue invoice's reminder emails it from the
// reminders sender with the PDF already sent, records the send and the decision, and takes it off the list; sending it
// again, or another step, is refused. Skipping keeps a reason. A receipt that settles an invoice takes its reminder off.
// req: FR-AR-08
// req: FR-INT-01
func TestRemindersWaitForAPerson(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	e.readyToEmail()
	overdue, tag := e.invoiceDue("2026-10-01", "2026-10-01")
	soon, _ := e.invoiceDue("2026-10-01", "2026-10-17")
	later, _ := e.invoiceDue("2026-10-01", "2026-11-30")
	qd := e.quoteDraft("quote", "1", "50.00")
	qd["valid_until"] = "2026-10-17"
	qc := e.call(http.MethodPost, "/v1/quotes", acc, qd)
	if s := e.call(http.MethodPost, "/v1/quotes/"+qc.str("id")+"/send", acc, nil, "If-Match", qc.Header.Get("ETag")); s.Status != http.StatusOK {
		t.Fatalf("sendQuote: %d %s", s.Status, s.Raw)
	}
	q := qc.str("id")
	// The overdue invoice was emailed once already: its reminder sends the same PDF again.
	if r := e.call(http.MethodPost, "/v1/invoices/"+overdue+"/email", acc, nil, "If-Match", tag); r.Status != http.StatusOK {
		t.Fatalf("emailInvoice: %d %s", r.Status, r.Raw)
	}

	due := e.remindersDue()
	if r := due[overdue]; r == nil || r["kind"] != "invoice" || r["step"] != float64(7) || r["days_overdue"] != float64(14) ||
		r["email_template"] != "reminder" || r["recipient"] != "ap@acme.example" || r["due_on"] != "2026-10-01" ||
		r["amount"].(map[string]any)["amount"] != "109.00" {
		t.Errorf("the overdue invoice's reminder: %v", r)
	}
	if r := due[soon]; r == nil || r["step"] != float64(-3) || r["days_overdue"] != float64(0) || r["email_template"] != "invoice" {
		t.Errorf("the invoice due soon: %v", r)
	}
	if r := due[q]; r == nil || r["kind"] != "quote" || r["step"] != float64(salesreminder.QuoteStep) || r["email_template"] != "quote" ||
		r["due_on"] != "2026-10-17" {
		t.Errorf("the quote about to expire: %v", r)
	}
	if due[later] != nil {
		t.Errorf("the invoice due in November: %v", due[later])
	}

	// A customer group's schedule replaces the standard one; a schedule out of order, or a name in use, is refused.
	e.problemIs(e.call(http.MethodPost, "/v1/customer-groups", acc, map[string]any{"name": "Bad", "reminder_days": []int{7, 0}}),
		"createCustomerGroup", 422, CodeInvalid)
	none := e.call(http.MethodPost, "/v1/customer-groups", acc, map[string]any{"name": "No reminders", "reminder_days": []int{}})
	if none.Status != http.StatusCreated || none.Header.Get("ETag") == "" {
		t.Fatalf("createCustomerGroup: %d %s", none.Status, none.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/customer-groups", acc, map[string]any{"name": " no reminders ", "reminder_days": []int{0}}),
		"createCustomerGroup", 409, storage.CodeDuplicate)
	if r := e.setCustomerGroup(none.str("id")); r.Status != http.StatusOK || r.str("customer_group_id") != none.str("id") {
		t.Fatalf("the customer's group: %d %s", r.Status, r.Raw)
	}
	if due := e.remindersDue(); due[overdue] != nil || due[soon] != nil || due[q] == nil {
		t.Errorf("with no days: %v", due)
	}
	g := e.call(http.MethodPut, "/v1/customer-groups/"+none.str("id"), acc, map[string]any{"name": "Fortnight", "reminder_days": []int{0, 14}},
		"If-Match", none.Header.Get("ETag"))
	if g.Status != http.StatusOK || g.str("name") != "Fortnight" || g.Body["version"] != float64(2) {
		t.Fatalf("replaceCustomerGroup: %d %s", g.Status, g.Raw)
	}
	if due := e.remindersDue(); due[overdue] == nil || due[overdue]["step"] != float64(14) || due[soon] != nil {
		t.Errorf("with 0 and 14 days: %v", due)
	}
	groups := e.call(http.MethodGet, "/v1/customer-groups", acc, nil)
	if items, _ := groups.Body["items"].([]any); groups.Status != 200 || len(items) != 1 || len(groups.Body["standard_reminder_days"].([]any)) != 4 {
		t.Errorf("listCustomerGroups: %d %s", groups.Status, groups.Raw)
	}
	inactive := e.call(http.MethodPut, "/v1/customer-groups/"+none.str("id"), acc, map[string]any{"name": "Fortnight", "reminder_days": []int{0, 14},
		"active": false}, "If-Match", g.Header.Get("ETag"))
	if inactive.Status != http.StatusOK || inactive.Body["active"] != false {
		t.Fatalf("an inactive group: %d %s", inactive.Status, inactive.Raw)
	}
	if r := e.setCustomerGroup(""); r.Status != http.StatusOK || r.str("customer_group_id") != "" {
		t.Fatalf("out of the group: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.setCustomerGroup(none.str("id")), "updateContact", 422, CodeInvalid) // inactive: given to no one new

	// Sending: the reminders sender, the PDF already sent, the decision; once.
	e.problemIs(e.call(http.MethodPost, "/v1/sales-reminders/"+overdue+"/send", acc, map[string]any{"step": 30}), "sendSalesReminder", 409,
		salesreminder.CodeNotDue)
	if pv := e.call(http.MethodPost, "/v1/sales-reminders/"+overdue+"/send", acc, map[string]any{"step": 7}, "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	sent := e.call(http.MethodPost, "/v1/sales-reminders/"+overdue+"/send", acc, map[string]any{"step": 7}, "Idempotency-Key", uuid.NewString())
	if sent.Status != http.StatusOK || sent.str("outcome") != "sent" || sent.str("send_id") == "" || sent.Body["decided_by"] == nil {
		t.Fatalf("sendSalesReminder: %d %s", sent.Status, sent.Raw)
	}
	inv := e.call(http.MethodGet, "/v1/invoices/"+overdue, acc, nil)
	sends, _ := inv.Body["sends"].([]any)
	if len(sends) != 2 || sends[0].(map[string]any)["evidence_id"] != sends[1].(map[string]any)["evidence_id"] {
		t.Errorf("the reminder's send carries the PDF already sent: %v", sends)
	}
	log := e.call(http.MethodGet, "/v1/organisation/email/messages?purpose=reminder", e.as(authz.Administrator), nil)
	if items, _ := log.Body["items"].([]any); log.Status != 200 || len(items) != 1 || items[0].(map[string]any)["recipient"] != "ap@acme.example" {
		t.Errorf("the delivery log: %d %s", log.Status, log.Raw)
	}
	if due := e.remindersDue(); due[overdue] != nil {
		t.Errorf("a sent reminder stays listed: %v", due[overdue])
	}
	e.problemIs(e.call(http.MethodPost, "/v1/sales-reminders/"+overdue+"/send", acc, map[string]any{"step": 7}), "sendSalesReminder", 409,
		salesreminder.CodeNotDue)

	// Skipping, with a reason.
	skip := e.call(http.MethodPost, "/v1/sales-reminders/"+q+"/skip", acc, map[string]any{"step": salesreminder.QuoteStep, "reason": "Asked by phone"})
	if skip.Status != http.StatusOK || skip.str("outcome") != "skipped" || skip.str("reason") != "Asked by phone" || skip.str("send_id") != "" {
		t.Fatalf("skipSalesReminder: %d %s", skip.Status, skip.Raw)
	}
	if due := e.remindersDue(); due[q] != nil {
		t.Errorf("a skipped reminder stays listed: %v", due[q])
	}

	// A receipt that settles the invoice due soon takes its reminder off.
	receipt := e.receiptOf(soon, "109.00")
	receipt["date"] = "2026-10-15"
	if r := e.call(http.MethodPost, "/v1/customer-receipts", acc, receipt); r.Status != http.StatusCreated {
		t.Fatalf("recordCustomerReceipt: %d %s", r.Status, r.Raw)
	}
	if due := e.remindersDue(); due[soon] != nil {
		t.Errorf("a settled invoice stays listed: %v", due[soon])
	}
	e.problemIs(e.call(http.MethodPost, "/v1/sales-reminders/"+soon+"/skip", acc, map[string]any{"step": -3}), "skipSalesReminder", 409,
		salesreminder.CodeNotDue)
}

// matrixOverdueInvoice is an invoice due a reminder 7 days after its due date, for the matrix's sends and skips: a new
// one per call, so each caller decides a reminder of its own.
func matrixOverdueInvoice(e *env) string {
	e.t.Helper()
	e.readyToEmail()
	id, _ := e.invoiceDue("2026-10-01", "2026-10-01")
	return id
}

// matrixCustomerGroup is a customer group the matrix's calls read and change.
func matrixCustomerGroup(e *env) string {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/customer-groups", e.as(authz.Accountant), map[string]any{"name": "Matrix " + uuid.NewString()[:8],
		"reminder_days": []int{0}})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createCustomerGroup = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// A regression test (found building p1-sales task 7.1): issuing an invoice dated where no period is open answered 500,
// for the storage refusal had no problem. It is a 409 that says which date no period covers and how to fix it (ENG-23).
// req: ENG-23
func TestAnInvoiceDatedOutsideEveryPeriodIsRefusedInWords(t *testing.T) {
	e := newEnv(t)
	d := e.invoiceDraft("1", "100.00")
	d["date"] = "2026-09-01"
	c := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), d)
	r := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", e.as(authz.Accountant), nil, "If-Match", c.Header.Get("ETag"))
	e.problemIs(r, "issueInvoice", 409, storage.CodeNoPeriod)
	if !strings.Contains(r.str("detail"), "2026-09-01") || r.str("fix") == "" {
		t.Errorf("the problem's words: %s", r.Raw)
	}
}
