//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
)

// orderDraft is an order to the supplier: quantity × price of rent, coded TX, and a set-up fee.
func (e *env) orderDraft(quantity, price string) map[string]any {
	return map[string]any{
		"supplier_id": e.supplier(), "reference": "Q-" + quantity, "date": "2026-10-14", "currency": "SGD",
		"lines": []map[string]any{
			{"account_id": e.books.Rent.ID.String(), "description": "Licence", "quantity": quantity, "unit_price": price, "gst_code": "TX"},
			{"account_id": e.books.Rent.ID.String(), "description": "Set-up", "quantity": "1", "unit_price": "200", "gst_code": "TX"},
		},
	}
}

// createOrder saves a draft order as an Accountant and returns its ID and ETag.
func (e *env) createOrder(quantity, price string) (id, tag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/purchase-orders", e.as(authz.Accountant), e.orderDraft(quantity, price))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createPurchaseOrder = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// approvedOrder saves and approves a small order as an Accountant and returns its ID and ETag.
func (e *env) approvedOrder() (id, tag string) {
	e.t.Helper()
	id, tag = e.createOrder("3", "33.333333")
	r := e.call(http.MethodPost, "/v1/purchase-orders/"+id+"/approve", e.as(authz.Accountant), nil, "If-Match", tag)
	if r.Status != http.StatusOK {
		e.t.Fatalf("approvePurchaseOrder = %d: %s", r.Status, r.Raw)
	}
	return id, r.Header.Get("ETag")
}

// FR-AP-01 through the API (the owner's decisions of 23 Oct 2026, built in rev 0.87): a draft order shows what it
// comes to, line by line under RND-LINE with the GST expected; at or below the self-approval threshold a founder
// approves it alone, logged for the weekly review (FM-2, FM-3); above it the approval is asked for, and its preparer
// never gives it (SOD-3). Bills are made from it by quantity; an approved order never changes; closed, it makes no more.
// req: FR-AP-01
// req: FM-2
// req: SOD-3
func TestPurchaseOrdersThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)

	r := e.call(http.MethodPost, "/v1/purchase-orders", ahkow, e.orderDraft("3", "33.333333"))
	if r.Status != http.StatusCreated || r.str("status") != "draft" || r.str("net", "amount") != "300.00" || r.str("gst", "amount") != "27.00" ||
		r.str("total", "amount") != "327.00" || r.str("total_base", "amount") != "327.00" || r.str("states", "billing") != "not_billed" ||
		!strings.Contains(r.Raw, `"amount":{"amount":"100.00","currency":"SGD"}`) {
		t.Fatalf("a draft order: %d %s", r.Status, r.Raw)
	}
	small, tag := r.str("id"), r.Header.Get("ETag")
	a := e.call(http.MethodPost, "/v1/purchase-orders/"+small+"/approve", ahkow, nil, "If-Match", tag)
	if a.Status != http.StatusOK || a.str("number") != "PO-0001" || a.str("status") != "approved" || a.str("approved_by", "id") != f.ahkow {
		t.Fatalf("approving at the threshold: %d %s", a.Status, a.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/review-log", weiling, nil); !strings.Contains(l.Raw, `"kind":"self_approved","object_type":"purchase_order","object_key":"`+small+`"`) {
		t.Errorf("the self-approval is not in the review log: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/purchase-orders/"+small, ahkow, e.orderDraft("4", "33.333333"), "If-Match", a.Header.Get("ETag")),
		"replacePurchaseOrder", 409, purchaseorder.CodeNotDraft)

	// Above the threshold: refused, asked for, refused to its preparer, approved by the other founder.
	big := e.call(http.MethodPost, "/v1/purchase-orders", weiling, e.orderDraft("10", "250"))
	e.problemIs(e.call(http.MethodPost, "/v1/purchase-orders/"+big.str("id")+"/approve", ahkow, nil, "If-Match", big.Header.Get("ETag")),
		"approvePurchaseOrder", 409, CodeOrderNeedsApproval)
	asked := e.call(http.MethodPost, "/v1/purchase-orders/"+big.str("id")+"/approval", ahkow, nil, "If-Match", big.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectOrder || asked.str("operation") != approval.OpOrderApprove ||
		!strings.Contains(asked.str("envelope"), `"total":"2943.00"`) {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/purchase-orders/"+big.str("id"), ahkow, nil); g.str("states", "approval") != "awaiting_approval" || g.str("approval_asked") != asked.str("id") {
		t.Errorf("the order does not show the request: %s", g.Raw)
	}
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnOrder)
	e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)
	mine := e.call(http.MethodPost, "/v1/purchase-orders", ahkow, e.orderDraft("10", "250"))
	asked = e.call(http.MethodPost, "/v1/purchase-orders/"+mine.str("id")+"/approval", ahkow, nil, "If-Match", mine.Header.Get("ETag"))
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("approving the order: %d %s", d.Status, d.Raw)
	}
	g := e.call(http.MethodGet, "/v1/purchase-orders/"+mine.str("id"), ahkow, nil)
	if g.str("status") != "approved" || g.str("approved_by", "id") != f.weiling || g.str("number") != "PO-0002" {
		t.Fatalf("after the approval: %s", g.Raw)
	}

	// A bill of four of the ten licences: 1,000.00 with 90.00 of GST, naming its order line; then the order shows it.
	b := e.call(http.MethodPost, "/v1/purchase-orders/"+mine.str("id")+"/bill", ahkow, map[string]any{"lines": []map[string]any{{"line": 1, "quantity": "4"}}},
		"If-Match", g.Header.Get("ETag"))
	if b.Status != http.StatusCreated || b.str("status") != "draft" || !strings.Contains(b.Raw, `"order":{"purchase_order_id":"`+mine.str("id")+`","line":1,"quantity":"4"}`) ||
		!strings.Contains(b.Raw, `"net":{"amount":"1000.00","currency":"SGD"}`) || b.Header.Get("Location") != "/v1/bills/"+b.str("id") {
		t.Fatalf("billing four: %d %s", b.Status, b.Raw)
	}
	g = e.call(http.MethodGet, "/v1/purchase-orders/"+mine.str("id"), ahkow, nil)
	if !strings.Contains(g.Raw, `"billed":"0","on_drafts":"4","left":"6"`) || len(g.Body["bills"].([]any)) != 1 {
		t.Errorf("the order with a draft bill: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/purchase-orders/"+mine.str("id")+"/bill", ahkow, map[string]any{"lines": []map[string]any{{"line": 1, "quantity": "7"}}},
		"If-Match", g.Header.Get("ETag")), "billPurchaseOrder", 422, purchaseorder.CodeLeft)

	// Closed: no more bills from it; the list shows it as closed.
	c := e.call(http.MethodPost, "/v1/purchase-orders/"+mine.str("id")+"/close", ahkow, map[string]any{"reason": "Six licences are enough for now"},
		"If-Match", g.Header.Get("ETag"))
	if c.Status != http.StatusOK || c.str("status") != "closed" || c.Body["states"].(map[string]any)["closed"] != true {
		t.Fatalf("closing: %d %s", c.Status, c.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/purchase-orders/"+mine.str("id")+"/bill", ahkow, nil, "If-Match", c.Header.Get("ETag")),
		"billPurchaseOrder", 409, purchaseorder.CodeNotApproved)
	if l := e.call(http.MethodGet, "/v1/purchase-orders?status=closed", weiling, nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the closed orders: %d %s", l.Status, l.Raw)
	}
}
