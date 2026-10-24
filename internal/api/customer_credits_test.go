//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/customercredit"
)

// issuedCreditNote saves and issues a standalone credit note to the customer as an Accountant and returns its ID.
func (e *env) issuedCreditNote(quantity, price string) string {
	e.t.Helper()
	c := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), e.creditNoteDraft("", quantity, price))
	if c.Status != http.StatusCreated {
		e.t.Fatalf("a credit note: %d %s", c.Status, c.Raw)
	}
	r := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", e.as(authz.Accountant), nil, "If-Match", c.Header.Get("ETag"))
	if r.Status != http.StatusOK {
		e.t.Fatalf("issuing the credit note: %d %s", r.Status, r.Raw)
	}
	return c.str("id")
}

// applyingCredit is an application of what credit has of amount, in SGD, to the invoice.
func applyingCredit(credit, inv, amount string) map[string]any {
	return map[string]any{"credit_id": credit, "invoice_id": inv, "date": "2026-10-22", "amount": map[string]any{"amount": amount, "currency": "SGD"}}
}

// appliedCustomerCredit applies an issued credit note to an issued invoice of the same total as an Accountant and returns
// the application's ID and ETag.
func (e *env) appliedCustomerCredit() (id, etag string) {
	e.t.Helper()
	inv, _ := e.issuedInvoice("1", "10.00")
	total := e.call(http.MethodGet, "/v1/invoices/"+inv, e.as(authz.Accountant), nil).str("total", "amount")
	r := e.call(http.MethodPost, "/v1/customer-credit-applications", e.as(authz.Accountant), applyingCredit(e.issuedCreditNote("1", "10.00"), inv, total))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("applyCustomerCredit = %d: %s", r.Status, r.Raw)
	}
	return r.str("application", "id"), r.Header.Get("ETag")
}

// FR-AR-04 and FR-AR-18 through the API (p1-sales task 3.1, part): a credit note applied to an invoice of the same
// customer settles both by the amount and, in the base currency, posts nothing; the invoice shows paid and the credit
// note applied; more than is left is refused; in Founder mode a void is asked for and approved by a second person
// (FM-1), which reopens both.
// req: FR-AR-04
// req: FR-AR-18
// req: FM-1
func TestCustomerCreditsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	inv, _ := e.issuedInvoice("1", "100.00")
	cn := e.issuedCreditNote("1", "100.00")
	total := e.call(http.MethodGet, "/v1/invoices/"+inv, ahkow, nil).str("total", "amount")

	// A preview applies nothing; the application settles both and posts nothing in one currency.
	if pv := e.call(http.MethodPost, "/v1/customer-credit-applications", ahkow, applyingCredit(cn, inv, total), "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+inv, ahkow, nil); g.str("states", "settlement") != "open" {
		t.Fatalf("the preview applied the credit: %s", g.Raw)
	}
	r := e.call(http.MethodPost, "/v1/customer-credit-applications", ahkow, applyingCredit(cn, inv, total))
	if r.Status != 201 || r.Body["entry"] != nil || r.str("application", "credit", "kind") != "credit_note" || r.str("application", "credit", "number") != "CN-0001" ||
		r.str("application", "invoice", "number") != "INV-0001" || r.str("application", "realised_fx", "amount") != "0.00" ||
		r.str("application", "invoice_cleared", "amount") != total || r.Header.Get("Location") != "/v1/customer-credit-applications/"+r.str("application", "id") ||
		len(r.Body["application"].(map[string]any)["ledger_impact"].([]any)) != 0 {
		t.Fatalf("applying the credit note: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("application", "id"), r.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/invoices/"+inv, ahkow, nil); g.str("states", "settlement") != "paid" || g.str("open", "amount") != "0.00" {
		t.Errorf("the invoice after the credit: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+cn, ahkow, nil); g.str("states", "settlement") != "applied" || g.str("open", "amount") != "0.00" {
		t.Errorf("the credit note after it is applied: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-credit-applications/"+id, weiling, nil); g.Status != 200 || g.Header.Get("ETag") != tag || g.str("status") != "posted" {
		t.Errorf("getCustomerCreditApplication: %d %s", g.Status, g.Raw)
	}

	// More than is left of the credit note, or than the invoice has open, is refused.
	inv2, _ := e.issuedInvoice("1", "50.00")
	e.problemIs(e.call(http.MethodPost, "/v1/customer-credit-applications", ahkow, applyingCredit(cn, inv2, "1.00")), "applyCustomerCredit", 422, customercredit.CodeTooMuch)

	// In Founder mode a void is asked for and approved by the other founder; both reopen.
	e.problemIs(e.call(http.MethodPost, "/v1/customer-credit-applications/"+id+"/void", ahkow, map[string]any{"date": "2026-10-23", "reason": "Wrong invoice"},
		"If-Match", tag), "voidCustomerCreditApplication", 409, CodeCustomerApplicationVoidApproval)
	asked := e.call(http.MethodPost, "/v1/customer-credit-applications/"+id+"/void-approval", ahkow, map[string]any{"date": "2026-10-23", "reason": "Wrong invoice"},
		"If-Match", tag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectCustomerApplication || asked.str("operation") != approval.OpCustomerApplicationVoid {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-credit-applications/"+id, ahkow, nil); g.str("approval_asked") != asked.str("id") {
		t.Errorf("the application does not show the request: %s", g.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-credit-applications/"+id, ahkow, nil); g.str("status") != "reversed" {
		t.Errorf("the voided application: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+inv, ahkow, nil); g.str("states", "settlement") != "open" || g.str("open", "amount") != total {
		t.Errorf("the invoice after the void: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+cn, ahkow, nil); g.str("states", "settlement") != "open" || g.str("open", "amount") != total {
		t.Errorf("the credit note after the void: %s", g.Raw)
	}
}
