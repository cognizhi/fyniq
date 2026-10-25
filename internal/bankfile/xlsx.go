package bankfile

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// XLSX: the first sheet's values, read with archive/zip and encoding/xml alone (design D3: no spreadsheet library for
// one sheet of untrusted values). The archive is bounded before anything is decompressed: its entries, each entry's
// size and the total, so a zip bomb stops at its limit. A number is the decimal text the sheet stores, read exactly; a
// sheet that stored an amount as an inexact binary number (such as 1350.4999999999998) is refused with a fix, never
// rounded. A date column's serial numbers become dates in the 1900 or 1904 system the workbook names.

const (
	xlsxMaxEntries = 200
	xlsxMaxEntry   = 20 << 20
	xlsxMaxTotal   = 50 << 20
)

type xlsxReader struct {
	files map[string]*zip.File
	total int64
}

func (x *xlsxReader) read(name string) ([]byte, error) {
	f, ok := x.files[name]
	if !ok {
		return nil, nil
	}
	if f.UncompressedSize64 > xlsxMaxEntry {
		return nil, refuse(CodeTooLarge, 0, name, "Save the sheet again, or export the statement as CSV.", "a part of the workbook is larger than %d bytes", xlsxMaxEntry)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, refuse(CodeFormat, 0, name, "Save the sheet again as .xlsx.", "the workbook cannot be read: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, xlsxMaxEntry+1))
	if err != nil {
		return nil, refuse(CodeFormat, 0, name, "Save the sheet again as .xlsx.", "the workbook cannot be read: %v", err)
	}
	if x.total += int64(len(b)); len(b) > xlsxMaxEntry || x.total > xlsxMaxTotal {
		return nil, refuse(CodeTooLarge, 0, name, "Save the sheet again, or export the statement as CSV.", "the workbook expands beyond its limit")
	}
	return b, nil
}

type xlsxWorkbook struct {
	Pr struct {
		Date1904 string `xml:"date1904,attr"`
	} `xml:"workbookPr"`
	Sheets []struct {
		RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"sheets>sheet"`
}

type xlsxRels struct {
	Rels []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

type xlsxText struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (t xlsxText) String() string {
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

type xlsxStrings struct {
	Items []xlsxText `xml:"si"`
}

type xlsxSheet struct {
	Rows []struct {
		Cells []struct {
			Ref    string   `xml:"r,attr"`
			Type   string   `xml:"t,attr"`
			Value  string   `xml:"v"`
			Inline xlsxText `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

func xlsxDecode(b []byte, v any, part string) error {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = true
	if err := dec.Decode(v); err != nil {
		return refuse(CodeFormat, 0, part, "Save the sheet again as .xlsx, or export the statement as CSV.", "the workbook's %s cannot be read: %v", part, err)
	}
	return nil
}

// xlsxColumn is a cell reference's column, from 0: "C7" is 2.
func xlsxColumn(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

// excelNumber is a stored number's exact decimal text: "1.35E+3" is "1350".
func excelNumber(v string) (string, bool) {
	var d apd.Decimal
	if _, _, err := d.SetString(strings.TrimSpace(v)); err != nil || d.Form != apd.Finite {
		return "", false
	}
	d.Reduce(&d)
	return d.Text('f'), true
}

func parseXLSX(data []byte, m Mapping, cur money.Currency, lim Limits) (Statement, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Statement{}, refuse(CodeFormat, 0, "", "Save the sheet as .xlsx, or export the statement as CSV.", "the file is not an .xlsx workbook: %v", err)
	}
	if len(zr.File) > xlsxMaxEntries {
		return Statement{}, refuse(CodeTooLarge, 0, "", "Save the sheet again, or export the statement as CSV.", "the workbook has %d parts; the most is %d", len(zr.File), xlsxMaxEntries)
	}
	x := &xlsxReader{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		x.files[f.Name] = f
	}
	wbb, err := x.read("xl/workbook.xml")
	if err != nil {
		return Statement{}, err
	}
	if wbb == nil {
		return Statement{}, refuse(CodeFormat, 0, "xl/workbook.xml", "Save the sheet as .xlsx, or export the statement as CSV.", "the workbook has no sheets")
	}
	var wb xlsxWorkbook
	if err := xlsxDecode(wbb, &wb, "workbook"); err != nil {
		return Statement{}, err
	}
	sheetPath := "xl/worksheets/sheet1.xml"
	if len(wb.Sheets) > 0 {
		relb, err := x.read("xl/_rels/workbook.xml.rels")
		if err != nil {
			return Statement{}, err
		}
		var rels xlsxRels
		if relb != nil {
			if err := xlsxDecode(relb, &rels, "relationships"); err != nil {
				return Statement{}, err
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
		return Statement{}, err
	} else if sb != nil {
		var sst xlsxStrings
		if err := xlsxDecode(sb, &sst, "shared strings"); err != nil {
			return Statement{}, err
		}
		for _, s := range sst.Items {
			shared = append(shared, s.String())
		}
	}
	shb, err := x.read(sheetPath)
	if err != nil {
		return Statement{}, err
	}
	if shb == nil {
		return Statement{}, refuse(CodeFormat, 0, sheetPath, "Save the sheet as .xlsx, or export the statement as CSV.", "the workbook's first sheet is missing")
	}
	var sheet xlsxSheet
	if err := xlsxDecode(shb, &sheet, "sheet"); err != nil {
		return Statement{}, err
	}
	if len(sheet.Rows) > lim.MaxLines+m.Skip+1 {
		return Statement{}, refuse(CodeTooLarge, 0, "", "Split the statement by month.", "the sheet has %d rows; the most is %d", len(sheet.Rows), lim.MaxLines)
	}
	table := make([][]string, 0, len(sheet.Rows))
	numeric := make([]map[int]bool, 0, len(sheet.Rows))
	for ri, r := range sheet.Rows {
		row := []string{}
		nums := map[int]bool{}
		for ci, c := range r.Cells {
			col := ci
			if c.Ref != "" {
				col = xlsxColumn(c.Ref)
			}
			if col < 0 || col > 200 {
				return Statement{}, refuse(CodeFormat, ri+1, c.Ref, "Keep the statement in the sheet's first 200 columns.", "a cell beyond column 200")
			}
			for len(row) <= col {
				row = append(row, "")
			}
			switch c.Type {
			case "s":
				i, err := strconv.Atoi(strings.TrimSpace(c.Value))
				if err != nil || i < 0 || i >= len(shared) {
					return Statement{}, refuse(CodeFormat, ri+1, c.Ref, "Save the sheet again as .xlsx.", "a cell names a missing shared string")
				}
				row[col] = shared[i]
			case "inlineStr":
				row[col] = c.Inline.String()
			case "str", "d", "e":
				row[col] = c.Value
			case "b":
				row[col] = c.Value
			default:
				n, ok := excelNumber(c.Value)
				if !ok && strings.TrimSpace(c.Value) != "" {
					return Statement{}, refuse(CodeFormat, ri+1, c.Ref, "Save the sheet again as .xlsx.", "%q is not a number", c.Value)
				}
				row[col], nums[col] = n, ok
			}
		}
		table = append(table, row)
		numeric = append(numeric, nums)
	}
	if len(table) <= m.Skip {
		return Statement{}, refuse(CodeFormat, 0, "", "Check the number of rows the mapping skips.", "the sheet has no header row")
	}
	// A date column's serial numbers become ISO dates, read by a layout added for them.
	dateCols := map[int]bool{}
	for i, h := range table[m.Skip] {
		name := strings.ToLower(strings.TrimSpace(h))
		if name != "" && (name == strings.ToLower(strings.TrimSpace(m.Date)) || name == strings.ToLower(strings.TrimSpace(m.ValueDate))) {
			dateCols[i] = true
		}
	}
	for ri := m.Skip + 1; ri < len(table); ri++ {
		for ci := range dateCols {
			if ci >= len(table[ri]) || !numeric[ri][ci] {
				continue
			}
			whole, _, _ := strings.Cut(table[ri][ci], ".")
			days, err := strconv.Atoi(whole)
			if err != nil || days < 1 || days > 2958465 {
				return Statement{}, refuse(CodeDate, ri+1, table[m.Skip][ci], "Format the date column as dates in the sheet.", "%q is not a date", table[ri][ci])
			}
			epoch := excelEpoch
			if wb.Pr.Date1904 == "1" || wb.Pr.Date1904 == "true" {
				epoch = epoch.AddDate(4, 0, 2) // the 1904 system's day 0, 1 January 1904
			}
			table[ri][ci] = epoch.AddDate(0, 0, days).Format("2006-01-02")
		}
	}
	mm := m
	mm.DateFormats = append(append([]string{}, m.DateFormats...), "2006-01-02")
	return rows(table, mm, cur, lim)
}
