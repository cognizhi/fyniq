package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Payment links on invoices (PRD-001 FR-AR-13, FR-PSP-02; p2-bank-feeds-payments design D1 to D3): a payment request made at
// the provider for an issued invoice's open balance, one live link per invoice, and the payments the provider's charges made under
// it, which auto-reconcile takes to the invoice.

// The statuses of a payment link: live until it is paid, expires or a person cancels it.
const (
	LinkLive      = "live"
	LinkPaid      = "paid"
	LinkExpired   = "expired"
	LinkCancelled = "cancelled"
)

// PaymentLink is a payment request at a provider for an invoice.
type PaymentLink struct {
	ID                uuid.UUID
	ConnectorID       uuid.UUID
	InvoiceID         uuid.UUID
	ProviderRequestID string
	URL               string
	Reference         string
	Currency          string
	Amount            Decimal
	Status            string
	ClosedReason      string
	ExpiresAt         *time.Time
	Version           int
	CreatedBy         string
	CreatedAt         time.Time
	ClosedAt          *time.Time
}

// NewPaymentLink is a link to record: made at the provider just now.
type NewPaymentLink struct {
	ConnectorID       uuid.UUID
	InvoiceID         uuid.UUID
	ProviderRequestID string
	URL               string
	Reference         string
	Currency          string
	Amount            Decimal
	ExpiresAt         *time.Time
	At                time.Time
}

// PaymentLinkPayment is a charge the provider made under a link.
type PaymentLinkPayment struct {
	ID                uuid.UUID
	LinkID            uuid.UUID
	ConnectorID       uuid.UUID
	ProviderPaymentID string
	Currency          string
	Amount            Decimal
	PaidAt            time.Time
	SeenAt            time.Time
}

// NewPaymentLinkPayment is a payment to record, once per provider charge.
type NewPaymentLinkPayment struct {
	LinkID            uuid.UUID
	ConnectorID       uuid.UUID
	ProviderPaymentID string
	Currency          string
	Amount            Decimal
	PaidAt            time.Time
	SeenAt            time.Time
}

// PaymentLinks are the organisation's payment links and the payments made under them. Each change is audited in the same
// transaction (AUD-01).
type PaymentLinks interface {
	// Create records a link; CodeDuplicate when the invoice has a live link already or the provider's request is recorded.
	Create(ctx context.Context, n NewPaymentLink) (PaymentLink, error)
	// CreateByFunction is Create through the database's narrow function, for the platform's job that holds no right on the table: only for a posted invoice and an enabled connector of the
	// organisation, a live link and nothing else.
	CreateByFunction(ctx context.Context, n NewPaymentLink) (PaymentLink, error)
	Get(ctx context.Context, id uuid.UUID) (PaymentLink, error)
	// OfInvoice are an invoice's links, newest first.
	OfInvoice(ctx context.Context, invoice uuid.UUID) ([]PaymentLink, error)
	// ByRequest is the link of a provider's payment request, ports.ErrNotFound when none.
	ByRequest(ctx context.Context, connector uuid.UUID, request string) (PaymentLink, error)
	// Close closes a live link at version, once, with a reason; CodeVersion when it moved on.
	Close(ctx context.Context, id uuid.UUID, status, reason string, at time.Time, version int) (PaymentLink, error)
	// RecordPayment records a payment under a link; false, and nothing changed, when it is recorded already.
	RecordPayment(ctx context.Context, n NewPaymentLinkPayment) (PaymentLinkPayment, bool, error)
	// PaymentByCharge is the payment recorded for a provider's charge, ports.ErrNotFound when none.
	PaymentByCharge(ctx context.Context, connector uuid.UUID, charge string) (PaymentLinkPayment, error)
	// PaymentsOf are a link's payments, oldest first.
	PaymentsOf(ctx context.Context, link uuid.UUID) ([]PaymentLinkPayment, error)
}
