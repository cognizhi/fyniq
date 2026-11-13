package migration

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// ------------------------------------------------------------------ the generic import (FR-MIG-03, design D9; rev 0.270)

// CodeGeneric is a file the generic import cannot read: not CSV or XLSX, without its entity's columns, or over its rows.
const CodeGeneric = "MIGRATION-17"

// MaxImportRows is how many rows one file of the generic import takes.
const MaxImportRows = 1000

// importEntity is one entity the generic import takes: the workbook's sheet its template lays out, the sheet's input
// columns the template has, how the sheet is read, and the records its rows become, the workbook import's own.
type importEntity struct {
	sheet   string
	columns []string
	read    func(w *Workbook, g sheet.Grid, base money.Currency)
	records func(w *Workbook, base money.Currency) []Record
}

// The entities the generic import takes, each by its workbook sheet's columns (design D9).
var importEntities = map[string]importEntity{
	"contacts": {sheet: "Contacts",
		columns: []string{"Name", "Customer / supplier", "Email", "Phone", "Billing address", "Currency", "Payment terms (days)", "Usual category (optional)",
			"Country", "Notes"},
		read:    func(w *Workbook, g sheet.Grid, _ money.Currency) { w.readContacts(g) },
		records: func(w *Workbook, _ money.Currency) []Record { return w.contactRecords(Mapping{}) }},
	"items": {sheet: "Items",
		columns: []string{"Item code", "Item name", "Description (printed)", "Type", "Unit", "Unit price (base currency)", "Revenue account", "GST code",
			"Active?", "Notes"},
		read:    func(w *Workbook, g sheet.Grid, _ money.Currency) { w.readItems(g) },
		records: func(w *Workbook, base money.Currency) []Record { return w.itemRecords(Mapping{}, base.Code) }},
	"accounts": {sheet: "Accounts",
		columns: []string{"Code", "Account name", "Type", "Report group (line on P&L / Balance Sheet)", "Cash-flow line (balance-sheet accounts)",
			"Function (for unit economics)", "Money account? (bank / card)", "Default GST code", "Active?", "What to use it for"},
		read: func(w *Workbook, g sheet.Grid, _ money.Currency) { w.readImportAccounts(g) },
		records: func(w *Workbook, base money.Currency) []Record {
			var out []Record
			for _, a := range w.Accounts {
				out = append(out, accountRecord(AccountMap{Row: a.Row, Workbook: a.Code, Name: a.Name, WorkbookType: a.Type, Fyniq: a.Code,
					Action: Create, Type: string(typeOf(a.Type)), ReportGroup: a.ReportGroup, CashFlowLine: a.CashFlowLine, Function: a.Function,
					MoneyAccount: a.Money, DefaultGST: a.DefaultGST}, Mapping{}, base.Code))
			}
			return out
		}},
}

// readImportAccounts reads the accounts of an accounts file: the Accounts sheet's columns of the template, its opening
// balances left to the opening balances' own import.
func (w *Workbook) readImportAccounts(g sheet.Grid) {
	t := w.standard("Accounts", g)
	for _, r := range t.rows() {
		a := Account{Origin: Origin{"Accounts", r}, Code: t.text(r, "Code"), Name: t.text(r, "Account name"), Type: t.text(r, "Type"),
			ReportGroup: t.text(r, "Report group (line on P&L / Balance Sheet)"), CashFlowLine: t.text(r, "Cash-flow line (balance-sheet accounts)"),
			Function: t.text(r, "Function (for unit economics)"), Money: t.yes(r, "Money account? (bank / card)"), DefaultGST: t.text(r, "Default GST code"),
			Active: t.yes(r, "Active?"), Use: t.text(r, "What to use it for")}
		if a.Code == "" {
			t.problem(r, "Code", Error, CodeValue, "an account has no code", "Give the account its code")
			continue
		}
		w.Accounts = append(w.Accounts, a)
	}
}

