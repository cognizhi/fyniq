//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
)

// receiptOf is a receipt of amount of an invoice into the current account on 20 October.
func (e *env) receiptOf(invoiceID, amount string) map[string]any {
	return map[string]any{"date": "2026-10-20", "money_account_id": e.moneyAccount(), "currency": "SGD", "reference": "FAST 7731",
		"invoices": []map[string]any{{"invoice_id": invoiceID, "amount": map[string]any{"amount": amount, "currency": "SGD"}}}}
}

// recordedReceipt issues an invoice and records a receipt of it in full as an Accountant; it returns the receipt's ID and
// ETag.
func (e *env) recordedReceipt() (id, etag string) {
	e.t.Helper()
	inv, _ := e.issuedInvoice("1", "10.00")
	r := e.call(http.MethodPost, "/v1/customer-receipts", e.as(authz.Accountant), e.receiptOf(inv, "10.00"))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordCustomerReceipt = %d: %s", r.Status, r.Raw)
	}
	return r.str("receipt", "id"), r.Header.Get("ETag")
}

// FR-AR-18 through the API (p1-sales task 2.1): a receipt previews, records with its number and settles the invoice,
// whose settlement state and open amount follow; the customer is the invoice's when left out; more than is open is
// refused; in Founder mode a void is asked for and approved by another person, and reopens the invoice; a settled
// invoice is not voided until its receipt is.
// req: FR-AR-18
// req: FR-AR-03
// req: FM-1
func TestCustomerReceiptsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	inv, _ := e.issuedInvoice("1", "1000.00")

	pv := e.call(http.MethodPost, "/v1/customer-receipts", ahkow, e.receiptOf(inv, "400.00"), "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || len(pv.Body["entry"].(map[string]any)["lines"].([]any)) != 2 {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	r := e.call(http.MethodPost, "/v1/customer-receipts", ahkow, e.receiptOf(inv, "400.00"), "Idempotency-Key", "receipt-1")
	if r.Status != 201 || r.str("receipt", "number") != "RC-0001" || r.str("receipt", "customer", "name") != "Acme Analytics Pte Ltd" ||
		!strings.Contains(r.Raw, `"invoice_number":"INV-0001"`) || r.Header.Get("Location") != "/v1/customer-receipts/"+r.str("receipt", "id") {
		t.Fatalf("recording: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+inv, weiling, nil); g.str("states", "settlement") != "part_paid" || g.str("open", "amount") != "600.00" {
		t.Errorf("the invoice after RC-0001: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/customer-receipts", ahkow, e.receiptOf(inv, "700.00")), "recordCustomerReceipt", 422, receipt.CodeOverpaid)
	r2 := e.call(http.MethodPost, "/v1/customer-receipts", ahkow, e.receiptOf(inv, "600.00"))
	if g := e.call(http.MethodGet, "/v1/invoices/"+inv, weiling, nil); r2.Status != 201 || g.str("states", "settlement") != "paid" || g.str("open", "amount") != "0.00" {
		t.Fatalf("the invoice after RC-0002: %d %s", r2.Status, g.Raw)
	}

	// A settled invoice is not voided until its receipts are (here asked for, so the refusal comes first).
	g := e.call(http.MethodGet, "/v1/invoices/"+inv, weiling, nil)
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+inv+"/void-approval", ahkow, map[string]any{"date": "2026-10-25", "reason": "Wrong customer"},
		"If-Match", g.Header.Get("ETag")), "requestInvoiceVoid", 409, invoice.CodeSettled)

	// Founder mode: the void is refused, asked for, approved by another person; the invoice reopens.
	id, tag := r2.str("receipt", "id"), r2.Header.Get("ETag")
	reversal := map[string]any{"date": "2026-10-25", "reason": "The transfer bounced"}
	e.problemIs(e.call(http.MethodPost, "/v1/customer-receipts/"+id+"/void", ahkow, reversal, "If-Match", tag), "voidCustomerReceipt", 409, CodeReceiptVoidApproval)
	asked := e.call(http.MethodPost, "/v1/customer-receipts/"+id+"/void-approval", ahkow, reversal, "If-Match", tag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectReceipt || !strings.Contains(asked.str("summary"), "RC-0002 from Acme Analytics Pte Ltd, 600.00 SGD") {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-receipts/"+id, weiling, nil); g.str("approval_asked") != asked.str("id") || len(g.Body["ledger_impact"].([]any)) != 2 {
		t.Errorf("the receipt does not show the request: %s", g.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+inv, weiling, nil); g.str("states", "settlement") != "part_paid" || g.str("open", "amount") != "600.00" {
		t.Errorf("the invoice after the void: %s", g.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/customer-receipts?status=posted", e.as(authz.Auditor), nil); l.Status != 200 || len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the posted receipts: %d %s", l.Status, l.Raw)
	}
}
