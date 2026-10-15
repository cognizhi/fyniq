// Package storage is the storage port (ADR-T8): the only way the domain reaches the system of
// record. The domain depends on these interfaces; SQL lives only in the PostgreSQL adapter
// (NFR-PORT-01). Every domain command runs in one unit of work: its document, ledger lines, audit
// event and outbox row commit or roll back together, and no repository opens or commits a
// transaction of its own (NFR-PORT-07).
//
// The database enforces the ledger's invariants whatever the caller does (INV-01…06, INV-08);
// failures come back as *Error with a stable code (ENG-23).
package storage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Store opens units of work.
type Store interface {
	// CreateOrganisation creates an organisation, with its ledger revision at 0, in a transaction
	// of its own. BaseCurrency is an ISO 4217 code the store knows.
	CreateOrganisation(ctx context.Context, o NewOrganisation) (Organisation, error)
	// Do runs fn as one unit of work for organisation org: one READ COMMITTED transaction whose
	// first statement sets the tenant (SEC-02). An error from fn, or a failure at commit, rolls
	// everything back. The UnitOfWork must not be used after fn returns.
	Do(ctx context.Context, org uuid.UUID, fn func(ctx context.Context, uow UnitOfWork) error) error
	// Organisations lists every organisation, for jobs that run for each (the worker's role only).
	Organisations(ctx context.Context) ([]uuid.UUID, error)
}

// UnitOfWork gives the repositories of one transaction, all bound to its organisation.
type UnitOfWork interface {
	Organisation(ctx context.Context) (Organisation, error)
	Accounts() Accounts
	Periods() Periods
	Documents() Documents
	Ledger() Ledger
	Balances() Balances
	Audit() Audit
	Outbox() Outbox
	Sweeping() Sweeping
	// AfterCommit runs fn after the unit of work commits, outside its transaction; never after a
	// rollback. The outbox dispatches its rows this way.
	AfterCommit(fn func(ctx context.Context))
}

// Accounts is the chart of accounts (minimal in P0; FR-COA-02 adds the attributes in P1).
type Accounts interface {
	Create(ctx context.Context, a NewAccount) (Account, error)
	Get(ctx context.Context, id uuid.UUID) (Account, error)
	ByCode(ctx context.Context, code string) (Account, error)
	SetActive(ctx context.Context, id uuid.UUID, active bool) (Account, error)
	// List returns every account, by code.
	List(ctx context.Context) ([]Account, error)
}

// Documents are the source documents and their immutable versions (§11.4).
type Documents interface {
	// Create saves a new document as a draft at version 1 with its payload (a JSON object).
	Create(ctx context.Context, d NewDocument) (Document, error)
	// Get returns a document at its current version.
	Get(ctx context.Context, id uuid.UUID) (Document, error)
	// Lock is Get with the document's row locked until commit: ENG-47 step 1, taken before the
	// ledger lock by any command that changes the document.
	Lock(ctx context.Context, id uuid.UUID) (Document, error)
	// SetStatus moves a document draft → posted → reversed; the database refuses any other move.
	SetStatus(ctx context.Context, id uuid.UUID, to DocumentStatus) error
}

// Periods are the accounting periods.
type Periods interface {
	Create(ctx context.Context, p NewPeriod) (Period, error)
	Get(ctx context.Context, id uuid.UUID) (Period, error)
	// Close hard-locks a period (INV-03) under the ENG-47 lock order and records the ledger revision
	// it closed at.
	Close(ctx context.Context, id uuid.UUID) (Period, error)
	// Reopen opens a closed period again, under the same locks.
	Reopen(ctx context.Context, id uuid.UUID) (Period, error)
}

// Ledger is the append-only journal.
type Ledger interface {
	// Append writes one balanced entry and its lines. The database assigns the next ledger
	// revision (ENG-46) and the period, and refuses an unbalanced entry at commit (INV-01).
	Append(ctx context.Context, e NewEntry) (Entry, error)
	Entry(ctx context.Context, id uuid.UUID) (Entry, error)
	// ByIdentity returns the entry posted with a posting identity.
	ByIdentity(ctx context.Context, postingIdentity string) (Entry, error)
	// Revision is the organisation's current ledger revision.
	Revision(ctx context.Context) (int64, error)
}

