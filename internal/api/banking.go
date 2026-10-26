package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ---------------------------------------------------------- banking (FR-BNK-01…04, FR-BNK-16; p1-bank-import-reconcile task 9.1)
//
// Money accounts and where their data comes from, saved column mappings, statement imports (a preview first: what
// would be added, flagged and dropped), the statements and the lines they brought. An import is an untrusted upload,
// checked and scanned as evidence is, and kept as its statement's file; once it is in, the system reconciles what it
// may of the new lines by itself (FR-BNK-14). An account's identifier is shown by its last four characters only
// (SEC-15).

// MoneyAccount is a bank, card, wallet, payment-gateway or cash account of the chart, as Banking keeps it.
type MoneyAccount struct {
	ID          string         `json:"id" format:"uuid" doc:"The money account's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	AccountID   string         `json:"account_id" format:"uuid" doc:"The chart account it posts to: one marked a money account, in its currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	AccountCode string         `json:"account_code" doc:"The chart account's code." example:"1010"`
	Kind        string         `json:"kind" enum:"bank,card,wallet,payment_gateway,cash" doc:"What kind of money account it is." example:"bank"`
	Name        string         `json:"name" doc:"Its name." example:"DBS current"`
	Institution string         `json:"institution,omitempty" doc:"The bank or provider." example:"DBS"`
	Identifier  string         `json:"identifier,omitempty" doc:"The last four characters of what statements name it by (an account number, an IBAN or a provider's balance ID); the rest is never shown (SEC-15)." example:"…4567"`
	Currency    string         `json:"currency" pattern:"^[A-Z]{3}$" doc:"Its currency, the chart account's." example:"SGD"`
	OpenedOn    string         `json:"opened_on,omitempty" format:"date" doc:"The day it opened." example:"2026-01-05"`
	ClosedOn    string         `json:"closed_on,omitempty" format:"date" doc:"The day it closed: a closed account takes no import." example:"2027-03-31"`
	Cardholder  *Person        `json:"cardholder,omitempty" doc:"A card: its holder, whose My claims shows its lines to complete with a receipt and a category (FR-AP-18)."`
	Source      *MoneySource   `json:"source,omitempty" doc:"Where its data comes from now; absent while its statements are uploaded as files."`
	Freshness   MoneyFreshness `json:"freshness" doc:"How up to date its data is (FR-BNK-16)."`
	Waiting     int            `json:"waiting" doc:"Its lines waiting in the queue: unreconciled, possible duplicates and in suspense." example:"12"`
	Gaps        []BankGap      `json:"gaps" nullable:"false" doc:"getMoneyAccount only: the breaks in its balance chain not yet resolved."`
	Sources     []MoneySource  `json:"sources" nullable:"false" doc:"getMoneyAccount only: every source it has had, earliest first (UC-BNK-03)."`
	Version     int            `json:"version" doc:"Its version, for If-Match." example:"1"`
	Changed     *time.Time     `json:"changed_at,omitempty" doc:"When it last changed." example:"2026-10-20T09:30:00Z"`
}

// MoneySource is where a money account's data comes from, from a day.
type MoneySource struct {
	Kind         string         `json:"kind" enum:"file,email,connector" doc:"file: statements a person uploads; email: reports the bank emails to the bank-ingest address; connector: an organisation connector's sync (FR-BNK-15)." example:"email"`
	FromDate     string         `json:"from_date" format:"date" doc:"The first day it is the source." example:"2026-11-01"`
	Senders      []EmailAddress `json:"senders" nullable:"false" doc:"email: the bank's registered addresses, the only senders accepted for it."`
	ReportFormat string         `json:"report_format,omitempty" enum:"csv,xlsx,ofx,camt053,mt940,mt940_sap" doc:"file and email: the format its statements come in." example:"csv"`
	MappingID    string         `json:"mapping_id,omitempty" format:"uuid" doc:"A CSV or XLSX format's saved column mapping." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b30"`
	ConnectorID  string         `json:"connector_id,omitempty" format:"uuid" doc:"connector: the organisation connector that syncs it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b40"`
	SetBy        string         `json:"set_by,omitempty" doc:"Who set it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
}

// MoneyFreshness says how up to date a money account's data is.
type MoneyFreshness struct {
	LastImport string `json:"last_import,omitempty" format:"date-time" doc:"When its last statement arrived." example:"2026-10-20T09:30:00Z"`
	CompleteTo string `json:"complete_to,omitempty" format:"date" doc:"The last day its statements cover with no gap before it." example:"2026-10-19"`
	OpenGaps   int    `json:"open_gaps" doc:"Breaks in its balance chain not yet resolved." example:"0"`
}

// BankGap is a break in a money account's balance chain (FR-BNK-16).
type BankGap struct {
	ID              string `json:"id" format:"uuid" doc:"The gap's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b50"`
	AfterDate       string `json:"after_date,omitempty" format:"date" doc:"The previous statement's last day." example:"2026-10-15"`
	BeforeDate      string `json:"before_date" format:"date" doc:"The next statement's first day." example:"2026-10-18"`
	ExpectedOpening *Money `json:"expected_opening,omitempty" doc:"The previous statement's closing balance."`
	FoundOpening    *Money `json:"found_opening,omitempty" doc:"The next statement's opening balance."`
	Resolution      string `json:"resolution,omitempty" doc:"How it was resolved, once it is." example:"The bank's statement for 16–17 Oct imported"`
}

// BankStatementIn is an imported statement.
type BankStatementIn struct {
	ID          string `json:"id" format:"uuid" doc:"The statement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b60"`
	Source      string `json:"source" enum:"file,email,connector" doc:"How it arrived." example:"file"`
	Format      string `json:"format" enum:"csv,xlsx,ofx,camt053,mt940,mt940_sap,api" doc:"Its format." example:"camt053"`
	Parser      string `json:"parser" doc:"The reader's version that read it." example:"camt053@1"`
	From        string `json:"from" format:"date" doc:"Its first day." example:"2026-10-01"`
	To          string `json:"to" format:"date" doc:"Its last day." example:"2026-10-31"`
	Opening     *Money `json:"opening,omitempty" doc:"Its opening balance, when the format gives it."`
	Closing     *Money `json:"closing,omitempty" doc:"Its closing balance, when the format gives it."`
	Lines       int    `json:"lines" doc:"How many lines it added." example:"42"`
	ImportedBy  string `json:"imported_by" doc:"Who or what imported it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	ImportedAt  string `json:"imported_at" format:"date-time" doc:"When." example:"2026-10-31T10:00:00Z"`
	HasOriginal bool   `json:"has_original" doc:"Whether the file it came in is kept with it." example:"true"`
}

