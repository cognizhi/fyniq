package sheet

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Writer writes a report's file as its rows come, so a long report is never held whole (NFR-PERF-05, rev 0.250): Begin
// a sheet with its header, give its rows, End it with its footer, and Close the file.
type Writer interface {
	Begin(name string, header []string) error
	Row(r Row) error
	// End ends the sheet: a blank row, then each footer line in the first column (REL-05).
	End(footer ...string) error
	Close() error
}

// Write writes whole sheets through w and closes it.
func Write(w Writer, sheets []Sheet) error {
	for _, s := range sheets {
		if err := w.Begin(s.Name, s.Header); err != nil {
			return err
		}
		for _, r := range s.Rows {
			if err := w.Row(r); err != nil {
				return err
			}
		}
		if err := w.End(s.Footer...); err != nil {
			return err
		}
	}
	return w.Close()
}

// errClosed refuses a row given outside a sheet.
var errClosed = errors.New("sheet: a row outside a sheet")

// NewCSV writes the first sheet as CSV: UTF-8 with CRLF line ends, the header first. Later sheets are left out, as a CSV
// file holds one table.
func NewCSV(w io.Writer) Writer { return &csvWriter{w: bufio.NewWriterSize(w, 64<<10)} }

type csvWriter struct {
	w      *bufio.Writer
	sheets int
	on     bool // in the first sheet
}

func (c *csvWriter) Begin(_ string, header []string) error {
	c.sheets++
	c.on = c.sheets == 1
	if !c.on {
		return nil
	}
	cells := make([]Cell, len(header))
	for i, h := range header {
		cells[i] = Text(h)
	}
	return csvLine(c.w, cells)
}

func (c *csvWriter) Row(r Row) error {
	if c.sheets == 0 {
		return errClosed
	}
	if !c.on {
		return nil
	}
	return csvLine(c.w, r.Cells)
}

func (c *csvWriter) End(footer ...string) error {
	if !c.on {
		return nil
	}
	c.on = false
	if len(footer) == 0 {
		return nil
	}
	if _, err := c.w.WriteString("\r\n"); err != nil {
		return err
	}
	for _, f := range footer {
		if err := csvLine(c.w, []Cell{Text(f)}); err != nil {
			return err
		}
	}
	return nil
}

func (c *csvWriter) Close() error { return c.w.Flush() }

// csvLine is one CSV line: numbers bare, text quoted and made inert.
func csvLine(w *bufio.Writer, cells []Cell) error {
	for i, c := range cells {
		if i > 0 {
			_ = w.WriteByte(',') // a bufio.Writer's error returns on its next write or flush
		}
		switch {
		case c.IsNumber && decimal.MatchString(c.Number):
			_, _ = w.WriteString(c.Number)
		case c.IsNumber:
			_, _ = w.WriteString(csvText(c.Number)) // not a plain decimal: kept, as text
		default:
			_, _ = w.WriteString(csvText(c.Text))
		}
	}
	_, err := w.WriteString("\r\n")
	return err
}

// MaxRows is how many rows a worksheet holds (Excel's limit). A sheet that would hold more goes on in another, named
// after it with its part's number, its header repeated; room is kept for the footer.
var MaxRows = 1_048_576

// footerRoom is the rows a sheet keeps free for its blank row and footer lines.
const footerRoom = 8

// NewXLSX writes the sheets as an Office Open XML workbook, the first sheet open, each worksheet as its rows come and
// the workbook's parts at the end. created is the workbook's creation time, so the same report gives the same bytes.
func NewXLSX(w io.Writer, created time.Time) Writer {
	return &xlsxWriter{z: zip.NewWriter(w), created: created.UTC()}
}

type xlsxWriter struct {
	z       *zip.Writer
	created time.Time
	names   []string      // the worksheets written, in order
	b       *bufio.Writer // the open worksheet's, nil when none is
	name    string        // the open sheet's name as given
	header  []string
	rows    int // rows in the open worksheet, its header counted
	part    int // the open sheet's part: 1, then 2 when it went on in another worksheet
}

func (x *xlsxWriter) entry(name string) (io.Writer, error) {
	return x.z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: x.created})
}

func (x *xlsxWriter) Begin(name string, header []string) error {
	x.name, x.header, x.part = name, header, 1
	return x.open(sheetName(name))
}

// open starts a worksheet with its header row.
func (x *xlsxWriter) open(name string) error {
	x.names = append(x.names, name)
	w, err := x.entry(fmt.Sprintf("xl/worksheets/sheet%d.xml", len(x.names)))
	if err != nil {
		return err
	}
	x.b = bufio.NewWriterSize(w, 64<<10)
	_, _ = x.b.WriteString(xml.Header + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	cells := make([]Cell, len(x.header))
	for i, h := range x.header {
		cells[i] = Text(h)
	}
	x.rows = 1
	return writeRow(x.b, 1, cells, true)
}

func (x *xlsxWriter) Row(r Row) error {
	if x.b == nil {
		return errClosed
	}
	if x.rows >= MaxRows-footerRoom {
		if err := x.close(); err != nil {
			return err
		}
		x.part++
		suffix := fmt.Sprintf(" (%d)", x.part)
		base := []rune(sheetName(x.name))
		if n := 31 - len(suffix); len(base) > n {
			base = base[:n]
		}
		if err := x.open(string(base) + suffix); err != nil {
			return err
		}
	}
	x.rows++
	return writeRow(x.b, x.rows, r.Cells, r.Strong)
}

func (x *xlsxWriter) End(footer ...string) error {
	if x.b == nil {
		return errClosed
	}
	for i, f := range footer { // after a blank row
		if err := writeRow(x.b, x.rows+2+i, []Cell{Text(f)}, false); err != nil {
			return err
		}
	}
	return x.close()
}

// close ends the open worksheet.
func (x *xlsxWriter) close() error {
	_, _ = x.b.WriteString(`</sheetData></worksheet>`)
	err := x.b.Flush()
	x.b = nil
	return err
}

func (x *xlsxWriter) Close() error {
	if x.b != nil {
		if err := x.close(); err != nil {
			return err
		}
	}
	var types, sheetsXML, rels strings.Builder
	for i, name := range x.names {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&sheetsXML, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(name), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(x.names)+1)
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
	for _, p := range parts {
		w, err := x.entry(p.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, xml.Header+p.body); err != nil {
			return err
		}
	}
	return x.z.Close()
}

// writeRow is one worksheet row: numbers as number cells with the currency's decimals, text as inline strings; a strong
// row bold.
func writeRow(b *bufio.Writer, n int, cells []Cell, strong bool) error {
	fmt.Fprintf(b, `<row r="%d">`, n)
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
			fmt.Fprintf(b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, style, c.Number)
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
		fmt.Fprintf(b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(text))
	}
	_, err := b.WriteString(`</row>`)
	return err
}
