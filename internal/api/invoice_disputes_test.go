//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/disputes"
)

// FR-AR-08 (P2 half) through the API: an overdue invoice is due a reminder; a dispute of part of it (an amount and a reason) pauses the
// reminder until it is resolved, and AR ageing shows the disputed amount apart while its rows still count it; a promise to pay on a day
// pauses it until that day has passed; at most one open record of each kind; a dispute needs an amount within what is open and a reason; a
// promise needs a day from today to 90 days away; a draft or paid invoice has none; a record is resolved once, with how it ended.
// req: FR-AR-08
// req: FR-AR-10
// asvs: V2.3.1
func TestADisputeOrPromisePausesRemindersAndSplitsTheAgeing(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	id, _ := e.invoiceDue("2026-10-01", "2026-10-05") // 10 days overdue on the frozen 15 Oct 2026
	if _, ok := e.remindersDue()[id]; !ok {
		t.Fatal("an overdue invoice is due no reminder")
	}
	post := func(body map[string]any) response {
		return e.call(http.MethodPost, "/v1/invoices/"+id+"/disputes", acc, body)
	}
	// A dispute needs an amount within what is open, and a reason.
	e.problemIs(post(map[string]any{"kind": "dispute", "amount": "100.01", "reason": "Not delivered"}), "createInvoiceDispute", 422, disputes.CodeAmount)
	e.problemIs(post(map[string]any{"kind": "dispute", "amount": "40.00"}), "createInvoiceDispute", 422, disputes.CodeWords)
	made := post(map[string]any{"kind": "dispute", "amount": "40.00", "reason": "The training was not delivered"})
	if made.Status != http.StatusCreated || made.str("status") != "open" || made.str("amount", "amount") != "40.00" {
		t.Fatalf("createInvoiceDispute = %d: %s", made.Status, made.Raw)
	}
	e.problemIs(post(map[string]any{"kind": "dispute", "amount": "10.00", "reason": "Again"}), "createInvoiceDispute", 409, disputes.CodeOpen)
	if _, ok := e.remindersDue()[id]; ok {
		t.Error("a disputed invoice is still chased")
	}
	// The ageing shows the disputed part apart and still counts it in the rows.
	ageing := e.call(http.MethodGet, "/v1/receivables", acc, nil)
	cust := ageing.Body["customers"].([]any)[0].(map[string]any)
	dis := cust["disputed"].([]any)
	if ageing.Status != 200 || len(dis) != 1 || dis[0].(map[string]any)["amount"] != "40.00" || cust["rows"].([]any)[0].(map[string]any)["total"].(map[string]any)["amount"] != "100.00" {
		t.Fatalf("the ageing: %d %s", ageing.Status, ageing.Raw)
	}
	// Resolving ends the pause, once.
	res := e.call(http.MethodPost, "/v1/invoice-disputes/"+made.str("id")+"/resolution", acc, map[string]any{"resolution": "Credit note issued for the training"})
	if res.Status != 200 || res.str("status") != "resolved" {
		t.Fatalf("resolveInvoiceDispute = %d: %s", res.Status, res.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/invoice-disputes/"+made.str("id")+"/resolution", acc, map[string]any{"resolution": "Again"}), "resolveInvoiceDispute", 409, disputes.CodeOpen)
	if _, ok := e.remindersDue()[id]; !ok {
		t.Error("a resolved dispute still pauses the reminders")
	}
	// A promise pauses the reminders until its day has passed.
	e.problemIs(post(map[string]any{"kind": "promise", "promised_on": "2026-10-14"}), "createInvoiceDispute", 422, disputes.CodeDate)
	e.problemIs(post(map[string]any{"kind": "promise", "promised_on": "2027-03-01"}), "createInvoiceDispute", 422, disputes.CodeDate)
	if p := post(map[string]any{"kind": "promise", "promised_on": "2026-10-20"}); p.Status != http.StatusCreated || p.str("promised_on") != "2026-10-20" {
		t.Fatalf("a promise = %d: %s", p.Status, p.Raw)
	}
	if _, ok := e.remindersDue()[id]; ok {
		t.Error("an invoice with a promise is still chased before its day")
	}
	e.clock.Advance(6 * 24 * time.Hour) // 21 Oct: the promised day has passed
	if _, ok := e.remindersDue()[id]; !ok {
		t.Error("an invoice is not chased after its promised day passed")
	}
	// The invoice's records, newest first; a draft or an unknown invoice has none to make.
	got := e.call(http.MethodGet, "/v1/invoices/"+id+"/disputes", acc, nil)
	if recs, _ := got.Body["records"].([]any); got.Status != 200 || len(recs) != 2 || recs[0].(map[string]any)["kind"] != "promise" {
		t.Errorf("getInvoiceDisputes = %d: %s", got.Status, got.Raw)
	}
	draft, _ := e.createInvoice("1", "10.00")
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+draft+"/disputes", acc, map[string]any{"kind": "promise", "promised_on": "2026-10-30"}), "createInvoiceDispute", 422, disputes.CodeNotOpen)
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/disputes", e.as(authz.Auditor), map[string]any{"kind": "promise", "promised_on": "2026-10-30"}); r.Status != http.StatusForbidden {
		t.Errorf("an Auditor recorded a promise: %d", r.Status)
	}
	if strings.Contains(got.Raw, "token") {
		t.Error("the records show a token")
	}
	_ = uuid.Nil
}

// matrixInvoiceDisputes are the authorisation matrix's rows for disputes and promises: a role the policy allows is answered past it by the invoice or the record.
var matrixInvoiceDisputes = map[string]func(e *env, tok string) (response, func() bool){
	"createInvoiceDispute": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoices/"+uuid.NewString()+"/disputes", tok, map[string]any{"kind": "promise", "promised_on": "2026-10-30"}), nil
	},
	"getInvoiceDisputes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/invoices/"+uuid.NewString()+"/disputes", tok, nil), nil
	},
	"resolveInvoiceDispute": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoice-disputes/"+uuid.NewString()+"/resolution", tok, map[string]any{"resolution": "Settled"}), nil
	},
}
