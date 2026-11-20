package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The kinds of record beside an invoice (FR-AR-08, P2): the customer disputes an amount, or promises a day.
const (
	DisputeKind = "dispute"
	PromiseKind = "promise"
)

// InvoiceDispute is a dispute or a promise to pay.
type InvoiceDispute struct {
	ID         uuid.UUID
	InvoiceID  uuid.UUID
	Kind       string
	Amount     *Decimal // a dispute's
	Currency   string
	Reason     string
	PromisedOn *time.Time // a promise's
	Status     string     // "open" or "resolved"
	Resolution string
	Version    int
	CreatedBy  string
	CreatedAt  time.Time
	ResolvedBy string
	ResolvedAt *time.Time
}

// NewInvoiceDispute is a record to make.
type NewInvoiceDispute struct {
	InvoiceID  uuid.UUID
	Kind       string
	Amount     *Decimal
	Currency   string
	Reason     string
	PromisedOn *time.Time
	At         time.Time
}

// InvoiceDisputes are the organisation's disputes and promises. Each change is audited in the same transaction (AUD-01).
type InvoiceDisputes interface {
	// Create records one; CodeInvalid when the invoice has an open record of that kind already.
	Create(ctx context.Context, n NewInvoiceDispute) (InvoiceDispute, error)
	Get(ctx context.Context, id uuid.UUID) (InvoiceDispute, error)
	// OfInvoice are an invoice's records, newest first.
	OfInvoice(ctx context.Context, invoice uuid.UUID) ([]InvoiceDispute, error)
	// Open are every open record of the organisation: what pauses reminders and splits the ageing.
	Open(ctx context.Context) ([]InvoiceDispute, error)
	// Resolve ends an open record at version with how it ended; CodeVersion when it moved on.
	Resolve(ctx context.Context, id uuid.UUID, resolution string, at time.Time, version int) (InvoiceDispute, error)
}
