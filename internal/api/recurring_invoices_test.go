//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
)

// makeInvoiceDrafts runs the scheduler's recurring invoices for e's organisation up to day, as the worker does.
func (e *env) makeInvoiceDrafts(day time.Time) journal.RecurringRun {
	e.t.Helper()
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(worker.Close)
	run, err := journal.Invoices{Store: worker, AfterMaking: notify.RecurringMade(outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock})}.MakeDue(
		context.Background(), e.books.Org, day)
	if err != nil {
		e.t.Fatal(err)
	}
	return run
}

// recurringInvoice is a recurring invoice to the customer of quantity × price on the sales account, coded SR.
func (e *env) recurringInvoice(name, price string, schedule map[string]any) map[string]any {
	d := e.invoiceDraft("1", price)
	return map[string]any{"name": name, "schedule": schedule, "customer_id": d["customer_id"], "reference": "PO-4411", "currency": "SGD",
		"lines": d["lines"], "notes": "Monthly subscription"}
}

func matrixRecurringInvoice(e *env) string {
	return e.call(http.MethodPost, "/v1/recurring-invoices", e.as(authz.Accountant),
		e.recurringInvoice("Matrix "+uuid.NewString()[:8], "10.00", monthly(1, "2026-11-01"))).str("id")
}

// FR-AR-07, FR-AUT-01 (the owner's decision of 24 Oct 2026, built in rev 0.135): a recurring invoice is an invoice's
// customer, reference, currency, lines and notes and a schedule, weekly to yearly; a schedule names weeks or months,
// never both nor neither. The scheduler makes the draft invoice of each occurrence, saved by
// scheduler:invoices.recurring and naming the recurring invoice's version; the draft shows it, and whoever wrote that
// version does not approve its issue (SOD-3). A weekly one from Monday 12 Oct makes its drafts of 12 and 19 Oct.
// req: FR-AR-07
// req: FR-AUT-01
// req: SOD-3
func TestRecurringInvoicesThroughTheAPI(t *testing.T) {
	e := newEnv(t) // its day is 15 October 2026
	acc := e.as(authz.Accountant)
	weekly := map[string]any{"every_weeks": 1, "starts_on": "2026-10-12"}
	for name, bad := range map[string]map[string]any{
		"both":           {"every_weeks": 1, "every_months": 1, "day_of_month": 1, "starts_on": "2026-10-12"},
		"neither":        {"starts_on": "2026-10-12"},
		"a day of weeks": {"every_weeks": 1, "day_of_month": 3, "starts_on": "2026-10-12"},
		"months, no day": {"every_months": 1, "starts_on": "2026-10-12"},
	} {
		r := e.call(http.MethodPost, "/v1/recurring-invoices", acc, e.recurringInvoice("Bad", "10.00", bad))
		if r.problemCode() != CodeInvalid {
			t.Errorf("a schedule of %s: %d %s", name, r.Status, r.Raw)
		}
		e.problemIs(r, "createRecurringInvoice", 422, CodeInvalid)
	}
	c := e.call(http.MethodPost, "/v1/recurring-invoices", acc, e.recurringInvoice("Support, Acme", "500.00", weekly), "Idempotency-Key", "recurring-invoice-support")
	if c.Status != http.StatusCreated || c.str("status") != "active" || c.str("next_on") != "2026-10-12" || c.Header.Get("ETag") != `"1"` ||
		c.str("net", "amount") != "500.00" || c.str("customer", "name") != "Acme Analytics Pte Ltd" || c.str("reference") != "PO-4411" ||
		c.Header.Get("Location") != "/v1/recurring-invoices/"+c.str("id") {
		t.Fatalf("adding a recurring invoice: %d %s", c.Status, c.Raw)
	}
	id := c.str("id")
	if run := e.makeInvoiceDrafts(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)); run.Made != 2 {
		t.Fatalf("the scheduler made %d drafts", run.Made)
	}
	g := e.call(http.MethodGet, "/v1/recurring-invoices/"+id, acc, nil)
	if g.str("last_on") != "2026-10-19" || g.str("next_on") != "2026-10-26" || g.str("last_invoice_id") == "" {
		t.Fatalf("after the run: %s", g.Raw)
	}
	inv := e.call(http.MethodGet, "/v1/invoices/"+g.str("last_invoice_id"), acc, nil)
	if inv.str("status") != "draft" || inv.str("date") != "2026-10-19" || inv.str("recurring_invoice", "id") != id || inv.Body["recurring_invoice"].(map[string]any)["version"] != float64(1) {
		t.Fatalf("its draft: %s", inv.Raw)
	}
	p := e.call(http.MethodPost, "/v1/recurring-invoices/"+id+"/pause", acc, nil, "If-Match", g.Header.Get("ETag"))
	if p.Status != 200 || p.str("status") != "paused" {
		t.Fatalf("pausing it: %d %s", p.Status, p.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/recurring-invoices/"+id+"/resume", acc, nil, "If-Match", p.Header.Get("ETag")); r.Status != 200 || r.str("status") != "active" ||
		r.str("next_on") != "2026-10-26" {
		t.Fatalf("resuming it on 15 October: %d %s", r.Status, r.Raw)
	}
}

// SOD-3 for recurring invoices (FR-AR-07, rev 0.135): Wei Ling writes a recurring invoice of S$5,000.00 a month; the
// scheduler's draft of 1 October is asked to be issued by Ah Kow, and Wei Ling, who wrote the version it came from,
// cannot approve it, though she never saved the draft.
// req: SOD-3
// req: FR-AR-07
func TestARecurringInvoicesAuthorDoesNotApproveItsDrafts(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	body := e.recurringInvoice("Retainer", "1000.00", monthly(1, "2026-10-01"))
	body["lines"].([]map[string]any)[0]["quantity"] = "5"
	if c := e.call(http.MethodPost, "/v1/recurring-invoices", weiling, body); c.Status != http.StatusCreated {
		t.Fatalf("adding it: %d %s", c.Status, c.Raw)
	}
	if run := e.makeInvoiceDrafts(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)); run.Made != 1 {
		t.Fatalf("the scheduler made %d drafts", run.Made)
	}
	l := e.call(http.MethodGet, "/v1/recurring-invoices", weiling, nil)
	draft := l.Body["items"].([]any)[0].(map[string]any)["last_invoice_id"].(string)
	tag := e.call(http.MethodGet, "/v1/invoices/"+draft, ahkow, nil).Header.Get("ETag")
	asked := e.call(http.MethodPost, "/v1/invoices/"+draft+"/issue-approval", ahkow, nil, "If-Match", tag)
	if asked.Status != 202 {
		t.Fatalf("asking to issue the draft: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnInvoice)
}
