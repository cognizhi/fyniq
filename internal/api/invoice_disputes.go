package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/disputes"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a disputed invoice and a promise to pay (FR-AR-08, P2 half)

// InvoiceDispute is a customer's dispute of an invoice, or their promise to pay it.
type InvoiceDispute struct {
	ID         string     `json:"id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f95"`
	InvoiceID  string     `json:"invoice_id" format:"uuid" doc:"The invoice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Kind       string     `json:"kind" enum:"dispute,promise" doc:"dispute: the customer disputes an amount; promise: they promise to pay on a day." example:"dispute"`
	Amount     *Money     `json:"amount,omitempty" doc:"A dispute's: the part of the invoice in dispute."`
	Reason     string     `json:"reason,omitempty" doc:"A dispute's: why." example:"The training was not delivered"`
	PromisedOn string     `json:"promised_on,omitempty" format:"date" doc:"A promise's: the day the customer says they will pay." example:"2026-12-05"`
	Status     string     `json:"status" enum:"open,resolved" doc:"open pauses the invoice's reminders: a dispute until it is resolved, a promise until its day has passed." example:"open"`
	Resolution string     `json:"resolution,omitempty" doc:"How it ended, once resolved." example:"Credit note issued for the training"`
	Version    int        `json:"version" doc:"Its version." example:"1"`
	CreatedAt  time.Time  `json:"created_at" doc:"When it was recorded." example:"2026-11-20T09:30:00Z"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty" doc:"When it was resolved." example:"2026-11-28T09:30:00Z"`
}

func disputeOf(d storage.InvoiceDispute) InvoiceDispute {
	out := InvoiceDispute{ID: d.ID.String(), InvoiceID: d.InvoiceID.String(), Kind: d.Kind, Reason: d.Reason, Status: d.Status, Resolution: d.Resolution, Version: d.Version,
		CreatedAt: d.CreatedAt.UTC(), ResolvedAt: utc(d.ResolvedAt)}
	if d.Amount != nil {
		m := moneyOf(*d.Amount, d.Currency)
		out.Amount = &m
	}
	if d.PromisedOn != nil {
		out.PromisedOn = dateOf(*d.PromisedOn)
	}
	return out
}

type createInvoiceDisputeInput struct {
	InvoicePath
	KeyHeader
	PreferHeader
	Body struct {
		Kind   string `json:"kind" enum:"dispute,promise" doc:"dispute or promise." example:"dispute"`
		Amount string `json:"amount,omitempty" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"A dispute's: the part of the invoice in dispute, at most what is open." example:"40.00"`
		Reason string `json:"reason,omitempty" maxLength:"500" doc:"A dispute's: why the customer disputes it." example:"The training was not delivered"`
		On     string `json:"promised_on,omitempty" format:"date" doc:"A promise's: the day, from today to 90 days away." example:"2026-12-05"`
	}
}

type invoiceDisputeOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     InvoiceDispute
}

type invoiceDisputesOutput struct {
	Body struct {
		Records []InvoiceDispute `json:"records" nullable:"false" doc:"The invoice's disputes and promises, newest first."`
	}
}

// InvoiceDisputePath names a dispute or promise in the path.
type InvoiceDisputePath struct {
	DisputeID string `path:"dispute_id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f95"`
}

type resolveInvoiceDisputeInput struct {
	InvoiceDisputePath
	KeyHeader
	PreferHeader
	Body struct {
		Resolution string `json:"resolution" minLength:"1" maxLength:"500" doc:"How it ended." example:"Credit note issued for the training"`
	}
}

func registerInvoiceDisputes(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)
	codes := append([]string{disputes.CodeNotOpen, disputes.CodeAmount, disputes.CodeDate, disputes.CodeOpen, disputes.CodeWords}, writes...)

	register(a, authz.CreateInvoiceDispute, exempt(huma.Operation{
		OperationID: "createInvoiceDispute", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/disputes", Tags: []string{"Sales"},
		Summary: "Record a customer's dispute of an invoice, or their promise to pay", DefaultStatus: http.StatusCreated,
		Description: "Records that the customer disputes part of an issued invoice (an amount, at most what is open, and a reason), or promises to pay it on a day (from today " +
			"to 90 days away) (FR-AR-08). An open dispute pauses the invoice's reminders until it is resolved; an open promise until its day has passed; AR ageing shows the " +
			"disputed amounts apart. An invoice has at most one open dispute and one open promise. It posts nothing and changes nothing of the invoice. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview records nothing.",
	}, "If-Match", "A dispute or promise is a record of its own and changes nothing of the invoice (FR-AR-08)."), codes,
		func(ctx context.Context, in *createInvoiceDisputeInput) (*invoiceDisputeOutput, error) {
			id, err := parseID("invoice_id", in.InvoiceID)
			if err != nil {
				return nil, err
			}
			var on time.Time
			if in.Body.On != "" {
				if on, err = time.Parse(time.DateOnly, in.Body.On); err != nil {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.promised_on", Message: "is not a date", Fix: "Use a date such as 2026-12-05"}})
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvoiceDispute, error) {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return InvoiceDispute{}, err
				}
				d, err := disputes.Make(ctx, u, disputes.Request{InvoiceID: id, Kind: in.Body.Kind, Amount: in.Body.Amount, Reason: in.Body.Reason, On: on}, today, a.deps.Clock.Now())
				return disputeOf(d), err
			})
			if err != nil {
				return nil, err
			}
			return &invoiceDisputeOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetInvoiceDisputes, huma.Operation{
		OperationID: "getInvoiceDisputes", Method: http.MethodGet, Path: "/v1/invoices/{invoice_id}/disputes", Tags: []string{"Sales"},
		Summary:     "Read an invoice's disputes and promises to pay",
		Description: "The invoice's disputes and promises, newest first, open or resolved (FR-AR-08). Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *InvoicePath) (*invoiceDisputesOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		var out invoiceDisputesOutput
		out.Body.Records = []InvoiceDispute{}
		p, _ := authn.From(ctx)
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Documents().Get(ctx, id); err != nil {
				return err
			}
			ds, err := u.InvoiceDisputes().OfInvoice(ctx, id)
			for _, d := range ds {
				out.Body.Records = append(out.Body.Records, disputeOf(d))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	register(a, authz.ResolveInvoiceDispute, huma.Operation{
		OperationID: "resolveInvoiceDispute", Method: http.MethodPost, Path: "/v1/invoice-disputes/{dispute_id}/resolution", Tags: []string{"Sales"},
		Summary: "Resolve a dispute or a promise to pay",
		Description: "Ends an open dispute or promise with how it ended (FR-AR-08): the invoice's reminders then follow its customer's schedule again. A record is " +
			"resolved once; a new one is recorded if the customer says something new. Idempotent with Idempotency-Key; Prefer: return=preview records nothing.",
	}, codes, func(ctx context.Context, in *resolveInvoiceDisputeInput) (*invoiceDisputeOutput, error) {
		id, err := parseID("dispute_id", in.DisputeID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceDispute, error) {
			d, err := disputes.Resolve(ctx, u, id, in.Body.Resolution, a.deps.Clock.Now())
			return disputeOf(d), err
		})
		if err != nil {
			return nil, err
		}
		return &invoiceDisputeOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})
}
