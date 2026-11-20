//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The roll-forward shows, per customer, what was billed, collected and recognised next to its deferred revenue (FR-RPT-13, rev 0.594). An invoice of
// 1,200.00 (no GST in this fixture) spread over three months from September, dated 14 October, with a receipt of 400.00 on 20 October and a credit note of
// 100.00 on 21 October: in October the customer was billed 1,200.00 - 100.00 = 1,100.00, collected 400.00 and recognised 800.00 (September's and
// October's months, released when the invoice was issued) less the credit note's 100.00 taken straight off revenue; the receivable moved by 700.00,
// which billed less collected explains exactly, and 400.00 is still deferred. A quiet month shows nothing for the customer.
// req: FR-RPT-13
func TestTheRollForwardShowsWhatEachCustomerWasBilledCollectedAndRecognised(t *testing.T) {
	e := newEnv(t)
	customer := e.customer()
	acc := e.as(authz.Accountant)
	d := e.invoiceDraft("1", "1200.00")
	d["lines"].([]map[string]any)[0]["spread_months"], d["lines"].([]map[string]any)[0]["spread_from"] = 3, "2026-09-20"
	r := e.call(http.MethodPost, "/v1/invoices", acc, d)
	if r.Status != 201 {
		t.Fatalf("saving: %d %s", r.Status, r.Raw)
	}
	id := r.str("id")
	if p := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", r.Header.Get("ETag")); p.Status != 200 {
		t.Fatalf("issuing: %d %s", p.Status, p.Raw)
	}
	if rc := e.call(http.MethodPost, "/v1/customer-receipts", acc, e.receiptOf(id, "400.00")); rc.Status != 201 {
		t.Fatalf("receipt: %d %s", rc.Status, rc.Raw)
	}
	e.issuedCreditNote("1", "100.00")

	amount := func(m map[string]any, field string) string { return m[field].(map[string]any)["amount"].(string) }
	g := e.call(http.MethodGet, "/v1/reports/deferral-roll-forward?from=2026-10&to=2026-10", e.as(authz.Auditor), nil)
	cs := g.Body["customers"].([]any)
	if g.Status != 200 || len(cs) != 1 {
		t.Fatalf("October: %d %s", g.Status, g.Raw)
	}
	x := cs[0].(map[string]any)
	if x["party_id"] != customer || amount(x, "billed") != "1100.00" || amount(x, "collected") != "400.00" || amount(x, "released") != "800.00" ||
		amount(x, "direct") != "-100.00" || amount(x, "recognised") != "700.00" || amount(x, "deferred_closing") != "400.00" ||
		amount(x, "receivable_movement") != "700.00" || amount(x, "other_movement") != "0.00" || amount(x, "unexplained") != "0.00" || x["explained"] != true {
		t.Errorf("October's customer: %v", x)
	}
	// Before the invoice nothing was billed, collected or recognised.
	if q := e.call(http.MethodGet, "/v1/reports/deferral-roll-forward?from=2026-08&to=2026-08", e.as(authz.Auditor), nil); q.Status != 200 || len(q.Body["customers"].([]any)) != 0 {
		t.Errorf("August: %d %s", q.Status, q.Raw)
	}
}

// Billed is what the customer owes more, GST included, because the check is against the receivable; recognised is the net revenue (FR-RPT-13, rev 0.594). A GST-registered
// company's invoice of 1,000.00 + 90.00 GST dated 14 October is billed 1,090.00 and recognised 1,000.00, and what the customer owes moved by 1,090.00.
// req: FR-RPT-13
func TestBilledIsGrossOfGSTAndRecognisedIsNet(t *testing.T) {
	e := newEnv(t)
	e.customer()
	if r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", e.as(authz.Controller), map[string]any{"registered_from": "2026-01-01", "gst_number": "202312345K"}); r.Status != 201 {
		t.Fatalf("registering for GST: %d %s", r.Status, r.Raw)
	}
	acc := e.as(authz.Accountant)
	r := e.call(http.MethodPost, "/v1/invoices", acc, e.invoiceDraft("1", "1000.00"))
	if r.Status != 201 {
		t.Fatalf("saving: %d %s", r.Status, r.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", acc, nil, "If-Match", r.Header.Get("ETag")); p.Status != 200 {
		t.Fatalf("issuing: %d %s", p.Status, p.Raw)
	}
	g := e.call(http.MethodGet, "/v1/reports/deferral-roll-forward?from=2026-10&to=2026-10", e.as(authz.Auditor), nil)
	cs, _ := g.Body["customers"].([]any)
	if g.Status != 200 || len(cs) != 1 {
		t.Fatalf("October: %d %s", g.Status, g.Raw)
	}
	x := cs[0].(map[string]any)
	amount := func(field string) string { return x[field].(map[string]any)["amount"].(string) }
	if amount("billed") != "1090.00" || amount("recognised") != "1000.00" || amount("receivable_movement") != "1090.00" || x["explained"] != true {
		t.Errorf("a GST-registered invoice: %v", x)
	}
}
