package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The payment-provider events (PRD-001 FR-PSP-01; p1-payment-providers design D3): what a provider's verified webhooks
// brought, kept as received, and where each stands. An event posts nothing: it only names an object to fetch.

// The statuses of a provider event: received until the worker works it; then processed, parked for a person (an unknown
// event type, or an object Fyniq cannot post) or failed (to be retried). A parked or failed one becomes processed once it
// is dealt with. The status moves forward only (FY621).
const (
	PSPReceived  = "received"
	PSPProcessed = "processed"
	PSPParked    = "parked"
	PSPFailed    = "failed"
)

// PSPEvent is a stored provider event, without its payload.
type PSPEvent struct {
	ID            uuid.UUID
	ConnectorID   uuid.UUID
	Provider      string
	EventID       string
	EventType     string
	APIVersion    string
	ParserVersion string
	ObjectKind    string // "" for an event that names no object
	ObjectID      string
	PayloadSHA256 string
	ReceivedAt    time.Time
	Status        string
	ParkedReason  string
	ProcessedAt   *time.Time
	LastError     string
}

// NewPSPEvent is a verified webhook to store: Payload is the raw body, sealed with the row's ID as its binding.
type NewPSPEvent struct {
	ID            uuid.UUID
	ConnectorID   uuid.UUID
	Provider      string
	EventID       string
	EventType     string
	APIVersion    string
	ParserVersion string
	ObjectKind    string
	ObjectID      string
	Payload       []byte // sealed
	PayloadSHA256 string
	ReceivedAt    time.Time
}

// PSPEventFilter narrows a list of events: by status ("" for any) and connector (uuid.Nil for any), newest first, after
// an event's ID.
type PSPEventFilter struct {
	Status    string
	Connector uuid.UUID
	Before    uuid.UUID
	Limit     int
}

// PSPEvents are the organisation's provider events. Each change is audited in the same transaction (AUD-01).
type PSPEvents interface {
	// Receive stores a verified webhook; false, and nothing changed, when its event ID was received already (a
	// redelivery, or Stripe's retry): the first row stands.
	Receive(ctx context.Context, n NewPSPEvent) (PSPEvent, bool, error)
	Get(ctx context.Context, id uuid.UUID) (PSPEvent, error)
	// Payload is the sealed raw body of an event, for a person's review or a re-read by a newer parser.
	Payload(ctx context.Context, id uuid.UUID) ([]byte, error)
	// Settle records where an event stands: processed, parked with a reason, or failed with the error (clipped, never a
	// payload). CodeVersion when the status would not move forward.
	Settle(ctx context.Context, id uuid.UUID, status, reason string, at time.Time) (PSPEvent, error)
	// List is a page of events, newest first.
	List(ctx context.Context, f PSPEventFilter) ([]PSPEvent, error)
}

// The payment providers' ledger-side records (PRD-001 FR-PSP-04, FR-PSP-05, FR-PSP-06; p1-payment-providers design D1, D7):
// the setup of a provider's money account, the provider's balance transactions and payouts as it reported them, and its
// own balance as each sweep read it. A money leg is posted from a statement line of the provider's reference, once: the
// line is reconciled once.

// PSPAccount is the setup of a provider's money account: its connector, the bank account its payouts land in (none: they
// wait for a person), the chart accounts its fees and its disputes post to (no disputes account: a dispute waits for a
// person), and how far the daily sweep has made it complete.
type PSPAccount struct {
	MoneyAccountID    uuid.UUID
	ConnectorID       uuid.UUID
	PayoutAccountID   *uuid.UUID
	FeeAccountID      uuid.UUID
	DisputesAccountID *uuid.UUID
	SweptThrough      *time.Time
	SweptAt           *time.Time
	SweepError        string
	Version           int
}

// NewPSPAccount sets a provider money account up, or changes its setup at Version (0 for new).
type NewPSPAccount struct {
	MoneyAccountID    uuid.UUID
	ConnectorID       uuid.UUID
	PayoutAccountID   *uuid.UUID
	FeeAccountID      uuid.UUID
	DisputesAccountID *uuid.UUID
	At                time.Time
}

// The categories a provider transaction is read as.
const (
	PSPCatCharge          = "charge"
	PSPCatRefund          = "refund"
	PSPCatDispute         = "dispute"
	PSPCatDisputeReversal = "dispute_reversal"
	PSPCatFee             = "fee"
	PSPCatPayout          = "payout"
	PSPCatOther           = "other"
)

