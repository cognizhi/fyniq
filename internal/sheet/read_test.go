package sheet

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

// The reader reads back what the writer writes: text, and numbers as their exact decimals; a file that is no workbook,
// one with too many rows, and a zip of too many parts are refused, marked too large where a bound was passed.
func TestReadXLSX(t *testing.T) {
	b, err := XLSX([]Sheet{{Name: "Budget", Header: []string{"Account", "2027-01"}, Rows: []Row{{Cells: []Cell{Text("6200 Rent"), Number("1234.50", 2)}}}}},
		time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	g, err := ReadXLSX(b, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Rows) != 2 || g.Rows[0][1] != "2027-01" || g.Rows[1][0] != "6200 Rent" || g.Rows[1][1] != "1234.5" || !g.Numeric[1][1] || g.Numeric[1][0] {
		t.Errorf("read back: %v %v", g.Rows, g.Numeric)
	}
	var re *ReadError
	if _, err := ReadXLSX([]byte("not a workbook"), 10); !errors.As(err, &re) || re.TooLarge {
		t.Errorf("not a workbook: %v", err)
	}
	if _, err := ReadXLSX(b, 1); !errors.As(err, &re) || !re.TooLarge {
		t.Errorf("too many rows: %v", err)
	}
	var zb bytes.Buffer
	w := zip.NewWriter(&zb)
	for i := range MaxEntries + 1 {
		f, _ := w.Create("x/" + string(rune('a'+i%26)) + time.Duration(i).String())
		_, _ = f.Write([]byte("x"))
	}
	_ = w.Close()
	if _, err := ReadXLSX(zb.Bytes(), 10); !errors.As(err, &re) || !re.TooLarge {
		t.Errorf("too many parts: %v", err)
	}
	if d, ok := (Grid{}).Date("46023"); !ok || d.Format(time.DateOnly) != "2026-01-01" {
		t.Errorf("serial 46023: %v %v", d, ok)
	}
	if d, ok := (Grid{Date1904: true}).Date("44561"); !ok || d.Format(time.DateOnly) != "2026-01-01" {
		t.Errorf("1904 serial 44561: %v %v", d, ok)
	}
}

// The finance workbook's sheets read by name (p1-workbook-migration design D1): its tables' columns found by their
// header on row 5, a cell's text and a number's exact decimal, a sheet it does not have reported as absent; each bound
// stops a workbook that passes it; a formula saved without its value is counted, so the import can refuse it.
// req: FR-MIG-01
func TestAWorkbookReadsItsSheetsByName(t *testing.T) {
	data, err := os.ReadFile("../../test/migration/workbook-sample.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	w, err := Open(data, WorkbookBounds)
	if err != nil {
		t.Fatal(err)
	}
	if names := w.Names(); len(names) != 24 || names[0] != "Settings" || names[9] != "Quotes & Orders" {
		t.Errorf("the sheets: %v", names)
	}
	acc, found, err := w.Sheet("Accounts")
	if err != nil || !found {
		t.Fatalf("Accounts: %v %v", found, err)
	}
	h := acc.Header(5)
	if h["Code"] != 0 || h["Account name"] != 1 || h["Opening balance"] != 10 {
		t.Errorf("Accounts' header: %v", h)
	}
	if acc.Cell(6, h["Code"]) != "1010" || acc.Cell(6, h["Account name"]) != "Petty cash" || acc.Cell(6, h["Opening balance"]) != "300" ||
		!acc.Numeric[5][h["Opening balance"]] || acc.Uncalculated != 0 {
		t.Errorf("Accounts' first row: %v", acc.Rows[5])
	}
	if _, found, err := w.Sheet("Engine"); found || err != nil {
		t.Errorf("a sheet the fixture does not hold: %v %v", found, err)
	}

	// Each bound stops the read at it: a size as too large, a cell past the columns as where it is.
	var re *ReadError
	for name, c := range map[string]struct {
		b     Bounds
		large bool
	}{
		"entries": {Bounds{Entries: 3, Entry: 64 << 20, Total: 256 << 20, Columns: 200}, true},
		"a part":  {Bounds{Entries: 400, Entry: 1 << 10, Total: 256 << 20, Columns: 200}, true},
		"in all":  {Bounds{Entries: 400, Entry: 64 << 20, Total: 64 << 10, Columns: 200}, true},
		"rows":    {Bounds{Entries: 400, Entry: 64 << 20, Total: 256 << 20, Columns: 200, Rows: 10}, true},
		"columns": {Bounds{Entries: 400, Entry: 64 << 20, Total: 256 << 20, Columns: 5}, false},
	} {
		w, err := Open(data, c.b)
		if err == nil {
			_, _, err = w.Sheet("Accounts")
			if err == nil {
				_, _, err = w.Sheet("Transactions")
			}
		}
		if !errors.As(err, &re) || re.TooLarge != c.large {
			t.Errorf("past its bound %s: %v", name, err)
		}
	}

	var zb bytes.Buffer
	z := zip.NewWriter(&zb)
	for name, body := range map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Totals" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>A1*2</f><v>4</v></c></row><row r="2"><c r="A2"><v>3</v></c><c r="B2"><f>A2*2</f></c></row></sheetData></worksheet>`,
	} {
		f, _ := z.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = z.Close()
	tw, err := Open(zb.Bytes(), WorkbookBounds)
	if err != nil {
		t.Fatal(err)
	}
	if g, _, err := tw.Sheet("Totals"); err != nil || g.Uncalculated != 1 || g.FirstUncalculated != "B2" || g.Cell(1, 1) != "4" {
		t.Errorf("a formula without its value: %+v %v", g, err)
	}
}
