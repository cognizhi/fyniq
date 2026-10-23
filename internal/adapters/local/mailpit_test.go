//go:build local

package local_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Mailpit stands in for the receiving provider (ADR-T4): a message is read through its API, in the shape Mailpit 1.31
// answers, and an attachment from its part; Mailpit checks no SPF, DKIM or DMARC, so they pass, in local mode only.
// req: communication/email-and-notifications Launch adapters and local substitutes
// req: FR-INT-22
func TestMailpitStandsInForTheReceivingProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/message/3OZ4cpKPS3mhXrcz3pUoXz":
			_, _ = w.Write([]byte(`{"ID":"3OZ4cpKPS3mhXrcz3pUoXz","MessageID":"x@mailpit","From":{"Name":"AR","Address":"ar@supplier.example"},` +
				`"To":[{"Name":"","Address":"bills@in.fyniq.local"}],"Subject":"Invoice INV-0042","Text":"Please find our invoice attached.\r\n",` +
				`"HTML":"","Attachments":[{"PartID":"2","FileName":"INV-0042.pdf","ContentType":"application/pdf","ContentID":"","Size":22}],"Inline":[]}`))
		case "/api/v1/message/3OZ4cpKPS3mhXrcz3pUoXz/part/2":
			_, _ = w.Write([]byte("%PDF-1.7\n% an invoice\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	m := local.MailpitInbound{URL: srv.URL}
	mail, err := m.Fetch(context.Background(), "3OZ4cpKPS3mhXrcz3pUoXz")
	if err != nil || m.Name() != "mailpit" || mail.From != "ar@supplier.example" || mail.Subject != "Invoice INV-0042" || mail.DMARC != "pass" ||
		len(mail.Attachments) != 1 || mail.Attachments[0].ID != "2" || mail.Attachments[0].FileName != "INV-0042.pdf" {
		t.Fatalf("%+v, %v", mail, err)
	}
	b, err := m.Attachment(context.Background(), "3OZ4cpKPS3mhXrcz3pUoXz", mail.Attachments[0], 1<<20)
	if err != nil || string(b) != "%PDF-1.7\n% an invoice\n" {
		t.Fatalf("the attachment: %q, %v", b, err)
	}
	if _, err := m.Attachment(context.Background(), "3OZ4cpKPS3mhXrcz3pUoXz", mail.Attachments[0], 10); !errors.Is(err, ports.ErrTooLarge) {
		t.Fatalf("beyond the limit: %v", err)
	}
	if _, err := m.Fetch(context.Background(), "missing"); err == nil {
		t.Fatal("a missing message was read")
	}
}
