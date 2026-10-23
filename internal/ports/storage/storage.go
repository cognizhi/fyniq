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
	//
	// Do called with the ctx that another Do gave its fn joins that unit of work: fn runs in the same
	// transaction and commits with it. It is an error when the organisations differ. The API joins a
	// domain command to the unit of work that claims the request's Idempotency-Key (FR-INT-08), so
	// the key, the change and the saved response commit together or not at all.
	//
	// Under WithPreview the unit of work is a preview (FR-INT-14): fn runs, then every check the commit
	// would run (the audit events, and the deferred checks such as INV-01's balance), and then
	// everything rolls back. Do then returns ErrPreviewed, or the error a commit would have returned.
	// AfterCommit functions never run for a preview.
	Do(ctx context.Context, org uuid.UUID, fn func(ctx context.Context, uow UnitOfWork) error) error
	// Organisations lists every organisation, for jobs that run for each (the worker's role only).
	Organisations(ctx context.Context) ([]uuid.UUID, error)
}

// UnitOfWork gives the repositories of one transaction, all bound to its organisation.
type UnitOfWork interface {
	Organisation(ctx context.Context) (Organisation, error)
	// UpdateOrganisation replaces the organisation's profile when its version is still version, and
	// returns it at its next version; CodeVersion when it changed since (FR-INT-09). It is audited.
	UpdateOrganisation(ctx context.Context, version int, p OrganisationProfile) (Organisation, error)
	Accounts() Accounts
	Periods() Periods
	Closes() Closes
	Documents() Documents
	Ledger() Ledger
	Balances() Balances
	Audit() Audit
	Members() Members
	MemberInvitations() MemberInvitations
	SuperAdmins() SuperAdmins
	Idempotency() Idempotency
	RateLimits() RateLimits
	GSTRegistrations() GSTRegistrations
	Allocations() Allocations
	Sequences() Sequences
	Contacts() Contacts
	Items() Items
	Evidence() EvidenceFiles
	Settings() Settings
	Approvals() Approvals
	ApprovalPolicies() ApprovalPolicies
	TeamModes() TeamModes
	Passkeys() Passkeys
	ReviewLog() ReviewLog
	Packs() Packs
	EmailMessages() EmailMessages
	Outbox() Outbox
	Sweeping() Sweeping
	RecurringJournals() RecurringJournals
	RecurringBills() RecurringBills
	WithholdingDecisions() WithholdingDecisions
	BillPlans() BillPlans
	AccountingBasis() AccountingBasis
	FXRates() FXRates
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
	// Page returns at most limit accounts with a code after after (all when after is ""), by code:
	// one page of a cursor list (FR-INT-10).
	Page(ctx context.Context, after string, limit int) ([]Account, error)
	// Count returns how many accounts the organisation has.
	Count(ctx context.Context) (int64, error)
	// Update changes an account at its version, and returns it at its next: CodeVersion when it changed
	// since. The database keeps its code, type and role, and a system account active.
	Update(ctx context.Context, id uuid.UUID, version int, c AccountChange) (Account, error)
}

