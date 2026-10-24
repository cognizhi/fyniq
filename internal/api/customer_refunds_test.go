//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/customercredit"
)

// refundingCredit is a refund of amount, in SGD, of credit from the money account.
func (e *env) refundingCredit(credit, amount string) map[string]any {
	return map[string]any{"credit_id": credit, "date": "2026-10-22", "money_account_id": e.moneyAccount(), "reference": "FAST 77120",
		"amount": map[string]any{"amount": amount, "currency": "SGD"}}
}

// refundedCustomerCredit refunds all of an issued credit note of S$10.00 as an Accountant and returns the refund's ID
// and ETag.
func (e *env) refundedCustomerCredit() (id, etag string) {
	e.t.Helper()
	cn := e.issuedCreditNote("1", "10.00")
	total := e.call(http.MethodGet, "/v1/invoices/"+cn, e.as(authz.Accountant), nil).str("total", "amount")
	r := e.call(http.MethodPost, "/v1/customer-refunds", e.as(authz.Accountant), e.refundingCredit(cn, total))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordCustomerRefund = %d: %s", r.Status, r.Raw)
	}
	return r.str("refund", "id"), r.Header.Get("ETag")
}

// FR-AR-04 and FR-AR-18 through the API (p1-sales task 3.1, part): the company pays a customer back part of a credit
// note, recorded as a refund (CR-0001) that posts Dr 1100, Cr the current account; the credit note shows part applied
// and has the rest left; more than is left is refused; in Founder mode a void is asked for and approved by a second
// person, which reopens it.
// req: FR-AR-04
// req: FR-AR-18
// req: FM-1
func TestCustomerRefundsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	cn := e.issuedCreditNote("1", "100.00")
	r := e.call(http.MethodPost, "/v1/customer-refunds", ahkow, e.refundingCredit(cn, "40.00"))
	if r.Status != 201 || r.str("refund", "number") != "CR-0001" || r.str("refund", "credit", "number") != "CN-0001" || r.str("refund", "credit", "kind") != "credit_note" ||
		r.str("refund", "cleared", "amount") != "40.00" || r.str("refund", "realised_fx", "amount") != "0.00" || r.str("entry", "rule_version") != "customer_refund@1" ||
		r.Header.Get("Location") != "/v1/customer-refunds/"+r.str("refund", "id") {
		t.Fatalf("recording the refund: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("refund", "id"), r.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/invoices/"+cn, ahkow, nil); g.str("states", "settlement") != "part_applied" || g.str("open", "amount") != "60.00" {
		t.Errorf("the credit note after the refund: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-refunds/"+id, weiling, nil); g.Status != 200 || g.Header.Get("ETag") != tag || g.str("money_account", "id") == "" {
		t.Errorf("getCustomerRefund: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/customer-refunds", ahkow, e.refundingCredit(cn, "60.01")), "recordCustomerRefund", 422, customercredit.CodeTooMuch)

	// In Founder mode a void is asked for and approved by the other founder; the credit note reopens.
	e.problemIs(e.call(http.MethodPost, "/v1/customer-refunds/"+id+"/void", ahkow, map[string]any{"date": "2026-10-23", "reason": "Bounced"}, "If-Match", tag),
		"voidCustomerRefund", 409, CodeCustomerRefundVoidApproval)
	asked := e.call(http.MethodPost, "/v1/customer-refunds/"+id+"/void-approval", ahkow, map[string]any{"date": "2026-10-23", "reason": "Bounced"}, "If-Match", tag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectCustomerRefund || asked.str("operation") != approval.OpCustomerRefundVoid {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-refunds/"+id, ahkow, nil); g.str("status") != "reversed" {
		t.Errorf("the voided refund: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+cn, ahkow, nil); g.str("states", "settlement") != "open" || g.str("open", "amount") != "100.00" {
		t.Errorf("the credit note after the void: %s", g.Raw)
	}
}
