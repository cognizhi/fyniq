//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

// creditNoteDraft is a credit note to the customer of quantity × price on the sales account, coded SR, crediting the
// invoice named, if any.
func (e *env) creditNoteDraft(invoiceID, quantity, price string) map[string]any {
	d := e.invoiceDraft(quantity, price)
	d["kind"], d["date"] = "credit_note", "2026-10-21"
	if invoiceID != "" {
		d["invoice_id"] = invoiceID
	}
	return d
}

// FR-AR-04 through the API (p1-sales task 3.1, part): a credit note is an invoice of kind credit_note, linked to the
// invoice it credits or standalone; issuing numbers it from its own sequence and posts credit_note@1; it shows what it
// takes off accounts receivable; the list keeps invoices and credit notes apart; its kind never changes; and its
// approvals are an invoice's, judged on its size (FM-2, SOD-3).
// req: FR-AR-04
// req: FR-INT-01
// req: FM-2
// req: SOD-3
func TestCreditNotesThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	inv, invTag := "", ""
	if r := e.call(http.MethodPost, "/v1/invoices", ahkow, e.invoiceDraft("1", "1500.00")); r.Status == http.StatusCreated {
		inv, invTag = r.str("id"), r.Header.Get("ETag")
	} else {
		t.Fatalf("an invoice: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoices/"+inv+"/issue", ahkow, nil, "If-Match", invTag); r.Status != 200 {
		t.Fatalf("issuing the invoice: %d %s", r.Status, r.Raw)
	}

	// A draft credit note of INV-0001: its kind, the invoice it credits, no due date, and what it takes off 1100.
	c := e.call(http.MethodPost, "/v1/invoices", ahkow, e.creditNoteDraft(inv, "1", "500.00"))
	if c.Status != http.StatusCreated || c.str("kind") != "credit_note" || c.str("credits", "id") != inv || c.str("credits", "number") != "INV-0001" ||
		c.Body["due_date"] != nil || c.str("receivable", "amount") != "500.00" || c.str("total", "amount") != "500.00" {
		t.Fatalf("a draft credit note: %d %s", c.Status, c.Raw)
	}
	id, tag := c.str("id"), c.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/invoices/"+id, weiling, nil); !strings.Contains(g.Raw, `"amount":"-500.00"`) || len(g.Body["checks"].([]any)) != 0 {
		t.Fatalf("the credit note's ledger impact: %s", g.Raw)
	}

	// Below the threshold a founder issues it alone: CN-0001, by credit_note@1, logged for the weekly review.
	p := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", ahkow, nil, "If-Match", tag)
	if p.Status != 200 || p.str("invoice", "number") != "CN-0001" || p.str("invoice", "kind") != "credit_note" ||
		p.str("entry", "rule_version") != ledger.RuleCreditNote || p.str("invoice", "receivable", "amount") != "500.00" ||
		p.str("invoice", "open", "amount") != "500.00" || p.str("invoice", "states", "due") != "not_due" {
		t.Fatalf("issuing the credit note: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/review-log", weiling, nil); !strings.Contains(l.Raw, "Issued credit note CN-0001 of 500.00 SGD alone") {
		t.Errorf("the self-approval is not in the review log: %s", l.Raw)
	}

	// The list keeps them apart: invoices by default, credit notes by kind.
	if l := e.call(http.MethodGet, "/v1/invoices?kind=credit_note", weiling, nil); len(l.Body["items"].([]any)) != 1 || !strings.Contains(l.Raw, `"number":"CN-0001"`) {
		t.Errorf("the credit notes: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/invoices", weiling, nil); len(l.Body["items"].([]any)) != 1 || strings.Contains(l.Raw, "CN-0001") {
		t.Errorf("the invoices: %s", l.Raw)
	}

	// Above the threshold, standalone: refused, asked for, approved by another person, never whoever prepared it.
	big := e.call(http.MethodPost, "/v1/invoices", weiling, e.creditNoteDraft("", "1", "2500.00"))
	bigID, bigTag := big.str("id"), big.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+bigID+"/issue", ahkow, nil, "If-Match", bigTag), "issueInvoice", 409, CodeInvoiceNeedsApproval)
	asked := e.call(http.MethodPost, "/v1/invoices/"+bigID+"/issue-approval", ahkow, nil, "If-Match", bigTag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectInvoice || !strings.Contains(asked.str("envelope"), `"receivable":"2500.00"`) ||
		!strings.Contains(asked.str("summary"), "Issue the draft credit note to Acme Analytics Pte Ltd") {
		t.Fatalf("asking to issue: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnInvoice)
	e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)
	mine := e.call(http.MethodPost, "/v1/invoices", ahkow, e.creditNoteDraft("", "1", "2500.00"))
	asked = e.call(http.MethodPost, "/v1/invoices/"+mine.str("id")+"/issue-approval", ahkow, nil, "If-Match", mine.Header.Get("ETag"))
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the issue: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+mine.str("id"), ahkow, nil); g.str("number") != "CN-0002" || g.str("status") != "posted" {
		t.Errorf("the approved credit note: %s", g.Raw)
	}

	// Its kind never changes, an invoice names no invoice, and a credit note of what is not an issued invoice is refused.
	kept := e.call(http.MethodPut, "/v1/invoices/"+bigID, weiling, e.invoiceDraft("1", "5.00"), "If-Match", bigTag) // no kind: it stays a credit note
	if kept.Status != 200 || kept.str("kind") != "credit_note" || kept.Body["version"] != float64(2) {
		t.Fatalf("revising a credit note: %d %s", kept.Status, kept.Raw)
	}
	retyped := e.invoiceDraft("1", "5.00")
	retyped["kind"] = "invoice"
	if r := e.call(http.MethodPut, "/v1/invoices/"+bigID, weiling, retyped, "If-Match", kept.Header.Get("ETag")); r.Status != 422 || !strings.Contains(r.Raw, `"body.kind"`) {
		t.Errorf("retyping a credit note: %d %s", r.Status, r.Raw)
	}
	naming := e.invoiceDraft("1", "5.00")
	naming["invoice_id"] = inv
	if r := e.call(http.MethodPost, "/v1/invoices", ahkow, naming); r.Status != 422 || !strings.Contains(r.Raw, invoice.CodeCredited) {
		t.Errorf("an invoice naming an invoice: %d %s", r.Status, r.Raw)
	}
	of := e.call(http.MethodPost, "/v1/invoices", ahkow, e.creditNoteDraft(id, "1", "5.00"))
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+of.str("id")+"/issue", ahkow, nil, "If-Match", of.Header.Get("ETag")), "issueInvoice", 422, invoice.CodeCredited)
}