// BankLine is a line of a statement and where it stands.
type BankLine struct {
	ID             string          `json:"id" format:"uuid" doc:"The line's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
	MoneyAccountID string          `json:"money_account_id" format:"uuid" doc:"Its money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	StatementID    string          `json:"statement_id" format:"uuid" doc:"The statement that brought it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b60"`
	BookingDate    string          `json:"booking_date" format:"date" doc:"The day the bank booked it." example:"2026-10-20"`
	ValueDate      string          `json:"value_date,omitempty" format:"date" doc:"Its value date, when it differs." example:"2026-10-21"`
	Amount         Money           `json:"amount" doc:"Signed: money in positive, money out negative."`
	Reference      string          `json:"reference,omitempty" doc:"The bank's own reference." example:"INV-0138"`
	Counterparty   string          `json:"counterparty,omitempty" doc:"Who paid or was paid." example:"ACME ROBOTICS"`
	Description    string          `json:"description,omitempty" doc:"The bank's description." example:"GIRO ACME ROBOTICS INV-0138"`
	Balance        *Money          `json:"balance,omitempty" doc:"The running balance after it, when the statement gives it."`
	Status         string          `json:"status" enum:"unreconciled,possible_duplicate,categorised,matched,transferred,suspense,excluded" doc:"Where it stands in the queue (FR-BNK-06)." example:"unreconciled"`
	DuplicateOf    string          `json:"duplicate_of,omitempty" format:"uuid" doc:"A possible duplicate: the line it may duplicate." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b71"`
	Match          *BankMatch      `json:"match,omitempty" doc:"getBankLine only: what it is matched to now."`
	Suggestions    []BankSuggested `json:"suggestions" nullable:"false" doc:"getBankLine only, while it waits: what it may match, best first (FR-BNK-06, FR-BNK-07); never applied by itself."`
	Version        int             `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// BankMatch is what a line is matched to.
type BankMatch struct {
	Kind       string `json:"kind" enum:"categorised,receipt,bill_payment,recorded,transfer,suspense" doc:"What it is." example:"receipt"`
	DocumentID string `json:"document_id,omitempty" format:"uuid" doc:"The document it posted or matched." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b80"`
	EntryID    string `json:"entry_id,omitempty" format:"uuid" doc:"The entry made from the line, if one was." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b81"`
	TransferID string `json:"transfer_id,omitempty" format:"uuid" doc:"The transfer it is a side of." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b82"`
	Basis      string `json:"basis" enum:"person,exact,transfer,rule" doc:"Who made it: a person, or the system for an exact match, a transfer's other side or an auto-apply rule (FR-BNK-14)." example:"person"`
	RuleID     string `json:"rule_id,omitempty" format:"uuid" doc:"The auto-apply rule that made it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b83"`
	MadeBy     string `json:"made_by" doc:"Who made it." example:"service:bank.auto-reconcile"`
	MadeAt     string `json:"made_at" format:"date-time" doc:"When." example:"2026-10-20T09:30:00Z"`
}

// BankSuggested is one suggestion for a line.
type BankSuggested struct {
	Found      string `json:"found" enum:"recorded,reference,normalised,amount,rule" doc:"How it was found: money recorded by hand, the document's number exactly, once normalised, its amount from the contact the counterparty names, or the first rule the line meets." example:"reference"`
	Kind       string `json:"kind" enum:"recorded,receipt,bill_payment,categorised" doc:"What accepting it makes." example:"receipt"`
	DocumentID string `json:"document_id,omitempty" format:"uuid" doc:"The document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b80"`
	Number     string `json:"number,omitempty" doc:"Its number." example:"INV-0138"`
	ContactID  string `json:"contact_id,omitempty" format:"uuid" doc:"Its customer or supplier." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Contact    string `json:"contact,omitempty" doc:"Its customer or supplier's name." example:"Acme Robotics Pte Ltd"`
	Open       *Money `json:"open,omitempty" doc:"What is open of it."`
	RuleID     string `json:"rule_id,omitempty" format:"uuid" doc:"The rule." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b83"`
	RuleName   string `json:"rule_name,omitempty" doc:"The rule's name." example:"AWS"`
}

