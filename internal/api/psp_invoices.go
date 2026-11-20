package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Model B: provider invoices and subscriptions (FR-PSP-03, FR-PSP-07)
//
// A billing provider's customers, subscriptions and invoices are mirrored by the worker; an open or paid invoice the invoice rules can take becomes a
// draft AR document through the database's create_provider_invoice_draft. The worker posts nothing (rev 0.143): the API posts each draft here, as the
// provider's connector acting for the person who asked, only when its total, GST included, is the provider's to the cent; any other is parked for a
// person. Posting runs when a person presses Reconcile now or Post provider invoices.

// PostedProviderInvoices is what posting the provider's drafts did.
type PostedProviderInvoices struct {
	Posted  int `json:"posted" doc:"Drafts issued: their total is the provider's to the cent." example:"3"`
	Parked  int `json:"parked" doc:"Set aside for a person: the invoice rules' total differs from the provider's, or the invoice cannot be issued." example:"0"`
	Waiting int `json:"waiting" doc:"Left as drafts for now, such as one dated in a closed period." example:"0"`
	// The provider's credit notes against these invoices (FR-PSP-07), issued the same way: only when their total is the provider's to the cent.
	CreditNotesPosted int `json:"credit_notes_posted" doc:"Credit note drafts issued: their total is the provider's to the cent." example:"1"`
	CreditNotesParked int `json:"credit_notes_parked" doc:"Credit note drafts set aside for a person: the total differs, or the invoice rules refuse them." example:"0"`
}

// postProviderInvoices posts the drafted provider invoices of organisation org, each in its own unit of work, as the connector the provider is, on behalf of
// the caller. One that fails does not stop the others.
func (a *API) postProviderInvoices(ctx context.Context, org uuid.UUID) (PostedProviderInvoices, error) {
	var out PostedProviderInvoices
	var drafted []storage.PSPInvoice
	if err := a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		drafted, err = u.PSPMirror().Invoices(ctx, storage.PSPInvoiceDrafted)
		return err
	}); err != nil {
		return out, err
	}
	caller := storage.AuditFrom(ctx).Actor
	for _, inv := range drafted {
		if inv.DocumentID == nil {
			continue
		}
		cur, err := money.Lookup(inv.Currency)
		if err != nil {
			return out, err
		}
		total, err := money.ParseStored(string(inv.Total), cur)
		if err != nil {
			return out, err
		}
		actx := connector.Principal(ctx, storage.InstalledConnector{ID: inv.ConnectorID}, caller)
		actx = storage.WithReason(actx, "Provider invoice "+inv.ProviderID+" (FR-PSP-07)")
		_, err = a.invoices.PostProvided(actx, org, *inv.DocumentID, total)
		park := ""
		var ie *invoice.Error
		var lp ledger.Problems
		switch {
		case err == nil:
		case errors.As(err, &ie) && ie.Code == journal.CodeProviderTotal:
			park = ie.Message + ". " + ie.Fix
		case errors.As(err, &lp) && len(lp) > 0:
			park = "The invoice rules refuse it: " + lp[0].Message
		default:
			out.Waiting++ // a locked period, a lock timeout: the draft waits for the next run
			continue
		}
		if err := a.deps.Store.Do(actx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			if park != "" {
				return u.PSPMirror().Park(ctx, inv.ID, park)
			}
			return u.PSPMirror().Posted(ctx, inv.ID)
		}); err != nil {
			return out, fmt.Errorf("provider invoice %s: %w", inv.ProviderID, err)
		}
		if park != "" {
			out.Parked++
		} else {
			out.Posted++
		}
	}
	if err := a.postProviderCreditNotes(ctx, org, caller, &out); err != nil {
		return out, err
	}
	return out, nil
}

