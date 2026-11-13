// Package sheet writes a report as a table in CSV or XLSX (FR-RPT-05; p1-reports design D5), with archive/zip and
// encoding/xml alone: no spreadsheet library. Amounts and quantities are numbers, never text: in CSV a plain decimal
// with an ASCII minus, unquoted; in XLSX a number cell with the currency's decimals. No text ever becomes a formula: in
// CSV every text cell is quoted, inner quotes doubled, and one that starts with =, +, -, @, a tab, CR or LF is prefixed
// with an apostrophe (OWASP CSV injection guidance); in XLSX text is an inline string, which a spreadsheet never
// evaluates. The platform writes no formula from text. Numbers past 15 significant digits lose precision in a
// spreadsheet that reads them as binary floating point (ENG-10's precision boundary); the CSV keeps them exact.
package sheet

import (
	"bytes"
	"encoding/xml"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Cell is a text or a number. A number is a plain decimal, such as "-1234.50".
type Cell struct {
	Text     string
	Number   string
	IsNumber bool
	Decimals int // a number's decimals as shown: the currency's minor units
}

// Text is a text cell.
func Text(s string) Cell { return Cell{Text: s} }

// Number is a number cell with decimals shown.
func Number(decimal string, decimals int) Cell {
	return Cell{Number: decimal, IsNumber: true, Decimals: decimals}
}

// Row is a row of cells; Strong rows are totals, shown bold in XLSX.
type Row struct {
	Cells  []Cell
	Strong bool
}

// Sheet is a named table: its header row and its rows.
type Sheet struct {
	Name   string
	Header []string
	Rows   []Row
	// Footer follows the rows after a blank row, each line in the first column: the platform and build that prepared
	// the report, as every exported report names them (REL-05).
	Footer []string
}

// Stamp names the platform version and the build's commit, as every exported report does (REL-05), such as
// "0.12.0 (3f2c1a9d)"; the version alone when the commit is not known.
func Stamp(version, commit string) string {
	commit = commit[:min(len(commit), 12)]
	if commit == "" || commit == "unknown" {
		return version
	}
	return version + " (" + commit + ")"
}

// Prepared is an exported report's footer line: the platform and build that prepared it, and when.
func Prepared(stamp string, at time.Time) string {
	if stamp != "" {
		stamp = " " + stamp
	}
	return "Prepared by Fyniq" + stamp + " on " + at.UTC().Format("2006-01-02 15:04") + " UTC"
}

// Footed is sheets, each with footer: the footer every exported report carries (REL-05).
func Footed(sheets []Sheet, footer ...string) []Sheet {
	out := make([]Sheet, len(sheets))
	for i, s := range sheets {
		s.Footer = append(slices.Clone(s.Footer), footer...)
		out[i] = s
	}
	return out
}

// decimal is a number as a cell takes it: an optional minus, digits, and optional decimals (FR-UX-20's strict
// pattern). Anything else is written as text.
var decimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// CSV is the first sheet as CSV: UTF-8 with CRLF line ends, the header first.
func CSV(s Sheet) []byte {
	var b bytes.Buffer
	_ = Write(NewCSV(&b), []Sheet{s}) // into a buffer, which never fails
	return b.Bytes()
}

// csvText is a text cell, quoted, made inert when a spreadsheet would read it as a formula (CWE-1236).
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r\n", rune(s[0])) {
		s = "'" + s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// XLSX is the sheets as an Office Open XML workbook, the first sheet open. created is the workbook's creation time,
// so that the same report gives the same bytes.
func XLSX(sheets []Sheet, created time.Time) ([]byte, error) {
	var b bytes.Buffer
	if err := Write(NewXLSX(&b, created), sheets); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// The styles: 0 plain; 1 bold; 2 a number with 2 decimals and a thousands separator; 3 the same, bold; 4 a whole
// number with a thousands separator; 5 the same, bold; 6 a number with 1 decimal, such as a percentage; 7 the same, bold.
const stylesXML = `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="3"><numFmt numFmtId="164" formatCode="#,##0.00;-#,##0.00"/><numFmt numFmtId="165" formatCode="#,##0;-#,##0"/>` +
	`<numFmt numFmtId="166" formatCode="#,##0.0;-#,##0.0"/></numFmts>` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="8"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` +
	`<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="165" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` +
	`<xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="166" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/></cellXfs></styleSheet>`

// column is a column's letters: 0 is A, 25 Z, 26 AA.
func column(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// escape is a cell's text for XML, without the characters XML 1.0 cannot hold.
func escape(s string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || r >= 0x10000 {
			return r
		}
		return -1
	}, s)
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(clean)) // into a buffer, which never fails
	return b.String()
}

// sheetName is a sheet's name as a workbook allows it: at most 31 characters, none of []:*?/\.
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	if strings.TrimSpace(s) == "" {
		s = "Sheet"
	}
	return s
}
