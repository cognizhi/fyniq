package migration

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/cognizhi/fyniq/internal/sheet"
)

// ReportFormat names the validation report's format: the sign-offs that follow name its SHA-256 (design D11).
const ReportFormat = "fyniq-migration-report@1"

// Report is the validation report (FR-MIG-01, design D2): what the workbook holds, sheet by sheet, and every problem
// found, before anything is committed. One with an error cannot be committed.
type Report struct {
	Format   string       `json:"format"`
	Workbook Facts        `json:"workbook"`
	Sheets   []SheetCount `json:"sheets"`
	Problems []Problem    `json:"problems"`
	Errors   int          `json:"errors"`
	Warnings int          `json:"warnings"`
}

// Facts are what the report says of the workbook as a whole.
type Facts struct {
	FileName     string `json:"file_name"`
	SHA256       string `json:"sha256"`
	Company      string `json:"company,omitempty"`
	BaseCurrency string `json:"base_currency,omitempty"`
	BooksStart   string `json:"books_start,omitempty"`
	YearStart    string `json:"year_start,omitempty"`
	AsOf         string `json:"as_of,omitempty"`
	ClosedTo     string `json:"closed_to,omitempty"`
}

// SheetCount is a sheet's rows the import reads, and its problems.
type SheetCount struct {
	Sheet    string `json:"sheet"`
	Rows     int    `json:"rows"`
	Errors   int    `json:"errors"`
	Warnings int    `json:"warnings"`
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// Freeze is M1's rule (§17.1): the frozen workbook's Checks show no ✖. Each one that does is an error.
func (w *Workbook) Freeze() {
	for _, c := range w.Checks {
		if c.Status == Blocked {
			w.Problems = append(w.Problems, Problem{Sheet: "Checks", Row: c.Row, Column: "Status", Severity: Error, Code: CodeChecks,
				Message: fmt.Sprintf("check %d, %q, shows ✖: %s", c.Number, c.Name, c.Result),
				Fix:     "Fix what the check names in the workbook, run make verify on it, and upload it again"})
		}
	}
}

// Report is the workbook's validation report, for the file named with its hash.
func (w *Workbook) Report(fileName, sha string) Report {
	s := w.Settings
	r := Report{Format: ReportFormat, Workbook: Facts{FileName: fileName, SHA256: sha, Company: s.Company, BaseCurrency: s.BaseCurrency,
		BooksStart: day(s.BooksStart), YearStart: day(s.YearStart), AsOf: day(s.AsOf), ClosedTo: day(s.ClosedTo)}, Problems: []Problem{}}
	rows := map[string]int{
		"Settings": len(s.SystemAccounts), "Accounts": len(w.Accounts), "Items": len(w.Items), "Contacts": len(w.Contacts),
		"Employees": len(w.Departments), "Lists": len(w.GSTCodes) + len(w.Currencies) + len(w.Rates) + len(w.BankRules),
		"Transactions": len(w.Transactions), "Invoices": len(w.Invoices), "Bills": len(w.Bills), "Quotes & Orders": len(w.QuotesOrders),
		"Doc Lines": len(w.DocLines), "Payroll": len(w.Payroll), "Timesheets": 0, "Assets": len(w.Assets), "Journal": len(w.Journal),
		"Recurring": len(w.Recurring), "Bank Rec": len(w.BankRecs), "Budget": len(w.Budget), "Funding": len(w.Holdings) + len(w.Instruments),
		"Trial Balance": len(w.TrialBalance), "Checks": len(w.Checks),
		"Carry Forward": len(w.CarryForward.Balances) + len(w.CarryForward.Invoices) + len(w.CarryForward.Bills),
	}
	order := map[string]int{}
	for i, name := range sheets {
		order[name] = i
		r.Sheets = append(r.Sheets, SheetCount{Sheet: name, Rows: rows[name]})
	}
	ps := slices.Clone(w.Problems)
	slices.SortStableFunc(ps, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(orderOf(order, a.Sheet), orderOf(order, b.Sheet)), cmp.Compare(a.Row, b.Row))
	})
	for _, p := range ps {
		r.Problems = append(r.Problems, p)
		i, inSheet := order[p.Sheet]
		switch p.Severity {
		case Error:
			r.Errors++
			if inSheet {
				r.Sheets[i].Errors++
			}
		default:
			r.Warnings++
			if inSheet {
				r.Sheets[i].Warnings++
			}
		}
	}
	return r
}

// orderOf is a sheet's place in the report; a problem of no sheet, such as the organisation's, comes first.
func orderOf(order map[string]int, sheet string) int {
	if i, ok := order[sheet]; ok {
		return i
	}
	return -1
}

// Unreadable is the report of a file that is not the workbook: its one problem says why.
func Unreadable(fileName, sha string, why Problem) Report {
	r := Report{Format: ReportFormat, Workbook: Facts{FileName: fileName, SHA256: sha}, Problems: []Problem{why}, Errors: 1}
	for _, name := range sheets {
		r.Sheets = append(r.Sheets, SheetCount{Sheet: name})
	}
	return r
}

// Sealed is the report as it is kept, and its SHA-256.
func (r Report) Sealed() ([]byte, string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, "", err
	}
	s := sha256.Sum256(b)
	return b, hex.EncodeToString(s[:]), nil
}

var statusMark = map[Status]string{Fine: "✔", Review: "⚠", Blocked: "✖"}

// ChecksExport is the workbook's Checks status as CSV, kept with the workbook as M1's evidence (§17.1): each check by
// its number, area and name, with its result and status.
func (w *Workbook) ChecksExport() []byte {
	s := sheet.Sheet{Name: "Checks", Header: []string{"#", "Area", "Check", "Result", "Status"}}
	for _, c := range w.Checks {
		s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Number(strconv.Itoa(c.Number), 0), sheet.Text(c.Area), sheet.Text(c.Name),
			sheet.Text(c.Result), sheet.Text(statusMark[c.Status])}})
	}
	return sheet.CSV(s)
}