// Balances is the materialised balances cache (INV-08).
type Balances interface {
	ForPeriod(ctx context.Context, periodID uuid.UUID) ([]Balance, error)
	// Differences compares the cache with a recomputation from the lines; empty means they agree.
	Differences(ctx context.Context) ([]BalanceDifference, error)
	// Rebuild recomputes the cache from the lines under the ledger lock and returns the rows written.
	Rebuild(ctx context.Context) (int64, error)
}

// Decimal is an exact decimal as plain text, such as "-1234.50". Money and rates cross the port as
// text, never as binary floating point (ENG-10); p0-money-posting-engine builds them from Money.
type Decimal string

var decimalPattern = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// Valid reports whether d is a plain decimal: an optional minus, digits, and optional decimals.
func (d Decimal) Valid() bool { return decimalPattern.MatchString(string(d)) }

// NewOrganisation is what CreateOrganisation needs.
type NewOrganisation struct {
	Name         string
	BaseCurrency string
}

// Organisation is a tenant.
type Organisation struct {
	ID             uuid.UUID
	Name           string
	BaseCurrency   string
	BaseMinorUnits int
	Version        int
}

// AccountType is one of the five account types (FR-COA-02).
type AccountType string

// The account types.
const (
	Asset     AccountType = "asset"
	Liability AccountType = "liability"
	Equity    AccountType = "equity"
	Revenue   AccountType = "revenue"
	Expense   AccountType = "expense"
)

// NewAccount is what Accounts.Create needs. A group account is a heading: postings go only to
// leaves (FR-COA-05).
type NewAccount struct {
	Code     string
	Name     string
	Type     AccountType
	ParentID *uuid.UUID
	Group    bool
}

// Account is an account of the chart.
type Account struct {
	ID       uuid.UUID
	Code     string
	Name     string
	Type     AccountType
	ParentID *uuid.UUID
	Group    bool
	Active   bool
	Version  int
}

// PeriodStatus is open, soft-locked or closed (INV-03; soft locks in FR-GL-09).
type PeriodStatus string

// The period statuses.
const (
	PeriodOpen       PeriodStatus = "open"
	PeriodSoftLocked PeriodStatus = "soft_locked"
	PeriodClosed     PeriodStatus = "closed"
)

// NewPeriod is what Periods.Create needs: both dates inclusive, as dates (the time of day is
// ignored).
type NewPeriod struct {
	StartsOn time.Time
	EndsOn   time.Time
}

// Period is an accounting period.
type Period struct {
	ID             uuid.UUID
	StartsOn       time.Time
	EndsOn         time.Time
	Status         PeriodStatus
	ClosedRevision *int64
	Version        int
}

// DocumentType is a kind of source document. P0 has the manual journal.
type DocumentType string

// The document types.
const DocumentJournal DocumentType = "journal"

// DocumentStatus is where a document is in its life (FR-GL-12).
type DocumentStatus string

// The document statuses.
const (
	DocumentDraft    DocumentStatus = "draft"
	DocumentPosted   DocumentStatus = "posted"
	DocumentReversed DocumentStatus = "reversed"
)

// NewDocument is what Documents.Create needs.
type NewDocument struct {
	Type    DocumentType
	Payload []byte
}

// Document is a source document at its current version.
type Document struct {
	ID      uuid.UUID
	Type    DocumentType
	Status  DocumentStatus
	Version int
	Payload []byte
}

// Source is where an entry comes from (ENG-03).
type Source struct {
	Type    string // e.g. "manual_journal", "invoice"
	ID      string
	Version int
}

// NewEntry is what Ledger.Append needs. PostingIdentity never expires: a second entry with the same
// identity is refused (§11.4).
type NewEntry struct {
	PostingDate     time.Time
	PostingIdentity string
	Source          Source
	RuleVersion     string
	ReversesEntryID *uuid.UUID
	Memo            string
	Lines           []NewLine
}

