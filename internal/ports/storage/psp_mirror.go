package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// The Model B mirror (PRD-001 FR-PSP-03, FR-PSP-07; p1-payment-providers 3.1 to 4.4): a provider that bills the customers is mirrored read-only,
// with each invoice's collection state. A provider invoice becomes a draft AR document only through the database's create_provider_invoice_draft.

// The states of a mirrored provider invoice: mirrored (read, no document yet), drafted (its AR document made, not posted), posted (the document
// issued) or parked (a person's to decide: it posts nothing).
const (
	PSPInvoiceMirrored = "mirrored"
	PSPInvoiceDrafted  = "drafted"
	PSPInvoicePosted   = "posted"
	PSPInvoiceParked   = "parked"
)

// PSPCustomer is a provider's customer, and the contact it was made into.
type PSPCustomer struct {
	ID          uuid.UUID
	ConnectorID uuid.UUID
	ProviderID  string
	Name        string
	Email       string
	ContactID   *uuid.UUID
	SeenAt      time.Time
}

// NewPSPCustomer mirrors a provider's customer.
type NewPSPCustomer struct {
	ConnectorID uuid.UUID
	ProviderID  string
	Name, Email string
	SeenAt      time.Time
}

// PSPSubscription is a provider's subscription as it reported it.
type PSPSubscription struct {
	ID                 uuid.UUID
	ConnectorID        uuid.UUID
	ProviderID         string
	ProviderCustomerID string
	Status             string
	Currency           string
	Amount             Decimal // billed each interval, before tax
	IntervalUnit       string
	IntervalCount      int
	PeriodStart        *time.Time
	PeriodEnd          *time.Time
	CancelAtPeriodEnd  bool
	CanceledOn         *time.Time
	SeenAt             time.Time
}

// NewPSPSubscription mirrors a subscription.
type NewPSPSubscription struct {
	ConnectorID        uuid.UUID
	ProviderID         string
	ProviderCustomerID string
	Status             string
	Currency           string
	Amount             Decimal
	IntervalUnit       string
	IntervalCount      int
	PeriodStart        *time.Time
	PeriodEnd          *time.Time
	CancelAtPeriodEnd  bool
	CanceledOn         *time.Time
	SeenAt             time.Time
}

// PSPInvoice is a provider's invoice as mirrored, with its collection state and the AR document made of it.
type PSPInvoice struct {
	ID                     uuid.UUID
	ConnectorID            uuid.UUID
	ProviderID             string
	ProviderCustomerID     string
	ProviderSubscriptionID string
	Number                 string
	Status                 string
	Currency               string
	Total                  Decimal
	AmountPaid             Decimal
	AttemptCount           int
	NextAttemptOn          *time.Time
	Outcome                string
	HostedURL              string
	FinalizedOn            *time.Time
	DueOn                  *time.Time
	Lines                  json.RawMessage
	State                  string
	ParkedReason           string
	DocumentID             *uuid.UUID
	Version                int
	SeenAt                 time.Time
}

// NewPSPInvoice mirrors an invoice: its lines and state are changed only while it has no document.
type NewPSPInvoice struct {
	ConnectorID            uuid.UUID
	ProviderID             string
	ProviderCustomerID     string
	ProviderSubscriptionID string
	Number                 string
	Status                 string
	Currency               string
	Total                  Decimal
	AmountPaid             Decimal
	AttemptCount           int
	NextAttemptOn          *time.Time
	Outcome                string
	HostedURL              string
	FinalizedOn            *time.Time
	DueOn                  *time.Time
	Lines                  json.RawMessage
	SeenAt                 time.Time
}

// PSPMirror is the organisation's mirror of a billing provider. Each change is audited in the same transaction (AUD-01).
type PSPMirror interface {
	// SaveCustomer mirrors a customer: made, or its name and email changed; false when nothing changed.
	SaveCustomer(ctx context.Context, n NewPSPCustomer) (PSPCustomer, bool, error)
	Customer(ctx context.Context, connector uuid.UUID, providerID string) (PSPCustomer, error)
	// SaveSubscription mirrors a subscription; false when nothing changed.
	SaveSubscription(ctx context.Context, n NewPSPSubscription) (PSPSubscription, bool, error)
	Subscriptions(ctx context.Context) ([]PSPSubscription, error)
	// SaveInvoice mirrors an invoice: made, or its collection state (and, while it has no document, its lines) changed; false when nothing changed.
	SaveInvoice(ctx context.Context, n NewPSPInvoice) (PSPInvoice, bool, error)
	Invoice(ctx context.Context, connector uuid.UUID, providerID string) (PSPInvoice, error)
	// Invoices are the mirrored invoices in a state ("" for every one), oldest first.
	Invoices(ctx context.Context, state string) ([]PSPInvoice, error)
	// Park sets a mirrored or drafted invoice aside with why, for a person.
	Park(ctx context.Context, id uuid.UUID, why string) error
	// MakeDraft is the database's create_provider_invoice_draft: the draft AR document of a mirrored invoice, once, and the customer's contact when it
	// had none (made reports it). The audit events of what it made are recorded here.
	MakeDraft(ctx context.Context, connector uuid.UUID, providerID string) (document, contact uuid.UUID, made bool, err error)
	// Posted records that the draft of a mirrored invoice was issued.
	Posted(ctx context.Context, id uuid.UUID) error
	// RecordChargeInvoice records which provider invoice a charge paid, once; false when it was recorded already.
	RecordChargeInvoice(ctx context.Context, connector uuid.UUID, charge, providerInvoice string, at time.Time) (bool, error)
	// InvoiceOfCharge is the provider invoice a charge paid, ports.ErrNotFound when none is recorded.
	InvoiceOfCharge(ctx context.Context, connector uuid.UUID, charge string) (string, error)
	// Documents are the AR documents made of provider invoices: the books' record that a provider bills them, whose reminders never run (FR-PSP-03).
	Documents(ctx context.Context) (map[uuid.UUID]bool, error)
}
