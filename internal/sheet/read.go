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

// Reading an .xlsx workbook's first sheet of values, with archive/zip and encoding/xml alone (no spreadsheet library for
// one sheet of untrusted values), for the bank statement import (p1-bank-import-reconcile design D3) and the budget
// import (p1-planning design D6). The archive is bounded before anything is decompressed: its entries, each entry's
// size and the total, so a zip bomb stops at its limit. A number is the decimal text the sheet stores, read exactly,
// never through binary floating point.

// The workbook's bounds.
const (
	MaxEntries = 200
	MaxEntry   = 20 << 20
	MaxTotal   = 50 << 20
	MaxColumns = 200
)

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
	files map[string]*zip.File
	total int64
}

func (x *reader) read(name string) ([]byte, error) {
	f, ok := x.files[name]
	if !ok {
		return nil, nil
	}
	if f.UncompressedSize64 > MaxEntry {
		return nil, refuse(true, 0, name, "Save the sheet again, or split it.", "a part of the workbook is larger than %d bytes", MaxEntry)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, refuse(false, 0, name, "Save the sheet again as .xlsx.", "the workbook cannot be read: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, MaxEntry+1))
	if err != nil {
		return nil, refuse(false, 0, name, "Save the sheet again as .xlsx.", "the workbook cannot be read: %v", err)
	}
	if x.total += int64(len(b)); len(b) > MaxEntry || x.total > MaxTotal {
		return nil, refuse(true, 0, name, "Save the sheet again, or split it.", "the workbook expands beyond its limit")
	}
	return b, nil
}

type workbookXML struct {
	Pr struct {
		Date1904 string `xml:"date1904,attr"`
	} `xml:"workbookPr"`
	Sheets []struct {
		RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
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
		Cells []struct {
			Ref    string  `xml:"r,attr"`
			Type   string  `xml:"t,attr"`
			Value  string  `xml:"v"`
			Inline textXML `xml:"is"`
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

// ReadXLSX reads the first sheet of an .xlsx workbook into a grid of at most maxRows rows and MaxColumns columns.
func ReadXLSX(data []byte, maxRows int) (Grid, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Grid{}, refuse(false, 0, "", "Save the sheet as .xlsx.", "the file is not an .xlsx workbook: %v", err)
	}
	if len(zr.File) > MaxEntries {
		return Grid{}, refuse(true, 0, "", "Save the sheet again, or split it.", "the workbook has %d parts; the most is %d", len(zr.File), MaxEntries)
	}
	x := &reader{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		x.files[f.Name] = f
	}
	wbb, err := x.read("xl/workbook.xml")
	if err != nil {
		return Grid{}, err
	}
	if wbb == nil {
		return Grid{}, refuse(false, 0, "xl/workbook.xml", "Save the sheet as .xlsx.", "the workbook has no sheets")
	}
	var wb workbookXML
	if err := decode(wbb, &wb, "workbook"); err != nil {
		return Grid{}, err
	}
	sheetPath := "xl/worksheets/sheet1.xml"
	if len(wb.Sheets) > 0 {
		relb, err := x.read("xl/_rels/workbook.xml.rels")
		if err != nil {
			return Grid{}, err
		}
		var rels relsXML
		if relb != nil {
			if err := decode(relb, &rels, "relationships"); err != nil {
				return Grid{}, err
			}
		}
		for _, r := range rels.Rels {
			if r.ID == wb.Sheets[0].RID {
				target := strings.TrimPrefix(r.Target, "/")
				if !strings.HasPrefix(target, "xl/") {
					target = path.Join("xl", target)
				}
				sheetPath = path.Clean(target)
			}
		}
	}
	var shared []string
	if sb, err := x.read("xl/sharedStrings.xml"); err != nil {
		return Grid{}, err
	} else if sb != nil {
		var sst stringsXML
		if err := decode(sb, &sst, "shared strings"); err != nil {
			return Grid{}, err
		}
		for _, s := range sst.Items {
			shared = append(shared, s.String())
		}
	}
	shb, err := x.read(sheetPath)
	if err != nil {
		return Grid{}, err
	}
	if shb == nil {
		return Grid{}, refuse(false, 0, sheetPath, "Save the sheet as .xlsx.", "the workbook's first sheet is missing")
	}
	var sh sheetXML
	if err := decode(shb, &sh, "sheet"); err != nil {
		return Grid{}, err
	}
	if len(sh.Rows) > maxRows {
		return Grid{}, refuse(true, 0, "", "Split the sheet into smaller files.", "the sheet has %d rows; the most is %d", len(sh.Rows), maxRows)
	}
	g := Grid{Rows: make([][]string, 0, len(sh.Rows)), Numeric: make([]map[int]bool, 0, len(sh.Rows)),
		Date1904: wb.Pr.Date1904 == "1" || wb.Pr.Date1904 == "true"}
	for ri, r := range sh.Rows {
		row := []string{}
		nums := map[int]bool{}
		for ci, c := range r.Cells {
			col := ci
			if c.Ref != "" {
				col = cellColumn(c.Ref)
			}
			if col < 0 || col >= MaxColumns {
				return Grid{}, refuse(false, ri+1, c.Ref, "Keep the data in the sheet's first 200 columns.", "a cell beyond column 200")
			}
			for len(row) <= col {
				row = append(row, "")
			}
			switch c.Type {
			case "s":
				i, err := strconv.Atoi(strings.TrimSpace(c.Value))
				if err != nil || i < 0 || i >= len(shared) {
					return Grid{}, refuse(false, ri+1, c.Ref, "Save the sheet again as .xlsx.", "a cell names a missing shared string")
				}
				row[col] = shared[i]
			case "inlineStr":
				row[col] = c.Inline.String()
			case "str", "d", "e", "b":
				row[col] = c.Value
			default:
				n, ok := number(c.Value)
				if !ok && strings.TrimSpace(c.Value) != "" {
					return Grid{}, refuse(false, ri+1, c.Ref, "Save the sheet again as .xlsx.", "%q is not a number", c.Value)
				}
				row[col], nums[col] = n, ok
			}
		}
		g.Rows = append(g.Rows, row)
		g.Numeric = append(g.Numeric, nums)
	}
	return g, nil
}
