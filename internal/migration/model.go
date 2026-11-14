// Package migration moves the books from the zh-sheet-accounting workbook into Fyniq and proves the move (PRD-001
// FR-MIG-01, FR-MIG-02, §17.1; p1-workbook-migration): the workbook read sheet by sheet into this typed model, a
// validation report before anything is written, the commit through the domain commands, and the reconciliation with
// the workbook's own engine.
package migration

import (
	"time"

	"github.com/cockroachdb/apd/v3"
)

// The model's problem codes (ENG-23).
const (
	CodeWorkbook     = "MIGRATION-01" // the file is not the workbook, or cannot be read
	CodeSheet        = "MIGRATION-02" // a sheet the import reads is missing
	CodeColumn       = "MIGRATION-03" // a column the import reads is missing
	CodeValue        = "MIGRATION-04" // a cell's value is not what its column holds
	CodeUncalculated = "MIGRATION-05" // the workbook was saved without its calculated values
	CodeBlocked      = "MIGRATION-06" // a row the workbook itself did not post (✖): kept out, as the workbook kept it
	CodeArchived     = "MIGRATION-07" // a sheet or row kept in the archived workbook (rev 0.254), not imported
	CodeChecks       = "MIGRATION-08" // the workbook's Checks show a ✖: M1 freezes a workbook with none (§17.1)
	CodePosted       = "MIGRATION-09" // the organisation holds a posted entry (rev 0.254)
	CodeOrganisation = "MIGRATION-10" // the organisation is not set up as the workbook is, such as another base currency
)

// Severity is how a problem weighs on the commit: an error stops it, a warning is reported and does not.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Problem is one thing the validation report says about the workbook: where, what is wrong and how to fix it.
type Problem struct {
	Sheet    string   `json:"sheet,omitempty"`
	Row      int      `json:"row,omitempty"` // the sheet's row; 0 for the sheet as a whole
	Column   string   `json:"column,omitempty"`
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix"`
}

// Status is a workbook status cell's kind.
type Status string

const (
	None    Status = ""
	Fine    Status = "fine"    // ✔
	Review  Status = "review"  // ⚠: posted, with something to look at
	Blocked Status = "blocked" // ✖: not posted
)

// Origin is where a record comes from in the workbook: every record the import writes names it (design D3).
type Origin struct {
	Sheet string
	Row   int
}

// Settings are the Settings sheet's values the import uses.
type Settings struct {
	Company        string
	UEN            string
	GSTNumber      string
	BaseCurrency   string
	BooksStart     time.Time
	YearStart      time.Time
	YearEnd        time.Time
	AsOf           time.Time // the report's "as of" date
	ClosedTo       time.Time // books closed up to
	GSTRegistered  bool
	PaymentTerms   *apd.Decimal
	SystemAccounts map[string]string // the system account's name on the sheet to its code
}

// Account is a row of Accounts.
type Account struct {
	Origin
	Code, Name, Type, ReportGroup, CashFlowLine, Function, DefaultGST, Use string
	Money, Active                                                          bool
	Opening                                                                *apd.Decimal // as typed, debit or credit as the account's side says
	Status                                                                 Status
}

// Item is a row of Items.
type Item struct {
	Origin
	Code, Name, Description, Type, Unit, RevenueAccount, GST, Notes string
	Price                                                           *apd.Decimal
	Active                                                          bool
}

// Contact is a row of Contacts.
type Contact struct {
	Origin
	Name, Role, Email, Phone, Address, Currency, Category, Country, Notes string
	Terms                                                                 *apd.Decimal
}

// Department is a row of the Employees sheet's departments table: a value of the department dimension (rev 0.254).
type Department struct {
	Origin
	Name, SalaryAccount string
}

// Employee is a row of Employees: only their department and salary account are imported, as payroll's dimension and
// account; the person stays in the archive (rev 0.254).
type Employee struct {
	Origin
	Name, Department, SalaryAccount string
}

// GSTCode, Currency, Rate and BankRule are the Lists sheet's tables.
type GSTCode struct {
	Origin
	Code, Description, Direction, F5 string
	Rate                             *apd.Decimal
}

type Currency struct {
	Origin
	Code, Name string
	Default    *apd.Decimal
}

type Rate struct {
	Origin
	Currency string
	Month    time.Time
	Rate     *apd.Decimal // base currency per one unit
}

type BankRule struct {
	Origin
	Contains, Account, GST string
}

// Transaction is a row of Transactions: money in or out of a bank or cash account.
type Transaction struct {
	Origin
	Date                                           time.Time
	Description, Bank, Category, GST, Contact, Ref string
	Project, Notes                                 string
	In, Out                                        *apd.Decimal
	Spread                                         *apd.Decimal
	Reconciled                                     bool
	Status                                         Status
	GSTUsed                                        string // the GST code the workbook used, its own or the category's default
}