// PSPTxn is a provider balance transaction as the provider reported it: Gross is signed, money in positive; Fee is what the
// provider keeps, positive; Net is Gross less Fee.
type PSPTxn struct {
	ID          uuid.UUID
	ConnectorID uuid.UUID
	TxnID       string
	Type        string
	Category    string
	SourceKind  string
	SourceID    string
	Currency    string
	Gross       Decimal
	Fee         Decimal
	Net         Decimal
	AvailableOn *time.Time
	PayoutID    string
	ProviderAt  time.Time
	SeenAt      time.Time
}

// NewPSPTxn is a provider balance transaction to record.
type NewPSPTxn struct {
	ConnectorID uuid.UUID
	TxnID       string
	Type        string
	Category    string
	SourceKind  string
	SourceID    string
	Currency    string
	Gross       Decimal
	Fee         Decimal
	Net         Decimal
	AvailableOn *time.Time
	PayoutID    string
	ProviderAt  time.Time
	SeenAt      time.Time
}

// PSPPayout is a provider payout: Amount is positive, money paid out.
type PSPPayout struct {
	ID          uuid.UUID
	ConnectorID uuid.UUID
	PayoutID    string
	Currency    string
	Amount      Decimal
	Status      string
	ArrivalDate *time.Time
	Automatic   bool
	ProviderAt  time.Time
	SeenAt      time.Time
}

// NewPSPPayout is a provider payout to record, or whose standing to update.
type NewPSPPayout struct {
	ConnectorID uuid.UUID
	PayoutID    string
	Currency    string
	Amount      Decimal
	Status      string
	ArrivalDate *time.Time
	Automatic   bool
	ProviderAt  time.Time
	SeenAt      time.Time
}

// PSPBalance is the provider's balance not yet paid out, as a sweep read it.
type PSPBalance struct {
	ID          uuid.UUID
	ConnectorID uuid.UUID
	Currency    string
	Amount      Decimal
	TakenAt     time.Time
}

// PSPLedger is the organisation's payment-provider records. Each change is audited in the same transaction (AUD-01).
type PSPLedger interface {
	Accounts(ctx context.Context) ([]PSPAccount, error)
	// Account is a money account's setup, ports.ErrNotFound when it has none.
	Account(ctx context.Context, moneyAccount uuid.UUID) (PSPAccount, error)
	// SaveAccount sets an account up (version 0) or changes it at version; CodeVersion when it moved on.
	SaveAccount(ctx context.Context, n NewPSPAccount, version int) (PSPAccount, error)
	// Swept records how far the sweep has made the account complete, or why it could not (why non-empty).
	Swept(ctx context.Context, moneyAccount uuid.UUID, through time.Time, at time.Time, why string) error

	// RecordTxn records a provider balance transaction; false, and nothing changed, when it is recorded already.
	RecordTxn(ctx context.Context, n NewPSPTxn) (PSPTxn, bool, error)
	// Txn is a transaction by the provider's ID, ports.ErrNotFound when none is recorded.
	Txn(ctx context.Context, connector uuid.UUID, txnID string) (PSPTxn, error)
	// TxnOfSource is the transaction that is of a source (a payout's own balance transaction), ports.ErrNotFound when none is recorded.
	TxnOfSource(ctx context.Context, connector uuid.UUID, kind, id string) (PSPTxn, error)
	// TxnsOfPayout are the transactions a payout settles, by ID.
	TxnsOfPayout(ctx context.Context, connector uuid.UUID, payoutID string) ([]PSPTxn, error)
	// SettleTxn says which payout settles a transaction, once; false when it has none recorded or already has it.
	SettleTxn(ctx context.Context, connector uuid.UUID, txnID, payoutID string) (bool, error)

	// RecordPayout records a payout, or updates the standing of one recorded; false when nothing changed.
	RecordPayout(ctx context.Context, n NewPSPPayout) (PSPPayout, bool, error)
	Payout(ctx context.Context, connector uuid.UUID, payoutID string) (PSPPayout, error)
	// Payouts are a connector's payouts, newest first.
	Payouts(ctx context.Context, connector uuid.UUID, limit int) ([]PSPPayout, error)

	// RecordBalance adds a reading of the provider's balance.
	RecordBalance(ctx context.Context, connector uuid.UUID, currency string, amount Decimal, at time.Time) (PSPBalance, error)
	// LatestBalance is the newest reading in a currency, ports.ErrNotFound when none was taken.
	LatestBalance(ctx context.Context, connector uuid.UUID, currency string) (PSPBalance, error)
}