// Documents are the source documents and their immutable versions (§11.4).
type Documents interface {
	// Create saves a new document as a draft at version 1 with its payload (a JSON object).
	Create(ctx context.Context, d NewDocument) (Document, error)
	// Get returns a document at its current version.
	Get(ctx context.Context, id uuid.UUID) (Document, error)
	// Version returns a document at one of its versions, with that version's payload and who saved it: the replay
	// re-runs a rule over the version that posted (ENG-06). ports.ErrNotFound when it has no such version.
	Version(ctx context.Context, id uuid.UUID, version int) (Document, error)
	// Lock is Get with the document's row locked until commit: ENG-47 step 1, taken before the
	// ledger lock by any command that changes the document.
	Lock(ctx context.Context, id uuid.UUID) (Document, error)
	// SetStatus moves a document draft → posted → reversed; the database refuses any other move. Posting
	// records who posted it and when (FY053).
	SetStatus(ctx context.Context, id uuid.UUID, to DocumentStatus) error
	// Revise saves a new version of a document with its payload, which becomes the current one.
	Revise(ctx context.Context, id uuid.UUID, payload []byte) (Document, error)
	// Edit saves the next version of a posted document with its payload, saved by preparer ("type:id"; empty for the
	// actor), and makes it the current one with the actor as who posted the edit, now (FR-GL-12). The caller reverses
	// the entry of the version it had and posts the new one in the same unit of work: the database refuses the commit
	// otherwise (CodeDocumentEdit).
	Edit(ctx context.Context, id uuid.UUID, payload []byte, preparer string) (Document, error)
	// Page lists documents of a type at their current version, newest date first, after a cursor.
	Page(ctx context.Context, f DocumentFilter) ([]Document, error)
	// ScheduledRevaluation saves a period's month-end revaluation as the scheduler does (FR-GL-04, FR-AUT-02; the
	// database's create_scheduled_revaluation): a posted fx_revaluation document whose version 1 is payload, saved and
	// posted by "scheduler:fx.revalue" at the clock's now. CodeFXRevaluation when the period is not open, or the payload
	// is not that period's revaluation (its period_id, date and reverse_on). The caller appends its entry in the same
	// unit of work.
	ScheduledRevaluation(ctx context.Context, periodID uuid.UUID, payload []byte) (Document, error)
	// DueReversals lists the posted journals whose automatic reversal is dated on or before day (YYYY-MM-DD), earliest
	// first, at most limit (FR-GL-02).
	DueReversals(ctx context.Context, day string, limit int) ([]uuid.UUID, error)
	// SpreadBills lists, by ID after the cursor afterID, at most limit posted bills whose current version spreads a
	// line's cost over months that run past from (YYYY-MM-DD), or that are dated on or after it: the bills whose
	// releases a period from that day on may still take (FR-GL-03, ENG-05).
	SpreadBills(ctx context.Context, from string, afterID *uuid.UUID, limit int) ([]uuid.UUID, error)
	// BillsOfOrder lists the bills, drafts and posted, not reversed, whose current version has a line made from the
	// purchase order (FR-AP-01), by ID.
	BillsOfOrder(ctx context.Context, orderID uuid.UUID) ([]Document, error)
	// WaitingReversals lists, earliest reversal first, the posted journals whose automatic reversal waits because no
	// period that is not closed covers its date (FR-GL-02; rev 0.58): at most limit of them, after the cursor's reversal
	// date and ID when afterID is set.
	WaitingReversals(ctx context.Context, afterOn string, afterID *uuid.UUID, limit int) ([]Document, error)
	// OpeningBalance is the organisation's opening balances document at its current version, or ports.ErrNotFound.
	// Create makes it, one at most: a second is refused (CodeDuplicate).
	OpeningBalance(ctx context.Context) (Document, error)
}

// DocumentFilter narrows a list of documents. Empty fields do not narrow it.
type DocumentFilter struct {
	Type     DocumentType
	Status   DocumentStatus
	From, To string // payload dates, YYYY-MM-DD, inclusive
	Search   string // matched in the memo or the number, as typed
	// After is the cursor: the date and ID of the last document of the previous page.
	AfterDate string
	AfterID   *uuid.UUID
	Limit     int
}

// Periods are the accounting periods.
type Periods interface {
	Create(ctx context.Context, p NewPeriod) (Period, error)
	Get(ctx context.Context, id uuid.UUID) (Period, error)
	// List returns every period, earliest first.
	List(ctx context.Context) ([]Period, error)
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
	// Entries are the entries posted after a ledger revision, in revision order, with their lines, at most limit: the
	// replay walks them a page at a time (ENG-06, QA-ENG-06).
	Entries(ctx context.Context, afterRevision int64, limit int) ([]Entry, error)
	// ByIdentity returns the entry posted with a posting identity.
	ByIdentity(ctx context.Context, postingIdentity string) (Entry, error)
	// Revision is the organisation's current ledger revision.
	Revision(ctx context.Context) (int64, error)
	// Lock takes the ledger lock (ENG-47 step 2) until commit, and returns the current revision: no entry posts
	// meanwhile.
	Lock(ctx context.Context) (int64, error)
	// AccountLines are an account's lines dated from..to, in posting order, at most limit (FR-GL-11). Only the
	// balance service reads them (ENG-04).
	AccountLines(ctx context.Context, account uuid.UUID, from, to time.Time, limit int) ([]AccountLine, error)
}

// AccountLine is one posted line of an account, with its entry's provenance (ENG-03) and its source's number.
type AccountLine struct {
	EntryID         uuid.UUID
	LedgerRevision  int64
	PostingDate     time.Time
	Source          Source
	Memo            string
	ReversesEntryID *uuid.UUID
	LineNo          int
	Currency        string
	Amount          Decimal
	BaseAmount      Decimal
	Description     string
	Number          string
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
	Profile        OrganisationProfile
}

// OrganisationProfile is what the organisation's documents print about it, and how its financial years
// run (FR-ORG-01, FR-ORG-02). Empty text and nil dates are not set.
type OrganisationProfile struct {
	LegalName, UEN, RegisteredAddress, MailingAddress, Email, Phone, Website string
	// The bank and PayNow details customers pay to: masked in logs and audit events (SEC-15).
	BankName, BankAccountName, BankAccountNumber, PayNowUEN string
	DocumentFooter                                          string
	FYStartMonth                                            int        // the month a financial year starts in, 1 to 12
	BooksStart, FirstYearEnd                                *time.Time // dates: the first financial year runs from one to the other
	TimeZone                                                string     // an IANA time zone, such as Asia/Singapore
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
	Role     AccountRole // fixed once the account exists
	AccountAttributes
}

