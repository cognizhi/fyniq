package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/remitmail"
)

// ------------------------------------------------ a failed remittance advice, sent again (FR-BNK-12, rev 0.160)

// CodeRemittanceNotFailed is a payment whose remittance advice did not fail: sent, waiting, queued or not wanted.
const CodeRemittanceNotFailed = "PB-24"

// RemittanceAgain is why a person sends a failed advice again.
type RemittanceAgain struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is sent again; the audit trail records it." example:"The email service was down on Tuesday"`
}

type remittanceAgainInput struct {
	PaymentBatchPath
	PaymentID string `path:"payment_id" format:"uuid" doc:"The batch's payment to the supplier." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5d20"`
	KeyHeader
	PreferHeader
	Body RemittanceAgain
}

func registerRemittanceAgain(a *API) {
	register(a, authz.SendRemittanceAgain, huma.Operation{
		OperationID: "sendRemittanceAgain", Method: http.MethodPost, Path: "/v1/payment-batches/{batch_id}/payments/{payment_id}/remittance-again",
		Tags: []string{"Purchases"}, Summary: "Send a failed remittance advice again",
		Description: "Queues once more the remittance advice of a released batch's payment to a supplier whose advice gave up after its attempts " +
			"(FR-BNK-12; the owner's decision of 29 Oct 2026, rev 0.160), for a person who can prepare batches. The worker renders and emails it " +
			"as the first time, so a payment voided since, or one sent meanwhile, sends nothing, and a payment gets at most one advice. An advice " +
			"sent, waiting for the match, queued or not wanted (no remittance email) is PB-24. Side effects: the job and an audit event of the " +
			"resend, with the reason. Idempotent with Idempotency-Key; Prefer: return=preview checks it and queues nothing.",
	}, append([]string{CodeNotFound, CodeRemittanceNotFailed, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld}, storage.CodeRetry, storage.CodeLockTimeout),
		func(ctx context.Context, in *remittanceAgainInput) (*batchWriteOutput, error) {
			batchID, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			paymentID, err := parseID("payment_id", in.PaymentID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			ctx = storage.WithReason(ctx, strings.TrimSpace(in.Body.Reason))
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentBatch, error) {
				doc, err := batchDoc(ctx, u, batchID, false)
				if err != nil {
					return PaymentBatch{}, err
				}
				pay, err := u.Documents().Lock(ctx, paymentID)
				if err != nil {
					return PaymentBatch{}, err
				}
				rel, err := u.PaymentBatches().ReleaseOfPayment(ctx, paymentID)
				if err != nil {
					return PaymentBatch{}, err
				}
				if rel == nil || rel.BatchID != batchID || pay.Type != storage.DocumentBillPayment {
					return PaymentBatch{}, problem(CodeNotFound, "The batch made no such payment to a supplier.", "Choose a payment of the batch's suppliers")
				}
				v, err := a.batchView(ctx, u)
				if err != nil {
					return PaymentBatch{}, err
				}
				b, err := a.batchOf(ctx, u, v, doc, false)
				if err != nil {
					return PaymentBatch{}, err
				}
				state := ""
				for _, x := range b.Payments {
					if x.PaymentID == paymentID.String() {
						state = x.Remittance
					}
				}
				if state != "failed" || pay.Status != storage.DocumentPosted {
					if pay.Status != storage.DocumentPosted {
						state = "voided"
					}
					return PaymentBatch{}, problem(CodeRemittanceNotFailed, "The payment's remittance advice is "+strings.ReplaceAll(state, "_", " ")+", not failed.",
						"Send again only an advice that gave up after its attempts")
				}
				if a.deps.Outbox != nil {
					if err := remitmail.Again(ctx, *a.deps.Outbox, u, p.Organisation, paymentID); err != nil {
						return PaymentBatch{}, err
					}
				}
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "remittance.sent_again", ObjectType: string(storage.DocumentBillPayment), ObjectID: paymentID,
					Changes: map[string][2]any{"remittance": {"failed", "queued"}}}); err != nil {
					return PaymentBatch{}, err
				}
				return a.batchOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			return &batchWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentPosted)), Replayed: replayedHeader(w.Replayed),
				Applied: applied(w), Body: w.Body}, nil
		})
}
