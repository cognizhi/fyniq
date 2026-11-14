package migration

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// The sheets the import reads (design D5); each is required, and a missing one is a problem the report names. The
// Forecast sheet's drivers are read with the forecast (task 4.6).
var sheets = []string{
	"Settings", "Accounts", "Items", "Contacts", "Employees", "Lists",
	"Transactions", "Invoices", "Bills", "Quotes & Orders", "Doc Lines", "Payroll", "Timesheets", "Assets", "Journal",
	"Recurring", "Bank Rec", "Budget", "Funding", "Trial Balance", "Checks", "Carry Forward",
}

// Read reads the published workbook into the model (FR-MIG-01): every sheet the import uses, its problems collected
// rather than stopping at the first, so the validation report says everything at once. A file that is not a workbook,
// or passes the workbook's bounds, is an error (a *sheet.ReadError); a workbook saved without its calculated values
// is a problem on each sheet that has them.
func Read(data []byte) (*Workbook, error) {
	f, err := sheet.Open(data, sheet.WorkbookBounds)
	if err != nil {
		return nil, err
	}
	w := &Workbook{}
	grids := map[string]sheet.Grid{}
	for _, name := range sheets {
		g, found, err := f.Sheet(name)
		if err != nil {
			return nil, err
		}
		if !found {
			w.Problems = append(w.Problems, Problem{Sheet: name, Severity: Error, Code: CodeSheet,
				Message: fmt.Sprintf("the workbook has no sheet %q", name), Fix: "Import the workbook as it is published, with every sheet"})
			continue
		}
		if g.Uncalculated > 0 {
			w.Problems = append(w.Problems, Problem{Sheet: name, Row: rowOf(g.FirstUncalculated), Severity: Error, Code: CodeUncalculated,
				Message: fmt.Sprintf("%d formulas were saved without their values, the first at %s", g.Uncalculated, g.FirstUncalculated),
				Fix:     "Open the workbook in Excel or LibreOffice, let it recalculate, and save it again"})
		}
		grids[name] = g
	}
	w.readSettings(grids["Settings"])
	base := w.base()
	w.readAccounts(grids["Accounts"], base)
	w.readItems(grids["Items"])
	w.readContacts(grids["Contacts"])
	w.readEmployees(grids["Employees"])
	w.readLists(grids["Lists"])
	w.readTransactions(grids["Transactions"], base)
	w.Invoices = w.readDocuments(w.standard("Invoices", grids["Invoices"]), invoice, base, false)
	w.Bills = w.readDocuments(w.standard("Bills", grids["Bills"]), bill, base, false)
	w.readQuotesOrders(grids["Quotes & Orders"], base)
	w.readDocLines(grids["Doc Lines"])
	w.readPayroll(grids["Payroll"], base)
	w.readTimesheets(grids["Timesheets"])
	w.readAssets(grids["Assets"], base)
	w.readJournal(grids["Journal"], base)
	w.readRecurring(grids["Recurring"], base)
	w.readBankRec(grids["Bank Rec"], base)
	w.readBudget(grids["Budget"], base)
	w.readFunding(grids["Funding"], base)
	w.readTrialBalance(grids["Trial Balance"], base)
	w.readChecks(grids["Checks"])
	w.readCarryForward(grids["Carry Forward"], base)
	return w, nil
}