// AccountAttributes are an account's attributes beyond its code, name and type (FR-COA-02). Empty text is
// not set.
type AccountAttributes struct {
	ReportGroup    string
	CashFlowLine   string
	Function       string // COGS, R&D, S&M, G&A or Other
	MoneyAccount   bool   // a bank, cash, card or wallet account money moves through
	DefaultGSTCode string
	Currency       string // a bank account's currency
	Control        bool   // has a sub-ledger (AR, AP, a director's account)
	PayrollCost    bool
	Guidance       string // shown when someone picks the account
	Revalue        bool   // its foreign-currency balances are revalued at month end: a monetary item (FR-GL-04)
}

// AccountRole marks an account the rules treat specially; empty for most accounts.
type AccountRole string

// The account roles. A share-capital account changes only through the register of share issues or
// opening balances: a manual journal line to it is refused (INV-13, from P0).
const RoleShareCapital AccountRole = "share-capital"

// RoleRetainedEarnings is the account that carries the profit or loss of every earlier financial year (FR-GL-10).
const RoleRetainedEarnings AccountRole = "retained-earnings"

// RoleUnrealisedFX is the account the month-end revaluation posts its gains and losses to (FR-GL-04; 7040).
const RoleUnrealisedFX AccountRole = "unrealised-fx"

// SystemRoles are the roles of B.5's system accounts (FR-COA-03, rev 0.32): each is on one account,
// share capital on two or more. The engine finds a system account by its role.
var SystemRoles = []AccountRole{"accounts-receivable", "accounts-payable", "gst-control", "fx-rounding", "unrealised-fx",
	"deferred-revenue", "prepayments", "salaries-payable", "cpf-sdl-payable", "employer-cpf", "sdl", "depreciation",
	"accumulated-depreciation", "suspense", "retained-earnings", "expense-claims-payable", "income-tax-payable", "income-tax-expense",
	"director-loan", "deferred-remuneration", RoleShareCapital, "capital-contribution", "withholding-tax-payable"}

