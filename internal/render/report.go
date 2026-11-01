package render

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Report is a report its caller laid out (FR-RPT-05; p1-reports task 3.2): a financial statement's title, columns and
// rows, every amount already written. The render service lays it out with report.typ as it lays a document out with
// document.typ: the Typst source only places the text, so the same report and creation time give the same bytes.
type Report struct {
	Title   string       `json:"title"`           // the report, such as "Profit and loss": the PDF's title (PDF/UA-1)
	Author  string       `json:"author"`          // the organisation it is printed for
	Lines   []string     `json:"lines"`           // under the title: the statements' title and basis, the period, the currency
	Label   string       `json:"label"`           // the first column's heading, such as the currency
	Columns []string     `json:"columns"`         // the amount columns' headings, after the first
	Rows    []ReportRow  `json:"rows"`            // in order
	Notes   []ReportNote `json:"notes,omitempty"` // after the table: a failed check, a restated presentation
	Footer  string       `json:"footer"`          // each page's foot, beside its number: the revision and the platform version
}

// The kinds of a report's row.
const (
	RowSection  = "section"  // a section's title, across the table
	RowLine     = "line"     // an account's figures, indented
	RowSubtotal = "subtotal" // a group's total, or a computed line with no accounts, indented
	RowTotal    = "total"    // a section's total, bold under a rule
	RowMeasure  = "measure"  // a line computed from those above it, such as gross profit, bold under a rule
)

// ReportRow is one row of a report: a section's title, or a label and one written figure per column.
type ReportRow struct {
	Kind  string   `json:"kind"`
	Code  string   `json:"code,omitempty"` // an account's code, before its label
	Label string   `json:"label"`
	Cells []string `json:"cells,omitempty"` // one per column; none for a section
}

// ReportNote is a note after the table: a heading and its items.
type ReportNote struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

// The bounds of a report: a P&L by month has twelve months, the total and the % of revenue.
const (
	MaxReportColumns = 14
	MaxReportRows    = 5000
	maxReportText    = 500
)

// ErrReport is a report the render service refuses to lay out.
var ErrReport = errors.New("render: not a report")

// Check says what is wrong with r, if anything: a title, at most MaxReportColumns columns and MaxReportRows rows, a
// known kind and a figure per column on each row, no section with figures, and no text longer than 500 characters.
func (r Report) Check() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrReport}, args...)...)
	}
	if strings.TrimSpace(r.Title) == "" {
		return bad("it has no title")
	}
	if len(r.Columns) == 0 || len(r.Columns) > MaxReportColumns {
		return bad("it has %d columns; a report has 1 to %d", len(r.Columns), MaxReportColumns)
	}
	if len(r.Rows) > MaxReportRows {
		return bad("it has %d rows; a report has at most %d", len(r.Rows), MaxReportRows)
	}
	texts := slices.Concat([]string{r.Title, r.Author, r.Label, r.Footer}, r.Lines, r.Columns)
	for i, x := range r.Rows {
		switch x.Kind {
		case RowSection:
			if len(x.Cells) > 0 {
				return bad("row %d is a section with figures", i+1)
			}
		case RowLine, RowSubtotal, RowTotal, RowMeasure:
			if len(x.Cells) != len(r.Columns) {
				return bad("row %d has %d figures for %d columns", i+1, len(x.Cells), len(r.Columns))
			}
		default:
			return bad("row %d is of kind %q", i+1, x.Kind)
		}
		texts = slices.Concat(texts, []string{x.Code, x.Label}, x.Cells)
	}
	for _, n := range r.Notes {
		texts = slices.Concat(texts, []string{n.Title}, n.Items)
	}
	for _, s := range texts {
		if utf8.RuneCountInString(s) > maxReportText {
			return bad("a text is longer than %d characters", maxReportText)
		}
	}
	return nil
}
