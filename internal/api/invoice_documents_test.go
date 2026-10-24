//go:build local

package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-05 through the API (p1-sales task 5.1): an invoice rendered with the platform's starter through the render
// service. Before the GST registration from 1 Sep it is a plain invoice and needs no particulars. Registered, a draft
// lacks the supplier's and the customer's addresses until the profile and the contact have them; then an SGD tax
// invoice has every particular, and a USD one at 1.34 its totals in SGD too. With simplified tax invoices on from
// 15 Oct, one of S$109.00 is simplified and one of S$1,090.00 is not. A draft dated 10 Sep shows ⚠ INVOICE-LATE on
// 15 Oct; one dated 14 Oct does not.
// req: FR-AR-05
// req: FR-INT-01
func TestInvoicesRenderAsTaxInvoices(t *testing.T) {
	e := newEnv(t)
	acc, ctl := e.as(authz.Accountant), e.as(authz.Controller)
	draft := func(date, currency, price string) string {
		t.Helper()
		d := e.invoiceDraft("1", price)
		d["date"], d["currency"] = date, currency
		if currency != "SGD" {
			d["fx_rate"] = "1.34"
		}
		r := e.call(http.MethodPost, "/v1/invoices", acc, d)
		if r.Status != http.StatusCreated {
			t.Fatalf("createInvoice: %d %s", r.Status, r.Raw)
		}
		return r.str("id")
	}
	render := func(id string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/invoices/"+id+"/document", acc, nil)
		if r.Status != 200 || r.str("pdf") == "" || len(r.str("sha256")) != 64 || r.str("template", "source") != "platform" {
			t.Fatalf("getInvoiceDocument %s: %d %.400s", id, r.Status, r.Raw)
		}
		return r
	}
	brief := func(r response) string { // without the PDF
		return fmt.Sprintf("%d tax_invoice=%v simplified=%v required=%v missing=%v", r.Status, r.Body["tax_invoice"], r.Body["simplified"],
			r.Body["required_particulars"], r.Body["missing_particulars"])
	}
	list := func(r response, key string) []string {
		var out []string
		for _, x := range r.Body[key].([]any) {
			out = append(out, x.(string))
		}
		return out
	}

	plain := render(draft("2026-08-20", "SGD", "100.00"))
	if plain.Body["tax_invoice"] != false || len(list(plain, "required_particulars")) != 0 || len(list(plain, "missing_particulars")) != 0 {
		t.Errorf("before the registration: %s", brief(plain))
	}
	if r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", ctl, map[string]any{"registered_from": "2026-09-01", "gst_number": "202312345K"}); r.Status != 201 {
		t.Fatalf("the registration: %d %s", r.Status, r.Raw)
	}
	sgd := draft("2026-10-14", "SGD", "1000.00")
	if r := render(sgd); r.Body["tax_invoice"] != true || !slices.Contains(list(r, "missing_particulars"), "supplier_address") ||
		!slices.Contains(list(r, "missing_particulars"), "customer_address") {
		t.Errorf("without the addresses: %s", brief(r))
	}
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctl, map[string]any{"legal_name": "Northwind Analytics Pte. Ltd.",
		"registered_address": "1 Raffles Place, #20-01, Singapore 048616"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the profile: %d %s", p.Status, p.Raw)
	}
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, uuid.MustParse(e.customer()))
		if err != nil {
			return err
		}
		c.Address = "2 Sample Street, Singapore 000002"
		_, err = u.Contacts().Update(ctx, c.ID, c.Version, c.ContactDetails, c.Active)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	full := render(sgd)
	if req := list(full, "required_particulars"); full.Body["simplified"] != false || len(list(full, "missing_particulars")) != 0 ||
		!slices.Contains(req, "title") || !slices.Contains(req, "supplier_gst_number") || !slices.Contains(req, "gst_total") || slices.Contains(req, "sgd_amounts") {
		t.Errorf("an SGD tax invoice: %s", brief(full))
	}
	if usd := render(draft("2026-10-14", "USD", "100.00")); !slices.Contains(list(usd, "required_particulars"), "sgd_amounts") || len(list(usd, "missing_particulars")) != 0 {
		t.Errorf("a USD tax invoice: %s", brief(usd))
	}

	if r := e.call(http.MethodPatch, "/v1/organisation/settings/sales.simplified_tax_invoices", ctl, map[string]any{"value": "true"}, "If-Match", `"0"`); r.Status != 200 {
		t.Fatalf("turning simplified tax invoices on: %d %s", r.Status, r.Raw)
	}
	smallID := draft("2026-10-15", "SGD", "100.00")
	small := render(smallID)
	if req := list(small, "required_particulars"); small.Body["simplified"] != true || len(list(small, "missing_particulars")) != 0 ||
		!slices.Contains(req, "includes_gst_statement") || slices.Contains(req, "customer_name") {
		t.Errorf("a simplified tax invoice of S$109.00: %s", brief(small))
	}
	g := e.call(http.MethodGet, "/v1/invoices/"+smallID, acc, nil)
	if is := e.call(http.MethodPost, "/v1/invoices/"+smallID+"/issue", acc, nil, "If-Match", g.Header.Get("ETag")); is.Status != 200 {
		t.Fatalf("issuing the small one: %d %s", is.Status, is.Raw)
	}
	if issued := render(smallID); issued.str("invoice_number") == "" || !slices.Contains(list(issued, "required_particulars"), "number") ||
		len(list(issued, "missing_particulars")) != 0 {
		t.Errorf("the small one issued: %s", brief(issued))
	}
	if big := render(draft("2026-10-15", "SGD", "1000.00")); big.Body["simplified"] != false {
		t.Errorf("an invoice of S$1,090.00: %s", brief(big))
	}

	late := func(id string) bool {
		g := e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil)
		for _, c := range g.Body["checks"].([]any) {
			if m := c.(map[string]any); m["code"] == invoice.WarnLate && m["severity"] == "warning" {
				return true
			}
		}
		return false
	}
	if !late(draft("2026-09-10", "SGD", "100.00")) || late(sgd) {
		t.Error("the 30-day warning")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/invoices/"+uuid.NewString()+"/document", acc, nil), "getInvoiceDocument", 404, CodeNotFound)
}