// AccountChange is what may change on an account: never its code, type or role (FR-COA-03).
type AccountChange struct {
	Name     string
	ParentID *uuid.UUID
	Group    bool
	Active   bool
	AccountAttributes
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
	Role     AccountRole
	AccountAttributes
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
const (
	DocumentJournal DocumentType = "journal"
	// DocumentOpeningBalance is the organisation's opening balances, one at most (FR-GL-06).
	DocumentOpeningBalance DocumentType = "opening_balance"
	// DocumentBill is a supplier's bill (FR-AP-02; p1-purchases).
	DocumentBill DocumentType = "bill"
	// DocumentBillPayment is a payment of suppliers' bills recorded by hand (FR-AP-02, FR-AR-18; p1-purchases).
	DocumentBillPayment DocumentType = "bill_payment"
	// DocumentVendorCredit is a supplier's credit note (FR-AP-03; p1-purchases).
	DocumentVendorCredit DocumentType = "vendor_credit"
	// DocumentCreditApplication applies a supplier credit to a bill (FR-AP-03, FR-AR-18; p1-purchases).
	DocumentCreditApplication DocumentType = "credit_application" // #nosec G101 -- a document type's name, not a credential
	// DocumentSupplierRefund is money a supplier paid back against a credit (FR-AP-03; p1-purchases).
	DocumentSupplierRefund DocumentType = "supplier_refund"
	// DocumentPurchaseOrder is an order to a supplier, with quantities and unit prices (FR-AP-01; p1-purchases): it posts
	// nothing; posted is approved, and reversed is closed.
	DocumentPurchaseOrder DocumentType = "purchase_order"
)

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
	Number  string // its document number, once it has one (FR-ORG-04); empty before
	// SavedBy is who saved the current version and PostedBy who posted it, as "type:id" (FR-GL-01); empty for
	// documents from before they were recorded.
	SavedBy  string
	PostedBy string
	PostedAt *time.Time
	Date     string // the payload's date, in a list
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
	CodeShareCapital   = "STORAGE-INV-13" // a manual journal line to a share-capital account
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
	CodeDocument       = "STORAGE-DOC-01"        // a document's type cannot change, or its version go back
	CodeDocumentStatus = "STORAGE-DOC-02"        // a document cannot move to that status
	CodeDocumentActor  = "STORAGE-DOC-03"        // a version or a posting names no actor, or who posted it changed
	CodeDocumentEdit   = "STORAGE-DOC-04"        // an edit or a reversal of a posted document posts its entries with it; a reversed one never changes
	CodeRecurring      = "STORAGE-RECURRING-01"  // a scheduled draft is made only for an active recurring journal's next occurrence, once; a version never changes
	CodeAccounting     = "STORAGE-ACCOUNTING-01" // a framework or policy version never changes; it is confirmed or approved once, never by its writer
	CodeClose          = "STORAGE-CLOSE-01"      // a close is attested by the one who ran its checks, or on checks not clean or of another period; or a close record changed
	CodeVariance       = "STORAGE-CLOSE-02"      // a variance explained in a period that is not open, or of an account not income or expense; or an explanation changed
	CodeChecklist      = "STORAGE-CLOSE-03"      // a checklist task marked in a period that is not open; or a checklist record changed
	CodeFXRate         = "STORAGE-FX-01"         // a rate of the base currency, a monthly rate not on a month's first day, a rate changed, or a feed rate changed
	CodeFXRevaluation  = "STORAGE-FX-02"         // the scheduler saves a revaluation only of an open period, as that period's
	CodeAllocation     = "STORAGE-ALLOCATION-01" // an allocation settles a bill from a bill payment, never changes, and is reversed exactly
	CodeBillRelease    = "STORAGE-RELEASE-01"    // a release releases the spread of a posted bill's current version
	CodePurchaseOrder  = "STORAGE-PO-01"         // a purchase order posts nothing: no entry names one
	CodeWithholding    = "STORAGE-WHT-01"        // a withholding decision never changes: a later one replaces it
	CodeBillPlan       = "STORAGE-PLAN-01"       // a bill's payment plan never changes: a later one replaces it
	CodeAudit          = "STORAGE-AUDIT-01"      // the audit trail cannot change, or a change has no audit event
	CodeOutbox         = "STORAGE-OUTBOX-01"     // an outbox row cannot change what it is, or move to that state
	CodeSetup          = "STORAGE-SETUP-01"      // the setup code is not valid, or setup is done
	CodeMembership     = "STORAGE-MEMBER-01"     // a membership or role assignment cannot be made or changed that way
	CodeSuperAdmin     = "STORAGE-SUPERADMIN-01" // at most two Super admins in P0, never none; removed, never changed
	CodeGrant          = "STORAGE-GRANT-01"      // an invitation or recovery code is not valid, used or expired
	CodeDuplicate      = "STORAGE-DUPLICATE-01"  // posting identity, account code or reversal already used
	CodeRetry          = "STORAGE-RETRY-01"      // deadlock or serialisation failure: retry with the same key
	CodeLockTimeout    = "STORAGE-LOCK-01"       // waited longer than the lock timeout (ENG-47)
	CodeRevision       = "STORAGE-REV-01"
	CodeInvalid        = "STORAGE-INPUT-01" // the request itself is malformed
	CodeStorageFailure = "STORAGE-FAILURE-01"
	CodePreviewed      = "STORAGE-PREVIEW-01"  // not a failure: a preview passed every check and was rolled back
	CodeVersion        = "STORAGE-VERSION-01"  // the record changed since it was read (FR-INT-09)
	CodeOverlap        = "STORAGE-DATES-01"    // a version's validity overlaps another's, or ends before it starts
	CodeSequence       = "STORAGE-SEQUENCE-01" // a number sequence cannot be set that way, or a number is used already
	CodeApproval       = "STORAGE-APPROVAL-01" // an approval or its request cannot be made or changed that way (FR-APR-03, FM-1)
	CodePack           = "STORAGE-PACK-01"     // a loaded statutory pack version cannot change; an activation needs a second person
	CodePasskey        = "STORAGE-PASSKEY-01"  // a passkey or its challenge cannot be made or used that way (FR-ADM-02, SEC-01)
	// CodeEvidence: evidence keeps what it is, is removed only from a draft, is append-only once its record is
	// not a draft, keeps a retain-until that only moves later, and is never deleted (FR-DOC-02, FR-DOC-07).
	CodeEvidence = "STORAGE-EVIDENCE-01"
)

// ErrPreviewed is what Do returns for a preview that passed every check the commit would run: nothing
// was saved (FR-INT-14).
var ErrPreviewed = &Error{Code: CodePreviewed, Message: "a preview: every check passed, and nothing was saved"}

type previewKey struct{}

// WithPreview makes the unit of work that Do opens with ctx a preview: it runs everything the commit
// would and then rolls back (FR-INT-14). It has no effect on a Do that joins a unit of work already open.
func WithPreview(ctx context.Context) context.Context {
	return context.WithValue(ctx, previewKey{}, true)
}

// IsPreview reports whether ctx asks for a preview.
func IsPreview(ctx context.Context) bool { b, _ := ctx.Value(previewKey{}).(bool); return b }

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