// postProviderCreditNotes issues the drafted credit notes of the provider, as postProviderInvoices does the invoices (FR-PSP-07): a credit note is issued only when its total,
// GST included, is the provider's to the cent; any other is parked for a person, and one that cannot be issued now waits.
func (a *API) postProviderCreditNotes(ctx context.Context, org uuid.UUID, caller *storage.Actor, out *PostedProviderInvoices) error {
	var drafted []storage.PSPCreditNote
	if err := a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		drafted, err = u.PSPMirror().CreditNotes(ctx, storage.PSPInvoiceDrafted)
		return err
	}); err != nil {
		return err
	}
	for _, cn := range drafted {
		if cn.DocumentID == nil {
			continue
		}
		cur, err := money.Lookup(cn.Currency)
		if err != nil {
			return err
		}
		total, err := money.ParseStored(string(cn.Total), cur)
		if err != nil {
			return err
		}
		actx := connector.Principal(ctx, storage.InstalledConnector{ID: cn.ConnectorID}, caller)
		actx = storage.WithReason(actx, "Provider credit note "+cn.ProviderID+" (FR-PSP-07)")
		_, err = a.invoices.PostProvided(actx, org, *cn.DocumentID, total)
		park := ""
		var ie *invoice.Error
		var lp ledger.Problems
		switch {
		case err == nil:
		case errors.As(err, &ie) && ie.Code == journal.CodeProviderTotal:
			park = ie.Message + ". " + ie.Fix
		case errors.As(err, &lp) && len(lp) > 0:
			park = "The invoice rules refuse it: " + lp[0].Message
		default:
			out.Waiting++
			continue
		}
		if err := a.deps.Store.Do(actx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			if park != "" {
				return u.PSPMirror().ParkCreditNote(ctx, cn.ID, park)
			}
			return u.PSPMirror().CreditNotePosted(ctx, cn.ID)
		}); err != nil {
			return fmt.Errorf("provider credit note %s: %w", cn.ProviderID, err)
		}
		if park != "" {
			out.CreditNotesParked++
		} else {
			out.CreditNotesPosted++
		}
	}
	return nil
}

// PaymentProviderCreditNote is a billing provider's credit note against one of its invoices as mirrored (FR-PSP-07): read-only, with the AR credit note made of it.
type PaymentProviderCreditNote struct {
	ID                string    `json:"id" format:"uuid" doc:"The mirror's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5003"`
	Provider          string    `json:"provider" doc:"The provider's connector." example:"stripe"`
	ProviderID        string    `json:"provider_credit_note_id" doc:"The provider's ID." example:"cn_1Q9"`
	ProviderInvoiceID string    `json:"provider_invoice_id" doc:"The provider's invoice it credits." example:"in_1Q9"`
	Number            string    `json:"number,omitempty" doc:"The provider's credit note number." example:"ACME-0001-CN-01"`
	Status            string    `json:"status" enum:"issued,void" doc:"The provider's status of the credit note." example:"issued"`
	Total             Money     `json:"total" doc:"The provider's total, tax included."`
	Reason            string    `json:"reason,omitempty" doc:"The reason the provider gives." example:"order_change"`
	State             string    `json:"state" enum:"mirrored,drafted,posted,parked" doc:"mirrored: read, no AR credit note yet; drafted: its AR credit note made, not issued; posted: issued; parked: for a person." example:"posted"`
	ParkedReason      string    `json:"parked_reason,omitempty" doc:"Why a parked credit note waits." example:"Not made into a credit note: it has a discount (FR-PSP-07). A person decides it."`
	DocumentID        string    `json:"document_id,omitempty" format:"uuid" doc:"The AR credit note made of it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f31"`
	SeenAt            time.Time `json:"seen_at" doc:"When Fyniq last read it." example:"2026-11-20T09:30:00Z"`
}

// PaymentProviderInvoice is a billing provider's invoice as mirrored (FR-PSP-03): read-only, with its collection state and the AR document made of it.
type PaymentProviderInvoice struct {
	ID            string    `json:"id" format:"uuid" doc:"The mirror's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5001"`
	Provider      string    `json:"provider" doc:"The provider's connector." example:"stripe"`
	ProviderID    string    `json:"provider_invoice_id" doc:"The provider's ID." example:"in_1Q9"`
	Number        string    `json:"number,omitempty" doc:"The provider's invoice number." example:"ACME-0001"`
	Customer      string    `json:"customer" doc:"The customer's name, as the provider has it; its ID when it has none." example:"Lumen Health Pte Ltd"`
	ContactID     string    `json:"contact_id,omitempty" format:"uuid" doc:"The contact the provider's customer was made into." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	Status        string    `json:"status" enum:"draft,open,paid,uncollectible,void" doc:"The provider's status of the invoice." example:"open"`
	Total         Money     `json:"total" doc:"The provider's total, tax included."`
	AttemptCount  int       `json:"attempt_count" doc:"The provider's payment attempts so far; Fyniq never retries or charges." example:"2"`
	NextAttemptOn string    `json:"next_attempt_on,omitempty" format:"date" doc:"When the provider will try next." example:"2026-11-27"`
	Outcome       string    `json:"outcome,omitempty" enum:"paid,uncollectible,void" doc:"How the provider says it ended." example:"paid"`
	State         string    `json:"state" enum:"mirrored,drafted,posted,parked" doc:"mirrored: read, no AR document yet; drafted: its AR document made, not issued; posted: issued; parked: for a person." example:"posted"`
	ParkedReason  string    `json:"parked_reason,omitempty" doc:"Why a parked invoice waits." example:"Not made into an invoice: it has a discount (FR-PSP-07). A person decides it."`
	DocumentID    string    `json:"document_id,omitempty" format:"uuid" doc:"The AR document made of it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	HostedURL     string    `json:"hosted_url,omitempty" doc:"The provider's page for the invoice." example:"https://invoice.stripe.com/i/acct/test_1"`
	FinalizedOn   string    `json:"finalized_on,omitempty" format:"date" doc:"The day the provider finalised it." example:"2026-11-01"`
	SeenAt        time.Time `json:"seen_at" doc:"When Fyniq last read it." example:"2026-11-20T09:30:00Z"`
}