// NewLine is one line of a NewEntry. Debits are positive and credits negative. BaseAmount is
// Amount × FXRate under RND-FX, computed once by the engine (ENG-14).
type NewLine struct {
	AccountID    uuid.UUID
	Currency     string
	Amount       Decimal
	FXRate       Decimal
	FXRateSource string
	BaseAmount   Decimal
	Description  string
}

// Entry is a posted journal entry.
type Entry struct {
	ID              uuid.UUID
	LedgerRevision  int64
	PostingDate     time.Time
	PeriodID        uuid.UUID
	PostingIdentity string
	Source          Source
	RuleVersion     string
	ReversesEntryID *uuid.UUID
	Memo            string
	RecordedAt      time.Time
	Lines           []Line
}

// Line is a posted journal line.
type Line struct {
	ID           uuid.UUID
	LineNo       int
	AccountID    uuid.UUID
	Currency     string
	Amount       Decimal
	FXRate       Decimal
	FXRateSource string
	BaseCurrency string
	BaseAmount   Decimal
	Description  string
}

// Balance is the cached total of one account, period and currency.
type Balance struct {
	AccountID  uuid.UUID
	PeriodID   uuid.UUID
	Currency   string
	Amount     Decimal
	BaseAmount Decimal
	LineCount  int64
}

// BalanceDifference is a cache row that disagrees with the lines; a nil side has no row.
type BalanceDifference struct {
	AccountID        uuid.UUID
	PeriodID         uuid.UUID
	Currency         string
	CachedAmount     *Decimal
	CachedBaseAmount *Decimal
	LinesAmount      *Decimal
	LinesBaseAmount  *Decimal
}

// The stable error codes of the storage port (ENG-23). The database's own codes (FYnnn) are mapped
// to these by the adapter; db/README.md lists both.
const (
	CodeUnbalanced     = "STORAGE-INV-01" // the entry does not balance
	CodeImmutable      = "STORAGE-INV-02" // posted history cannot change
	CodePeriodClosed   = "STORAGE-INV-03" // the posting date is in a closed period
	CodeAccountNotPost = "STORAGE-INV-04" // the account is missing, a group or inactive
	CodeAmount         = "STORAGE-INV-05" // an amount, rate or currency is not valid
	CodeNoTenant       = "STORAGE-INV-06" // no organisation, or a row of another organisation
	CodeNoPeriod       = "STORAGE-PERIOD-01"
	CodePeriodDates    = "STORAGE-PERIOD-02" // dates cannot change, or overlap another period
	CodePeriodState    = "STORAGE-PERIOD-03" // already closed, or not closed
	CodeAccountFixed   = "STORAGE-ACCOUNT-01"
	CodeAccountTree    = "STORAGE-ACCOUNT-02"
	CodeOrganisation   = "STORAGE-ORG-01"
	CodeDocument       = "STORAGE-DOC-01"       // a document's type cannot change, or its version go back
	CodeDocumentStatus = "STORAGE-DOC-02"       // a document cannot move to that status
	CodeAudit          = "STORAGE-AUDIT-01"     // the audit trail cannot change, or a change has no audit event
	CodeOutbox         = "STORAGE-OUTBOX-01"    // an outbox row cannot change what it is, or move to that state
	CodeDuplicate      = "STORAGE-DUPLICATE-01" // posting identity, account code or reversal already used
	CodeRetry          = "STORAGE-RETRY-01"     // deadlock or serialisation failure: retry with the same key
	CodeLockTimeout    = "STORAGE-LOCK-01"      // waited longer than the lock timeout (ENG-47)
	CodeRevision       = "STORAGE-REV-01"
	CodeInvalid        = "STORAGE-INPUT-01" // the request itself is malformed
	CodeStorageFailure = "STORAGE-FAILURE-01"
)

// Error is a storage failure with a stable code, a plain message and, where there is one, a fix.
type Error struct {
	Code    string
	Message string
	Hint    string
	Err     error
}

func (e *Error) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Hint)
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// Retryable reports whether the operation may succeed if the whole unit of work is run again
// (ENG-47: the API retries with the same Idempotency-Key, up to 3 times).
func (e *Error) Retryable() bool { return e.Code == CodeRetry }

// CodeOf returns the stable code of err, or "" when it is not a storage error.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