// MoneyAccountDraft is a money account as written.
type MoneyAccountDraft struct {
	AccountID    string `json:"account_id,omitempty" format:"uuid" doc:"createMoneyAccount only: the chart account, marked a money account; its currency is the account's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Kind         string `json:"kind" enum:"bank,card,wallet,payment_gateway,cash" doc:"What kind of money account it is." example:"bank"`
	Name         string `json:"name" minLength:"1" maxLength:"100" doc:"Its name." example:"DBS current"`
	Institution  string `json:"institution,omitempty" maxLength:"100" doc:"The bank or provider." example:"DBS"`
	Identifier   string `json:"identifier,omitempty" maxLength:"64" doc:"What statements name it by: the account number, an IBAN or a provider's balance ID. Written only; read back by its last four characters." example:"072-123456-7"`
	OpenedOn     string `json:"opened_on,omitempty" format:"date" doc:"The day it opened." example:"2026-01-05"`
	ClosedOn     string `json:"closed_on,omitempty" format:"date" doc:"replaceMoneyAccount only: the day it closed." example:"2027-03-31"`
	CardholderID string `json:"cardholder_id,omitempty" format:"uuid" doc:"A card only: its holder, an active member's user ID, who completes its lines from My claims (FR-AP-18). Left out, it has none." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

// MoneySourceDraft is a source to set from a day after the current one's.
type MoneySourceDraft struct {
	Kind         string         `json:"kind" enum:"file,email,connector" doc:"file, email or connector." example:"email"`
	FromDate     string         `json:"from_date" format:"date" doc:"The first day it is the source: after the current one's, so no day has two." example:"2026-11-01"`
	Senders      []EmailAddress `json:"senders,omitempty" maxItems:"5" doc:"email: the bank's registered addresses (DMARC must pass for each, FR-INT-22)."`
	ReportFormat string         `json:"report_format,omitempty" enum:"csv,xlsx,ofx,camt053,mt940,mt940_sap" doc:"file and email: the statements' format." example:"csv"`
	MappingID    string         `json:"mapping_id,omitempty" format:"uuid" doc:"CSV and XLSX: the saved column mapping." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b30"`
	ConnectorID  string         `json:"connector_id,omitempty" format:"uuid" doc:"connector: the organisation connector that syncs it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b40"`
}

// StatementMappingSpec is how a CSV or XLSX statement's columns read (FR-BNK-02).
type StatementMappingSpec struct {
	Skip         int           `json:"skip,omitempty" minimum:"0" maximum:"100" doc:"Rows before the header row." example:"0"`
	Delimiter    string        `json:"delimiter,omitempty" pattern:"^[,;|\\t]$" doc:"CSV only: the delimiter, a comma by default; or ;, | or a tab." example:","`
	DateFormats  []DateLayout  `json:"date_formats" maxItems:"5" doc:"Go layouts for its dates, the first that reads winning, such as 02/01/2006 for day/month/year."`
	Number       MappingNumber `json:"number" doc:"How its amounts are written."`
	Date         string        `json:"date" minLength:"1" maxLength:"100" doc:"The booking date's column." example:"Transaction Date"`
	ValueDate    string        `json:"value_date,omitempty" maxLength:"100" doc:"The value date's column." example:"Value Date"`
	Amount       string        `json:"amount,omitempty" maxLength:"100" doc:"A signed amount column; or in and out." example:"Amount"`
	In           string        `json:"in,omitempty" maxLength:"100" doc:"The money-in column." example:"Credit"`
	Out          string        `json:"out,omitempty" maxLength:"100" doc:"The money-out column." example:"Debit"`
	Reference    string        `json:"reference,omitempty" maxLength:"100" doc:"The bank's reference column." example:"Reference"`
	Counterparty string        `json:"counterparty,omitempty" maxLength:"100" doc:"The counterparty's column." example:"Payee"`
	Description  []ColumnName  `json:"description,omitempty" maxItems:"5" doc:"Columns joined, in order, as the description."`
	Balance      string        `json:"balance,omitempty" maxLength:"100" doc:"The running balance's column." example:"Balance"`
}

// MappingNumber is how a mapping's amounts are written.
type MappingNumber struct {
	Decimal   string `json:"decimal,omitempty" pattern:"^[.,]$" doc:"The decimal mark: . by default, or ,." example:"."`
	Thousands string `json:"thousands,omitempty" pattern:"^[,. ']$" doc:"The thousands separator, if any: a comma, a dot, a space or an apostrophe." example:","`
	Brackets  bool   `json:"brackets,omitempty" doc:"Whether (1,234.56) is money out." example:"false"`
}

// StatementMapping is a saved column mapping.
type StatementMapping struct {
	ID          string               `json:"id" format:"uuid" doc:"The mapping's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b30"`
	Name        string               `json:"name" doc:"Its name." example:"DBS IDEAL CSV"`
	Institution string               `json:"institution,omitempty" doc:"The bank it is for." example:"DBS"`
	Format      string               `json:"format" enum:"csv,xlsx" doc:"The format it reads." example:"csv"`
	Spec        StatementMappingSpec `json:"spec" doc:"How the columns read."`
	Version     int                  `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// StatementMappingDraft is a mapping as written.
type StatementMappingDraft struct {
	Name        string               `json:"name" minLength:"1" maxLength:"100" doc:"Its name." example:"DBS IDEAL CSV"`
	Institution string               `json:"institution,omitempty" maxLength:"100" doc:"The bank it is for." example:"DBS"`
	Format      string               `json:"format" enum:"csv,xlsx" doc:"The format it reads." example:"csv"`
	Spec        StatementMappingSpec `json:"spec" doc:"How the columns read."`
}

// BankImportUpload is a statement file to import.
type BankImportUpload struct {
	File           huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"The statement: CSV, XLSX, OFX or QFX, camt.053 or MT940, up to 10 MB. It is checked and scanned for malware as evidence is, and kept as the statement's file."`
	Format         string        `form:"format" required:"true" enum:"csv,xlsx,ofx,camt053,mt940,mt940_sap" doc:"The file's format." example:"camt053"`
	MoneyAccountID string        `form:"money_account_id" required:"false" format:"uuid" doc:"The money account: required for CSV and XLSX; for the others, the file's own account must be it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	MappingID      string        `form:"mapping_id" required:"false" format:"uuid" doc:"CSV and XLSX: the saved column mapping." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b30"`
}

// BankImport is what an import added, flagged and dropped, statement by statement, and what the system then reconciled.
type BankImport struct {
	Statements []ImportedStatement `json:"statements" nullable:"false" doc:"Each statement the file held, for its money account."`
	Auto       *AutoReconciled     `json:"auto_reconciled,omitempty" doc:"Once imported, not on a preview: what the system reconciled by itself (FR-BNK-14)."`
}

// ImportedStatement is what an incoming statement became.
type ImportedStatement struct {
	MoneyAccountID string         `json:"money_account_id" format:"uuid" doc:"Its money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	StatementID    string         `json:"statement_id,omitempty" format:"uuid" doc:"The statement saved; absent on a preview, or when every line was held already." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b60"`
	From           string         `json:"from" format:"date" doc:"Its first day." example:"2026-10-01"`
	To             string         `json:"to" format:"date" doc:"Its last day." example:"2026-10-31"`
	Opening        *Money         `json:"opening,omitempty" doc:"Its opening balance, as given or derived."`
	Closing        *Money         `json:"closing,omitempty" doc:"Its closing balance, as given or derived."`
	Added          int            `json:"added" doc:"New lines." example:"40"`
	Flagged        int            `json:"flagged" doc:"Lines kept as possible duplicates, for a person (C.2 #51)." example:"1"`
	Dropped        int            `json:"dropped" doc:"Lines held already, dropped on proof (FR-BNK-04)." example:"1"`
	Gap            *BankGap       `json:"gap,omitempty" doc:"A break in the balance chain this statement shows (FR-BNK-16)."`
	Lines          []ImportedLine `json:"lines" nullable:"false" doc:"Each incoming line and what becomes of it."`
}

// ImportedLine is an incoming line and its outcome.
type ImportedLine struct {
	Position     int    `json:"position" doc:"Its place in the statement." example:"1"`
	BookingDate  string `json:"booking_date" format:"date" doc:"Its booking date." example:"2026-10-20"`
	Amount       Money  `json:"amount" doc:"Signed: money in positive."`
	Reference    string `json:"reference,omitempty" doc:"The bank's reference." example:"INV-0138"`
	Counterparty string `json:"counterparty,omitempty" doc:"Who paid or was paid." example:"ACME ROBOTICS"`
	Description  string `json:"description,omitempty" doc:"The bank's description." example:"GIRO ACME ROBOTICS"`
	Outcome      string `json:"outcome" enum:"add,possible_duplicate,duplicate" doc:"add: a new line; possible_duplicate: kept for a person; duplicate: dropped, held already." example:"add"`
	DuplicateOf  string `json:"duplicate_of,omitempty" format:"uuid" doc:"The held line it duplicates or may duplicate." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b71"`
	Transfer     string `json:"transfer,omitempty" format:"uuid" doc:"A transfer in transit it may be the other side of." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b82"`
	LineID       string `json:"line_id,omitempty" format:"uuid" doc:"The line saved, once imported." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
}

// AutoReconciled is what a run of auto-reconcile did.
type AutoReconciled struct {
	Reconciled int            `json:"reconciled" doc:"Lines reconciled by the system." example:"12"`
	Left       map[string]int `json:"left" doc:"Lines left for a person, by why: no_match, ambiguous, line_cap, rule_cap, no_rate, refused." example:"{\"no_match\":20,\"line_cap\":2}"`
}

// EmailAddress, DateLayout and ColumnName are the items of a source's senders and a mapping's date layouts and
// description columns: strings, each with its example (FR-INT-13).
type (
	EmailAddress string
	DateLayout   string
	ColumnName   string
)

// Schema implements huma.SchemaProvider.
func (EmailAddress) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "email", Description: "An email address.", Examples: []any{"ideal@dbs.example"}}
}

// Schema implements huma.SchemaProvider.
func (DateLayout) Schema(huma.Registry) *huma.Schema {
	n := 40
	return &huma.Schema{Type: "string", MaxLength: &n, Description: "A Go date layout, such as 02/01/2006 for day/month/year.", Examples: []any{"02/01/2006"}}
}

// Schema implements huma.SchemaProvider.
func (ColumnName) Schema(huma.Registry) *huma.Schema {
	n := 100
	return &huma.Schema{Type: "string", MaxLength: &n, Description: "A column's header, as the file writes it.", Examples: []any{"Description"}}
}

// strs and typed convert between a slice of a named string type and plain strings, through reflection: a `~string`
// constraint would say it better, but Semgrep's Go parser refuses it (as in connectors.go). T is always a string type.
func strs[T any](xs []T) []string {
	out := make([]string, len(xs))
	for i := range xs {
		out[i] = reflect.ValueOf(xs[i]).String()
	}
	return out
}

func typed[T any](xs []string) []T {
	out := make([]T, len(xs))
	for i, x := range xs {
		reflect.ValueOf(&out[i]).Elem().SetString(x)
	}
	return out
}

func moneyAccountETag(m MoneyAccount) string { return etag(m.Version) }

// page is one page of items held in memory, after the item a cursor names: the items and the cursor of the next page.
func page[T any](items []T, id func(T) string, q PageQuery) ([]T, string, error) {
	start := 0
	if q.Cursor != "" {
		after, err := decodeCursor(q.Cursor)
		if err != nil {
			return nil, "", problem(CodeCursor, "The cursor is not one this API gave.", "Leave it out to start from the first page.")
		}
		start = len(items)
		for i, x := range items {
			if id(x) == after {
				start = i + 1
			}
		}
	}
	size := q.PageSize
	if size <= 0 {
		size = 100
	}
	rest := items[start:]
	if len(rest) > size {
		return rest[:size], encodeCursor(id(rest[size-1])), nil
	}
	return rest, "", nil
}

// moneyAccountOf is a money account as the API shows it; full adds its gaps and every source.
func moneyAccountOf(ctx context.Context, u storage.UnitOfWork, a storage.MoneyAccount, full bool) (MoneyAccount, error) {
	out := MoneyAccount{ID: a.ID.String(), AccountID: a.AccountID.String(), Kind: a.Kind, Name: a.Name, Institution: a.Institution, Currency: a.Currency,
		Version: a.Version, Gaps: []BankGap{}, Sources: []MoneySource{}}
	if a.CardholderID != nil {
		out.Cardholder = &Person{Kind: "user", ID: a.CardholderID.String()}
		ms, err := u.Members().List(ctx, a.ChangedAt) // the members' names; the day decides only their roles
		if err != nil {
			return MoneyAccount{}, err
		}
		for _, m := range ms {
			if m.UserID == *a.CardholderID {
				out.Cardholder.Name = m.DisplayName
			}
		}
	}
	changed := a.ChangedAt
	out.Changed = &changed
	if a.Identifier != "" {
		id := strings.Map(func(r rune) rune {
			if strings.ContainsRune(" -./", r) {
				return -1
			}
			return r
		}, a.Identifier)
		if len(id) > 4 {
			id = "…" + id[len(id)-4:]
		}
		out.Identifier = id
	}
	if a.OpenedOn != nil {
		out.OpenedOn = dateOf(*a.OpenedOn)
	}
	if a.ClosedOn != nil {
		out.ClosedOn = dateOf(*a.ClosedOn)
	}
	acc, err := u.Accounts().Get(ctx, a.AccountID)
	if err != nil {
		return out, err
	}
	out.AccountCode = acc.Code
	sources, err := u.Banking().Sources(ctx, a.ID)
	if err != nil {
		return out, err
	}
	for _, s := range sources {
		x := MoneySource{Kind: s.Kind, FromDate: dateOf(s.FromDate), Senders: typed[EmailAddress](s.Senders), ReportFormat: s.ReportFormat, SetBy: s.SetBy}
		if s.MappingID != nil {
			x.MappingID = s.MappingID.String()
		}
		if s.ConnectorID != nil {
			x.ConnectorID = s.ConnectorID.String()
		}
		if full {
			out.Sources = append(out.Sources, x)
		}
		cur := x
		out.Source = &cur
	}
	f, err := banking.FreshnessOf(ctx, u, a.ID)
	if err != nil {
		return out, err
	}
	if f.LastImport != nil {
		out.Freshness.LastImport = f.LastImport.UTC().Format(time.RFC3339)
	}
	if f.CompleteTo != nil {
		out.Freshness.CompleteTo = dateOf(*f.CompleteTo)
	}
	out.Freshness.OpenGaps = f.OpenGaps
	waiting, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &a.ID,
		Statuses: []string{storage.LineUnreconciled, storage.LinePossibleDuplicate, storage.LineSuspense}})
	if err != nil {
		return out, err
	}
	out.Waiting = len(waiting)
	if full {
		gaps, err := u.Banking().Gaps(ctx, a.ID, true)
		if err != nil {
			return out, err
		}
		for _, g := range gaps {
			out.Gaps = append(out.Gaps, gapOf(g))
		}
	}
	return out, nil
}

func gapOf(g storage.BankGap) BankGap {
	out := BankGap{ID: g.ID.String(), BeforeDate: dateOf(g.BeforeDate), Resolution: g.Resolution}
	if g.AfterDate != nil {
		out.AfterDate = dateOf(*g.AfterDate)
	}
	if g.ExpectedOpening != nil {
		m := moneyOf(*g.ExpectedOpening, g.Currency)
		out.ExpectedOpening = &m
	}
	if g.FoundOpening != nil {
		m := moneyOf(*g.FoundOpening, g.Currency)
		out.FoundOpening = &m
	}
	return out
}

func statementOf(s storage.BankStatement) BankStatementIn {
	out := BankStatementIn{ID: s.ID.String(), Source: s.Source, Format: s.Format, Parser: s.ParserVersion, From: dateOf(s.PeriodFrom), To: dateOf(s.PeriodTo),
		Lines: s.LineCount, ImportedBy: s.ImportedBy, ImportedAt: s.ImportedAt.UTC().Format(time.RFC3339), HasOriginal: s.FileRef != ""}
	if s.Opening != nil {
		m := moneyOf(*s.Opening, s.Currency)
		out.Opening = &m
	}
	if s.Closing != nil {
		m := moneyOf(*s.Closing, s.Currency)
		out.Closing = &m
	}
	return out
}

func bankLineOf(l storage.BankLine) BankLine {
	out := BankLine{ID: l.ID.String(), MoneyAccountID: l.MoneyAccountID.String(), StatementID: l.StatementID.String(), BookingDate: dateOf(l.BookingDate),
		Amount: moneyOf(l.Amount, l.Currency), Reference: l.Reference, Counterparty: l.Counterparty, Description: l.Description, Status: l.Status,
		Version: l.Version, Suggestions: []BankSuggested{}}
	if l.ValueDate != nil {
		out.ValueDate = dateOf(*l.ValueDate)
	}
	if l.RunningBalance != nil {
		m := moneyOf(*l.RunningBalance, l.Currency)
		out.Balance = &m
	}
	if l.DuplicateOf != nil {
		out.DuplicateOf = l.DuplicateOf.String()
	}
	return out
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// optionalID reads an ID that may be left out.
func optionalID(field, s string) (*uuid.UUID, *FieldProblem) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, &FieldProblem{Code: CodeInvalid, Field: field, Message: "is not an ID", Fix: "Send the ID"}
	}
	return &id, nil
}

// fieldProblems are the problems found among those that may be nil.
func fieldProblems(found ...*FieldProblem) []FieldProblem {
	var out []FieldProblem
	for _, f := range found {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out
}

func optionalDate(field, s string) (*time.Time, *FieldProblem) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, &FieldProblem{Code: CodeInvalid, Field: field, Message: "is not a date", Fix: "Send YYYY-MM-DD"}
	}
	return &t, nil
}

func mappingSpec(s StatementMappingSpec) ([]byte, error) {
	b, err := json.Marshal(bankfile.Mapping{Skip: s.Skip, Delimiter: s.Delimiter, DateFormats: strs(s.DateFormats), Date: s.Date, ValueDate: s.ValueDate,
		Amount: s.Amount, In: s.In, Out: s.Out, Reference: s.Reference, Counterparty: s.Counterparty, Description: strs(s.Description), Balance: s.Balance,
		Number: bankfile.Number{Decimal: s.Number.Decimal, Thousands: s.Number.Thousands, Brackets: s.Number.Brackets}})
	if err != nil {
		return nil, err
	}
	if _, err := bankfile.ReadMapping(b); err != nil {
		return nil, err
	}
	return b, nil
}

func mappingOf(m storage.StatementMapping) StatementMapping {
	out := StatementMapping{ID: m.ID.String(), Name: m.Name, Institution: m.Institution, Format: m.Format, Version: m.Version}
	if x, err := bankfile.ReadMapping(m.Spec); err == nil {
		out.Spec = StatementMappingSpec{Skip: x.Skip, Delimiter: x.Delimiter, DateFormats: typed[DateLayout](x.DateFormats), Date: x.Date, ValueDate: x.ValueDate,
			Amount: x.Amount, In: x.In, Out: x.Out, Reference: x.Reference, Counterparty: x.Counterparty, Description: typed[ColumnName](x.Description), Balance: x.Balance,
			Number: MappingNumber{Decimal: x.Number.Decimal, Thousands: x.Number.Thousands, Brackets: x.Number.Brackets}}
	}
	if out.Spec.DateFormats == nil {
		out.Spec.DateFormats = []DateLayout{}
	}
	return out
}

// StatementKey is where an imported statement's file is kept in the staging store: outside the evidence sweeper's
// prefix, as inbound mail's files are, so the statement keeps it.
func StatementKey(org, file uuid.UUID) string {
	return "statements/" + org.String() + "/" + file.String()
}

type moneyAccountsOutput struct {
	Body struct {
		Items      []MoneyAccount `json:"items" nullable:"false" doc:"The organisation's money accounts, by name."`
		NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type MoneyAccountPath struct {
	MoneyAccountID string `path:"money_account_id" format:"uuid" doc:"The money account's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
}

type moneyAccountOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The money account's version, for If-Match."`
	Location string `header:"Location" doc:"The money account's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     MoneyAccount
}

type createMoneyAccountInput struct {
	KeyHeader
	PreferHeader
	Body MoneyAccountDraft
}

type replaceMoneyAccountInput struct {
	MoneyAccountPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body MoneyAccountDraft
}

type addSourceInput struct {
	MoneyAccountPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body MoneySourceDraft
}

type resolveGapInput struct {
	MoneyAccountPath
	GapID string `path:"gap_id" format:"uuid" doc:"The gap's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b50"`
	KeyHeader
	PreferHeader
	Body struct {
		Resolution string `json:"resolution" minLength:"1" maxLength:"500" doc:"How it was resolved: the statement imported, or why there is none." example:"The bank's statement for 16–17 Oct imported"`
	}
}

type gapOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankGap
}

type mappingsOutput struct {
	Body struct {
		Items      []StatementMapping `json:"items" nullable:"false" doc:"The saved mappings, by name."`
		NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type createMappingInput struct {
	KeyHeader
	PreferHeader
	Body StatementMappingDraft
}

type replaceMappingInput struct {
	MappingID string `path:"mapping_id" format:"uuid" doc:"The mapping's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b30"`
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body StatementMappingDraft
}

type mappingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The mapping's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     StatementMapping
}

type importInput struct {
	KeyHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[BankImportUpload]
}

type importOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankImport
}

type statementsOutput struct {
	Body struct {
		Items      []BankStatementIn `json:"items" nullable:"false" doc:"Its statements, latest first."`
		NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type statementsInput struct {
	MoneyAccountPath
	PageQuery
}

type linesInput struct {
	PageQuery
	MoneyAccountID string `query:"money_account_id" format:"uuid" doc:"Only this money account's lines." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Status         string `query:"status" enum:"unreconciled,possible_duplicate,categorised,matched,transferred,suspense,excluded,waiting" doc:"Only lines with this status; waiting is the queue: unreconciled, possible duplicates and suspense." example:"waiting"`
	From           string `query:"from" format:"date" doc:"Only lines booked on or after this day." example:"2026-10-01"`
	To             string `query:"to" format:"date" doc:"Only lines booked on or before this day." example:"2026-10-31"`
}

