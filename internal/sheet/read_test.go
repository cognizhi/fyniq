package sheet

import (
	"archive/zip"
	"bytes"
	"errors"
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
