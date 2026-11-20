// Package disputes is a customer's dispute of an invoice, or promise to pay it (PRD-001 FR-AR-08, P2 half; p2-customer-portal-subscriptions
// design D4). A posted invoice never changes, so each is a record beside it. An open dispute pauses the invoice's reminders until it is
// resolved, an open promise until its day has passed, and the AR ageing shows the disputed amounts apart. Nothing here posts.
package disputes

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/paymentlink"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Stable codes (ENG-23).
const (
	CodeNotOpen = "AR-DISPUTE-01" // the invoice is not an issued invoice with something open
	CodeAmount  = "AR-DISPUTE-02" // a disputed amount that is not positive, or more than what is open
	CodeDate    = "AR-DISPUTE-03" // a promised day in the past, or more than 90 days away
	CodeOpen    = "AR-DISPUTE-04" // the invoice has an open record of that kind already
	CodeWords   = "AR-DISPUTE-05" // a reason or resolution that is empty or too long
)

// MaxPromiseDays is how far ahead a promised day may be.
const MaxPromiseDays = 90

// Error is a refusal in plain words.
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Message + " (" + e.Fix + ")" }

// Request is a record to make: a dispute (Amount and Reason) or a promise (On).
type Request struct {
	InvoiceID uuid.UUID
	Kind      string
	Amount    string
	Reason    string
	On        time.Time
}

// Make checks and records a dispute or promise against the invoice's open balance as at today.
func Make(ctx context.Context, u storage.UnitOfWork, r Request, today, now time.Time) (storage.InvoiceDispute, error) {
	b, err := paymentlink.OpenInvoice(ctx, u, r.InvoiceID, today)
	var pe *paymentlink.Error
	if errors.As(err, &pe) {
		return storage.InvoiceDispute{}, &Error{CodeNotOpen, "A dispute or a promise is for an issued invoice with something open: it is a draft, void, paid or not an invoice.", "Pick an invoice that is still owed"}
	}
	if err != nil {
		return storage.InvoiceDispute{}, err
	}
	existing, err := u.InvoiceDisputes().OfInvoice(ctx, r.InvoiceID)
	if err != nil {
		return storage.InvoiceDispute{}, err
	}
	for _, e := range existing {
		if e.Status == "open" && e.Kind == r.Kind {
			return storage.InvoiceDispute{}, &Error{CodeOpen, "The invoice has an open " + r.Kind + " already.", "Resolve it first"}
		}
	}
	n := storage.NewInvoiceDispute{InvoiceID: r.InvoiceID, Kind: r.Kind, At: now}
	switch r.Kind {
	case storage.DisputeKind:
		amount, err := money.Parse(r.Amount, b.Open.Currency())
		if err != nil || amount.Sign() <= 0 {
			return storage.InvoiceDispute{}, &Error{CodeAmount, "The disputed amount is not a positive amount in " + b.Open.Currency().Code + ".", "Enter what is in dispute, such as 40.00"}
		}
		if c, _ := amount.Cmp(b.Open); c > 0 {
			return storage.InvoiceDispute{}, &Error{CodeAmount, "The disputed amount is more than the " + b.Open.String() + " still open.", "Dispute at most what is open"}
		}
		reason := strings.TrimSpace(r.Reason)
		if reason == "" || len(reason) > 500 {
			return storage.InvoiceDispute{}, &Error{CodeWords, "A dispute needs its reason, in up to 500 characters.", "Say why the customer disputes it"}
		}
		a := storage.Decimal(amount.String())
		n.Amount, n.Currency, n.Reason = &a, b.Open.Currency().Code, reason
	case storage.PromiseKind:
		if r.On.IsZero() || r.On.Before(today) || r.On.After(today.AddDate(0, 0, MaxPromiseDays)) {
			return storage.InvoiceDispute{}, &Error{CodeDate, "A promised day is from today to 90 days away.", "Enter the day the customer says they will pay"}
		}
		on := r.On
		n.PromisedOn = &on
	default:
		return storage.InvoiceDispute{}, &Error{CodeAmount, "The kind is dispute or promise.", "Choose dispute or promise"}
	}
	return u.InvoiceDisputes().Create(ctx, n)
}

// Resolve ends an open record with how it ended.
func Resolve(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, resolution string, now time.Time) (storage.InvoiceDispute, error) {
	resolution = strings.TrimSpace(resolution)
	if resolution == "" || len(resolution) > 500 {
		return storage.InvoiceDispute{}, &Error{CodeWords, "A resolution is how it ended, in up to 500 characters.", "Say how it was settled"}
	}
	d, err := u.InvoiceDisputes().Get(ctx, id)
	if err != nil {
		return storage.InvoiceDispute{}, err
	}
	if d.Status != "open" {
		return storage.InvoiceDispute{}, &Error{CodeOpen, "That record was resolved already.", "Record a new one if the customer says something new"}
	}
	return u.InvoiceDisputes().Resolve(ctx, id, resolution, now, d.Version)
}

// Pause says which invoices' reminders are paused as at today: those with an open dispute, and those with an open promise whose
// day has not passed.
func Pause(ctx context.Context, u storage.UnitOfWork, today time.Time) (map[uuid.UUID]bool, error) {
	open, err := u.InvoiceDisputes().Open(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, d := range open {
		if d.Kind == storage.DisputeKind || (d.PromisedOn != nil && !d.PromisedOn.Before(today)) {
			out[d.InvoiceID] = true
		}
	}
	return out, nil
}

// Disputed are the open disputes' amounts by invoice.
func Disputed(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]money.Money, error) {
	open, err := u.InvoiceDisputes().Open(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]money.Money{}
	for _, d := range open {
		if d.Kind != storage.DisputeKind || d.Amount == nil {
			continue
		}
		cur, err := money.Lookup(d.Currency)
		if err != nil {
			return nil, err
		}
		m, err := money.ParseStored(string(*d.Amount), cur)
		if err != nil {
			return nil, err
		}
		out[d.InvoiceID] = m
	}
	return out, nil
}