// ImportEntities are the entities the generic import takes, by name.
func ImportEntities() []string {
	out := make([]string, 0, len(importEntities))
	for k := range importEntities {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Template is an entity's template (FR-MIG-03, design D9): its workbook sheet's input columns as the header, with no
// row, as CSV or XLSX; a file filled in it, or the workbook's own sheet pasted, is what the import reads.
func Template(entity, format string, created time.Time) ([]byte, error) {
	e, ok := importEntities[entity]
	if !ok {
		return nil, fmt.Errorf("migration: no entity %q", entity)
	}
	var b bytes.Buffer
	var w sheet.Writer
	switch format {
	case "csv":
		w = sheet.NewCSV(&b)
	case "xlsx":
		w = sheet.NewXLSX(&b, created)
	default:
		return nil, fmt.Errorf("migration: no format %q", format)
	}
	err := sheet.Write(w, []sheet.Sheet{{Name: e.sheet, Header: e.columns}})
	return b.Bytes(), err
}

// isoDate is a date as a CSV file writes it.
var isoDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// excelDay is a date as the workbook keeps it, the days since 30 December 1899.
func excelDay(t time.Time) string {
	return strconv.Itoa(int(t.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24))
}

// gridOf is an uploaded file's table as a sheet's grid, its header on row 5 as the workbook's tables have it: an XLSX
// file's sheet of the entity's name, else its first, or a CSV file whose dates are written YYYY-MM-DD; the header is the
// first of the first ten rows that names the entity's first column. shift is how many rows the header moved down, so a
// row is named as the file numbers it.
func gridOf(e importEntity, data []byte) (sheet.Grid, int, error) {
	refuse := func(msg, fix string) error {
		return Refused{{Code: CodeGeneric, Field: "file", Message: msg, Fix: fix}}
	}
	var g sheet.Grid
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		wb, err := sheet.Open(data, sheet.WorkbookBounds)
		var re *sheet.ReadError
		if errors.As(err, &re) {
			return g, 0, refuse(re.Message, re.Fix)
		}
		if err != nil {
			return g, 0, err
		}
		name := e.sheet
		if !slices.Contains(wb.Names(), name) && len(wb.Names()) > 0 {
			name = wb.Names()[0]
		}
		if g, _, err = wb.Sheet(name); err != nil {
			return g, 0, err
		}
	} else {
		r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
		r.FieldsPerRecord = -1
		for {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return g, 0, refuse(fmt.Sprintf("the file is neither an .xlsx workbook nor CSV: %v", err), "Save it as CSV (UTF-8) or as an .xlsx workbook")
			}
			for i, v := range rec {
				if isoDate.MatchString(strings.TrimSpace(v)) {
					if d, err := time.Parse(time.DateOnly, strings.TrimSpace(v)); err == nil {
						rec[i] = excelDay(d)
					}
				}
			}
			g.Rows = append(g.Rows, rec)
		}
	}
	head := -1
	for r := 0; r < min(10, len(g.Rows)) && head < 0; r++ {
		if slices.ContainsFunc(g.Rows[r], func(c string) bool { return strings.TrimSpace(c) == e.columns[0] }) {
			head = r
		}
	}
	if head < 0 {
		return g, 0, refuse(fmt.Sprintf("the file has no header naming %q in its first ten rows", e.columns[0]),
			"Fill in the template, whose first row names the columns")
	}
	if n := len(g.Rows) - head - 1; n > MaxImportRows {
		return g, 0, refuse(fmt.Sprintf("the file has %d rows; one file takes at most %d", n, MaxImportRows), "Split it into files of at most 1,000 rows")
	}
	// The header to row 5, as the workbook's tables have it (headerRow).
	rows := append(make([][]string, headerRow-1), g.Rows[head:]...)
	if head >= headerRow-1 {
		rows = g.Rows[head-(headerRow-1):]
	}
	g.Rows = rows
	return g, headerRow - 1 - head, nil
}

// ReadImport reads an uploaded file of an entity (FR-MIG-03): its rows as the workbook's sheet's are read, in the
// organisation's base currency, with the problems reading found.
func ReadImport(entity string, data []byte, base money.Currency) (*Workbook, error) {
	e, ok := importEntities[entity]
	if !ok {
		return nil, fmt.Errorf("migration: no entity %q", entity)
	}
	g, shift, err := gridOf(e, data)
	if err != nil {
		return nil, err
	}
	w := &Workbook{Settings: Settings{BaseCurrency: base.Code}}
	e.read(w, g, base)
	// Each row as the file numbers it.
	back := func(o *Origin) {
		if o.Row > 0 {
			o.Row -= shift
		}
	}
	for i := range w.Contacts {
		back(&w.Contacts[i].Origin)
	}
	for i := range w.Items {
		back(&w.Items[i].Origin)
	}
	for i := range w.Accounts {
		back(&w.Accounts[i].Origin)
	}
	for i := range w.Problems {
		if w.Problems[i].Row > 0 {
			w.Problems[i].Row -= shift
		}
	}
	return w, nil
}

// ImportResult is what a generic import made, or would make: a record per row, and every problem with its row.
type ImportResult struct {
	Rows     int
	Made     []Made
	Problems []Problem
}

// RunImport makes an entity's records from w's rows in u (FR-MIG-03): each through its command, as the workbook import
// makes it, in its unit of work. Every row is tried, and every refusal is a problem with its row; a row that fails
// otherwise stops the run. With any problem, the caller rolls the unit of work back: a file is imported whole or not at
// all.
func RunImport(ctx context.Context, u storage.UnitOfWork, entity string, w *Workbook, base money.Currency, e *Env) (ImportResult, error) {
	ie, ok := importEntities[entity]
	if !ok {
		return ImportResult{}, fmt.Errorf("migration: no entity %q", entity)
	}
	out := ImportResult{Problems: append([]Problem{}, w.Problems...)}
	if e.made == nil {
		e.made = map[string]uuid.UUID{}
	}
	for _, r := range ie.records(w, base) {
		out.Rows++
		made, err := r.make(ctx, u, e)
		var refusals Refused
		switch {
		case errors.As(err, &refusals):
			for _, x := range refusals {
				out.Problems = append(out.Problems, Problem{Sheet: r.Sheet, Row: r.Row, Column: x.Field, Severity: Error, Code: x.Code, Message: x.Message, Fix: x.Fix})
			}
		case err != nil:
			var se *storage.Error
			if errors.As(err, &se) && se.Code == storage.CodeDuplicate {
				out.Problems = append(out.Problems, Problem{Sheet: r.Sheet, Row: r.Row, Severity: Error, Code: CodeGeneric,
					Message: "the organisation has a record of this name or code already", Fix: "Leave the row out, or give it another name or code"})
				return out, nil // the unit of work cannot go on after the database refused a write
			}
			return out, err
		default:
			e.made[r.Key] = made.ID
			out.Made = append(out.Made, made)
		}
	}
	return out, nil
}