// rowOf is a cell reference's row: "B12" is 12.
func rowOf(ref string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(ref, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	return n
}

// base is the workbook's base currency; one Fyniq does not keep is a problem, and SGD stands in so the report can go
// on.
func (w *Workbook) base() money.Currency {
	c, err := money.Lookup(w.Settings.BaseCurrency)
	if err != nil {
		w.Problems = append(w.Problems, Problem{Sheet: "Settings", Column: "Base (reporting) currency", Severity: Error, Code: CodeValue,
			Message: fmt.Sprintf("%q is not a currency Fyniq keeps", w.Settings.BaseCurrency), Fix: "Set the base currency to an ISO 4217 code, such as SGD"})
		c, _ = money.Lookup("SGD")
	}
	return c
}

// section is the table that starts at a ✍ mark in column from and ends at the next one.
func (w *Workbook) section(name string, g sheet.Grid, from int) table {
	return newTable(name, g, from, marked(g, "✍", from+1), &w.Problems)
}

// standard is a record sheet's table: from its first ✍ mark.
func (w *Workbook) standard(name string, g sheet.Grid) table {
	return w.section(name, g, max(0, marked(g, "✍", 0)))
}

// headed is a table whose headers are on row head rather than row 5, read to the row's end.
func (w *Workbook) headed(name string, g sheet.Grid, head int) table {
	return table{sheet: name, g: g, cols: g.Header(head), inputs: -1, probs: &w.Problems}
}

// kept reports a row the workbook did not post (✖) and says whether the row is kept: the import keeps it out, as the
// workbook did.
func kept(t table, row int, s Status) bool {
	if s != Blocked {
		return true
	}
	t.problem(row, "Status", Warning, CodeBlocked, "the workbook did not post this row (✖), so it is not imported",
		"Fix the row in the workbook and import again if it should be in the books")
	return false
}

func (w *Workbook) readSettings(g sheet.Grid) {
	const labels, values = 1, 2 // columns B and C
	label := func(prefix string) (string, int) {
		for r := 1; r <= len(g.Rows); r++ {
			if strings.HasPrefix(strings.TrimSpace(g.Cell(r, labels)), prefix) {
				return strings.TrimSpace(g.Cell(r, values)), r
			}
		}
		w.Problems = append(w.Problems, Problem{Sheet: "Settings", Column: prefix, Severity: Error, Code: CodeValue,
			Message: fmt.Sprintf("the Settings sheet has no %q", prefix), Fix: "Restore the setting's row from the published workbook"})
		return "", 0
	}
	date := func(prefix string) time.Time {
		v, r := label(prefix)
		if v == "" {
			return time.Time{}
		}
		d, ok := g.Date(v)
		if !ok {
			w.Problems = append(w.Problems, Problem{Sheet: "Settings", Row: r, Column: prefix, Severity: Error, Code: CodeValue,
				Message: fmt.Sprintf("%q is not a date", v), Fix: "Enter a date"})
		}
		return d
	}
	s := &w.Settings
	s.Company, _ = label("Company name")
	s.UEN, _ = label("Company registration no.")
	s.GSTNumber, _ = label("GST registration no.")
	s.BaseCurrency, _ = label("Base (reporting) currency")
	s.BooksStart = date("Books start date")
	s.YearStart = date("Financial year start")
	s.YearEnd = date("Financial year end")
	s.AsOf = date("Report 'as of' date")
	s.ClosedTo = date("Books closed up to")
	reg, _ := label("GST registered?")
	s.GSTRegistered = strings.EqualFold(reg, "yes")
	if terms, r := label("Default payment terms"); terms != "" {
		d, _, err := apd.NewFromString(terms)
		if err != nil {
			w.Problems = append(w.Problems, Problem{Sheet: "Settings", Row: r, Column: "Default payment terms (days)", Severity: Error,
				Code: CodeValue, Message: fmt.Sprintf("%q is not a number of days", terms), Fix: "Enter a number of days"})
		}
		s.PaymentTerms = d
	}
	// The system accounts are a section: its title in capitals, then a row per account.
	s.SystemAccounts = map[string]string{}
	in := false
	for r := 1; r <= len(g.Rows); r++ {
		b := strings.TrimSpace(g.Cell(r, labels))
		switch {
		case strings.HasPrefix(b, "SYSTEM ACCOUNTS"):
			in = true
		case in && b != "" && strings.ToUpper(b) == b:
			in = false // the next section
		case in && b != "":
			s.SystemAccounts[b] = strings.TrimSpace(g.Cell(r, values))
		}
	}
}

func (w *Workbook) readAccounts(g sheet.Grid, base money.Currency) {
	t := w.standard("Accounts", g)
	for _, r := range t.rows() {
		a := Account{Origin: Origin{"Accounts", r}, Code: t.text(r, "Code"), Name: t.text(r, "Account name"), Type: t.text(r, "Type"),
			ReportGroup: t.text(r, "Report group (line on P&L / Balance Sheet)"), CashFlowLine: t.text(r, "Cash-flow line (balance-sheet accounts)"),
			Function: t.text(r, "Function (for unit economics)"), Money: t.yes(r, "Money account? (bank / card)"),
			DefaultGST: t.text(r, "Default GST code"), Active: t.yes(r, "Active?"), Use: t.text(r, "What to use it for"),
			Opening: t.typed(r, "Opening balance", base.MinorUnits), Status: statusOf(t.text(r, "Status"))}
		if a.Code == "" {
			t.problem(r, "Code", Error, CodeValue, "an account has no code", "Give the account its code")
			continue
		}
		w.Accounts = append(w.Accounts, a)
	}
}

func (w *Workbook) readItems(g sheet.Grid) {
	t := w.standard("Items", g)
	for _, r := range t.rows() {
		w.Items = append(w.Items, Item{Origin: Origin{"Items", r}, Code: t.text(r, "Item code"), Name: t.text(r, "Item name"),
			Description: t.text(r, "Description (printed)"), Type: t.text(r, "Type"), Unit: t.text(r, "Unit"),
			Price: t.typed(r, "Unit price (base currency)", money.UnitPriceScale), RevenueAccount: codeOf(t.text(r, "Revenue account")),
			GST: t.text(r, "GST code"), Active: t.yes(r, "Active?"), Notes: t.text(r, "Notes")})
	}
}

func (w *Workbook) readContacts(g sheet.Grid) {
	t := w.standard("Contacts", g)
	for _, r := range t.rows() {
		w.Contacts = append(w.Contacts, Contact{Origin: Origin{"Contacts", r}, Name: t.text(r, "Name"), Role: t.text(r, "Customer / supplier"),
			Email: t.text(r, "Email"), Phone: t.text(r, "Phone"), Address: t.text(r, "Billing address"), Currency: t.text(r, "Currency"),
			Terms: t.number(r, "Payment terms (days)"), Category: codeOf(t.text(r, "Usual category (optional)")), Country: t.text(r, "Country"),
			Notes: t.text(r, "Notes")})
	}
}

// readEmployees reads each employee's department, and the departments table beside them: a department and its
// salary account are typed; the code beside them is the workbook's.
func (w *Workbook) readEmployees(g sheet.Grid) {
	t := w.standard("Employees", g)
	for _, r := range t.rows() {
		w.Employees = append(w.Employees, Employee{Origin: Origin{"Employees", r}, Name: t.text(r, "Employee name"),
			Department: t.text(r, "Department")})
	}
	salary := map[string]string{}
	if from := marked(g, "✍ DEPARTMENTS", 0); from >= 0 {
		d := newTable("Employees", g, from, from+2, &w.Problems)
		for _, r := range d.rows() {
			dep := Department{Origin: Origin{"Employees", r}, Name: d.text(r, "Department"), SalaryAccount: codeOf(d.text(r, "Salary account"))}
			w.Departments = append(w.Departments, dep)
			salary[dep.Name] = dep.SalaryAccount
		}
	}
	for i, e := range w.Employees {
		w.Employees[i].SalaryAccount = salary[e.Department]
		if salary[e.Department] == "" {
			t.problem(e.Row, "Department", Error, CodeValue, fmt.Sprintf("the department %q is not in the departments table", e.Department),
				"Add the department, with its salary account, to the departments table on the right")
		}
	}
}

func (w *Workbook) readLists(g sheet.Grid) {
	if from := marked(g, "✍ GST CODES", 0); from >= 0 {
		t := w.section("Lists", g, from)
		for _, r := range t.rows() {
			w.GSTCodes = append(w.GSTCodes, GSTCode{Origin: Origin{"Lists", r}, Code: t.text(r, "GST code"), Description: t.text(r, "Description"),
				Rate: t.number(r, "Rate"), Direction: t.text(r, "Direction"), F5: t.text(r, "GST F5 return")})
		}
	}
	if from := marked(g, "✍ CURRENCIES", 0); from >= 0 {
		t := w.section("Lists", g, from)
		for _, r := range t.rows() {
			w.Currencies = append(w.Currencies, Currency{Origin: Origin{"Lists", r}, Code: t.text(r, "Currency"), Name: t.text(r, "Name"),
				Default: t.number(r, "Default rate (1 unit = ? base)")})
		}
	}
	if from := marked(g, "✍ MONTHLY FX RATES", 0); from >= 0 {
		t := w.section("Lists", g, from)
		for _, r := range t.rows() {
			w.Rates = append(w.Rates, Rate{Origin: Origin{"Lists", r}, Currency: t.text(r, "Currency"),
				Month: t.date(r, "Month (any date in it)"), Rate: t.number(r, "Rate (1 unit = ? base)")})
		}
	}
	if from := marked(g, "✍ BANK IMPORT RULES", 0); from >= 0 {
		t := w.section("Lists", g, from)
		for _, r := range t.rows() {
			w.BankRules = append(w.BankRules, BankRule{Origin: Origin{"Lists", r}, Contains: t.text(r, "If bank description contains"),
				Account: codeOf(t.text(r, "…use category")), GST: t.text(r, "GST code (optional)")})
		}
	}
}

func (w *Workbook) readTransactions(g sheet.Grid, base money.Currency) {
	t := w.standard("Transactions", g)
	for _, r := range t.rows() {
		x := Transaction{Origin: Origin{"Transactions", r}, Date: t.date(r, "Date"), Description: t.text(r, "Description"),
			In: t.typed(r, "Money in", base.MinorUnits), Out: t.typed(r, "Money out", base.MinorUnits),
			Bank: codeOf(t.text(r, "Bank / cash account")), Category: codeOf(t.text(r, "Category")), GST: t.text(r, "GST code (optional)"),
			Contact: t.text(r, "Customer / supplier"), Ref: t.text(r, "Invoice / bill # (Reference)"), Project: t.text(r, "Project"),
			Spread: t.number(r, "Spread over months"), Reconciled: t.yes(r, "Reconciled?"), Notes: t.text(r, "Notes / receipt link"),
			Status: statusOf(t.text(r, "Status"))}
		if kept(t, r, x.Status) {
			w.Transactions = append(w.Transactions, x)
		}
	}
}

// documentColumns are the headers that differ between Invoices and Bills.
type documentColumns struct{ number, date, contact, category string }

var (
	invoice = documentColumns{"Invoice #", "Invoice date", "Customer", "Revenue category"}
	bill    = documentColumns{"Bill # (supplier's invoice no.)", "Bill date", "Supplier", "Expense / asset category"}
)

// readDocuments reads the invoices or bills of table t: the Invoices or Bills sheet, or Carry Forward's open items,
// whose amounts the workbook calculated (calculated is true).
func (w *Workbook) readDocuments(t table, c documentColumns, base money.Currency, calculated bool) []Document {
	var out []Document
	_, hasStatus := t.cols["Status"]
	for _, r := range t.rows() {
		cur, ccy := t.text(r, "Currency"), base
		if cur != "" {
			var err error
			if ccy, err = money.Lookup(cur); err != nil {
				t.problem(r, "Currency", Error, CodeValue, fmt.Sprintf("%q is not a currency Fyniq keeps", cur), "Use an ISO 4217 code, such as USD")
				ccy = base
			}
		}
		const amount = "Amount before GST (in its currency)"
		d := Document{Origin: Origin{t.sheet, r}, Number: t.text(r, c.number), Date: t.date(r, c.date), Contact: t.text(r, c.contact),
			Description: t.text(r, "Description"), Category: codeOf(t.text(r, c.category)), Currency: cur, GST: t.text(r, "GST code (optional)"),
			Terms: t.number(r, "Payment terms (days)"), ServiceMonths: t.number(r, "Service months (spread)"),
			ServiceStart: t.date(r, "Service start (optional)"), Rate: t.number(r, "FX rate (optional)"), Project: t.text(r, "Project"),
			OpeningItem: t.yes(r, "Opening balance item?"), FullyPaid: t.yes(r, "Mark as fully paid?"), Draft: t.yes(r, "Draft? (not booked)"),
			Notes: t.text(r, "Notes / document link")}
		if calculated {
			d.Net = t.calculated(r, amount, ccy)
		} else {
			d.Net = t.typed(r, amount, ccy.MinorUnits)
		}
		if hasStatus {
			d.Status = statusOf(t.text(r, "Status"))
		}
		if _, ok := t.cols["Total (own currency)"]; ok && !calculated { // the Invoices and Bills sheets' own figures
			d.Total, d.GSTUsed, d.RateUsed, d.Due = t.calculated(r, "Total (own currency)", ccy), t.text(r, "GST code used"),
				t.number(r, "FX rate used"), t.date(r, "Due date")
		}
		if kept(t, r, d.Status) {
			out = append(out, d)
		}
	}
	return out
}

func (w *Workbook) readQuotesOrders(g sheet.Grid, base money.Currency) {
	t := w.standard("Quotes & Orders", g)
	for _, r := range t.rows() {
		places := base.MinorUnits
		if c, err := money.Lookup(t.text(r, "Currency")); err == nil {
			places = c.MinorUnits
		}
		w.QuotesOrders = append(w.QuotesOrders, QuoteOrder{Origin: Origin{"Quotes & Orders", r}, Number: t.text(r, "Number"),
			Kind: t.text(r, "Type"), Date: t.date(r, "Date"), Contact: t.text(r, "Customer / supplier"), Description: t.text(r, "Description"),
			Currency: t.text(r, "Currency"), Net: t.typed(r, "Amount before GST (blank = Doc Lines)", places),
			GST: t.text(r, "GST code (optional)"), Until: t.date(r, "Valid until / delivery date"), State: t.text(r, "Status"),
			Converted: t.text(r, "Invoice / bill #"), Project: t.text(r, "Project"), Notes: t.text(r, "Notes / terms")})
	}
}

func (w *Workbook) readDocLines(g sheet.Grid) {
	t := w.standard("Doc Lines", g)
	for _, r := range t.rows() {
		l := DocLine{Origin: Origin{"Doc Lines", r}, Document: t.text(r, "Document # (invoice / quote / order)"), Item: codeOf(t.text(r, "Item")),
			Description: t.text(r, "Description (blank = item's)"), Quantity: t.number(r, "Qty (blank = 1)"),
			Price: t.number(r, "Unit price (blank = item's)"), Discount: t.number(r, "Discount %"), Status: statusOf(t.text(r, "Status")),
			DescriptionUsed: t.text(r, "Description used"), QuantityUsed: t.number(r, "Qty used"), PriceUsed: t.number(r, "Price used")}
		if kept(t, r, l.Status) {
			w.DocLines = append(w.DocLines, l)
		}
	}
}

// readPayroll keeps each row's figures as the workbook calculated them, which the month's summary journal posts
// (design D7).
func (w *Workbook) readPayroll(g sheet.Grid, base money.Currency) {
	t := w.standard("Payroll", g)
	for _, r := range t.rows() {
		p := PayRow{Origin: Origin{"Payroll", r}, Month: t.date(r, "Pay month (any date)"), Employee: t.text(r, "Employee"),
			Gross: t.calculated(r, "Gross pay", base), EmployeeCPF: t.calculated(r, "Employee CPF", base),
			EmployerCPF: t.calculated(r, "Employer CPF", base), SDL: t.calculated(r, "SDL", base),
			Net: t.calculated(r, "Net pay to employee", base), SHG: t.calculated(r, "SHG used", base),
			TotalEmployerCost: t.calculated(r, "Total cost to company", base), Status: statusOf(t.text(r, "Status"))}
		if kept(t, r, p.Status) {
			w.Payroll = append(w.Payroll, p)
		}
	}
}

func (w *Workbook) readTimesheets(g sheet.Grid) {
	t := w.standard("Timesheets", g)
	if n := len(t.rows()); n > 0 {
		w.Timesheets = n
		t.problem(0, "", Warning, CodeArchived, fmt.Sprintf("%d timesheet rows stay in the archived workbook", n),
			"Nothing to do: projects and timesheets start in Fyniq after go-live")
	}
}

func (w *Workbook) readAssets(g sheet.Grid, base money.Currency) {
	t := w.standard("Assets", g)
	for _, r := range t.rows() {
		a := Asset{Origin: Origin{"Assets", r}, Description: t.text(r, "Asset description"), Account: codeOf(t.text(r, "Asset account")),
			Bought: t.date(r, "Purchase date"), Cost: t.typed(r, "Cost (excl. GST)", base.MinorUnits), Life: t.number(r, "Useful life (months)"),
			Residual: t.typed(r, "Residual value", base.MinorUnits), From: t.date(r, "Depreciate from (optional)"),
			Disposed: t.date(r, "Disposal date"), Notes: t.text(r, "Serial no. / location / notes"),
			Accumulated: t.calculated(r, "Accumulated depreciation (as of)", base), Status: statusOf(t.text(r, "Status"))}
		if kept(t, r, a.Status) {
			w.Assets = append(w.Assets, a)
		}
	}
}

func (w *Workbook) readJournal(g sheet.Grid, base money.Currency) {
	t := w.standard("Journal", g)
	for _, r := range t.rows() {
		l := JournalLine{Origin: Origin{"Journal", r}, Entry: t.text(r, "Entry #"), Date: t.date(r, "Date"), Account: codeOf(t.text(r, "Account")),
			Description: t.text(r, "Description"), Debit: t.typed(r, "Debit", base.MinorUnits), Credit: t.typed(r, "Credit", base.MinorUnits),
			Contact: t.text(r, "Contact"), Project: t.text(r, "Project"), Reference: t.text(r, "Reference"), Status: statusOf(t.text(r, "Status"))}
		if kept(t, r, l.Status) {
			w.Journal = append(w.Journal, l)
		}
	}
}

func (w *Workbook) readRecurring(g sheet.Grid, base money.Currency) {
	t := w.standard("Recurring", g)
	for _, r := range t.rows() {
		w.Recurring = append(w.Recurring, Recurring{Origin: Origin{"Recurring", r}, Active: t.yes(r, "Active?"), Kind: t.text(r, "Create a…"),
			Every: t.text(r, "Every"), First: t.date(r, "First date"), Last: t.date(r, "Last date (optional)"),
			Contact: t.text(r, "Customer / supplier"), Description: t.text(r, "Description"),
			Account: codeOf(t.text(r, "Category / revenue / debit account")),
			Counter: codeOf(t.text(r, "Bank account (transactions) / credit account (journals)")),
			Amount:  t.typed(r, "Amount (+in / −out; before GST for invoices & bills)", base.MinorUnits),
			GST:     t.text(r, "GST code"), Project: t.text(r, "Project"), Prefix: t.text(r, "Number prefix (invoices / bills)")})
	}
}

func (w *Workbook) readBankRec(g sheet.Grid, base money.Currency) {
	t := w.standard("Bank Rec", g)
	for _, r := range t.rows() {
		w.BankRecs = append(w.BankRecs, BankRec{Origin: Origin{"Bank Rec", r}, Account: codeOf(t.text(r, "Bank / card account")),
			Date: t.date(r, "Statement date"), Closing: t.typed(r, "Statement closing balance", base.MinorUnits),
			Difference: t.calculated(r, "Difference (statement − reconciled)", base), Status: statusOf(t.text(r, "Status"))})
	}
}

var budgetMonths = [12]string{"M1 Jan", "M2 Feb", "M3 Mar", "M4 Apr", "M5 May", "M6 Jun", "M7 Jul", "M8 Aug", "M9 Sep", "M10 Oct",
	"M11 Nov", "M12 Dec"}

func (w *Workbook) readBudget(g sheet.Grid, base money.Currency) {
	t := w.standard("Budget", g)
	for _, r := range t.rows() {
		b := BudgetLine{Origin: Origin{"Budget", r}, Year: t.date(r, "Financial year starting"), Line: t.text(r, "P&L line"),
			Notes: t.text(r, "Notes / assumptions")}
		for i, m := range budgetMonths {
			b.Months[i] = t.typed(r, m, base.MinorUnits)
		}
		w.Budget = append(w.Budget, b)
	}
}

// readFunding reads the Funding sheet's first two sections, each with its headers on the row after its title: the cap
// table, whose rows end at its "Issued shares" total, and the SAFEs and convertible notes, whose rows end at their
// "Total". The next round and the ownership after it are what-ifs, not records.
func (w *Workbook) readFunding(g sheet.Grid, base money.Currency) {
	const names = 1 // column B
	title := func(prefix string) int {
		for r := 1; r <= len(g.Rows); r++ {
			if strings.HasPrefix(strings.TrimSpace(g.Cell(r, names)), prefix) {
				return r
			}
		}
		w.Problems = append(w.Problems, Problem{Sheet: "Funding", Severity: Error, Code: CodeColumn,
			Message: fmt.Sprintf("the Funding sheet has no section %q", prefix), Fix: "Import the workbook as it is published"})
		return 0
	}
	if r := title("1. CURRENT CAP TABLE"); r > 0 {
		t := w.headed("Funding", g, r+1)
		for row := r + 2; row <= len(g.Rows); row++ {
			holder := t.text(row, "Shareholder")
			if strings.HasPrefix(holder, "Issued shares") {
				break
			}
			if holder == "" {
				continue
			}
			w.Holdings = append(w.Holdings, Holding{Origin: Origin{"Funding", row}, Holder: holder, Kind: t.text(row, "Type"),
				Security: t.text(row, "Security"), Shares: t.number(row, "Shares"), Paid: t.typed(row, "Amount paid", base.MinorUnits)})
		}
	}
	if r := title("2. SAFEs"); r > 0 {
		t := w.headed("Funding", g, r+1)
		for row := r + 2; row <= len(g.Rows); row++ {
			investor := t.text(row, "Investor")
			if investor == "Total" {
				break
			}
			if investor == "" {
				continue
			}
			w.Instruments = append(w.Instruments, Instrument{Origin: Origin{"Funding", row}, Investor: investor,
				Amount: t.typed(row, "Amount", base.MinorUnits), Cap: t.typed(row, "Valuation cap", base.MinorUnits),
				Discount: t.number(row, "Discount")})
		}
	}
}

// readTrialBalance reads the trial balance the workbook's engine worked: its header is on row 7, with each month's end
// as a date serial over the movements, between "Balance at FY start" and "FY movement", and over the closings after
// it.
func (w *Workbook) readTrialBalance(g sheet.Grid, base money.Currency) {
	const head = 7
	t := w.headed("Trial Balance", g, head)
	code, opening, atStart, fy := t.col("Code"), t.col("Opening (books start)"), t.col("Balance at FY start"), t.col("FY movement")
	if code < 0 || opening < 0 || atStart < 0 || fy < 0 {
		return
	}
	var months []time.Time
	var moves []int
	for c := atStart + 1; c < fy; c++ {
		if d, ok := g.Date(g.Cell(head, c)); ok {
			months, moves = append(months, d), append(moves, c)
		}
	}
	at := func(r, c int) *apd.Decimal {
		v := strings.TrimSpace(g.Cell(r, c))
		if v == "" {
			return nil
		}
		d, _, err := apd.NewFromString(v)
		if err != nil || d.Form != apd.Finite {
			t.problem(r, g.Cell(head, c), Error, CodeValue, fmt.Sprintf("%q is not an amount", v), "Open the workbook, recalculate and save it again")
			return nil
		}
		return rounding.Import(d, base)
	}
	for r := head + 1; r <= len(g.Rows); r++ {
		c := strings.TrimSpace(g.Cell(r, code))
		if c == "" {
			continue
		}
		tb := TrialBalance{Origin: Origin{"Trial Balance", r}, Code: c, Name: strings.TrimSpace(g.Cell(r, code+1)),
			Opening: at(r, opening), AtYearStart: at(r, atStart), Months: months}
		for i, mc := range moves {
			tb.Movements = append(tb.Movements, at(r, mc))
			tb.Closing = append(tb.Closing, at(r, fy+1+i))
		}
		w.TrialBalance = append(w.TrialBalance, tb)
	}
}

// readChecks reads the workbook's checks: their header is on row 6, and each check has its number.
func (w *Workbook) readChecks(g sheet.Grid) {
	const head = 6
	t := w.headed("Checks", g, head)
	num := t.col("#")
	if num < 0 {
		return
	}
	for r := head + 1; r <= len(g.Rows); r++ {
		n, err := strconv.Atoi(strings.TrimSpace(g.Cell(r, num)))
		if err != nil {
			continue
		}
		w.Checks = append(w.Checks, Check{Origin: Origin{"Checks", r}, Number: n, Area: t.text(r, "Area"), Name: t.text(r, "Check"),
			Result: t.text(r, "Result"), Status: statusOf(t.text(r, "Status"))})
	}
}

// readCarryForward reads the clean start's three tables, side by side with their headers on row 5: the balances, the
// open invoices and the open bills, each ending at the next or, for the bills, at the first column without a header.
// Every figure in them is the workbook's.
func (w *Workbook) readCarryForward(g sheet.Grid, base money.Currency) {
	h := g.Header(headerRow)
	inv, okInv := h[invoice.number]
	bil, okBill := h[bill.number]
	if !okInv || !okBill {
		w.Problems = append(w.Problems, Problem{Sheet: "Carry Forward", Row: headerRow, Severity: Error, Code: CodeColumn,
			Message: "the Carry Forward sheet has no open invoices or open bills table", Fix: "Import the workbook as it is published"})
		return
	}
	end := bil
	for strings.TrimSpace(g.Cell(headerRow, end)) != "" {
		end++
	}
	bal := newTable("Carry Forward", g, 0, inv, &w.Problems)
	for _, r := range bal.rows() {
		if code := bal.text(r, "Code"); code != "" {
			w.CarryForward.Balances = append(w.CarryForward.Balances, Balance{Origin: Origin{"Carry Forward", r}, Code: code,
				Balance: bal.calculated(r, "Opening balance (paste into Accounts)", base)})
		}
	}
	w.CarryForward.Invoices = w.readDocuments(newTable("Carry Forward", g, inv, bil, &w.Problems), invoice, base, true)
	w.CarryForward.Bills = w.readDocuments(newTable("Carry Forward", g, bil, end, &w.Problems), bill, base, true)
}
