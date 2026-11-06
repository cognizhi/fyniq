//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// A contact's balances and activity (FR-CT-02; PRD-001 rev 0.185): the customer of an issued invoice (S$100.00, no GST in these books) owes its total,
// current, and was invoiced it in the year; the supplier of a posted bill is owed its total, and billed it; each sees
// its document on its timeline with what it moved. Whoever does not view bills sees neither the payable side nor the
// bill.
// req: FR-CT-02
// req: master-data/contacts Balances are live
// req: master-data/contacts Financial-year totals and ageing
// req: master-data/contacts Each side shows to whoever views its documents (rev 0.185)
// req: master-data/contacts A timeline entry shows what it moved (rev 0.185)
func TestAContactShowsWhatItStandsAt(t *testing.T) {
	e := newEnv(t)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Controller), nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller),
		map[string]any{"fy_start_month": 1, "books_start": "2026-01-01", "first_year_end": "2026-12-31"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the years: %d %s", p.Status, p.Raw)
	}
	inv, _ := e.issuedInvoice("1", "100.00")
	bill, _ := e.postedBill("200.00", "18.00")
	summary := func(tok, contact string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/contacts/"+contact+"/summary", tok, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("the summary: %d %s", r.Status, r.Raw)
		}
		return r
	}
	acc := e.as(authz.Accountant)
	c := summary(acc, e.customer())
	rows := c.Body["receivable"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["total"].(map[string]any)["amount"] != "100.00" ||
		c.str("invoiced", "amount") != "100.00" || c.str("year", "from") != "2026-01-01" || len(c.Body["payable"].([]any)) != 0 {
		t.Fatalf("the customer: %s", c.Raw)
	}
	if !strings.Contains(c.Raw, `"id":"`+inv+`"`) || !strings.Contains(c.Raw, `"amount":{"amount":"100.00","currency":"SGD"}`) {
		t.Errorf("the customer's timeline: %s", c.Raw)
	}
	s := summary(acc, e.supplier())
	if rows := s.Body["payable"].([]any); len(rows) != 1 || rows[0].(map[string]any)["total"].(map[string]any)["amount"] != "218.00" ||
		s.str("billed", "amount") != "218.00" || !strings.Contains(s.Raw, `"id":"`+bill+`"`) {
		t.Fatalf("the supplier: %s", s.Raw)
	}
	ar := summary(e.as(authz.ARSpecialist), e.supplier())
	if len(ar.Body["payable"].([]any)) != 0 || ar.Body["billed"] != nil || strings.Contains(ar.Raw, bill) {
		t.Errorf("an AR specialist sees the payable side: %s", ar.Raw)
	}
}