type linesOutput struct {
	Body struct {
		Items      []BankLine `json:"items" nullable:"false" doc:"The lines, by booking date then position."`
		NextCursor string     `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type lineOutput struct {
	ETag string `header:"ETag" doc:"The line's version, for If-Match."`
	Body BankLine
}

func registerBanking(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeBanking}, locks...)
	creates := append([]string{storage.CodeDuplicate}, writes...) // a second money account on a chart account; a mapping's name in use

	register(a, authz.ListMoneyAccounts, huma.Operation{
		OperationID: "listMoneyAccounts", Method: http.MethodGet, Path: "/v1/money-accounts", Tags: []string{"Banking"},
		Summary: "List the money accounts",
		Description: "Every money account Banking keeps (FR-BNK-01), by name: its chart account, kind, currency, current source, how up to date " +
			"its data is (FR-BNK-16) and how many of its lines wait in the queue. An identifier shows its last four characters only. Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *PageQuery) (*moneyAccountsOutput, error) {
		p, _ := authn.From(ctx)
		out := &moneyAccountsOutput{}
		out.Body.Items = []MoneyAccount{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Banking().MoneyAccounts(ctx)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(m storage.MoneyAccount) string { return m.ID.String() }, *in); err != nil {
				return err
			}
			for _, x := range all {
				m, err := moneyAccountOf(ctx, u, x, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, m)
			}
			return nil
		})
		return out, err
	})

	register(a, authz.CreateMoneyAccount, huma.Operation{
		OperationID: "createMoneyAccount", Method: http.MethodPost, Path: "/v1/money-accounts", Tags: []string{"Banking"},
		Summary: "Set up a money account", DefaultStatus: http.StatusCreated,
		Description: "Sets up a chart account marked a money account in Banking (FR-BNK-01): its kind, name, bank and what statements name it " +
			"by, in the chart account's currency; a card's holder, an active member who completes its lines from My claims (FR-AP-18). One per chart account; an identifier is unique per currency. Its statements are uploaded " +
			"as files until a source says otherwise. Side effects: the money account and its audit event, which keeps the identifier's " +
			"last four characters only (SEC-15). Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{banking.CodeCardholder}, creates...), func(ctx context.Context, in *createMoneyAccountInput) (*moneyAccountOutput, error) {
		account, err := parseID("body.account_id", in.Body.AccountID)
		if err != nil {
			return nil, err
		}
		opened, bad := optionalDate("body.opened_on", in.Body.OpenedOn)
		holder, bad2 := optionalID("body.cardholder_id", in.Body.CardholderID)
		if bad != nil || bad2 != nil {
			return nil, invalid(fieldProblems(bad, bad2))
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (MoneyAccount, error) {
			acc, err := u.Accounts().Get(ctx, account)
			if err != nil {
				return MoneyAccount{}, err
			}
			if err := banking.CheckCardholder(ctx, u, in.Body.Kind, holder); err != nil {
				return MoneyAccount{}, err
			}
			m, err := u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: account, Kind: in.Body.Kind, Name: strings.TrimSpace(in.Body.Name),
				Institution: strings.TrimSpace(in.Body.Institution), Identifier: strings.TrimSpace(in.Body.Identifier), Currency: acc.Currency, OpenedOn: opened,
				CardholderID: holder, At: a.deps.Clock.Now()})
			if err != nil {
				return MoneyAccount{}, err
			}
			return moneyAccountOf(ctx, u, m, true)
		})
		if err != nil {
			return nil, err
		}
		out := &moneyAccountOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}
		if !w.Previewed {
			out.ETag, out.Location = moneyAccountETag(w.Body), "/v1/money-accounts/"+w.Body.ID
		}
		return out, nil
	})

	register(a, authz.GetMoneyAccount, huma.Operation{
		OperationID: "getMoneyAccount", Method: http.MethodGet, Path: "/v1/money-accounts/{money_account_id}", Tags: []string{"Banking"},
		Summary: "Get a money account",
		Description: "Returns a money account with its ETag: its current source and every earlier one (UC-BNK-03), its freshness, its open " +
			"gaps and how many lines wait. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *MoneyAccountPath) (*moneyAccountOutput, error) {
		id, err := parseID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out MoneyAccount
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			m, err := u.Banking().MoneyAccount(ctx, id)
			if err != nil {
				return err
			}
			out, err = moneyAccountOf(ctx, u, m, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &moneyAccountOutput{Status: http.StatusOK, ETag: moneyAccountETag(out), Body: out}, nil
	})

	register(a, authz.ReplaceMoneyAccount, huma.Operation{
		OperationID: "replaceMoneyAccount", Method: http.MethodPut, Path: "/v1/money-accounts/{money_account_id}", Tags: []string{"Banking"},
		Summary: "Change a money account",
		Description: "Changes a money account's kind, name, bank, identifier, the days it opened and closed, and a card's holder: never its chart account or " +
			"currency. A closed account takes no more imports. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion, banking.CodeCardholder}, writes...), func(ctx context.Context, in *replaceMoneyAccountInput) (*moneyAccountOutput, error) {
		id, err := parseID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		opened, bad := optionalDate("body.opened_on", in.Body.OpenedOn)
		closed, bad2 := optionalDate("body.closed_on", in.Body.ClosedOn)
		holder, bad3 := optionalID("body.cardholder_id", in.Body.CardholderID)
		if bad != nil || bad2 != nil || bad3 != nil {
			return nil, invalid(fieldProblems(bad, bad2, bad3))
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MoneyAccount, error) {
			m, err := u.Banking().MoneyAccount(ctx, id)
			if err != nil {
				return MoneyAccount{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(m.Version)); err != nil {
				return MoneyAccount{}, err
			}
			identifier := strings.TrimSpace(in.Body.Identifier)
			if identifier == "" {
				identifier = m.Identifier // written only: left out, it stays
			}
			if err := banking.CheckCardholder(ctx, u, in.Body.Kind, holder); err != nil {
				return MoneyAccount{}, err
			}
			if m, err = u.Banking().ChangeMoneyAccount(ctx, id, m.Version, storage.MoneyAccountChange{Kind: in.Body.Kind, Name: strings.TrimSpace(in.Body.Name),
				Institution: strings.TrimSpace(in.Body.Institution), Identifier: identifier, OpenedOn: opened, ClosedOn: closed, CardholderID: holder,
				At: a.deps.Clock.Now()}); err != nil {
				return MoneyAccount{}, err
			}
			return moneyAccountOf(ctx, u, m, true)
		})
		if err != nil {
			return nil, err
		}
		out := &moneyAccountOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}
		if !w.Previewed {
			out.ETag = moneyAccountETag(w.Body)
		}
		return out, nil
	})

	register(a, authz.AddMoneyAccountSource, huma.Operation{
		OperationID: "addMoneyAccountSource", Method: http.MethodPost, Path: "/v1/money-accounts/{money_account_id}/sources", Tags: []string{"Banking"},
		Summary: "Change where a money account's data comes from", DefaultStatus: http.StatusCreated,
		Description: "Sets a money account's source from a day after its current one's (FR-BNK-15, UC-BNK-03): statements uploaded as files, " +
			"reports the bank emails to the bank-ingest address from its registered senders, or an organisation connector's sync. Earlier " +
			"sources are kept with their days, so the balance chain runs on without a gap. Needs a right on \"Bank data… connections\". Needs " +
			"If-Match with the account's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *addSourceInput) (*moneyAccountOutput, error) {
		id, err := parseID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		from, bad := optionalDate("body.from_date", in.Body.FromDate)
		if bad != nil || from == nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.from_date", Message: "is not a date", Fix: "Send YYYY-MM-DD"}})
		}
		var mapping, connector *uuid.UUID
		for _, x := range []struct {
			field, s string
			to       **uuid.UUID
		}{{"body.mapping_id", in.Body.MappingID, &mapping}, {"body.connector_id", in.Body.ConnectorID, &connector}} {
			if x.s == "" {
				continue
			}
			v, err := parseID(x.field, x.s)
			if err != nil {
				return nil, err
			}
			*x.to = &v
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (MoneyAccount, error) {
			m, err := u.Banking().MoneyAccount(ctx, id)
			if err != nil {
				return MoneyAccount{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(m.Version)); err != nil {
				return MoneyAccount{}, err
			}
			if _, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: id, Kind: in.Body.Kind, ConnectorID: connector,
				Senders: strs(in.Body.Senders), ReportFormat: in.Body.ReportFormat, MappingID: mapping, FromDate: *from, At: a.deps.Clock.Now()}); err != nil {
				return MoneyAccount{}, err
			}
			return moneyAccountOf(ctx, u, m, true)
		})
		if err != nil {
			return nil, err
		}
		return &moneyAccountOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), ETag: moneyAccountETag(w.Body), Body: w.Body}, nil
	})

	register(a, authz.ResolveBankGap, huma.Operation{
		OperationID: "resolveBankGap", Method: http.MethodPost, Path: "/v1/money-accounts/{money_account_id}/gaps/{gap_id}/resolve", Tags: []string{"Banking"},
		Summary: "Resolve a gap in a money account's statements",
		Description: "Records how a break in a money account's balance chain was resolved (FR-BNK-16): the missing statement imported, or why " +
			"there is none. Once only. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound}, writes...), func(ctx context.Context, in *resolveGapInput) (*gapOutput, error) {
		account, err := parseID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		gap, err := parseID("gap_id", in.GapID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankGap, error) {
			gaps, err := u.Banking().Gaps(ctx, account, false)
			if err != nil {
				return BankGap{}, err
			}
			found := false
			for _, g := range gaps {
				found = found || g.ID == gap
			}
			if !found {
				return BankGap{}, problem(CodeNotFound, "That gap is not in this money account's statements.", "Read the account to find its gaps.")
			}
			g, err := u.Banking().ResolveGap(ctx, gap, strings.TrimSpace(in.Body.Resolution), a.deps.Clock.Now())
			return gapOf(g), err
		})
		if err != nil {
			return nil, err
		}
		return &gapOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ListStatementMappings, huma.Operation{
		OperationID: "listStatementMappings", Method: http.MethodGet, Path: "/v1/statement-mappings", Tags: []string{"Banking"},
		Summary:     "List the saved column mappings",
		Description: "The saved column mappings of CSV and XLSX statements, one per bank's layout (FR-BNK-02). Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *PageQuery) (*mappingsOutput, error) {
		p, _ := authn.From(ctx)
		out := &mappingsOutput{}
		out.Body.Items = []StatementMapping{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Banking().Mappings(ctx)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(m storage.StatementMapping) string { return m.ID.String() }, *in); err != nil {
				return err
			}
			for _, m := range all {
				out.Body.Items = append(out.Body.Items, mappingOf(m))
			}
			return err
		})
		return out, err
	})

	mappingCodes := append([]string{bankfile.CodeMapping}, creates...)
	register(a, authz.CreateStatementMapping, huma.Operation{
		OperationID: "createStatementMapping", Method: http.MethodPost, Path: "/v1/statement-mappings", Tags: []string{"Banking"},
		Summary: "Save a column mapping", DefaultStatus: http.StatusCreated,
		Description: "Saves how a bank's CSV or XLSX statements read (FR-BNK-02): its date, amount (signed, or in and out), reference, " +
			"counterparty, description and balance columns, its date layouts and how its amounts are written. A mapping that names no date " +
			"or amount is refused (BANKFILE-06). Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, mappingCodes, func(ctx context.Context, in *createMappingInput) (*mappingOutput, error) {
		spec, err := mappingSpec(in.Body.Spec)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (StatementMapping, error) {
			m, err := u.Banking().SaveMapping(ctx, storage.NewStatementMapping{Name: strings.TrimSpace(in.Body.Name), Institution: strings.TrimSpace(in.Body.Institution),
				Format: in.Body.Format, Spec: spec, At: a.deps.Clock.Now()})
			return mappingOf(m), err
		})
		if err != nil {
			return nil, err
		}
		return &mappingOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), ETag: etag(w.Body.Version), Body: w.Body}, nil
	})

	register(a, authz.ReplaceStatementMapping, huma.Operation{
		OperationID: "replaceStatementMapping", Method: http.MethodPut, Path: "/v1/statement-mappings/{mapping_id}", Tags: []string{"Banking"},
		Summary: "Change a column mapping",
		Description: "Changes a saved mapping. Statements imported before keep what it read then. Needs If-Match with its ETag. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, mappingCodes...), func(ctx context.Context, in *replaceMappingInput) (*mappingOutput, error) {
		id, err := parseID("mapping_id", in.MappingID)
		if err != nil {
			return nil, err
		}
		spec, err := mappingSpec(in.Body.Spec)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (StatementMapping, error) {
			all, err := u.Banking().Mappings(ctx)
			if err != nil {
				return StatementMapping{}, err
			}
			version := -1
			for _, m := range all {
				if m.ID == id {
					version = m.Version
				}
			}
			if version < 0 {
				return StatementMapping{}, problem(CodeNotFound, "No saved mapping with that ID exists in this organisation.", "List the mappings.")
			}
			if err := checkIfMatch(in.IfMatch, etag(version)); err != nil {
				return StatementMapping{}, err
			}
			m, err := u.Banking().ChangeMapping(ctx, id, version, storage.NewStatementMapping{Name: strings.TrimSpace(in.Body.Name),
				Institution: strings.TrimSpace(in.Body.Institution), Format: in.Body.Format, Spec: spec, At: a.deps.Clock.Now()})
			return mappingOf(m), err
		})
		if err != nil {
			return nil, err
		}
		return &mappingOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), ETag: etag(w.Body.Version), Body: w.Body}, nil
	})

	importCodes := append([]string{CodeUnavailable, banking.CodeAccount, banking.CodeCurrency, banking.CodeClosed, banking.CodeFile, banking.CodeRequest,
		bankfile.CodeTooLarge, bankfile.CodeFormat, bankfile.CodeAmount, bankfile.CodeDate, bankfile.CodeCurrency, bankfile.CodeMapping}, append(writes, EvidenceCodes...)...)
	register(a, authz.ImportBankStatements, huma.Operation{
		OperationID: "importBankStatements", Method: http.MethodPost, Path: "/v1/bank-imports", Tags: []string{"Banking"},
		Summary: "Import bank statements", DefaultStatus: http.StatusCreated,
		Description: "Imports a statement file, as multipart/form-data (FR-BNK-02): CSV or XLSX through a saved mapping into the account " +
			"chosen; OFX or QFX, camt.053 or MT940 into the money accounts their statements name, several at once. The file is an untrusted " +
			"upload: its type is decided from its content and it is scanned for malware, as evidence is (FR-DOC-08), then read by a bounded " +
			"reader that refuses what it cannot read with the line and field (BANKFILE-nn). Each line is new, kept as a possible duplicate " +
			"for a person, or dropped only on proof (FR-BNK-04); a break in the balance chain is recorded as a gap (FR-BNK-16). Prefer: " +
			"return=preview shows all of that and saves nothing. Once imported, the file is kept as its statements' file and the system " +
			"reconciles what it may of the new lines by itself, within the organisation's limits (FR-BNK-14). Idempotent with Idempotency-Key.",
	}, importCodes, func(ctx context.Context, in *importInput) (*importOutput, error) {
		up := in.RawBody.Data()
		if up == nil || up.File.File == nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the statement in the form field file"}})
		}
		defer up.File.Close()
		if a.deps.Scanner == nil || a.deps.Staging == nil {
			return nil, problem(CodeUnavailable, "Statements cannot be imported here: no file store or malware scanner is configured.", "Ask your administrator to configure them.")
		}
		data, err := io.ReadAll(io.LimitReader(up.File, int64(bankfile.DefaultLimits.MaxBytes)+1))
		if err != nil {
			return nil, problem(CodeMalformed, "The file could not be read in full.", "Send it again.")
		}
		file, problems, err := evidence.Check(ctx, a.deps.Scanner, up.File.Filename, data)
		if err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The malware scan is not available, so the statement was not imported.", "Try again in a few minutes.")
		}
		if len(problems) > 0 {
			var bad []FieldProblem
			for _, x := range problems {
				bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
			}
			return nil, invalid(bad)
		}
		var account, mappingID *uuid.UUID
		for _, x := range []struct {
			field, s string
			to       **uuid.UUID
		}{{"body.money_account_id", up.MoneyAccountID, &account}, {"body.mapping_id", up.MappingID, &mappingID}} {
			if x.s == "" {
				continue
			}
			v, err := parseID(x.field, x.s)
			if err != nil {
				return nil, err
			}
			*x.to = &v
		}
		p, _ := authn.From(ctx)
		key := StatementKey(p.Organisation, uuid.Must(uuid.NewV7()))
		request := struct{ Format, Account, Mapping, SHA256 string }{up.Format, up.MoneyAccountID, up.MappingID, file.OriginalSHA256}
		if !wantsPreview(in.Prefer) {
			if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
				logFailure(ctx, err)
				return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
			}
		}
		var added []uuid.UUID
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BankImport, error) {
			r := banking.ImportRequest{Format: up.Format, Data: data, Account: account, Source: storage.MoneySourceFile, At: a.deps.Clock.Now()}
			if !wantsPreview(in.Prefer) {
				r.FileRef = key
			}
			if mappingID != nil {
				all, err := u.Banking().Mappings(ctx)
				if err != nil {
					return BankImport{}, err
				}
				for _, m := range all {
					if m.ID == *mappingID {
						x, err := bankfile.ReadMapping(m.Spec)
						if err != nil {
							return BankImport{}, err
						}
						r.Mapping = &x
					}
				}
				if r.Mapping == nil {
					return BankImport{}, problem(CodeNotFound, "No saved mapping with that ID exists in this organisation.", "List the mappings.")
				}
			}
			stmts, err := banking.Import(ctx, u, r)
			if err != nil {
				return BankImport{}, err
			}
			out := BankImport{Statements: []ImportedStatement{}}
			for _, s := range stmts {
				x := ImportedStatement{MoneyAccountID: s.Account.ID.String(), From: dateOf(s.From), To: dateOf(s.To), Added: s.Added, Flagged: s.Flagged,
					Dropped: s.Dropped, Lines: []ImportedLine{}}
				if s.Statement != nil {
					x.StatementID = s.Statement.ID.String()
				}
				if s.Opening != nil {
					m := moneyOut(*s.Opening)
					x.Opening = &m
				}
				if s.Closing != nil {
					m := moneyOut(*s.Closing)
					x.Closing = &m
				}
				if s.Gap != nil {
					g := BankGap{BeforeDate: dateOf(s.Gap.BeforeDate)}
					if s.Gap.AfterDate != nil {
						g.AfterDate = dateOf(*s.Gap.AfterDate)
					}
					if s.Gap.ExpectedOpening != nil {
						m := moneyOf(*s.Gap.ExpectedOpening, s.Account.Currency)
						g.ExpectedOpening = &m
					}
					if s.Gap.FoundOpening != nil {
						m := moneyOf(*s.Gap.FoundOpening, s.Account.Currency)
						g.FoundOpening = &m
					}
					x.Gap = &g
				}
				for _, l := range s.Lines {
					il := ImportedLine{Position: l.Position, BookingDate: dateOf(l.Booked), Amount: moneyOut(l.Amount), Reference: l.Reference,
						Counterparty: l.Counterparty, Description: l.Description, Outcome: l.Outcome, DuplicateOf: idText(l.DuplicateOf),
						Transfer: idText(l.Transfer), LineID: idText(l.ID)}
					if l.ID != nil {
						added = append(added, *l.ID)
					}
					x.Lines = append(x.Lines, il)
				}
				out.Statements = append(out.Statements, x)
			}
			return out, nil
		})
		if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
			_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no statement keeps it
		}
		if err != nil {
			return nil, err
		}
		if !w.Previewed && !w.Replayed && len(added) > 0 {
			run, err := banking.AutoReconcile(context.WithoutCancel(ctx), a.deps.Store, p.Organisation, added, a.deps.Clock.Now())
			if err != nil {
				logFailure(ctx, fmt.Errorf("auto-reconcile after an import: %w", err)) // the lines wait in the queue; the import stands
			}
			auto := &AutoReconciled{Reconciled: len(run.Reconciled), Left: map[string]int{}}
			for _, l := range run.Left {
				auto.Left[l.Why]++
			}
			w.Body.Auto = auto
		}
		return &importOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	if op := a.OpenAPI().Paths["/v1/bank-imports"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"<?xml version=\"1.0\"?><Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">…"}
		}
	}

	register(a, authz.ListBankStatements, huma.Operation{
		OperationID: "listBankStatements", Method: http.MethodGet, Path: "/v1/money-accounts/{money_account_id}/statements", Tags: []string{"Banking"},
		Summary: "List a money account's statements",
		Description: "A money account's imported statements, latest first: how each arrived, its reader's version, its days, its balances " +
			"when the format gives them, and how many lines it added. Read-only.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *statementsInput) (*statementsOutput, error) {
		id, err := parseID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &statementsOutput{}
		out.Body.Items = []BankStatementIn{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Banking().MoneyAccount(ctx, id); err != nil {
				return err
			}
			all, err := u.Banking().Statements(ctx, id)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(s storage.BankStatement) string { return s.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			for _, s := range all {
				out.Body.Items = append(out.Body.Items, statementOf(s))
			}
			return err
		})
		return out, err
	})

	register(a, authz.ListBankLines, huma.Operation{
		OperationID: "listBankLines", Method: http.MethodGet, Path: "/v1/bank-lines", Tags: []string{"Banking"},
		Summary: "List bank lines",
		Description: "Bank lines by booking date then position, a page at a time: one account's or every account's, of a status or the queue " +
			"(waiting: unreconciled, possible duplicates and suspense), between two days (FR-BNK-06). Read-only.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *linesInput) (*linesOutput, error) {
		f := storage.BankLineFilter{}
		if in.MoneyAccountID != "" {
			id, err := parseID("money_account_id", in.MoneyAccountID)
			if err != nil {
				return nil, err
			}
			f.MoneyAccountID = &id
		}
		switch in.Status {
		case "":
		case "waiting":
			f.Statuses = []string{storage.LineUnreconciled, storage.LinePossibleDuplicate, storage.LineSuspense}
		default:
			f.Statuses = []string{in.Status}
		}
		var bad []FieldProblem
		from, b1 := optionalDate("from", in.From)
		to, b2 := optionalDate("to", in.To)
		for _, b := range []*FieldProblem{b1, b2} {
			if b != nil {
				bad = append(bad, *b)
			}
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		f.From, f.To = from, to
		p, _ := authn.From(ctx)
		out := &linesOutput{}
		out.Body.Items = []BankLine{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Banking().Lines(ctx, f)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(l storage.BankLine) string { return l.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			for _, l := range all {
				out.Body.Items = append(out.Body.Items, bankLineOf(l))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.GetBankLine, huma.Operation{
		OperationID: "getBankLine", Method: http.MethodGet, Path: "/v1/bank-lines/{line_id}", Tags: []string{"Banking"},
		Summary: "Get a bank line",
		Description: "Returns a line with its ETag: what it is matched to now, and while it waits, what it may match, best first: money " +
			"recorded by hand, documents whose number it names exactly or once normalised, documents of its amount from the contact it " +
			"names, and the first rule it meets (FR-BNK-06, FR-BNK-07). Suggestions are never applied by themselves. Read-only.",
	}, []string{CodeNotFound, banking.CodeRule}, func(ctx context.Context, in *LinePath) (*lineOutput, error) {
		id, err := parseID("line_id", in.LineID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out BankLine
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			l, err := u.Banking().Line(ctx, id)
			if err != nil {
				return err
			}
			out = bankLineOf(l)
			matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &id})
			if err != nil {
				return err
			}
			if len(matches) > 0 {
				m := matches[0]
				out.Match = &BankMatch{Kind: m.Kind, DocumentID: idText(m.DocumentID), EntryID: idText(m.EntryID), TransferID: idText(m.TransferID),
					Basis: m.Basis, RuleID: idText(m.RuleID), MadeBy: m.MadeBy, MadeAt: m.MadeAt.UTC().Format(time.RFC3339)}
			}
			if l.Status != storage.LineUnreconciled && l.Status != storage.LineSuspense {
				return nil
			}
			rules, _, err := banking.Rules(ctx, u)
			if err != nil {
				return err
			}
			sug, err := banking.Suggest(ctx, u, l, rules)
			if err != nil {
				return err
			}
			for _, s := range sug {
				x := BankSuggested{Found: s.Found, Kind: s.Kind, Number: s.Number, Contact: s.Contact}
				if s.DocumentID != uuid.Nil {
					x.DocumentID = s.DocumentID.String()
				}
				if s.ContactID != uuid.Nil {
					x.ContactID = s.ContactID.String()
				}
				if s.Open != nil {
					m := moneyOut(*s.Open)
					x.Open = &m
				}
				if s.Rule != nil {
					x.RuleID, x.RuleName = s.Rule.ID.String(), s.Rule.Name
				}
				out.Suggestions = append(out.Suggestions, x)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &lineOutput{ETag: etag(out.Version), Body: out}, nil
	})
}
