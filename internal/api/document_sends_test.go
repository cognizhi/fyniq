//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-06 through the API (p1-sales task 5.2): emailing an issued tax invoice and a sent quote. A draft is not
// emailed; a tax invoice whose customer has no address lacks a particular and is not sent; a customer with no billing
// email needs an address given. Once complete, emailInvoice sends INV-0001 to the billing email: its delivery is sent,
// the send names the version, the PDF in its evidence (source generated) and the Classic starter, and the delivery log
// has the invoice's message from the invoices sender's purpose; a preview sends nothing. emailQuote sends QT-0001.
// req: FR-AR-06
// req: FR-AR-16
// req: FR-INT-01
func TestInvoicesAndQuotesAreEmailed(t *testing.T) {
	e := newEnv(t)
	acc, ctl := e.as(authz.Accountant), e.as(authz.Controller)
	if r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", ctl, map[string]any{"registered_from": "2026-09-01", "gst_number": "202312345K"}); r.Status != 201 {
		t.Fatalf("the registration: %d %s", r.Status, r.Raw)
	}
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctl, map[string]any{"legal_name": "Northwind Analytics Pte. Ltd.",
		"registered_address": "1 Raffles Place, #20-01, Singapore 048616"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the profile: %d %s", p.Status, p.Raw)
	}
	customer := func(address, billing string) {
		t.Helper()
		ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.Contacts().Get(ctx, uuid.MustParse(e.customer()))
			if err != nil {
				return err
			}
			c.Address, c.BillingEmail = address, billing
			_, err = u.Contacts().Update(ctx, c.ID, c.Version, c.ContactDetails, c.Active)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	emailIt := func(path, etag string, body map[string]any, headers ...string) response {
		var b any // no body at all, not null, when none is given
		if body != nil {
			b = body
		}
		return e.call(http.MethodPost, path+"/email", acc, b, append([]string{"If-Match", etag}, headers...)...)
	}

	draft, draftTag := e.createInvoice("1", "100.00")
	e.problemIs(emailIt("/v1/invoices/"+draft, draftTag, nil), "emailInvoice", 409, salesdoc.CodeNotSent)
	id, tag := e.issuedInvoice("1", "100.00")
	customer("", "")
	e.problemIs(emailIt("/v1/invoices/"+id, tag, map[string]any{"to": "ap@acme.example"}), "emailInvoice", 422, salesdoc.CodeParticulars)
	customer("2 Sample Street, Singapore 000002", "")
	e.problemIs(emailIt("/v1/invoices/"+id, tag, nil), "emailInvoice", 422, salesdoc.CodeNoRecipient)
	e.problemIs(emailIt("/v1/invoices/"+id, tag, map[string]any{"to": "Acme <ap@acme.example>"}), "emailInvoice", 422, CodeInvalid)
	customer("2 Sample Street, Singapore 000002", "ap@acme.example")

	if pv := emailIt("/v1/invoices/"+id, tag, nil, "Prefer", "return=preview"); pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" ||
		pv.str("states", "delivery") != "not_sent" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	r := emailIt("/v1/invoices/"+id, tag, nil, "Idempotency-Key", uuid.NewString())
	sends, _ := r.Body["sends"].([]any)
	if r.Status != 200 || r.str("states", "delivery") != "sent" || len(sends) != 1 {
		t.Fatalf("emailInvoice: %d %s", r.Status, r.Raw)
	}
	s := sends[0].(map[string]any)
	if s["to"] != "ap@acme.example" || s["version"] != float64(1) || s["delivery"] != "sent" || s["message_status"] != "queued" ||
		s["template_source"] != "platform" || len(s["pdf_sha256"].(string)) != 64 || s["sent_by"] == nil {
		t.Errorf("the send: %v", s)
	}
	ev := e.call(http.MethodGet, "/v1/invoices/"+id+"/evidence", acc, nil)
	found := false
	for _, x := range ev.Body["items"].([]any) {
		f := x.(map[string]any)
		if f["id"] == s["evidence_id"] && f["source"] == "generated" && f["media_type"] == "application/pdf" && f["sha256"] == s["pdf_sha256"] {
			found = true
		}
	}
	if !found {
		t.Errorf("the PDF in the invoice's evidence: %s", ev.Raw)
	}
	log := e.call(http.MethodGet, "/v1/organisation/email/messages?purpose=invoice", e.as(authz.Administrator), nil)
	if items, _ := log.Body["items"].([]any); log.Status != 200 || len(items) != 1 || items[0].(map[string]any)["recipient"] != "ap@acme.example" {
		t.Errorf("the delivery log: %d %s", log.Status, log.Raw)
	}

	qid, qtag := e.sentQuote()
	q := e.call(http.MethodPost, "/v1/quotes/"+qid+"/email", acc, nil, "If-Match", qtag)
	if qs, _ := q.Body["sends"].([]any); q.Status != 200 || q.str("delivery") != "sent" || len(qs) != 1 {
		t.Fatalf("emailQuote: %d %s", q.Status, q.Raw)
	}
}
