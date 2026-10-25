package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The banking store (PRD-001 FR-BNK-01, FR-BNK-02, FR-BNK-04, FR-BNK-15, FR-BNK-16; p1-bank-import-reconcile design D1,
// D2): money accounts and where their data comes from, saved column mappings, and the statements, lines and gaps that
// files, emailed reports and API connectors all produce alike.

// The kinds of money account (FR-BNK-01).
const (
	MoneyBank           = "bank"
	MoneyCard           = "card"
	MoneyWallet         = "wallet"
	MoneyPaymentGateway = "payment_gateway"
	MoneyCash           = "cash"
)

// The kinds of source a money account's data comes from (FR-BNK-15).
const (
	MoneySourceFile      = "file"
	MoneySourceEmail     = "email"
	MoneySourceConnector = "connector"
)

// The statement formats (FR-BNK-02): files and reports, or an API connector's pull.
const (
	FormatCSV      = "csv"
	FormatXLSX     = "xlsx"
	FormatOFX      = "ofx"
	FormatCAMT053  = "camt053"
	FormatMT940    = "mt940"
	FormatMT940SAP = "mt940_sap"
	FormatAPI      = "api"
)

// The statuses of a bank line, as the Categorise queue moves it (FR-BNK-06): unreconciled and possible duplicate wait
// for a person; categorised, matched and transferred are done; suspense posted to 2999 and waits to be classified;
// excluded is out of the books (a duplicate, or not the company's).
const (
	LineUnreconciled      = "unreconciled"
	LinePossibleDuplicate = "possible_duplicate"
	LineCategorised       = "categorised"
	LineMatched           = "matched"
	LineTransferred       = "transferred"
	LineSuspense          = "suspense"
	LineExcluded          = "excluded"
)

// MoneyAccount is the register of a chart account marked a money account.
type MoneyAccount struct {
	ID          uuid.UUID
	AccountID   uuid.UUID // the chart account it posts to
	Kind        string
	Name        string
	Institution string // "" for none
	Identifier  string // what statements name it by: the account number, an IBAN or a provider's balance ID; "" for none
	Currency    string
	OpenedOn    *time.Time
	ClosedOn    *time.Time
	CreatedBy   string
	CreatedAt   time.Time
	ChangedAt   time.Time
	Version     int
}

// NewMoneyAccount opens a money account on a chart account.
type NewMoneyAccount struct {
	AccountID   uuid.UUID
	Kind        string
	Name        string
	Institution string
	Identifier  string
	Currency    string
	OpenedOn    *time.Time
	At          time.Time
}

// MoneyAccountChange is what may change on a money account: everything but its chart account and currency.
type MoneyAccountChange struct {
	Kind        string
	Name        string
	Institution string
	Identifier  string
	OpenedOn    *time.Time
	ClosedOn    *time.Time
	At          time.Time
}

// MoneyAccountSource is where a money account's data comes from, from a date (UC-BNK-03 keeps the history).
type MoneyAccountSource struct {
	ID             uuid.UUID
	MoneyAccountID uuid.UUID
	Kind           string
	ConnectorID    *uuid.UUID // an installed organisation connector, for MoneySourceConnector
	Senders        []string   // the bank's registered addresses, for MoneySourceEmail
	ReportFormat   string     // the format its files or reports come in; "" for a connector
	MappingID      *uuid.UUID // the saved mapping of a CSV or XLSX format
	FromDate       time.Time
	SetBy          string
	SetAt          time.Time
}

// NewMoneyAccountSource sets a money account's source from a date after its current one.
type NewMoneyAccountSource struct {
	MoneyAccountID uuid.UUID
	Kind           string
	ConnectorID    *uuid.UUID
	Senders        []string
	ReportFormat   string
	MappingID      *uuid.UUID
	FromDate       time.Time
	At             time.Time
}

// StatementMapping is a saved column mapping for an institution's CSV or XLSX statements (FR-BNK-02).
type StatementMapping struct {
	ID          uuid.UUID
	Name        string
	Institution string
	Format      string
	Spec        []byte // the mapping as JSON (internal/bankfile reads it)
	CreatedBy   string
	CreatedAt   time.Time
	ChangedAt   time.Time
	Version     int
}

// NewStatementMapping saves a mapping; a change to one carries its version.
type NewStatementMapping struct {
	Name        string
	Institution string
	Format      string
	Spec        []byte
	At          time.Time
}

// BankStatement is one imported statement: a file, a report or a connector's pull.
type BankStatement struct {
	ID             uuid.UUID
	MoneyAccountID uuid.UUID
	Source         string
	Format         string
	ParserVersion  string
	PeriodFrom     time.Time
	PeriodTo       time.Time
	Currency       string
	Opening        *Decimal // when the format gives it
	Closing        *Decimal
	LineCount      int
	FileRef        string // the kept upload or report, "" for a connector's pull
	ConnectorID    *uuid.UUID
	ImportedBy     string
	ImportedAt     time.Time
}

// NewBankLine is a line as its statement gives it.
type NewBankLine struct {
	Position       int
	BookingDate    time.Time
	ValueDate      *time.Time
	Amount         Decimal // signed: money in positive, money out negative
	Reference      string  // the bank's own reference, "" for none
	Counterparty   string
	Description    string
	RunningBalance *Decimal
	Fingerprint    string     // hex SHA-256 of the fingerprint fields (FR-BNK-04)
	DuplicateOf    *uuid.UUID // a line already held that this one may duplicate: the line is kept as a possible duplicate
}

// NewBankGap is a break in the balance chain a statement shows (FR-BNK-16).
type NewBankGap struct {
	AfterDate       *time.Time // the previous statement's end, nil for the first
	BeforeDate      time.Time  // this statement's start
	ExpectedOpening *Decimal
	FoundOpening    *Decimal
}

// NewBankStatement is an import: the statement, its lines and the gap it reveals, written in one transaction.
type NewBankStatement struct {
	MoneyAccountID uuid.UUID
	Source         string
	Format         string
	ParserVersion  string
	PeriodFrom     time.Time
	PeriodTo       time.Time
	Currency       string
	Opening        *Decimal
	Closing        *Decimal
	FileRef        string
	ConnectorID    *uuid.UUID
	Lines          []NewBankLine
	Gap            *NewBankGap
	At             time.Time
}

// BankLine is one line of a statement and where it stands.
type BankLine struct {
	ID             uuid.UUID
	StatementID    uuid.UUID
	MoneyAccountID uuid.UUID
	Position       int
	BookingDate    time.Time
	ValueDate      *time.Time
	Amount         Decimal
	Currency       string
	Reference      string
	Counterparty   string
	Description    string
	RunningBalance *Decimal
	Fingerprint    string
	Status         string
	DuplicateOf    *uuid.UUID
	StatusBy       string
	StatusAt       time.Time
	Version        int
}

// BankGap is a break in an account's balance chain, and how it was resolved.
type BankGap struct {
	ID              uuid.UUID
	MoneyAccountID  uuid.UUID
	StatementID     uuid.UUID
	AfterDate       *time.Time
	BeforeDate      time.Time
	Currency        string
	ExpectedOpening *Decimal
	FoundOpening    *Decimal
	FoundAt         time.Time
	ResolvedBy      string // "" while open
	ResolvedAt      *time.Time
	Resolution      string
}

// BankLineFilter narrows the lines listed: an account, statuses, booking dates (inclusive); zero values select all.
type BankLineFilter struct {
	MoneyAccountID *uuid.UUID
	Statuses       []string
	From, To       *time.Time
}

// Banking is the banking store. Every change is audited in its transaction (AUD-01); the database refuses what the
// design does not allow: a money account off a money chart account or in another currency, a changed source or
// statement, a line's facts changed, a status moved off the queue, an undo in a closed period (CodeBanking).
type Banking interface {
	CreateMoneyAccount(ctx context.Context, n NewMoneyAccount) (MoneyAccount, error)
	// ChangeMoneyAccount changes a money account at version: CodeVersion when it changed since.
	ChangeMoneyAccount(ctx context.Context, id uuid.UUID, version int, c MoneyAccountChange) (MoneyAccount, error)
	// MoneyAccount is one money account; ports.ErrNotFound for none.
	MoneyAccount(ctx context.Context, id uuid.UUID) (MoneyAccount, error)
	MoneyAccounts(ctx context.Context) ([]MoneyAccount, error)
	// AddSource sets a money account's source from a date after its current one.
	AddSource(ctx context.Context, n NewMoneyAccountSource) (MoneyAccountSource, error)
	// Sources are a money account's sources, earliest first.
	Sources(ctx context.Context, moneyAccountID uuid.UUID) ([]MoneyAccountSource, error)

	SaveMapping(ctx context.Context, n NewStatementMapping) (StatementMapping, error)
	// ChangeMapping changes a saved mapping at version: CodeVersion when it changed since.
	ChangeMapping(ctx context.Context, id uuid.UUID, version int, n NewStatementMapping) (StatementMapping, error)
	Mappings(ctx context.Context) ([]StatementMapping, error)

	// Import writes a statement with its lines and its gap, audited as one import, and returns them.
	Import(ctx context.Context, n NewBankStatement) (BankStatement, []BankLine, error)
	// Statements are a money account's statements, latest first.
	Statements(ctx context.Context, moneyAccountID uuid.UUID) ([]BankStatement, error)
	// LinesByFingerprint are the lines held with any of these fingerprints, for duplicate detection (FR-BNK-04).
	LinesByFingerprint(ctx context.Context, fingerprints []string) ([]BankLine, error)
	// Lines are the lines the filter selects, by booking date then position.
	Lines(ctx context.Context, f BankLineFilter) ([]BankLine, error)
	// Line is one line; ports.ErrNotFound for none.
	Line(ctx context.Context, id uuid.UUID) (BankLine, error)
	// SetLineStatus moves a line along the queue at version: CodeVersion when it changed since.
	SetLineStatus(ctx context.Context, id uuid.UUID, version int, status string, at time.Time) (BankLine, error)
	// Gaps are a money account's gaps, open ones only when open is true.
	Gaps(ctx context.Context, moneyAccountID uuid.UUID, open bool) ([]BankGap, error)
	// ResolveGap resolves an open gap with its reason: CodeBanking when it is resolved already.
	ResolveGap(ctx context.Context, id uuid.UUID, resolution string, at time.Time) (BankGap, error)

	// Feed is a money account's feed, locked for its sync; ports.ErrNotFound before its first sync.
	Feed(ctx context.Context, moneyAccountID uuid.UUID) (BankFeed, error)
	// Feeds are every money account's feed.
	Feeds(ctx context.Context) ([]BankFeed, error)
	// SaveFeed records where a feed got to: its first record at version 0, else the next version of the one held at
	// version (CodeVersion when it moved since).
	SaveFeed(ctx context.Context, n NewBankFeed, version int) (BankFeed, error)
}

// BankFeed is where a money account fed by an organisation's bank-data connector got to (FR-BNK-15, FR-BNK-16): the
// provider's account it is, the cursor after the last transaction imported, its last sync, and why it stopped.
type BankFeed struct {
	MoneyAccountID  uuid.UUID
	ConnectorID     uuid.UUID
	ProviderAccount string
	Cursor          string
	SyncedAt        time.Time
	LastError       string // "" when the last sync finished
	Version         int
}

// NewBankFeed is a feed's position as a sync leaves it.
type NewBankFeed struct {
	MoneyAccountID  uuid.UUID
	ConnectorID     uuid.UUID
	ProviderAccount string
	Cursor          string
	SyncedAt        time.Time
	LastError       string
}

// ConnectorRoute is an installed organisation connector as a provider's webhook finds it, before any tenant is set.
type ConnectorRoute struct {
	Organisation uuid.UUID
	Connector    string // its id in the catalogue
	State        string
}

// ConnectorRoutes find the organisation of an installed connector, before any tenant is set.
type ConnectorRoutes interface {
	// OrganisationConnectorRoute is the route of installed connector id; ports.ErrNotFound for none.
	OrganisationConnectorRoute(ctx context.Context, id uuid.UUID) (ConnectorRoute, error)
}

// BankRule codes lines in its order (FR-BNK-05); it auto-applies only under an approved request (FR-BNK-14).
type BankRule struct {
	ID                  uuid.UUID
	Name                string
	Position            int
	Conditions          []byte // JSON, as internal/domain/banking reads it
	Action              []byte // JSON: the coding it sets
	Active              bool
	FromAIProposal      bool
	AutoApply           bool
	AutoApplyApprovalID *uuid.UUID
	CreatedBy           string
	CreatedAt           time.Time
	ChangedAt           time.Time
	Version             int
}

// NewBankRule creates a rule; a change to one carries its version.
type NewBankRule struct {
	Name           string
	Position       int
	Conditions     []byte
	Action         []byte
	Active         bool
	FromAIProposal bool
	At             time.Time
}

// The bases of a match (FR-BNK-14): a person made it, or the system principal did, for the one open document that
// matches exactly, for both sides of a transfer, or under an auto-apply rule.
const (
	BasisPerson   = "person"
	BasisExact    = "exact"
	BasisTransfer = "transfer"
	BasisRule     = "rule"
)

// The kinds of match: a category posted from the line, a receipt or bill payment made from it, one recorded by hand
// that it matches, a transfer, or suspense (2999) until it is classified.
const (
	MatchCategorised = "categorised"
	MatchReceipt     = "receipt"
	MatchBillPayment = "bill_payment"
	MatchRecorded    = "recorded"
	MatchTransfer    = "transfer"
	MatchSuspense    = "suspense"
)

// BankMatch is what a line settled or became; a line has one live match.
type BankMatch struct {
	ID         uuid.UUID
	LineID     uuid.UUID
	Kind       string
	DocumentID *uuid.UUID
	EntryID    *uuid.UUID
	TransferID *uuid.UUID
	Basis      string
	RuleID     *uuid.UUID
	MadeBy     string
	MadeAt     time.Time
	UndoneBy   string // "" while live
	UndoneAt   *time.Time
	UndoReason string
}

// NewBankMatch is a match made.
type NewBankMatch struct {
	LineID     uuid.UUID
	Kind       string
	DocumentID *uuid.UUID
	EntryID    *uuid.UUID
	TransferID *uuid.UUID
	Basis      string
	RuleID     *uuid.UUID
	At         time.Time
}

// BankMatchFilter narrows the matches listed: a line's, a document's, or those the system made since a time; live ones
// only unless All.
type BankMatchFilter struct {
	LineID     *uuid.UUID
	DocumentID *uuid.UUID
	SystemOnly bool
	Since      *time.Time
	All        bool
}

// BankTransfer is one transfer between money accounts, in transit until both lines are known (FR-BNK-09).
type BankTransfer struct {
	ID            uuid.UUID
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	FromLineID    *uuid.UUID
	ToLineID      *uuid.UUID
	OnDate        time.Time
	FromCurrency  string
	FromAmount    Decimal // positive: what left the paying account
	ToCurrency    string
	ToAmount      Decimal // positive: what arrived
	Fee           *Decimal
	EntryID       *uuid.UUID
	MadeBy        string
	MadeAt        time.Time
	UndoneBy      string // "" while it stands
	UndoneAt      *time.Time
	UndoReason    string
	Version       int
}

// NewBankTransfer records a transfer.
type NewBankTransfer struct {
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	FromLineID    *uuid.UUID
	ToLineID      *uuid.UUID
	OnDate        time.Time
	FromCurrency  string
	FromAmount    Decimal
	ToCurrency    string
	ToAmount      Decimal
	Fee           *Decimal
	EntryID       *uuid.UUID
	At            time.Time
}

// The statuses of a pending signal (UC-BNK-02).
const (
	SignalPending   = "pending"
	SignalClosed    = "closed"
	SignalExpired   = "expired"
	SignalDismissed = "dismissed" // by a person, with a reason (rev 0.145)
)

// BankSignal is an incoming-funds alert: it points to the likely invoice and posts nothing (FR-BNK-16).
type BankSignal struct {
	ID               uuid.UUID
	MoneyAccountID   uuid.UUID
	Amount           Decimal
	Currency         string
	Payer            string
	Reference        string
	ReceivedAt       time.Time
	InboundMessageID *uuid.UUID
	InvoiceID        *uuid.UUID
	Status           string
	ClosedLineID     *uuid.UUID
	ClosedAt         *time.Time
}

// NewBankSignal records an alert.
type NewBankSignal struct {
	MoneyAccountID   uuid.UUID
	Amount           Decimal
	Currency         string
	Payer            string
	Reference        string
	ReceivedAt       time.Time
	InboundMessageID *uuid.UUID
	InvoiceID        *uuid.UUID
}

// BankReconciliation is one money account's reconciliation for a period (FR-BNK-10).
type BankReconciliation struct {
	ID                uuid.UUID
	MoneyAccountID    uuid.UUID
	PeriodID          uuid.UUID
	Currency          string
	StatementBalance  Decimal
	BookBalance       Decimal
	UnreconciledTotal Decimal
	UnreconciledCount int
	Difference        Decimal // what the unreconciled items do not explain
	LedgerRevision    int64
	PreparedBy        string
	PreparedAt        time.Time
	SignedBy          string // "" while unsigned
	SignedAt          *time.Time
	Version           int
}

// NewBankReconciliation is a reconciliation as prepared; preparing again while unsigned replaces it.
type NewBankReconciliation struct {
	MoneyAccountID    uuid.UUID
	PeriodID          uuid.UUID
	Currency          string
	StatementBalance  Decimal
	BookBalance       Decimal
	UnreconciledTotal Decimal
	UnreconciledCount int
	Difference        Decimal
	LedgerRevision    int64
	At                time.Time
}

// BankMatching is the rest of the banking store: rules, matches, transfers, signals and reconciliations. Every change
// is audited in its transaction (AUD-01); the database refuses what the design does not allow: a rule auto-applying
// without its approved request, a changed or deleted match, a second live match of a line, an undo in a closed period,
// a transfer's legs on the wrong accounts or amounts, a signal reopened, a reconciliation signed by its preparer or
// changed once signed or once its period is closed (CodeBanking).
type BankMatching interface {
	CreateRule(ctx context.Context, n NewBankRule) (BankRule, error)
	// ChangeRule changes a rule at version (not its auto-apply): CodeVersion when it changed since.
	ChangeRule(ctx context.Context, id uuid.UUID, version int, n NewBankRule) (BankRule, error)
	// SetRuleAutoApply marks a rule auto-apply under approval, an approved request for it, at version.
	SetRuleAutoApply(ctx context.Context, id uuid.UUID, version int, approval uuid.UUID, at time.Time) (BankRule, error)
	// Rules are the organisation's rules in their order; Rule is one, ports.ErrNotFound for none.
	Rules(ctx context.Context) ([]BankRule, error)
	Rule(ctx context.Context, id uuid.UUID) (BankRule, error)

	// MakeMatch records a match; the line's status moves with it through Banking.SetLineStatus.
	MakeMatch(ctx context.Context, n NewBankMatch) (BankMatch, error)
	// UndoMatch undoes a live match with its reason: CodeBanking when it is undone already or its period is closed.
	UndoMatch(ctx context.Context, id uuid.UUID, reason string, at time.Time) (BankMatch, error)
	Matches(ctx context.Context, f BankMatchFilter) ([]BankMatch, error)

	RecordTransfer(ctx context.Context, n NewBankTransfer) (BankTransfer, error)
	// CompleteTransfer adds a missing side's line, or the entry, at version: CodeVersion when it changed since.
	CompleteTransfer(ctx context.Context, id uuid.UUID, version int, fromLine, toLine, entry *uuid.UUID) (BankTransfer, error)
	// UndoTransfer marks a transfer undone with its reason at version, once its entry is reversed in the same unit of work
	// (FY223): CodeVersion when it changed since.
	UndoTransfer(ctx context.Context, id uuid.UUID, version int, reason string, at time.Time) (BankTransfer, error)
	Transfer(ctx context.Context, id uuid.UUID) (BankTransfer, error)
	// InTransit are the transfers with one side not yet known, and not undone.
	InTransit(ctx context.Context) ([]BankTransfer, error)

	RecordSignal(ctx context.Context, n NewBankSignal) (BankSignal, error)
	// CloseSignal closes a pending signal (by the line that reconciled it), expires it or dismisses it: CodeBanking when it
	// is not pending.
	CloseSignal(ctx context.Context, id uuid.UUID, status string, line *uuid.UUID, at time.Time) (BankSignal, error)
	PendingSignals(ctx context.Context) ([]BankSignal, error)
	// Signals are every signal, latest received first; Signal is one, ports.ErrNotFound for none.
	Signals(ctx context.Context) ([]BankSignal, error)
	Signal(ctx context.Context, id uuid.UUID) (BankSignal, error)

	// PrepareReconciliation prepares, or prepares again while unsigned, an account's reconciliation for a period.
	PrepareReconciliation(ctx context.Context, n NewBankReconciliation) (BankReconciliation, error)
	// SignReconciliation signs a prepared reconciliation at version, by another person who passed step-up at mfaAt.
	SignReconciliation(ctx context.Context, id uuid.UUID, version int, mfaAt, at time.Time) (BankReconciliation, error)
	// Reconciliations are a period's reconciliations; Reconciliation is one account's, ports.ErrNotFound for none.
	Reconciliations(ctx context.Context, periodID uuid.UUID) ([]BankReconciliation, error)
	Reconciliation(ctx context.Context, moneyAccountID, periodID uuid.UUID) (BankReconciliation, error)
}
