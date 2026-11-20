// Package paymentlinkjob makes a payment link for an invoice that was just issued, outside the request (PRD-001 FR-AR-13, rev 0.588; p2-bank-feeds-payments task 5.2): issuing the invoice queues
// the job when the organisation has switched on automatic payment links, and the worker asks the payment provider for the link and records it through the database's narrow function. The
// provider is never called while the invoice is issued, and the email an invoice is sent with carries the link only once it exists (a send before that goes without it). The worker posts
// nothing and holds no right on the link table: only the function, which makes a live link for a posted invoice.
package paymentlinkjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/paymentlink"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of making one invoice's link; Setting is the organisation's switch.
const (
	Kind    = "payment_link.make"
	Setting = "invoices.payment_link_on_issue"
)

// Actor is the job's principal in the audit trail.
var Actor = storage.Actor{Type: "scheduler", ID: "payment_link"}

type payload struct {
	InvoiceID uuid.UUID `json:"invoice_id"`
}

// Queue queues the making of a link for an invoice just issued in u, when the organisation has the setting on; it does nothing otherwise.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, invoice uuid.UUID) error {
	def, _ := settings.Lookup(Setting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return err
	}
	if settings.On(def, changes, d.Clock.Now()).Value != "true" {
		return nil
	}
	b, err := json.Marshal(payload{InvoiceID: invoice})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b})
	return err
}

// OnIssue is the hook of journal.Invoices.AfterIssued: whichever path issues an invoice, with the setting on, the link is queued (FR-AR-13).
func OnIssue(d outbox.Dispatcher) func(ctx context.Context, u storage.UnitOfWork, org, invoice uuid.UUID) error {
	return func(ctx context.Context, u storage.UnitOfWork, org, invoice uuid.UUID) error {
		return Queue(ctx, d, u, org, invoice)
	}
}

// Maker makes the links.
type Maker struct {
	Store   storage.Store
	Runtime *connector.Runtime
	Clock   ports.Clock
}

// Work is the outbox handler of Kind. A refusal that will not change, such as an invoice that is no longer open, ends the job without a link; a provider that cannot be reached is an error, so the
// job is retried; a link the provider made but Fyniq could not record is an error too, and the provider's request is left to expire.
func (m Maker) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.InvoiceID == uuid.Nil {
			return fmt.Errorf("paymentlinkjob: outbox row %s is not a link request", row.ID)
		}
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor, Reason: "A payment link made when the invoice was issued (FR-AR-13)"})
		now := m.Clock.Now()
		var (
			cid   uuid.UUID
			req   ports.PaymentRequest
			links ports.PaymentLinks
		)
		skip := false
		if err := m.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			def, _ := settings.Lookup(Setting)
			changes, err := u.Settings().Changes(ctx, def.Key)
			if err != nil {
				return err
			}
			if settings.On(def, changes, now).Value != "true" {
				skip = true
				return nil
			}
			all, err := u.Connectors().Organisation(ctx)
			if err != nil {
				return err
			}
			var found []uuid.UUID
			for _, c := range all {
				if b, ok := m.Runtime.Catalogue.Get(c.Connector); ok && b.PaymentLinks != nil && c.State == storage.ConnectorEnabled {
					found = append(found, c.ID)
				}
			}
			if len(found) != 1 {
				skip = true // no single enabled payment-link connector: nothing to ask
				return nil
			}
			cid = found[0]
			_, req, err = paymentlink.Check(ctx, u, cid, p.InvoiceID, now)
			var refusal *paymentlink.Error
			if errors.As(err, &refusal) {
				skip = true // not issued, settled, nothing open, a live link already, or no provider account collects the currency
				return nil
			}
			if err != nil {
				return err
			}
			links, _, err = m.Runtime.PaymentLinks(ctx, u, cid, connector.LinkRequest)
			return err
		}); err != nil {
			return err
		}
		if skip {
			return nil
		}
		made, err := links.CreatePaymentRequest(ctx, req) // the provider, outside any unit of work
		if err != nil {
			if errors.Is(err, ports.ErrPaymentRequestRefused) {
				return nil // refused for good: a person makes the link by hand and sees why
			}
			return err
		}
		return m.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.PaymentLinks().CreateByFunction(ctx, storage.NewPaymentLink{ConnectorID: cid, InvoiceID: p.InvoiceID, ProviderRequestID: made.ID, URL: made.URL,
				Reference: req.Reference, Currency: req.Currency, Amount: storage.Decimal(req.Amount), ExpiresAt: made.ExpiresAt, At: now})
			return err
		})
	}
}
