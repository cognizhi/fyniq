// Package sheet writes a report as a table in CSV or XLSX (FR-RPT-05; p1-reports design D5), with archive/zip and
// encoding/xml alone: no spreadsheet library. Amounts and quantities are numbers, never text: in CSV a plain decimal
// with an ASCII minus, unquoted; in XLSX a number cell with the currency's decimals. No text ever becomes a formula: in
// CSV every text cell is quoted, inner quotes doubled, and one that starts with =, +, -, @, a tab, CR or LF is prefixed
// with an apostrophe (OWASP CSV injection guidance); in XLSX text is an inline string, which a spreadsheet never
// evaluates. The platform writes no formula from text. Numbers past 15 significant digits lose precision in a
// spreadsheet that reads them as binary floating point (ENG-10's precision boundary); the CSV keeps them exact.
package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
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
}

// decimal is a number as a cell takes it: an optional minus, digits, and optional decimals (FR-UX-20's strict
// pattern). Anything else is written as text.
var decimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// CSV is the first sheet as CSV: UTF-8 with CRLF line ends, the header first.
func CSV(s Sheet) []byte {
	var b bytes.Buffer
	line := func(cells []Cell) {
		for i, c := range cells {
			if i > 0 {
				b.WriteByte(',')
			}
			switch {
			case c.IsNumber && decimal.MatchString(c.Number):
				b.WriteString(c.Number)
			case c.IsNumber:
				b.WriteString(csvText(c.Number)) // not a plain decimal: kept, as text
			default:
				b.WriteString(csvText(c.Text))
			}
		}
		b.WriteString("\r\n")
	}
	header := make([]Cell, len(s.Header))
	for i, h := range s.Header {
		header[i] = Text(h)
	}
	line(header)
	for _, r := range s.Rows {
		line(r.Cells)
	}
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
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: created.UTC()})
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(xml.Header + body))
		return err
	}
	var types, sheetsXML, rels strings.Builder
	for i, s := range sheets {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&sheetsXML, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(sheetName(s.Name)), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	styles := len(sheets) + 1
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, styles)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			types.String() + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets>` + sheetsXML.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`},
		{"xl/styles.xml", stylesXML},
	}
	for i, s := range sheets {
		parts = append(parts, struct{ name, body string }{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), worksheet(s)})
	}
	for _, p := range parts {
		if err := put(p.name, p.body); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

// worksheet is one sheet's XML: the header row bold, then the rows, a strong row bold.
func worksheet(s Sheet) string {
	var b strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	row := func(n int, cells []Cell, strong bool) {
		fmt.Fprintf(&b, `<row r="%d">`, n)
		for i, c := range cells {
			ref := column(i) + fmt.Sprint(n)
			if c.IsNumber && decimal.MatchString(c.Number) {
				style := 2
				switch c.Decimals {
				case 0:
					style = 4
				case 1:
					style = 6
				}
				if strong {
					style++
				}
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, style, c.Number)
				continue
			}
			text := c.Text
			if c.IsNumber {
				text = c.Number // not a plain decimal: kept, as text
			}
			style := 0
			if strong {
				style = 1
			}
			fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(text))
		}
		b.WriteString(`</row>`)
	}
	header := make([]Cell, len(s.Header))
	for i, h := range s.Header {
		header[i] = Text(h)
	}
	row(1, header, true)
	for i, r := range s.Rows {
		row(i+2, r.Cells, r.Strong)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

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
