package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// Reading an .xlsx workbook's sheets of values, with archive/zip and encoding/xml alone (no spreadsheet library for
// untrusted values): the first sheet for the bank statement import (p1-bank-import-reconcile design D3) and the budget
// import (p1-planning design D6), and sheets by name for the workbook import (p1-workbook-migration design D1). The
// archive is bounded before anything is decompressed: its entries, each entry's size and the total read, so a zip bomb
// stops at its limit. A number is the decimal text the sheet stores, read exactly, never through binary floating point.

// A statement's or a budget's bounds.
const (
	MaxEntries = 200
	MaxEntry   = 20 << 20
	MaxTotal   = 50 << 20
	MaxColumns = 200
)

// Bounds are what a reading allows: the archive's entries, each part's size, the total of the parts read, the columns
// of a sheet and its rows.
type Bounds struct {
	Entries int
	Entry   int64
	Total   int64
	Columns int
	Rows    int
}

// StatementBounds are a statement's or a budget's: one sheet of values.
var StatementBounds = Bounds{Entries: MaxEntries, Entry: MaxEntry, Total: MaxTotal, Columns: MaxColumns}

// WorkbookBounds are the finance workbook's (p1-workbook-migration design D1): forty-odd sheets, of which the import reads
// a score, each at most 64 MB and 20,000 rows, 256 MB read in all.
var WorkbookBounds = Bounds{Entries: 400, Entry: 64 << 20, Total: 256 << 20, Columns: MaxColumns, Rows: 20000}

// ReadError is why a workbook cannot be read, for its reader to say in its own words.
type ReadError struct {
	TooLarge bool   // a bound was passed; else the file is not a workbook Fyniq reads
	Row      int    // the sheet's row, from 1; 0 for the workbook as a whole
	Ref      string // the part or the cell
	Message  string // what is wrong
	Fix      string // how to fix it
}

func (e *ReadError) Error() string { return e.Message }

func refuse(large bool, row int, ref, fix, format string, args ...any) error {
	return &ReadError{TooLarge: large, Row: row, Ref: ref, Fix: fix, Message: fmt.Sprintf(format, args...)}
}

// Grid is a sheet's values, row by row from the first, each cell as its text; Numeric marks the cells the sheet stored
// as numbers, whose text is the number's exact decimal.
type Grid struct {
	Rows     [][]string
	Numeric  []map[int]bool
	Date1904 bool // the workbook counts dates from 1904
	// Uncalculated counts the formulas saved without the value they calculate, as a workbook written by a program and
	// never opened in a spreadsheet application is; FirstUncalculated is the first one's cell.
	Uncalculated      int
	FirstUncalculated string
}

// Header is the columns of a row, by their text: the workbook's tables name their columns on one row, so a column found
// by its header still reads when a later release moves it. An empty cell names nothing; the first of two equal headers
// wins.
func (g Grid) Header(row int) map[string]int {
	out := map[string]int{}
	if row < 1 || row > len(g.Rows) {
		return out
	}
	for i, h := range g.Rows[row-1] {
		if h = strings.TrimSpace(h); h != "" {
			if _, seen := out[h]; !seen {
				out[h] = i
			}
		}
	}
	return out
}

// Cell is the text at a row (from 1) and column (from 0), "" where the sheet has none.
func (g Grid) Cell(row, col int) string {
	if row < 1 || row > len(g.Rows) || col < 0 || col >= len(g.Rows[row-1]) {
		return ""
	}
	return g.Rows[row-1][col]
}

// excelEpoch is day 0 of Excel's 1900 date system, as Excel counts it (it has a 29 February 1900 that never was, so
// serials from 61 on are one day late against a true calendar; 30 December 1899 corrects that for every date a sheet
// of ours can hold).
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// Date is the day a date serial stands for, in the workbook's date system.
func (g Grid) Date(serial string) (time.Time, bool) {
	whole, _, _ := strings.Cut(serial, ".")
	days, err := strconv.Atoi(whole)
	if err != nil || days < 1 || days > 2958465 {
		return time.Time{}, false
	}
	epoch := excelEpoch
	if g.Date1904 {
		epoch = epoch.AddDate(4, 0, 2) // the 1904 system's day 0, 1 January 1904
	}
	return epoch.AddDate(0, 0, days), true
}

type reader struct {
	files  map[string]*zip.File
	total  int64
	bounds Bounds
}

