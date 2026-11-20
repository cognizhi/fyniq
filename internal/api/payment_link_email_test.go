//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// queuedEmailTexts are the bodies of the emails the outbox holds, oldest first; the queue's other tasks are dropped.
func (e *env) queuedEmailTexts() []string {
	e.t.Helper()
	ctx := context.Background()
	var out []string
	for {
		task, err := e.queue.Next(ctx, "worker")
		if err != nil {
			return out
		}
		var m outbox.Message
		if err := json.Unmarshal(task.Payload, &m); err != nil {
			e.t.Fatal(err)
		}
		var row storage.OutboxRow
		if err := e.store.Do(ctx, m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			row, err = u.Outbox().Get(ctx, m.OutboxID)
			return err
		}); err != nil {
			e.t.Fatal(err)
		}
		if row.Kind != email.Kind {
			continue
		}
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, p.Text)
	}
}

// FR-AR-13 (rev 0.575): an invoice emailed while it has a live payment link says where to pay it online, from the link already stored (sending asks the provider for nothing); one with no link,
// or a cancelled link, carries no such line.
// req: FR-AR-13
// req: FR-AR-06
func TestAnInvoiceEmailCarriesItsLivePaymentLinkOnlyWhileItIsLive(t *testing.T) {
	e := newEnv(t)
	e.readyToEmail()
	acc := e.as(authz.Accountant)
	c := e.enabledBankConnector() // any installed connector will do: the link row is stored, the provider is not asked
	id, tag := e.issuedInvoice("1", "100.00")
	send := func(tag string) string {
		t.Helper()
		if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/email", acc, nil, "If-Match", tag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
			t.Fatalf("emailInvoice: %d %s", r.Status, r.Raw)
		}
		texts := e.queuedEmailTexts()
		if len(texts) == 0 {
			t.Fatal("no email was queued")
		}
		return texts[len(texts)-1]
	}
	if text := send(tag); strings.Contains(text, "pay this invoice online") {
		t.Errorf("an invoice with no link: %s", text)
	}
	const url = "https://securecheckout.example/payment-request/abc123"
	var link storage.PaymentLink
	expires := e.clock.Now().Add(48 * time.Hour)
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		link, err = u.PaymentLinks().Create(ctx, storage.NewPaymentLink{ConnectorID: c.ID, InvoiceID: uuid.MustParse(id), ProviderRequestID: "pr_" + uuid.NewString()[:8], URL: url,
			Reference: "INV-0001", Currency: "SGD", Amount: "100.00", ExpiresAt: &expires, At: e.clock.Now()})
		return err
	})
	if text := send(tag); !strings.Contains(text, "You can pay this invoice online: "+url) {
		t.Errorf("an invoice with a live link: %s", text)
	}
	// Once the link is cancelled it carries none.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Close(ctx, link.ID, "cancelled", "Replaced", e.clock.Now(), link.Version)
		return err
	})
	if text := send(tag); strings.Contains(text, "pay this invoice online") {
		t.Errorf("a cancelled link: %s", text)
	}
}
