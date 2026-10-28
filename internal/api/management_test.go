//go:build local

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The management reports through the API (FR-RPT-03), worked by hand. In October, Acme is invoiced 2 × Platform at
// 500.00 and a set-up line of 200.00 with no item, and credited 1 × Platform; Lumen is invoiced 3 × Support at 100.00,
// and 1 × Platform at 1,000.00 that is voided the same month; any GST is left out. Sales by
// customer: Acme 1,200.00 − 500.00 = 700.00 (70.0%, two documents), Lumen 300.00 + 1,000.00 − 1,000.00 = 300.00 (30.0%,
// two documents); 1,000.00 in all. By item: Platform 1,000.00 − 500.00 + 1,000.00 − 1,000.00 = 500.00, 1 sold net
// (2 − 1 + 1 − 1); Support 300.00, 3 sold; the lines without an item 200.00. Two bills of rent from one supplier,
// 400.00 and 100.00 with GST of 36.00 and 9.00, which the company, not GST-registered, cannot claim, so it is a cost:
// spend of 545.00 and the top expense, 100.0%. As at the revision before
// the void, Lumen's figure keeps the 1,000.00. A sales view (AR) reads the sales reports but not the spend or the top
// expenses; a purchases view (AP) the spend only.
// req: FR-RPT-03
// req: FR-RPT-08
// asvs: V8.2.1
func TestManagementReportsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	acme := e.customer()
	var lumen, platform, support string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Lumen Health Pte Ltd", Customer: true})
		if err != nil {
			return err
		}
		lumen = c.ID.String()
		p, err := u.Items().Create(ctx, "PLATFORM", storage.ItemDetails{Name: "Platform", Type: storage.ItemService, Unit: "month", RevenueAccountID: &e.books.Sales.ID, GSTCode: "SR"})
		if err != nil {
			return err
		}
		s, err := u.Items().Create(ctx, "SUPPORT", storage.ItemDetails{Name: "Support", Type: storage.ItemService, Unit: "hour", RevenueAccountID: &e.books.Sales.ID, GSTCode: "SR"})
		platform, support = p.ID.String(), s.ID.String()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	line := func(item, description, quantity, price string) map[string]any {
		l := map[string]any{"account_id": e.books.Sales.ID.String(), "description": description, "gst_code": "SR", "quantity": quantity, "unit_price": price}
		if item != "" {
			l["item_id"] = item
		}
		return l
	}
	accountant := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	issue := func(body map[string]any) (string, string) {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/invoices", accountant, body)
		if r.Status != http.StatusCreated {
			t.Fatalf("a draft: %d %s", r.Status, r.Raw)
		}
		p := e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", accountant, nil, "If-Match", r.Header.Get("ETag"))
		if p.Status != http.StatusOK {
			t.Fatalf("issuing it: %d %s", p.Status, p.Raw)
		}
		return r.str("id"), p.Header.Get("ETag")
	}
	invoice := func(customer string, lines ...map[string]any) map[string]any {
		return map[string]any{"customer_id": customer, "date": "2026-10-14", "currency": "SGD", "lines": lines}
	}
	toAcme, _ := issue(invoice(acme, line(platform, "Platform, October", "2", "500.00"), line("", "Set-up", "1", "200.00")))
	issue(invoice(lumen, line(support, "Support hours", "3", "100.00")))
	credit := invoice(acme, line(platform, "Platform, a month back", "1", "500.00"))
	credit["kind"], credit["date"], credit["invoice_id"] = "credit_note", "2026-10-21", toAcme
	issue(credit)
	voided, tag := issue(invoice(lumen, line(platform, "Platform, a year", "1", "1000.00")))
	before := e.call(http.MethodGet, "/v1/reports/sales-by-customer?from=2026-10&to=2026-10", accountant, nil)
	void := e.call(http.MethodPost, "/v1/invoices/"+voided+"/void-approval", accountant, map[string]any{"date": "2026-10-22", "reason": "Sent twice"}, "If-Match", tag)
	if a := f.decide(weiling, void.str("id"), "approve", hashOf(void)); a.Status != http.StatusOK {
		t.Fatalf("voiding the invoice: %d %s", a.Status, a.Raw)
	}
	e.postedBill("400.00", "36.00")
	e.postedBill("100.00", "9.00")

	rows := func(r response) string {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("a report: %d %s", r.Status, r.Raw)
		}
		var out []string
		for _, x := range r.Body["rows"].([]any) {
			row := x.(map[string]any)
			s := row["name"].(string) + " " + row["amount"].(map[string]any)["amount"].(string) + " " + row["share"].(string) + "%"
			if q, ok := row["quantity"].(string); ok {
				s += " ×" + q
			}
			if d := row["documents"].(float64); d > 0 {
				s += " in " + strings.Repeat("|", int(d))
			}
			out = append(out, s)
		}
		return strings.Join(out, "; ") + " = " + r.str("total", "amount")
	}
	for _, c := range []struct{ path, want string }{
		{"/v1/reports/sales-by-customer?from=2026-10&to=2026-10", "Acme Analytics Pte Ltd 700.00 70.0% in ||; Lumen Health Pte Ltd 300.00 30.0% in || = 1000.00"},
		{"/v1/reports/sales-by-item?from=2026-10&to=2026-10", "Platform 500.00 50.0% ×1 in |||; Support 300.00 30.0% ×3 in |; Lines without an item 200.00 20.0% in | = 1000.00"},
		{"/v1/reports/spend-by-supplier?from=2026-10&to=2026-10", "Atlassian Pty Ltd 545.00 100.0% in || = 545.00"},
		{"/v1/reports/top-expenses?from=2026-10&to=2026-10", "Rent 545.00 100.0% = 545.00"},
	} {
		if got := rows(e.call(http.MethodGet, c.path, accountant, nil)); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.path, got, c.want)
		}
	}

	// As at the revision before the void, Lumen's 1,000.00 is still there.
	at := "/v1/reports/sales-by-customer?from=2026-10&to=2026-10&as_at_revision=" + strconv.FormatInt(int64(before.Body["ledger_revision"].(float64)), 10)
	if got := rows(e.call(http.MethodGet, at, accountant, nil)); got != "Lumen Health Pte Ltd 1300.00 65.0% in ||; Acme Analytics Pte Ltd 700.00 35.0% in || = 2000.00" {
		t.Errorf("as at the revision before the void: %s", got)
	}
	// A month without a period is refused, as for the statements.
	if r := e.call(http.MethodGet, "/v1/reports/top-expenses?to=2031-01", accountant, nil); r.Status != 422 || !strings.Contains(r.Raw, `"code":"`+CodeNoPeriod+`"`) {
		t.Errorf("a month without a period: %d %s", r.Status, r.Raw)
	}

	// A sales view reads the sales reports only; a purchases view the spend only.
	ar, ap := e.as(authz.ARSpecialist), e.as(authz.APSpecialist)
	for _, c := range []struct {
		tok, path string
		status    int
	}{
		{ar, "/v1/reports/sales-by-customer", 200}, {ar, "/v1/reports/sales-by-item", 200}, {ar, "/v1/reports/spend-by-supplier", 403}, {ar, "/v1/reports/top-expenses", 403},
		{ap, "/v1/reports/sales-by-customer", 403}, {ap, "/v1/reports/spend-by-supplier", 200}, {ap, "/v1/reports/top-expenses", 403},
	} {
		if r := e.call(http.MethodGet, c.path+"?from=2026-10&to=2026-10", c.tok, nil); r.Status != c.status {
			t.Errorf("%s by %s: %d, want %d", c.path, c.tok, r.Status, c.status)
		}
	}
}