// Document is a row of Invoices or of Bills.
type Document struct {
	Origin
	Number, Contact, Description, Category, Currency, GST, Project, Notes string
	Date, ServiceStart                                                    time.Time
	Net                                                                   *apd.Decimal // before GST, in its currency
	Terms, ServiceMonths, Rate                                            *apd.Decimal
	OpeningItem, FullyPaid, Draft                                         bool
	Status                                                                Status
	// As the workbook calculated them, on the Invoices and Bills sheets: its total in its currency, the GST code and
	// rate it used, and its due date (rev 0.259).
	Total    *apd.Decimal
	GSTUsed  string
	RateUsed *apd.Decimal
	Due      time.Time
}

// QuoteOrder is a row of Quotes & Orders.
type QuoteOrder struct {
	Origin
	Number, Kind, Contact, Description, Currency, GST, State, Converted, Project, Notes string
	Date, Until                                                                         time.Time
	Net                                                                                 *apd.Decimal
}

// DocLine is a row of Doc Lines: a line of an invoice, a quote or an order.
type DocLine struct {
	Origin
	Document, Item, Description string
	Quantity, Price, Discount   *apd.Decimal
	Status                      Status
	// As the workbook resolved its blanks: the description, quantity and unit price it used (rev 0.259).
	DescriptionUsed         string
	QuantityUsed, PriceUsed *apd.Decimal
}

// PayRow is a row of Payroll: one employee's month, as the workbook calculated it (design D7).
type PayRow struct {
	Origin
	Month                                                             time.Time
	Employee                                                          string
	Gross, EmployeeCPF, EmployerCPF, SDL, Net, SHG, TotalEmployerCost *apd.Decimal
	Status                                                            Status
}

// Asset is a row of Assets.
type Asset struct {
	Origin
	Description, Account, Notes string
	Bought, From, Disposed      time.Time
	Cost, Life, Residual        *apd.Decimal
	Accumulated                 *apd.Decimal // as calculated, at the workbook's "as of"
	Status                      Status
}

// JournalLine is a row of Journal: a line of a journal entry.
type JournalLine struct {
	Origin
	Entry, Account, Description, Contact, Project, Reference string
	Date                                                     time.Time
	Debit, Credit                                            *apd.Decimal
	Status                                                   Status
}

// Recurring is a row of Recurring.
type Recurring struct {
	Origin
	Kind, Every, Contact, Description, Account, Counter, GST, Project, Prefix string
	First, Last                                                               time.Time
	Amount                                                                    *apd.Decimal
	Active                                                                    bool
}

// BankRec is a row of Bank Rec: a statement's closing balance and what the workbook found.
type BankRec struct {
	Origin
	Account    string
	Date       time.Time
	Closing    *apd.Decimal
	Difference *apd.Decimal
	Status     Status
}

// BudgetLine is a row of Budget: a P&L line's twelve months of a financial year.
type BudgetLine struct {
	Origin
	Year   time.Time
	Line   string
	Months [12]*apd.Decimal
	Notes  string
}

// Holding is a row of Funding's cap table; Instrument a row of its SAFEs and notes.
type Holding struct {
	Origin
	Holder, Kind, Security string
	Shares, Paid           *apd.Decimal
}

type Instrument struct {
	Origin
	Investor              string
	Amount, Cap, Discount *apd.Decimal
}

// TrialBalance is a row of the Trial Balance sheet: an account's opening at the books start, its movement in each
// month of the financial year and its closing at each month end, as the workbook's engine worked them (design D8).
type TrialBalance struct {
	Origin
	Code, Name  string
	Opening     *apd.Decimal
	AtYearStart *apd.Decimal
	Months      []time.Time // each month's end, in order
	Movements   []*apd.Decimal
	Closing     []*apd.Decimal
}

// Check is a row of Checks: one of the workbook's checks by its number, and its status.
type Check struct {
	Origin
	Number             int
	Area, Name, Result string
	Status             Status
}

// CarryForward is the Carry Forward sheet: the balances and the open invoices and bills at its "as of" date, for a
// clean start (§17.1 M3).
type CarryForward struct {
	Balances []Balance
	Invoices []Document
	Bills    []Document
}

// Balance is an account's balance in Carry Forward.
type Balance struct {
	Origin
	Code    string
	Balance *apd.Decimal
}

// Workbook is the workbook as the import reads it.
type Workbook struct {
	Settings     Settings
	Accounts     []Account
	Items        []Item
	Contacts     []Contact
	Departments  []Department
	Employees    []Employee
	GSTCodes     []GSTCode
	Currencies   []Currency
	Rates        []Rate
	BankRules    []BankRule
	Transactions []Transaction
	Invoices     []Document
	Bills        []Document
	QuotesOrders []QuoteOrder
	DocLines     []DocLine
	Payroll      []PayRow
	Timesheets   int // rows kept in the archive (rev 0.254)
	Assets       []Asset
	Journal      []JournalLine
	Recurring    []Recurring
	BankRecs     []BankRec
	Budget       []BudgetLine
	Holdings     []Holding
	Instruments  []Instrument
	TrialBalance []TrialBalance
	Checks       []Check
	CarryForward CarryForward
	// Problems are what reading found, before the mapping and the commands' previews add theirs.
	Problems []Problem
}

// Errors says whether any problem stops the commit.
func (w *Workbook) Errors() bool {
	for _, p := range w.Problems {
		if p.Severity == Error {
			return true
		}
	}
	return false
}
