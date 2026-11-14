package migration

import (
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// The workbook's tables (p1-workbook-migration design D1): row 4 marks the columns a person fills in (✍) and the ones
// the workbook calculates (⚙), row 5 names them, and data starts at row 6.
const (
	markRow   = 4
	headerRow = 5
	firstRow  = 6
)

// table is one of a sheet's tables: its columns by header, within the columns from..end (a sheet such as Lists holds
// several tables side by side, two of which have a "Currency" column). The columns from its ⚙ mark on are the
// workbook's; only the ones before it, which a person fills in, make a row.
type table struct {
	sheet  string
	g      sheet.Grid
	cols   map[string]int
	from   int // the table's first column
	inputs int // the first column the workbook calculates; -1 for the row's end
	probs  *[]Problem
}

// newTable is the table whose headers start at column from and end before column end (-1: the row's end).
func newTable(name string, g sheet.Grid, from, end int, probs *[]Problem) table {
	t := table{sheet: name, g: g, cols: map[string]int{}, from: from, inputs: end, probs: probs}
	if m := marked(g, "⚙", from); m >= 0 && (end < 0 || m < end) {
		t.inputs = m
	}
	row := []string{}
	if headerRow <= len(g.Rows) {
		row = g.Rows[headerRow-1]
	}
	for i := from; i < len(row) && (end < 0 || i < end); i++ {
		if h := strings.TrimSpace(row[i]); h != "" {
			if _, seen := t.cols[h]; !seen {
				t.cols[h] = i
			}
		}
	}
	return t
}

// marked is the first column of row 4 from column from whose mark starts with prefix, or -1.
func marked(g sheet.Grid, prefix string, from int) int {
	if markRow > len(g.Rows) {
		return -1
	}
	for i, m := range g.Rows[markRow-1] {
		if i >= from && strings.HasPrefix(strings.TrimSpace(m), prefix) {
			return i
		}
	}
	return -1
}

// col is the column of header h; a missing one is a problem for the whole sheet, said once.
func (t table) col(h string) int {
	c, ok := t.cols[h]
	if !ok {
		for _, p := range *t.probs {
			if p.Sheet == t.sheet && p.Row == headerRow && p.Column == h {
				return -1
			}
		}
		*t.probs = append(*t.probs, Problem{Sheet: t.sheet, Row: headerRow, Column: h, Severity: Error, Code: CodeColumn,
			Message: fmt.Sprintf("the sheet has no column %q", h),
			Fix:     "Use the workbook's published release, or restore the column's header on row 5"})
		return -1
	}
	return c
}

func (t table) text(row int, h string) string {
	if c := t.col(h); c >= 0 {
		return strings.TrimSpace(t.g.Cell(row, c))
	}
	return ""
}

// rows are the table's rows from row 6 that hold anything a person filled in.
func (t table) rows() []int {
	var out []int
	for r := firstRow; r <= len(t.g.Rows); r++ {
		row := t.g.Rows[r-1]
		for i := t.from; i < len(row) && (t.inputs < 0 || i < t.inputs); i++ {
			if strings.TrimSpace(row[i]) != "" {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func (t table) problem(row int, h string, sev Severity, code, msg, fix string) {
	*t.probs = append(*t.probs, Problem{Sheet: t.sheet, Row: row, Column: h, Severity: sev, Code: code, Message: msg, Fix: fix})
}

// date is the cell's date: a serial number, as the workbook keeps dates; empty is the zero time.
func (t table) date(row int, h string) time.Time {
	v := t.text(row, h)
	if v == "" {
		return time.Time{}
	}
	d, ok := t.g.Date(v)
	if !ok {
		t.problem(row, h, Error, CodeValue, fmt.Sprintf("%q is not a date", v), "Enter a date the workbook reads as one")
	}
	return d
}

// typed is an amount a person typed: read as the spreadsheet holds it and never rounded (RND-IMPORT's Saved); more
// decimals than places is a problem.
func (t table) typed(row int, h string, places int) *apd.Decimal {
	d := t.number(row, h)
	if d == nil {
		return nil
	}
	if d.Exponent < -int32(places) { // #nosec G115 -- a currency's minor units
		t.problem(row, h, Error, CodeValue, fmt.Sprintf("%s has more than %d decimals", d.Text('f'), places),
			fmt.Sprintf("Enter it with at most %d decimals, as the currency has", places))
		return nil
	}
	return d
}

// number is a typed figure, such as a quantity, a rate or a number of days: read as the spreadsheet holds it.
func (t table) number(row int, h string) *apd.Decimal {
	v := t.text(row, h)
	if v == "" {
		return nil
	}
	d, _, err := apd.NewFromString(v)
	if err != nil || d.Form != apd.Finite {
		t.problem(row, h, Error, CodeValue, fmt.Sprintf("%q is not a number", v), "Enter a number")
		return nil
	}
	return rounding.Saved(d)
}

// calculated is a figure the workbook calculated, as RND-IMPORT reads it: rounded once to cur's minor units.
func (t table) calculated(row int, h string, cur money.Currency) *apd.Decimal {
	v := t.text(row, h)
	if v == "" {
		return nil
	}
	d, _, err := apd.NewFromString(v)
	if err != nil || d.Form != apd.Finite {
		t.problem(row, h, Error, CodeValue, fmt.Sprintf("%q is not an amount", v), "Open the workbook, recalculate and save it again")
		return nil
	}
	return rounding.Import(d, cur)
}

// yes reads a Yes / No cell: empty is no.
func (t table) yes(row int, h string) bool { return strings.EqualFold(t.text(row, h), "yes") }

// codeOf is a dropdown's code: "6310 - Rent & co-working" is "6310"; text without " - " is itself.
func codeOf(label string) string {
	if code, _, ok := strings.Cut(label, " - "); ok {
		return strings.TrimSpace(code)
	}
	return strings.TrimSpace(label)
}

// statusOf is a workbook status cell's kind: ✔ fine, ⚠ to review, ✖ not posted; anything else none.
func statusOf(s string) Status {
	switch {
	case strings.HasPrefix(s, "✖"):
		return Blocked
	case strings.HasPrefix(s, "⚠"):
		return Review
	case strings.HasPrefix(s, "✔"):
		return Fine
	}
	return None
}
