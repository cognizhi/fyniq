package sheet

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

var table = Sheet{Name: "Profit and loss: Oct/2026", Header: []string{"Account", "Oct 2026"}, Rows: []Row{
	{Cells: []Cell{Text("4010 Subscription revenue"), Number("5000.00", 2)}},
	{Cells: []Cell{Text(`=HYPERLINK("x") "quoted"`), Number("-1234.50", 2)}},
	{Cells: []Cell{Text("@SUM(A1)"), Number("12", 0)}},
	{Cells: []Cell{Text("Net profit"), Number("3765.50", 2)}, Strong: true},
	{Cells: []Cell{Text("Not a number"), Number("1,234", 2)}},
}}

// FR-RPT-05: amounts are plain decimals with an ASCII minus, unquoted; every text is quoted, inner quotes doubled, and
// one that starts like a formula gets an apostrophe; something that is not a plain decimal stays text.
// req: FR-RPT-05
func TestCSVWritesNumbersAsNumbersAndNoFormula(t *testing.T) {
	want := "\"Account\",\"Oct 2026\"\r\n" +
		"\"4010 Subscription revenue\",5000.00\r\n" +
		"\"'=HYPERLINK(\"\"x\"\") \"\"quoted\"\"\",-1234.50\r\n" +
		"\"'@SUM(A1)\",12\r\n" +
		"\"Net profit\",3765.50\r\n" +
		"\"Not a number\",\"1,234\"\r\n"
	if got := string(CSV(table)); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// read is a part of a workbook.
func read(t *testing.T, data []byte, name string) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("no part %s", name)
	return ""
}

// FR-RPT-05: an XLSX of two sheets holds each amount as a number cell with the currency's decimals and each text as an
// inline string, which a spreadsheet never evaluates; totals are bold; a sheet's name is cut to what a workbook allows;
// the same report gives the same bytes.
// req: FR-RPT-05
func TestXLSXWritesNumberCellsAndInlineText(t *testing.T) {
	created := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	data := Sheet{Name: "Data", Header: []string{"Code", "Amount"}, Rows: []Row{{Cells: []Cell{Text("4010"), Number("5000.00", 2)}}}}
	x, err := XLSX([]Sheet{table, data}, created)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := XLSX([]Sheet{table, data}, created)
	if !bytes.Equal(x, again) {
		t.Error("the same report gave different bytes")
	}
	wb := read(t, x, "xl/workbook.xml")
	if !strings.Contains(wb, `<sheet name="Profit and loss- Oct-2026" sheetId="1" r:id="rId1"/>`) || !strings.Contains(wb, `<sheet name="Data" sheetId="2"`) {
		t.Errorf("the workbook: %s", wb)
	}
	s1 := read(t, x, "xl/worksheets/sheet1.xml")
	for _, want := range []string{
		`<c r="A1" s="1" t="inlineStr"><is><t xml:space="preserve">Account</t></is></c>`,
		`<c r="B2" s="2"><v>5000.00</v></c>`,
		`<c r="A3" s="0" t="inlineStr"><is><t xml:space="preserve">=HYPERLINK(&#34;x&#34;) &#34;quoted&#34;</t></is></c>`,
		`<c r="B3" s="2"><v>-1234.50</v></c>`,
		`<c r="B4" s="4"><v>12</v></c>`,
		`<c r="A5" s="1" t="inlineStr"><is><t xml:space="preserve">Net profit</t></is></c><c r="B5" s="3"><v>3765.50</v></c>`,
		`<c r="B6" s="0" t="inlineStr"><is><t xml:space="preserve">1,234</t></is></c>`,
	} {
		if !strings.Contains(s1, want) {
			t.Errorf("sheet1 lacks %s:\n%s", want, s1)
		}
	}
	if strings.Contains(s1, "<f>") {
		t.Error("a formula was written")
	}
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 701: "ZZ", 702: "AAA"} {
		if got := column(i); got != want {
			t.Errorf("column(%d) = %s, want %s", i, got, want)
		}
	}
}

// Every exported report names the platform version and the build's commit in its footer, after a blank row, in CSV and
// XLSX alike; the commit is cut to 12 characters, and a build without one names the version alone (REL-05).
// req: REL-05
func TestAnExportsFooterNamesTheVersionAndCommit(t *testing.T) {
	at := time.Date(2026, 11, 11, 2, 5, 0, 0, time.FixedZone("SGT", 8*3600))
	line := Prepared(Stamp("0.12.0", "3f2c1a9d4e5b6c7d8e9f"), at)
	if line != "Prepared by Fyniq 0.12.0 (3f2c1a9d4e5b) on 2026-11-10 18:05 UTC" {
		t.Errorf("the line: %q", line)
	}
	if got := Prepared("", at); got != "Prepared by Fyniq on 2026-11-10 18:05 UTC" {
		t.Errorf("a build with no stamp: %q", got)
	}
	for _, commit := range []string{"", "unknown"} {
		if got := Stamp("0.0.0-dev", commit); got != "0.0.0-dev" {
			t.Errorf("Stamp with commit %q = %q", commit, got)
		}
	}
	one := Sheet{Name: "TB", Header: []string{"Code", "Amount"}, Rows: []Row{{Cells: []Cell{Text("4010"), Number("5.00", 2)}}}}
	footed := Footed([]Sheet{one}, line)
	if len(one.Footer) != 0 {
		t.Error("Footed changed the sheet it was given")
	}
	if got := string(CSV(footed[0])); got != "\"Code\",\"Amount\"\r\n\"4010\",5.00\r\n\r\n\""+line+"\"\r\n" {
		t.Errorf("the CSV: %q", got)
	}
	x, err := XLSX(footed, at)
	if err != nil {
		t.Fatal(err)
	}
	// row 1 the header, row 2 the data, row 3 blank, row 4 the footer
	if s := read(t, x, "xl/worksheets/sheet1.xml"); !strings.Contains(s, `<row r="4"><c r="A4" s="0" t="inlineStr"><is><t xml:space="preserve">`+line+`</t>`) ||
		strings.Contains(s, `<row r="3">`) {
		t.Errorf("the XLSX's footer: %s", s)
	}
}
