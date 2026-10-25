package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Incoming-funds signals (FR-BNK-16, UC-BNK-02; p1-bank-import-reconcile task 7.3, design D10): what the banks' alerts
// said, each pointing to the open invoice it most likely pays, which shows "likely paid" until the statement line is
// reconciled to it. Home lists the pending ones; a person dismisses one with a reason. A signal posts nothing.

// BankSignal is one incoming-funds alert and where it stands.
type BankSignal struct {
	ID             string      `json:"id" format:"uuid" doc:"The signal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
	MoneyAccountID string      `json:"money_account_id" format:"uuid" doc:"The money account the money arrived in." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Amount         Money       `json:"amount" doc:"What the alert says arrived, in its currency."`
	Payer          string      `json:"payer,omitempty" doc:"Who the alert says paid, as the bank names them." example:"ACME ROBOTICS"`
	Reference      string      `json:"reference,omitempty" doc:"The bank's reference for it." example:"2610-0001"`
	ReceivedAt     time.Time   `json:"received_at" doc:"When the alert arrived." example:"2026-10-26T09:12:00Z"`
	Invoice        *InvoiceRef `json:"invoice,omitempty" doc:"The open invoice it most likely pays: of its amount and currency, its customer named by the payer; absent when there is none, or more than one."`
	Status         string      `json:"status" enum:"pending,closed,expired,dismissed" doc:"pending while it waits for its statement line; closed by the line reconciled to its invoice; expired after the organisation's bank.signal_expiry_days (7 by default); dismissed by a person." example:"pending"`
	ClosedLineID   string      `json:"closed_line_id,omitempty" format:"uuid" doc:"A closed signal: the statement line that closed it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e50"`
	ClosedAt       *time.Time  `json:"closed_at,omitempty" doc:"When it closed, expired or was dismissed." example:"2026-10-27T01:05:00Z"`
}

// LikelyPaid is an open invoice's pending signal: "likely paid" until its statement line arrives (UC-BNK-02).
type LikelyPaid struct {
	SignalID   string    `json:"signal_id" format:"uuid" doc:"The signal." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
	Amount     Money     `json:"amount" doc:"What the alert says arrived."`
	Payer      string    `json:"payer,omitempty" doc:"Who the alert says paid." example:"ACME ROBOTICS"`
	ReceivedAt time.Time `json:"received_at" doc:"When the alert arrived." example:"2026-10-26T09:12:00Z"`
}

// BankSignalPage is a page of signals, latest received first.
type BankSignalPage struct {
	Items      []BankSignal `json:"items" nullable:"false"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// SignalPath names a signal in the path.
type SignalPath struct {
	SignalID string `path:"signal_id" format:"uuid" doc:"The signal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
}

type signalsInput struct {
	PageQuery
	Status string `query:"status" enum:"pending,closed,expired,dismissed" doc:"Only signals with this status: pending for Home's list." example:"pending"`
}

type signalsOutput struct{ Body BankSignalPage }

type dismissSignalInput struct {
	SignalPath
	KeyHeader
	PreferHeader
	Body ReasonBody
}

type signalOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankSignal
}

func signalOf(ctx context.Context, u storage.UnitOfWork, s storage.BankSignal) BankSignal {
	out := BankSignal{ID: s.ID.String(), MoneyAccountID: s.MoneyAccountID.String(), Amount: moneyOf(s.Amount, s.Currency), Payer: s.Payer,
		Reference: s.Reference, ReceivedAt: s.ReceivedAt.UTC(), Status: s.Status, ClosedLineID: idText(s.ClosedLineID), ClosedAt: utc(s.ClosedAt)}
	if s.InvoiceID != nil {
		out.Invoice = &InvoiceRef{ID: s.InvoiceID.String()}
		if d, err := u.Documents().Get(ctx, *s.InvoiceID); err == nil {
			out.Invoice.Number = d.Number
		}
	}
	return out
}

// likelyPaid are the pending signals by the invoice they point to.
func likelyPaid(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]*LikelyPaid, error) {
	pending, err := u.BankMatching().PendingSignals(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]*LikelyPaid{}
	for _, s := range pending {
		if s.InvoiceID != nil {
			out[*s.InvoiceID] = &LikelyPaid{SignalID: s.ID.String(), Amount: moneyOf(s.Amount, s.Currency), Payer: s.Payer, ReceivedAt: s.ReceivedAt.UTC()}
		}
	}
	return out, nil
}

func registerBankSignals(a *API) {
	register(a, authz.ListBankSignals, huma.Operation{
		OperationID: "listBankSignals", Method: http.MethodGet, Path: "/v1/bank-signals", Tags: []string{"Banking"},
		Summary: "List the incoming-funds signals",
		Description: "The banks' incoming-funds alerts, latest first, a page at a time (FR-BNK-16, UC-BNK-02): each with the open invoice it " +
			"most likely pays, which shows \"likely paid\" while the signal is pending. Home lists the pending ones. A signal posts nothing; " +
			"the statement line reconciled to its invoice closes it, and one no line closes expires after the organisation's " +
			"bank.signal_expiry_days. Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *signalsInput) (*signalsOutput, error) {
		p, _ := authn.From(ctx)
		out := &signalsOutput{}
		out.Body.Items = []BankSignal{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.BankMatching().Signals(ctx)
			if err != nil {
				return err
			}
			if in.Status != "" {
				kept := all[:0]
				for _, s := range all {
					if s.Status == in.Status {
						kept = append(kept, s)
					}
				}
				all = kept
			}
			if all, out.Body.NextCursor, err = page(all, func(s storage.BankSignal) string { return s.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			for _, s := range all {
				out.Body.Items = append(out.Body.Items, signalOf(ctx, u, s))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.DismissBankSignal, huma.Operation{
		OperationID: "dismissBankSignal", Method: http.MethodPost, Path: "/v1/bank-signals/{signal_id}/dismiss", Tags: []string{"Banking"},
		Summary: "Dismiss an incoming-funds signal",
		Description: "Dismisses a pending signal with its reason, kept in the audit trail, such as an alert that points to the wrong invoice " +
			"(FR-BNK-16): its invoice stops showing \"likely paid\". Nothing is posted and the invoice is unchanged. A signal that closed, " +
			"expired or was dismissed already is refused. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound,
		storage.CodeBanking, banking.CodeRequest, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *dismissSignalInput) (*signalOutput, error) {
			id, err := parseID("signal_id", in.SignalID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankSignal, error) {
				if _, err := u.BankMatching().Signal(ctx, id); err != nil {
					return BankSignal{}, err
				}
				s, err := banking.DismissSignal(ctx, u, id, in.Body.Reason, a.deps.Clock.Now())
				if err != nil {
					return BankSignal{}, err
				}
				return signalOf(ctx, u, s), nil
			})
			if err != nil {
				return nil, err
			}
			return &signalOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