// PaymentProviderSubscription is a billing provider's subscription as mirrored: what it bills each interval before tax, and its standing.
type PaymentProviderSubscription struct {
	ID                string `json:"id" format:"uuid" doc:"The mirror's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5002"`
	Provider          string `json:"provider" doc:"The provider's connector." example:"stripe"`
	ProviderID        string `json:"provider_subscription_id" doc:"The provider's ID." example:"sub_1"`
	Customer          string `json:"customer" doc:"The customer's name, as the provider has it; its ID when it has none." example:"Lumen Health Pte Ltd"`
	Status            string `json:"status" doc:"The provider's status: active, past_due, unpaid, canceled, paused, trialing…" example:"active"`
	Amount            Money  `json:"amount" doc:"What it bills each interval, before tax, from its flat-priced items."`
	Interval          string `json:"interval" doc:"How often, such as every month or every 3 months." example:"every month"`
	PeriodEnd         string `json:"current_period_end,omitempty" format:"date" doc:"The end of the period it is in." example:"2026-12-01"`
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end" doc:"Whether it ends at the end of the period." example:"false"`
	CanceledOn        string `json:"canceled_on,omitempty" format:"date" doc:"The day it was cancelled." example:"2026-11-10"`
}

type pspInvoicesOutput struct {
	Body struct {
		Invoices      []PaymentProviderInvoice      `json:"invoices" nullable:"false" doc:"The mirrored invoices, newest first, with the AR document made of each."`
		Subscriptions []PaymentProviderSubscription `json:"subscriptions" nullable:"false" doc:"The mirrored subscriptions."`
		CreditNotes   []PaymentProviderCreditNote   `json:"credit_notes" nullable:"false" doc:"The provider's credit notes against those invoices, newest first, with the AR credit note made of each (FR-PSP-07)."`
	}
}

type postProviderInvoicesInput struct {
	PreferHeader
}

type postProviderInvoicesOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was posted (FR-INT-14)."`
	Body     PostedProviderInvoices
}

