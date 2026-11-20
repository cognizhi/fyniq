package ports

import (
	"context"
	"errors"
	"time"
)

// PaymentProvider is a payment-provider connector's read-only view of the provider's objects (PRD-001 FR-PSP-01,
// FR-PSP-03; p1-payment-providers design D2): an object by its provider ID, a period's objects of one kind, and the
// provider's balance. It has no method that writes: Fyniq never charges, refunds, retries or cancels at a provider
// (FR-INT-27), and a test holds the method set to these. The provider's balance transactions reach the books as the
// statement of a money account through BankData; this port is for what the books need beside them: what a charge or a
// payout is of, and the invoices Model B mirrors.
type PaymentProvider interface {
	// Name names the provider, as its webhooks and ledger references do: "stripe" or "hitpay".
	Name() string
	// Object reads the object kind id from the provider: what an event only names (FR-PSP-01). ErrProviderNotFound when
	// the provider has no such object.
	Object(ctx context.Context, kind ProviderKind, id string) (ProviderObject, error)
	// List is a page of the objects of one kind created from from up to but not including to, oldest first, after cursor
	// ("" for the first page); the completeness sweep fetches what the webhooks missed with it (FR-PSP-01).
	List(ctx context.Context, kind ProviderKind, from, to time.Time, cursor string) (ProviderPage, error)
	// Balance is the provider's balance not yet paid out, per currency (FR-PSP-06, check C.2 #50).
	Balance(ctx context.Context) ([]ProviderBalance, error)
}

// ProviderKind is a kind of provider object.
type ProviderKind string

// The kinds of provider object Fyniq reads.
const (
	ProviderCharge       ProviderKind = "charge"
	ProviderRefund       ProviderKind = "refund"
	ProviderDispute      ProviderKind = "dispute"
	ProviderPayout       ProviderKind = "payout"
	ProviderInvoice      ProviderKind = "invoice"
	ProviderCreditNote   ProviderKind = "credit_note"
	ProviderSubscription ProviderKind = "subscription"
	ProviderCustomer     ProviderKind = "customer"
	// ProviderBalanceTransaction is a line of the provider's balance, which BankData brings as statement lines; it is a
	// kind only as a link, such as the balance transactions a payout settles.
	ProviderBalanceTransaction ProviderKind = "balance_transaction"
)

// ProviderEvent is a verified webhook as the provider's parser reads it (FR-PSP-01): which object it names and what kind
// of event it is. It carries no data to post: an event only tells Fyniq which object to fetch.
type ProviderEvent struct {
	// ID is the event's ID for deduplication on receipt: Stripe's event ID, or, for a provider that sends none (HitPay),
	// one made of the event's type, its object's ID and a digest of its body, so a redelivery repeats it and a changed
	// object does not.
	ID         string
	Type       string
	APIVersion string
	Kind       ProviderKind // what the event names; empty for an event with no object to fetch
	ObjectID   string
	// Known says the parser knows the event type; an unknown type is stored and parked for a person (UC-PSP-01).
	Known bool
	// Handled says a known event is one the posting rules act on; a known event that is not handled is informational and
	// is recorded as processed.
	Handled bool
}

// ProviderObject is an object as the provider reports it now, in the provider's neutral terms. Amounts are exact decimal
// strings in major units of Currency (ENG-12). Links name the objects this one belongs to, by kind, such as a charge's
// invoice or a payout's balance transactions; Raw is the provider's JSON as it answered, for the mirror and for a
// person's review, and never for a log.
type ProviderObject struct {
	Kind     ProviderKind
	ID       string
	Status   string
	Created  time.Time
	Amount   string // gross, as the provider reports it; "" for an object with none
	Currency string
	// Of a balance transaction (ProviderBalanceTransaction): its provider type, how Fyniq reads it (one of storage's PSPCat
	// categories: charge, refund, dispute, dispute_reversal, fee, payout or other), the fee the provider keeps and the net,
	// as exact decimal strings (Amount is the gross, signed), and the date it becomes available.
	Type, Category, Fee, Net string
	AvailableOn              time.Time
	Links                    map[ProviderKind][]string
	Raw                      []byte
	// Typed data of the objects Model B mirrors (FR-PSP-03): an invoice, a customer or a subscription, read as the adapter understood it. Nil for any
	// other kind. Amounts are exact decimal strings, never floats.
	Invoice      *InvoiceData
	Customer     *CustomerData
	Subscription *SubscriptionData
}

// InvoiceData is a billing provider's invoice as Fyniq mirrors it (FR-PSP-03, FR-PSP-07): its totals, its lines with their service periods, and its
// collection state. Unsupported says why it cannot be made into an AR document by the invoice rules (a discount, a credit line, more lines than one page,
// lines and tax that do not add up to the total): empty when it can.
type InvoiceData struct {
	CustomerID     string
	CustomerName   string // as the invoice carries them: the provider's customer at finalisation
	CustomerEmail  string
	SubscriptionID string
	Number         string
	Status         string // draft, open, paid, uncollectible or void
	HostedURL      string
	Total          string
	AmountPaid     string
	AttemptCount   int
	NextAttempt    *time.Time
	Finalized      *time.Time
	Due            *time.Time
	Lines          []InvoiceLine
	Unsupported    string
}

// InvoiceLine is a line of a provider's invoice: its amount before tax and the service period it covers.
type InvoiceLine struct {
	ID          string
	Description string
	Amount      string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// CustomerData is a provider's customer.
type CustomerData struct{ Name, Email string }

// SubscriptionData is a provider's subscription: what it bills each interval before tax, and its standing.
type SubscriptionData struct {
	CustomerID        string
	Status            string
	Amount            string
	IntervalUnit      string // day, week, month or year
	IntervalCount     int
	PeriodStart       *time.Time
	PeriodEnd         *time.Time
	CancelAtPeriodEnd bool
	CanceledAt        *time.Time
}

// ProviderPage is a page of objects: Cursor continues after the last, and More says another page waits.
type ProviderPage struct {
	Objects []ProviderObject
	Cursor  string
	More    bool
}

// ProviderBalance is the provider's balance in one currency not yet paid out: money it holds for the organisation
// (available and pending), as an exact decimal string.
type ProviderBalance struct {
	Currency string
	Amount   string
}

// ErrProviderNotFound is an object the provider does not have.
var ErrProviderNotFound = errors.New("the provider has no such object")
