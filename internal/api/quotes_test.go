//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/quote"
)

// quoteDraft is a quote (or an order) to the customer of quantity × price on the sales account, coded SR.
func (e *env) quoteDraft(kind, quantity, price string) map[string]any {
	d := map[string]any{"kind": kind, "customer_id": e.customer(), "reference": "RFQ-" + price, "date": "2026-10-14", "currency": "SGD",
		"lines": []map[string]any{{"account_id": e.books.Sales.ID.String(), "description": "Analytics platform", "gst_code": "SR", "quantity": quantity,
			"unit_price": price}}}
	if kind == "quote" {
		d["valid_until"] = "2026-11-13"
	}
	return d
}

// sentQuote saves and sends a quote as an Accountant and returns its ID and ETag.
func (e *env) sentQuote() (id, etag string) {
	e.t.Helper()
	c := e.call(http.MethodPost, "/v1/quotes", e.as(authz.Accountant), e.quoteDraft("quote", "1", "100.00"))
	if c.Status != http.StatusCreated {
		e.t.Fatalf("createQuote = %d: %s", c.Status, c.Raw)
	}
	s := e.call(http.MethodPost, "/v1/quotes/"+c.str("id")+"/send", e.as(authz.Accountant), nil, "If-Match", c.Header.Get("ETag"))
	if s.Status != http.StatusOK {
		e.t.Fatalf("sendQuote = %d: %s", s.Status, s.Raw)
	}
	return c.str("id"), s.Header.Get("ETag")
}

// FR-AR-01 through the API (p1-sales task 4.1, part): a draft quote shows its totals and its checks; sending numbers it
// QT-0001; the pipeline adds up what the sent quotes and orders come to while valid; a quote with no validity date is
// refused; cancelling one takes it out of the pipeline; the list keeps quotes and orders apart; a kind never changes.
// req: FR-AR-01
// req: FR-INT-01
func TestQuotesThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	c := e.call(http.MethodPost, "/v1/quotes", acc, e.quoteDraft("quote", "2", "500.00"))
	if c.Status != 201 || c.str("status") != "draft" || c.Body["number"] != nil || c.str("total", "amount") != "1000.00" || c.str("base", "amount") != "1000.00" ||
		c.str("valid_until") != "2026-11-13" || c.str("customer", "name") != "Acme Analytics Pte Ltd" {
		t.Fatalf("a draft quote: %d %s", c.Status, c.Raw)
	}
	id, tag := c.str("id"), c.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/quotes/"+id, acc, nil); g.Status != 200 || len(g.Body["checks"].([]any)) != 0 || g.Header.Get("ETag") != tag {
		t.Errorf("getQuote: %d %s", g.Status, g.Raw)
	}
	s := e.call(http.MethodPost, "/v1/quotes/"+id+"/send", acc, nil, "If-Match", tag)
	if s.Status != 200 || s.str("number") != "QT-0001" || s.str("status") != "sent" || s.str("sent_by", "id") == "" {
		t.Fatalf("sending it: %d %s", s.Status, s.Raw)
	}

	// A quote with no validity date is refused; an order needs none.
	bad := e.quoteDraft("quote", "1", "10.00")
	delete(bad, "valid_until")
	b := e.call(http.MethodPost, "/v1/quotes", acc, bad)
	if g := e.call(http.MethodGet, "/v1/quotes/"+b.str("id"), acc, nil); !strings.Contains(g.Raw, `"code":"`+quote.CodeValidity+`"`) {
		t.Errorf("a draft with no validity date shows no check: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/quotes/"+b.str("id")+"/send", acc, nil, "If-Match", b.Header.Get("ETag")), "sendQuote", 422, quote.CodeValidity)
	o := e.call(http.MethodPost, "/v1/quotes", acc, e.quoteDraft("sales_order", "1", "300.00"))
	if r := e.call(http.MethodPost, "/v1/quotes/"+o.str("id")+"/send", acc, nil, "If-Match", o.Header.Get("ETag")); r.Status != 200 || r.str("number") != "SO-0001" || r.str("kind") != "sales_order" {
		t.Fatalf("sending an order: %d %s", r.Status, r.Raw)
	}

	// The pipeline: QT-0001's S$1,000.00 and SO-0001's S$300.00 on 1 November; after 13 November the quote has lapsed.
	if p := e.call(http.MethodGet, "/v1/quotes/pipeline?as_at=2026-11-01", acc, nil); p.Status != 200 || p.str("quotes", "total", "amount") != "1000.00" ||
		p.Body["quotes"].(map[string]any)["count"] != float64(1) || p.str("orders", "total", "amount") != "300.00" || p.str("total", "amount") != "1300.00" {
		t.Fatalf("the pipeline: %d %s", p.Status, p.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/quotes/pipeline?as_at=2026-11-14", acc, nil); p.str("quotes", "total", "amount") != "0.00" || p.str("total", "amount") != "300.00" {
		t.Errorf("the pipeline after the quote lapsed: %s", p.Raw)
	}

	// The list keeps them apart; a kind never changes; cancelling takes QT-0001 out of the pipeline.
	if l := e.call(http.MethodGet, "/v1/quotes?status=sent", acc, nil); len(l.Body["items"].([]any)) != 1 || !strings.Contains(l.Raw, "QT-0001") {
		t.Errorf("the sent quotes: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/quotes?kind=sales_order", acc, nil); len(l.Body["items"].([]any)) != 1 || !strings.Contains(l.Raw, "SO-0001") {
		t.Errorf("the orders: %s", l.Raw)
	}
	retyped := e.quoteDraft("sales_order", "1", "10.00")
	if r := e.call(http.MethodPut, "/v1/quotes/"+b.str("id"), acc, retyped, "If-Match", b.Header.Get("ETag")); r.Status != 422 || !strings.Contains(r.Raw, `"body.kind"`) {
		t.Errorf("retyping a quote: %d %s", r.Status, r.Raw)
	}
	x := e.call(http.MethodPost, "/v1/quotes/"+id+"/cancel", acc, map[string]any{"reason": "The customer chose another supplier"}, "If-Match", s.Header.Get("ETag"))
	if x.Status != 200 || x.str("status") != "cancelled" {
		t.Fatalf("cancelling it: %d %s", x.Status, x.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/quotes/pipeline?as_at=2026-11-01", acc, nil); p.str("quotes", "total", "amount") != "0.00" {
		t.Errorf("the pipeline after the cancel: %s", p.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/quotes/"+b.str("id")+"/cancel", acc, map[string]any{"reason": "Never sent"}, "If-Match", b.Header.Get("ETag")),
		"cancelQuote", 409, quote.CodeNotOpen)
}
