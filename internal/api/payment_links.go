package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/paymentlink"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes of payment links (ENG-23); PSP-09 to PSP-11 are the domain's.
const (
	CodeLinkConnector = "PSP-12" // no payment-link connector is enabled, or the one named is not
	CodeLinkClosed    = "PSP-13" // the invoice has no live payment link to cancel
)

// PaymentLinkPayment is a charge the provider made under a link.
type PaymentLinkPayment struct {
	ProviderPaymentID string    `json:"provider_payment_id" doc:"The provider's charge, the reference of its statement line." example:"90f28b43-2cff-4f86-a29e-15697424b3e7"`
	Amount            Money     `json:"amount" doc:"What the provider reports it charged."`
	PaidAt            time.Time `json:"paid_at" doc:"When the provider says it was paid." example:"2026-11-21T02:00:00Z"`
}

// InvoicePaymentLink is the payment link of an invoice (FR-AR-13).
type InvoicePaymentLink struct {
	ID           string               `json:"id,omitempty" format:"uuid" doc:"The link's ID; empty in a preview, which saves nothing." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f90"`
	InvoiceID    string               `json:"invoice_id" format:"uuid" doc:"The invoice it collects." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	ConnectorID  string               `json:"connector_id" format:"uuid" doc:"The payment-link connector that made it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b11"`
	URL          string               `json:"url,omitempty" doc:"The provider's page where the customer pays; empty in a preview." example:"https://securecheckout.hit-pay.com/payment-request/@cognizhi/9b2c"`
	Reference    string               `json:"reference" doc:"The invoice's number, which the provider keeps on its charges." example:"INV-0150"`
	Amount       Money                `json:"amount" doc:"The invoice's open balance when the link was made, read on the server."`
	Status       string               `json:"status" enum:"live,paid,expired,cancelled" doc:"live until it is paid, runs out or a person cancels it." example:"live"`
	ClosedReason string               `json:"closed_reason,omitempty" doc:"Why a closed link was closed." example:"The customer asked for a new link"`
	ExpiresAt    *time.Time           `json:"expires_at,omitempty" doc:"When the link stops working." example:"2026-12-31T00:00:00Z"`
	CreatedAt    *time.Time           `json:"created_at,omitempty" doc:"When it was made." example:"2026-11-20T03:00:00Z"`
	Version      int                  `json:"version,omitempty" doc:"Its version, for a change." example:"1"`
	Payments     []PaymentLinkPayment `json:"payments" nullable:"false" doc:"The charges the provider made under it, oldest first."`
}

type createPaymentLinkInput struct {
	InvoicePath
	KeyHeader
	PreferHeader
	Body struct {
		ConnectorID string `json:"connector_id,omitempty" format:"uuid" doc:"The payment-link connector to use; left out, the organisation's one enabled connector. The amount is never sent: it is the invoice's open balance." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b11"`
	}
}

type paymentLinkOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved and nothing was asked of the provider (FR-INT-14)."`
	Body     InvoicePaymentLink
}

type getPaymentLinkOutput struct{ Body InvoicePaymentLink }

type cancelPaymentLinkInput struct {
	InvoicePath
	KeyHeader
	PreferHeader
	Body struct {
		Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the link is cancelled." example:"The customer asked for a new link"`
	}
}

func paymentLinkOf(l storage.PaymentLink, payments []storage.PaymentLinkPayment) InvoicePaymentLink {
	out := InvoicePaymentLink{ID: l.ID.String(), InvoiceID: l.InvoiceID.String(), ConnectorID: l.ConnectorID.String(), URL: l.URL, Reference: l.Reference,
		Amount: moneyOf(l.Amount, l.Currency), Status: l.Status, ClosedReason: l.ClosedReason, ExpiresAt: l.ExpiresAt, CreatedAt: &l.CreatedAt, Version: l.Version,
		Payments: []PaymentLinkPayment{}}
	for _, p := range payments {
		out.Payments = append(out.Payments, PaymentLinkPayment{ProviderPaymentID: p.ProviderPaymentID, Amount: moneyOf(p.Amount, p.Currency), PaidAt: p.PaidAt})
	}
	return out
}

// linkConnector is the payment-link connector to use: the one named, which must be enabled and offer the capability, or the
// organisation's only one.
func linkConnector(ctx context.Context, a *API, u storage.UnitOfWork, named string) (uuid.UUID, error) {
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	var found []uuid.UUID
	for _, c := range all {
		if b, ok := a.deps.Connectors.Catalogue.Get(c.Connector); ok && b.PaymentLinks != nil && c.State == storage.ConnectorEnabled {
			found = append(found, c.ID)
		}
	}
	if named != "" {
		id, err := parseID("body.connector_id", named)
		if err != nil {
			return uuid.Nil, err
		}
		for _, f := range found {
			if f == id {
				return id, nil
			}
		}
		return uuid.Nil, problem(CodeLinkConnector, "That connector is not an enabled payment-link connector.", "Enable HitPay under Settings › Integrations")
	}
	if len(found) != 1 {
		return uuid.Nil, problem(CodeLinkConnector, "The organisation has no single enabled payment-link connector.", "Enable HitPay under Settings › Integrations, or name the connector")
	}
	return found[0], nil
}