func (x *reader) read(name string) ([]byte, error) {
	f, ok := x.files[name]
	if !ok {
		return nil, nil
	}
	if f.UncompressedSize64 > uint64(x.bounds.Entry) { // #nosec G115 -- a positive bound
		return nil, refuse(true, 0, name, "Save the sheet again, or split it.", "a part of the workbook is larger than %d bytes", x.bounds.Entry)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, refuse(false, 0, name, "Save the sheet again as .xlsx.", "the workbook cannot be read: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, x.bounds.Entry+1))
	if err != nil {
		return nil, refuse(false, 0, name, "Save the sheet again as .xlsx.", "the workbook cannot be read: %v", err)
	}
	if x.total += int64(len(b)); int64(len(b)) > x.bounds.Entry || x.total > x.bounds.Total {
		return nil, refuse(true, 0, name, "Save the sheet again, or split it.", "the workbook expands beyond its limit")
	}
	return b, nil
}

type workbookXML struct {
	Pr struct {
		Date1904 string `xml:"date1904,attr"`
	} `xml:"workbookPr"`
	Sheets []struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"sheets>sheet"`
}

type relsXML struct {
	Rels []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

type textXML struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (t textXML) String() string {
	if len(t.Runs) == 0 {
		return t.T
	}
	var b strings.Builder
	b.WriteString(t.T)
	for _, r := range t.Runs {
		b.WriteString(r.T)
	}
	return b.String()
}

type stringsXML struct {
	Items []textXML `xml:"si"`
}

type sheetXML struct {
	Rows []struct {
		Num   string `xml:"r,attr"`
		Cells []struct {
			Ref     string  `xml:"r,attr"`
			Type    string  `xml:"t,attr"`
			Value   string  `xml:"v"`
			Formula *string `xml:"f"`
			Inline  textXML `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

func decode(b []byte, v any, part string) error {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = true
	if err := dec.Decode(v); err != nil {
		return refuse(false, 0, part, "Save the sheet again as .xlsx.", "the workbook's %s cannot be read: %v", part, err)
	}
	return nil
}

// cellColumn is a cell reference's column, from 0: "C7" is 2.
func cellColumn(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

// number is a stored number's exact decimal text: "1.35E+3" is "1350".
func number(v string) (string, bool) {
	var d apd.Decimal
	if _, _, err := d.SetString(strings.TrimSpace(v)); err != nil || d.Form != apd.Finite {
		return "", false
	}
	d.Reduce(&d)
	return d.Text('f'), true
}

// Workbook is an .xlsx workbook opened to read its sheets, each only when asked for, within its bounds.
type Workbook struct {
	x        *reader
	names    []string
	paths    map[string]string // a sheet's name to its part
	date1904 bool
	shared   []string
	loaded   bool
	// compact keeps a sheet's rows in the order it stores them, skipping the rows it leaves out, as the statement and
	// budget imports read them; otherwise each row sits at its number, so row 5 is the sheet's row 5.
	compact bool
}

// Open opens an .xlsx workbook within bounds b, reading its list of sheets and nothing else yet.
func Open(data []byte, b Bounds) (*Workbook, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, refuse(false, 0, "", "Save the sheet as .xlsx.", "the file is not an .xlsx workbook: %v", err)
	}
	if len(zr.File) > b.Entries {
		return nil, refuse(true, 0, "", "Save the sheet again, or split it.", "the workbook has %d parts; the most is %d", len(zr.File), b.Entries)
	}
	x := &reader{files: map[string]*zip.File{}, bounds: b}
	for _, f := range zr.File {
		x.files[f.Name] = f
	}
	wbb, err := x.read("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	if wbb == nil {
		return nil, refuse(false, 0, "xl/workbook.xml", "Save the sheet as .xlsx.", "the workbook has no sheets")
	}
	var wb workbookXML
	if err := decode(wbb, &wb, "workbook"); err != nil {
		return nil, err
	}
	w := &Workbook{x: x, paths: map[string]string{}, date1904: wb.Pr.Date1904 == "1" || wb.Pr.Date1904 == "true"}
	var rels relsXML
	if len(wb.Sheets) > 0 {
		relb, err := x.read("xl/_rels/workbook.xml.rels")
		if err != nil {
			return nil, err
		}
		if relb != nil {
			if err := decode(relb, &rels, "relationships"); err != nil {
				return nil, err
			}
		}
	}
	for i, sh := range wb.Sheets {
		part := ""
		for _, r := range rels.Rels {
			if r.ID == sh.RID {
				target := strings.TrimPrefix(r.Target, "/")
				if !strings.HasPrefix(target, "xl/") {
					target = path.Join("xl", target)
				}
				part = path.Clean(target)
			}
		}
		if part == "" && i == 0 {
			part = "xl/worksheets/sheet1.xml"
		}
		if _, seen := w.paths[sh.Name]; !seen && part != "" {
			w.names = append(w.names, sh.Name)
			w.paths[sh.Name] = part
		}
	}
	if len(wb.Sheets) == 0 {
		w.names, w.paths[""] = []string{""}, "xl/worksheets/sheet1.xml"
	}
	return w, nil
}

// Names are the workbook's sheets, in its order.
func (w *Workbook) Names() []string { return append([]string(nil), w.names...) }

// Sheet reads the sheet named name; found is false when the workbook has none of that name.
func (w *Workbook) Sheet(name string) (g Grid, found bool, err error) {
	part, ok := w.paths[name]
	if !ok {
		return Grid{}, false, nil
	}
	if !w.loaded {
		w.loaded = true
		if sb, err := w.x.read("xl/sharedStrings.xml"); err != nil {
			return Grid{}, true, err
		} else if sb != nil {
			var sst stringsXML
			if err := decode(sb, &sst, "shared strings"); err != nil {
				return Grid{}, true, err
			}
			for _, s := range sst.Items {
				w.shared = append(w.shared, s.String())
			}
		}
	}
	shb, err := w.x.read(part)
	if err != nil {
		return Grid{}, true, err
	}
	if shb == nil {
		return Grid{}, true, refuse(false, 0, part, "Save the sheet as .xlsx.", "the workbook's sheet %q is missing", name)
	}
	var sh sheetXML
	if err := decode(shb, &sh, "sheet"); err != nil {
		return Grid{}, true, err
	}
	b := w.x.bounds
	if b.Rows > 0 && len(sh.Rows) > b.Rows {
		return Grid{}, true, refuse(true, 0, "", "Split the sheet into smaller files.", "the sheet has %d rows; the most is %d", len(sh.Rows), b.Rows)
	}
	g = Grid{Rows: make([][]string, 0, len(sh.Rows)), Numeric: make([]map[int]bool, 0, len(sh.Rows)), Date1904: w.date1904}
	for ri, r := range sh.Rows {
		if n, err := strconv.Atoi(r.Num); err == nil && !w.compact {
			if n < 1 || (b.Rows > 0 && n > b.Rows) || n <= len(g.Rows) {
				return Grid{}, true, refuse(n > b.Rows, ri+1, r.Num, "Split the sheet into smaller files.", "row %d is out of place or past row %d", n, b.Rows)
			}
			for len(g.Rows) < n-1 { // the rows the sheet leaves out, empty
				g.Rows = append(g.Rows, nil)
				g.Numeric = append(g.Numeric, map[int]bool{})
			}
		}
		row := []string{}
		nums := map[int]bool{}
		for ci, c := range r.Cells {
			col := ci
			if c.Ref != "" {
				col = cellColumn(c.Ref)
			}
			if col < 0 || col >= b.Columns {
				return Grid{}, true, refuse(false, ri+1, c.Ref, fmt.Sprintf("Keep the data in the sheet's first %d columns.", b.Columns),
					"a cell beyond column %d", b.Columns)
			}
			for len(row) <= col {
				row = append(row, "")
			}
			if c.Formula != nil && strings.TrimSpace(c.Value) == "" && c.Type != "inlineStr" {
				if g.Uncalculated++; g.FirstUncalculated == "" {
					g.FirstUncalculated = c.Ref
				}
			}
			switch c.Type {
			case "s":
				i, err := strconv.Atoi(strings.TrimSpace(c.Value))
				if err != nil || i < 0 || i >= len(w.shared) {
					return Grid{}, true, refuse(false, ri+1, c.Ref, "Save the sheet again as .xlsx.", "a cell names a missing shared string")
				}
				row[col] = w.shared[i]
			case "inlineStr":
				row[col] = c.Inline.String()
			case "str", "d", "e", "b":
				row[col] = c.Value
			default:
				n, ok := number(c.Value)
				if !ok && strings.TrimSpace(c.Value) != "" {
					return Grid{}, true, refuse(false, ri+1, c.Ref, "Save the sheet again as .xlsx.", "%q is not a number", c.Value)
				}
				row[col], nums[col] = n, ok
			}
		}
		g.Rows = append(g.Rows, row)
		g.Numeric = append(g.Numeric, nums)
	}
	return g, true, nil
}

// ReadXLSX reads the first sheet of an .xlsx workbook into a grid of at most maxRows rows and MaxColumns columns.
func ReadXLSX(data []byte, maxRows int) (Grid, error) {
	b := StatementBounds
	b.Rows = maxRows
	w, err := Open(data, b)
	if err != nil {
		return Grid{}, err
	}
	w.compact = true
	g, found, err := w.Sheet(w.names[0])
	if err == nil && !found {
		err = refuse(false, 0, "", "Save the sheet as .xlsx.", "the workbook's first sheet is missing")
	}
	return g, err
}