func registerPaymentProviderInvoices(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)

	register(a, authz.ListPaymentProviderInvoices, huma.Operation{
		OperationID: "getPaymentProviderInvoices", Method: http.MethodGet, Path: "/v1/payment-providers/invoices", Tags: []string{"Banking"},
		Summary: "Read the invoices and subscriptions a billing provider bills",
		Description: "What Fyniq mirrors of a provider that bills the customers (Model B, FR-PSP-03): each invoice with the provider's status, its collection state (the attempts so far, the next " +
			"retry, the outcome) and the AR document made of it, and each subscription with what it bills each interval. Read-only: Fyniq never retries, charges or changes anything at the provider. " +
			"A provider with no revenue account named on its provider account is not mirrored.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *struct{}) (*pspInvoicesOutput, error) {
		p, _ := authn.From(ctx)
		var out pspInvoicesOutput
		out.Body.Invoices, out.Body.Subscriptions, out.Body.CreditNotes = []PaymentProviderInvoice{}, []PaymentProviderSubscription{}, []PaymentProviderCreditNote{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			names := map[uuid.UUID]string{}
			provider := func(id uuid.UUID) string {
				if n, ok := names[id]; ok {
					return n
				}
				all, _ := u.Connectors().Organisation(ctx)
				for _, c := range all {
					names[c.ID] = c.Connector
				}
				return names[id]
			}
			customers := map[string]storage.PSPCustomer{}
			customer := func(connectorID uuid.UUID, id string) storage.PSPCustomer {
				k := connectorID.String() + "/" + id
				if c, ok := customers[k]; ok {
					return c
				}
				c, _ := u.PSPMirror().Customer(ctx, connectorID, id)
				customers[k] = c
				return c
			}
			who := func(c storage.PSPCustomer, id string) string {
				switch {
				case c.Name != "":
					return c.Name
				case c.Email != "":
					return c.Email
				}
				return id
			}
			invoices, err := u.PSPMirror().Invoices(ctx, "")
			if err != nil {
				return err
			}
			for i := len(invoices) - 1; i >= 0; i-- {
				x := invoices[i]
				c := customer(x.ConnectorID, x.ProviderCustomerID)
				y := PaymentProviderInvoice{ID: x.ID.String(), Provider: provider(x.ConnectorID), ProviderID: x.ProviderID, Number: x.Number, Customer: who(c, x.ProviderCustomerID), Status: x.Status,
					Total: moneyOf(x.Total, x.Currency), AttemptCount: x.AttemptCount, Outcome: x.Outcome, State: x.State, ParkedReason: x.ParkedReason, HostedURL: x.HostedURL, SeenAt: x.SeenAt.UTC()}
				if c.ContactID != nil {
					y.ContactID = c.ContactID.String()
				}
				if x.DocumentID != nil {
					y.DocumentID = x.DocumentID.String()
				}
				if x.NextAttemptOn != nil {
					y.NextAttemptOn = dateOf(*x.NextAttemptOn)
				}
				if x.FinalizedOn != nil {
					y.FinalizedOn = dateOf(*x.FinalizedOn)
				}
				out.Body.Invoices = append(out.Body.Invoices, y)
			}
			notes, err := u.PSPMirror().CreditNotes(ctx, "")
			if err != nil {
				return err
			}
			for i := len(notes) - 1; i >= 0; i-- {
				x := notes[i]
				y := PaymentProviderCreditNote{ID: x.ID.String(), Provider: provider(x.ConnectorID), ProviderID: x.ProviderID, ProviderInvoiceID: x.ProviderInvoiceID, Number: x.Number,
					Status: x.Status, Total: moneyOf(x.Total, x.Currency), Reason: x.Reason, State: x.State, ParkedReason: x.ParkedReason, SeenAt: x.SeenAt.UTC()}
				if x.DocumentID != nil {
					y.DocumentID = x.DocumentID.String()
				}
				out.Body.CreditNotes = append(out.Body.CreditNotes, y)
			}
			subs, err := u.PSPMirror().Subscriptions(ctx)
			for _, s := range subs {
				c := customer(s.ConnectorID, s.ProviderCustomerID)
				every := "every " + s.IntervalUnit
				if s.IntervalCount > 1 {
					every = fmt.Sprintf("every %d %ss", s.IntervalCount, s.IntervalUnit)
				}
				y := PaymentProviderSubscription{ID: s.ID.String(), Provider: provider(s.ConnectorID), ProviderID: s.ProviderID, Customer: who(c, s.ProviderCustomerID), Status: s.Status,
					Amount: moneyOf(s.Amount, s.Currency), Interval: every, CancelAtPeriodEnd: s.CancelAtPeriodEnd}
				if s.PeriodEnd != nil {
					y.PeriodEnd = dateOf(*s.PeriodEnd)
				}
				if s.CanceledOn != nil {
					y.CanceledOn = dateOf(*s.CanceledOn)
				}
				out.Body.Subscriptions = append(out.Body.Subscriptions, y)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	register(a, authz.PostPaymentProviderInvoices, exempt(huma.Operation{
		OperationID: "postPaymentProviderInvoices", Method: http.MethodPost, Path: "/v1/payment-providers/invoices/post", Tags: []string{"Banking"},
		Summary: "Issue the drafts made of a billing provider's invoices",
		Description: "Issues each draft AR document made of a provider's invoice (FR-PSP-07), as the provider's connector acting for the caller, only when its total, GST included, is the provider's " +
			"to the cent: one whose total differs, or the invoice rules refuse, is parked with why for a person, and one that cannot be issued now (a closed period) waits. The worker posts nothing: " +
			"this runs here and when a person presses Reconcile now. Needs the right to post on Invoices. Idempotent by nature (a draft already issued is not posted again); Prefer: return=preview issues nothing.",
	}, "Idempotency-Key", "A draft already issued or parked is not posted again, so a repeat does nothing: the operation is idempotent by its state (FR-PSP-07)."), writes, func(ctx context.Context, in *postProviderInvoicesInput) (*postProviderInvoicesOutput, error) {
		p, _ := authn.From(ctx)
		if wantsPreview(in.Prefer) {
			var n int
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				d, err := u.PSPMirror().Invoices(ctx, storage.PSPInvoiceDrafted)
				n = len(d)
				return err
			}); err != nil {
				return nil, err
			}
			var c int
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				d, err := u.PSPMirror().CreditNotes(ctx, storage.PSPInvoiceDrafted)
				c = len(d)
				return err
			}); err != nil {
				return nil, err
			}
			return &postProviderInvoicesOutput{Status: http.StatusOK, Applied: previewApplied, Body: PostedProviderInvoices{Waiting: n + c}}, nil
		}
		out, err := a.postProviderInvoices(ctx, p.Organisation)
		if err != nil {
			return nil, err
		}
		return &postProviderInvoicesOutput{Status: http.StatusOK, Body: out}, nil
	})
}