func registerPaymentLinks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)

	register(a, authz.CreateInvoicePaymentLink, exempt(huma.Operation{
		OperationID: "createInvoicePaymentLink", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/payment-link", Tags: []string{"Sales"},
		Summary: "Ask the payment provider for a payment link on an invoice", DefaultStatus: http.StatusCreated,
		Description: "Asks the organisation's payment-link connector (HitPay) for a payment request for an issued invoice's open balance, read on the server in the " +
			"same transaction, with the invoice's number as its reference, and records it: one live link per invoice (FR-AR-13, FR-PSP-02). When the provider's charge " +
			"under it arrives, auto-reconcile records the receipt once. It is refused for a draft, void or paid invoice, when no provider account collects the " +
			"invoice's currency, and when the invoice has a live link. It is the connector's one write at the provider and a person's alone: the AI and MCP have no tool " +
			"for it. Side effects: one payment request at the provider; the audit event. Idempotent with Idempotency-Key; Prefer: return=preview runs the checks and asks the provider nothing.",
	}, "If-Match", "A payment link is a record of its own and changes nothing of the invoice, whose open balance the server reads in the same transaction (FR-AR-13)."), append([]string{paymentlink.CodeNotIssued, paymentlink.CodeNoAccount, paymentlink.CodeLive, CodeLinkConnector}, writes...),
		func(ctx context.Context, in *createPaymentLinkInput) (*paymentLinkOutput, error) {
			id, err := parseID("invoice_id", in.InvoiceID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvoicePaymentLink, error) {
				cid, err := linkConnector(ctx, a, u, in.Body.ConnectorID)
				if err != nil {
					return InvoicePaymentLink{}, err
				}
				now := a.deps.Clock.Now()
				if storage.IsPreview(ctx) { // a preview checks the request and asks the provider nothing
					_, req, err := paymentlink.Check(ctx, u, cid, id, now)
					if err != nil {
						return InvoicePaymentLink{}, err
					}
					return InvoicePaymentLink{InvoiceID: id.String(), ConnectorID: cid.String(), Reference: req.Reference, Amount: Money{Amount: req.Amount, Currency: req.Currency},
						Status: storage.LinkLive, ExpiresAt: req.ExpiresAt, Payments: []PaymentLinkPayment{}}, nil
				}
				links, _, err := a.deps.Connectors.Runtime.PaymentLinks(ctx, u, cid, connector.LinkRequest)
				if err != nil {
					return InvoicePaymentLink{}, linkConnectorProblem(err)
				}
				l, err := paymentlink.Create(ctx, u, links, cid, id, now)
				if err != nil {
					return InvoicePaymentLink{}, linkProviderProblem(err)
				}
				return paymentLinkOf(l, nil), nil
			})
			if err != nil {
				return nil, err
			}
			return &paymentLinkOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetInvoicePaymentLink, huma.Operation{
		OperationID: "getInvoicePaymentLink", Method: http.MethodGet, Path: "/v1/invoices/{invoice_id}/payment-link", Tags: []string{"Sales"},
		Summary: "Read the payment link of an invoice",
		Description: "The invoice's latest payment link: live or closed, its URL, the amount it was made for, and the charges the provider made under it (FR-AR-13). " +
			"Read-only; not found when the invoice never had one.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *InvoicePath) (*getPaymentLinkOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out InvoicePaymentLink
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			links, err := u.PaymentLinks().OfInvoice(ctx, id)
			if err != nil {
				return err
			}
			if len(links) == 0 {
				return ports.ErrNotFound
			}
			pay, err := u.PaymentLinks().PaymentsOf(ctx, links[0].ID)
			out = paymentLinkOf(links[0], pay)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &getPaymentLinkOutput{Body: out}, nil
	})

	register(a, authz.CancelInvoicePaymentLink, exempt(huma.Operation{
		OperationID: "cancelInvoicePaymentLink", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/payment-link/cancellation", Tags: []string{"Sales"},
		Summary: "Cancel an invoice's live payment link",
		Description: "Closes the invoice's live link in Fyniq with a reason, so a new one can be made (FR-AR-13). Fyniq asks the provider nothing: the request stays payable " +
			"at the provider until it expires [live-verify: provider-side cancellation], and a charge under a cancelled link is never reconciled by itself: a person decides the line. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, "If-Match", "The link is closed once, from live, in one transaction that reads its version: there is no stale write to guard against (FR-AR-13)."), append([]string{CodeLinkClosed}, writes...), func(ctx context.Context, in *cancelPaymentLinkInput) (*paymentLinkOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the link is cancelled"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoicePaymentLink, error) {
			links, err := u.PaymentLinks().OfInvoice(ctx, id)
			if err != nil {
				return InvoicePaymentLink{}, err
			}
			if len(links) == 0 || links[0].Status != storage.LinkLive {
				return InvoicePaymentLink{}, problem(CodeLinkClosed, "The invoice has no live payment link.", "Only a live link is cancelled")
			}
			ctx = storage.WithReason(ctx, reason)
			l, err := u.PaymentLinks().Close(ctx, links[0].ID, storage.LinkCancelled, reason, a.deps.Clock.Now(), links[0].Version)
			if err != nil {
				return InvoicePaymentLink{}, err
			}
			pay, err := u.PaymentLinks().PaymentsOf(ctx, l.ID)
			return paymentLinkOf(l, pay), err
		})
		if err != nil {
			return nil, err
		}
		return &paymentLinkOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})
}

// linkConnectorProblem says in words that the connector could not be used now.
func linkConnectorProblem(err error) error {
	if errors.Is(err, connector.ErrNotEnabled) || errors.Is(err, connector.ErrTrigger) {
		return problem(CodeLinkConnector, "The payment-link connector cannot be used now.", "Check that it is enabled under Settings › Integrations")
	}
	return err
}

// linkProviderProblem keeps a provider's refusal out of the answer: its words are never shown, its body never kept.
func linkProviderProblem(err error) error {
	if errors.Is(err, ports.ErrPaymentRequestRefused) {
		return problem(CodeLinkConnector, "The payment provider refused the payment request.", "Check the connector's key and the provider's status, then try again")
	}
	return err
}
