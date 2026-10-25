package bankfile_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

var sgd, usd = money.MustLookup("SGD"), money.MustLookup("USD")

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// want is a line as a test writes it: date, amount, reference, counterparty, description.
type want struct {
	booked             time.Time
	amount, ref, party string
	desc               string
}

func checkLines(t *testing.T, s bankfile.Statement, lines []want) {
	t.Helper()
	if len(s.Lines) != len(lines) {
		t.Fatalf("%d lines, want %d: %+v", len(s.Lines), len(lines), s.Lines)
	}
	for i, w := range lines {
		l := s.Lines[i]
		if l.Position != i+1 || !l.Booked.Equal(w.booked) || l.Amount.String() != w.amount || l.Amount.Currency() != s.Currency ||
			l.Reference != w.ref || l.Counterparty != w.party || l.Description != w.desc {
			t.Errorf("line %d: %d %s %s ref %q party %q desc %q; want %s %s ref %q party %q desc %q", i+1, l.Position, l.Booked.Format("2006-01-02"),
				l.Amount, l.Reference, l.Counterparty, l.Description, w.booked.Format("2006-01-02"), w.amount, w.ref, w.party, w.desc)
		}
	}
}

func balance(m *money.Money) string {
	if m == nil {
		return "none"
	}
	return m.String()
}

// The readers turn each format's synthetic statement into the same model: the account, the currency, the period, the
// opening and closing balances the format gives, and each booked line with its exact signed amount, the bank's
// reference, the counterparty and the description.
// req: FR-BNK-02
// req: FR-BNK-15
func TestEachFormatReadsIntoOneModel(t *testing.T) {
	t.Run("MT940 SWIFT", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatMT940, fixture(t, "swift.mt940"), nil, money.Currency{}, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		s := got[0]
		if s.Account != "0721234567" || s.Currency != sgd || balance(s.Opening) != "1000.00" || balance(s.Closing) != "-261.00" ||
			!s.From.Equal(day(2025, 10, 1)) || !s.To.Equal(day(2025, 12, 31)) {
			t.Fatalf("statement: %+v", s)
		}
		checkLines(t, s, []want{
			{day(2025, 10, 3), "109.00", "BR25100301", "", "INV-0001 ACME ROBOTICS PTE LTD INVOICE INV-0001"},
			{day(2025, 10, 4), "-1350.00", "BR25100402", "", "TRANSFER TO WISE"},
			{day(2025, 12, 31), "-20.00", "BR25123103", "", "SERVICE CHARGE"},
		})
	})
	t.Run("MT940 SAP", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatMT940SAP, fixture(t, "sap.mt940"), nil, money.Currency{}, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		checkLines(t, got[0], []want{
			{day(2025, 10, 5), "200.00", "BR251005", "NORTHWIND TRADERS", "REF123 Payment for INV-0002"},
			{day(2025, 10, 6), "50.00", "BR251006", "", "Reversal of debit"}, // RD: a debit reversed is money in
		})
	})
	t.Run("camt.053", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatCAMT053, fixture(t, "statement.camt053.xml"), nil, money.Currency{}, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		s := got[0]
		if s.Account != "GB33BUKB20201555555555" || s.Currency != usd || balance(s.Opening) != "0.00" || balance(s.Closing) != "995.00" ||
			!s.From.Equal(day(2025, 10, 1)) || !s.To.Equal(day(2025, 10, 31)) {
			t.Fatalf("statement: %+v", s)
		}
		checkLines(t, s, []want{ // the pending entry is not a line
			{day(2025, 10, 4), "1000.00", "WISE-TX-1", "Cognizhi Pte Ltd", "Top up from DBS"},
			{day(2025, 10, 4), "-5.00", "WISE-FEE-1", "", "Wise fee"},
		})
		if v := s.Lines[0].Value; v == nil || !v.Equal(day(2025, 10, 5)) {
			t.Errorf("value date: %v", v)
		}
	})
	t.Run("OFX 1 (SGML)", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatOFX, fixture(t, "statement.ofx"), nil, money.Currency{}, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		s := got[0]
		if s.Account != "0721234567" || s.Opening != nil || balance(s.Closing) != "-241.00" || !s.To.Equal(day(2025, 10, 31)) {
			t.Fatalf("statement: %+v", s)
		}
		checkLines(t, s, []want{
			{day(2025, 10, 3), "109.00", "OFX-1", "ACME ROBOTICS", "INV-0001"},
			{day(2025, 10, 4), "-1350.00", "OFX-2", "WISE", "Top up"},
		})
	})
	t.Run("OFX 2 (XML), a card", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatOFX, fixture(t, "statement2.ofx"), nil, money.Currency{}, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		checkLines(t, got[0], []want{{day(2025, 10, 10), "-88.20", "CC-1", "AWS", ""}})
	})
	t.Run("CSV through a mapping", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatCSV, fixture(t, "dbs.csv"), &dbsMapping, sgd, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		s := got[0]
		checkLines(t, s, []want{
			{day(2025, 10, 3), "109.00", "BR25100301", "", "ACME ROBOTICS, INV-0001"},
			{day(2025, 10, 4), "-1350.00", "BR25100402", "", "TRANSFER TO WISE"},
		})
		if balance(s.Lines[0].Balance) != "1109.00" || balance(s.Lines[1].Balance) != "-241.00" {
			t.Errorf("running balances: %s %s", balance(s.Lines[0].Balance), balance(s.Lines[1].Balance))
		}
	})
	t.Run("XLSX through a mapping", func(t *testing.T) {
		got, err := bankfile.Parse(bankfile.FormatXLSX, workbook(t, xlsxRows), &xlsxMapping, sgd, bankfile.DefaultLimits)
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v, %v", got, err)
		}
		checkLines(t, got[0], []want{
			{day(2025, 10, 3), "109.00", "R1", "ACME ROBOTICS", "INV-0001"},
			{day(2025, 10, 4), "-1350.50", "R2", "WISE", "Top up"},
		})
	})
}

var dbsMapping = bankfile.Mapping{Skip: 2, DateFormats: []string{"02/01/2006"}, Number: bankfile.Number{Thousands: ","},
	Date: "Transaction Date", ValueDate: "Value Date", In: "Deposit (SGD)", Out: "Withdrawal (SGD)", Reference: "Reference",
	Description: []string{"Description"}, Balance: "Balance"}

var xlsxMapping = bankfile.Mapping{DateFormats: []string{"02/01/2006"}, Date: "Date", Amount: "Amount", Reference: "Ref",
	Counterparty: "Payee", Description: []string{"Memo"}}

// xlsxRows: a header and two rows; a date stored as a serial (45933 is 3 October 2025), another as text; a shared
// string, an inline string, and numbers stored as Excel writes them.
var xlsxRows = [][]string{
	{"s:Date", "s:Amount", "s:Ref", "s:Payee", "s:Memo"},
	{"n:45933", "n:109", "s:R1", "i:ACME ROBOTICS", "s:INV-0001"},
	{"s:04/10/2025", "n:-1.3505E+3", "s:R2", "s:WISE", "s:Top up"},
}

// workbook builds a minimal .xlsx with rows: each cell "s:" a shared string, "i:" an inline string, "n:" a number.
func workbook(t testing.TB, rows [][]string) []byte {
	t.Helper()
	var shared []string
	var sheet strings.Builder
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, r := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, ri+1)
		for ci, c := range r {
			ref := fmt.Sprintf("%c%d", 'A'+ci, ri+1)
			kind, v, _ := strings.Cut(c, ":")
			switch kind {
			case "s":
				fmt.Fprintf(&sheet, `<c r="%s" t="s"><v>%d</v></c>`, ref, len(shared))
				shared = append(shared, v)
			case "i":
				fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, v)
			default:
				fmt.Fprintf(&sheet, `<c r="%s"><v>%s</v></c>`, ref, v)
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	var sst strings.Builder
	sst.WriteString(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range shared {
		fmt.Fprintf(&sst, `<si><t>%s</t></si>`, s)
	}
	sst.WriteString(`</sst>`)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Statement" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   sheet.String(),
		"xl/sharedStrings.xml":       sst.String(),
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Every refusal names what is wrong, where, and how to fix it: an oversized file, an amount with more decimals than its
// currency or an inexact binary number from a sheet, a date in another form, a mapping naming a missing column or
// incomplete, a line with money in and out, a currency Fyniq does not know, a statement mixing currencies, a file of
// another format, and a workbook that expands beyond its limit.
// req: FR-BNK-02
// asvs: V5.2.1, V5.2.3, V1.5.1
func TestRefusalsNameTheLineAndField(t *testing.T) {
	small := bankfile.Limits{MaxBytes: 100, MaxLines: 10, MaxField: 20}
	bomb := func() []byte { // a sheet of 30 MB of one character, compressed to a few kilobytes
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		w, _ := z.Create("xl/workbook.xml")
		_, _ = w.Write([]byte(`<workbook><sheets/></workbook>`))
		w, _ = z.Create("xl/worksheets/sheet1.xml")
		_, _ = w.Write(bytes.Repeat([]byte("a"), 30<<20))
		_ = z.Close()
		return buf.Bytes()
	}
	for name, x := range map[string]struct {
		format string
		data   []byte
		m      *bankfile.Mapping
		lim    bankfile.Limits
		code   string
		line   int
	}{
		"a file beyond the size limit": {bankfile.FormatMT940, fixture(t, "swift.mt940"), nil, small, bankfile.CodeTooLarge, 0},
		"three decimals in SGD":        {bankfile.FormatMT940, []byte(":20:X\n:60F:C251001SGD1,005\n"), nil, bankfile.DefaultLimits, bankfile.CodeAmount, 2},
		"an unknown currency":          {bankfile.FormatMT940, []byte(":20:X\n:60F:C251001XYZ1,00\n"), nil, bankfile.DefaultLimits, bankfile.CodeCurrency, 2},
		"a line that is not MT940":     {bankfile.FormatMT940, []byte(":20:X\n:60F:C251001SGD1,00\n:61:garbage\n"), nil, bankfile.DefaultLimits, bankfile.CodeFormat, 3},
		"two currencies in one":        {bankfile.FormatMT940, []byte(":20:X\n:60F:C251001SGD1,00\n:62F:C251002USD1,00\n"), nil, bankfile.DefaultLimits, bankfile.CodeCurrency, 3},
		"not MT940 at all":             {bankfile.FormatMT940, []byte("hello"), nil, bankfile.DefaultLimits, bankfile.CodeFormat, 0},
		"not camt.053":                 {bankfile.FormatCAMT053, []byte("<html></html>"), nil, bankfile.DefaultLimits, bankfile.CodeFormat, 0},
		"camt.053 with a DTD":          {bankfile.FormatCAMT053, []byte(`<?xml version="1.0"?><!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]><Document><BkToCstmrStmt><Stmt><Acct><Ccy>&e;</Ccy></Acct></Stmt></BkToCstmrStmt></Document>`), nil, bankfile.DefaultLimits, bankfile.CodeFormat, 0},
		"not OFX":                      {bankfile.FormatOFX, []byte("hello"), nil, bankfile.DefaultLimits, bankfile.CodeFormat, 0},
		"a CSV without its mapping":    {bankfile.FormatCSV, fixture(t, "dbs.csv"), nil, bankfile.DefaultLimits, bankfile.CodeMapping, 0},
		"a mapping naming a missing column": {bankfile.FormatCSV, fixture(t, "dbs.csv"), &bankfile.Mapping{Skip: 2, DateFormats: []string{"02/01/2006"}, Date: "Posted", Amount: "Balance"},
			bankfile.DefaultLimits, bankfile.CodeMapping, 3},
		"a mapping with no amount": {bankfile.FormatCSV, fixture(t, "dbs.csv"), &bankfile.Mapping{DateFormats: []string{"02/01/2006"}, Date: "x"},
			bankfile.DefaultLimits, bankfile.CodeMapping, 0},
		"a date in another form": {bankfile.FormatCSV, fixture(t, "dbs.csv"), func() *bankfile.Mapping { m := dbsMapping; m.DateFormats = []string{"2006-01-02"}; return &m }(),
			bankfile.DefaultLimits, bankfile.CodeDate, 4},
		"money in and out on one row": {bankfile.FormatCSV, []byte("Date,In,Out\n03/10/2025,1.00,2.00\n"), &bankfile.Mapping{DateFormats: []string{"02/01/2006"}, Date: "Date", In: "In", Out: "Out"},
			bankfile.DefaultLimits, bankfile.CodeAmount, 2},
		"an inexact number from a sheet": {bankfile.FormatXLSX, workbook(t, [][]string{{"s:Date", "s:Amount"}, {"s:03/10/2025", "n:1350.4999999999998"}}),
			&bankfile.Mapping{DateFormats: []string{"02/01/2006"}, Date: "Date", Amount: "Amount"}, bankfile.DefaultLimits, bankfile.CodeAmount, 2},
		"a field beyond its limit": {bankfile.FormatCSV, []byte("Date,Amount,Memo\n03/10/2025,1.00," + strings.Repeat("x", 30) + "\n"),
			&bankfile.Mapping{DateFormats: []string{"02/01/2006"}, Date: "Date", Amount: "Amount", Description: []string{"Memo"}},
			bankfile.Limits{MaxBytes: 1000, MaxLines: 10, MaxField: 20}, bankfile.CodeTooLarge, 2},
		"a zip bomb":   {bankfile.FormatXLSX, bomb(), &xlsxMapping, bankfile.DefaultLimits, bankfile.CodeTooLarge, 0},
		"not an .xlsx": {bankfile.FormatXLSX, []byte("PK not really"), &xlsxMapping, bankfile.DefaultLimits, bankfile.CodeFormat, 0},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bankfile.Parse(x.format, x.data, x.m, sgd, x.lim)
			var e *bankfile.Error
			if bankfile.CodeOf(err) != x.code {
				t.Fatalf("got %v, want %s", err, x.code)
			}
			if ok := asError(err, &e); !ok || e.Line != x.line || e.Message == "" || e.Fix == "" {
				t.Fatalf("got %+v, want line %d with a message and a fix", e, x.line)
			}
		})
	}
}

func asError(err error, e **bankfile.Error) bool {
	x, ok := err.(*bankfile.Error)
	if ok {
		*e = x
	}
	return ok
}

// A saved mapping is read from its JSON and refused when it names unknown fields or is incomplete.
// req: FR-BNK-02
func TestMappingsReadFromTheirJSON(t *testing.T) {
	m, err := bankfile.ReadMapping([]byte(`{"skip":2,"date_formats":["02/01/2006"],"number":{"thousands":","},"date":"Transaction Date",
		"in":"Deposit (SGD)","out":"Withdrawal (SGD)","reference":"Reference","description":["Description"],"balance":"Balance"}`))
	if err != nil || m.Skip != 2 || m.In != "Deposit (SGD)" {
		t.Fatalf("%+v, %v", m, err)
	}
	for _, bad := range []string{`{"date":"d","amount":"a","date_formats":["x"],"colour":"red"}`, `{"amount":"a","date_formats":["x"]}`, `not json`} {
		if _, err := bankfile.ReadMapping([]byte(bad)); bankfile.CodeOf(err) != bankfile.CodeMapping {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// checkModel holds of anything a reader returns without error: every line is in its statement's currency, its
// positions run from 1, and no text field exceeds the limit.
func checkModel(t *testing.T, got []bankfile.Statement, lim bankfile.Limits) {
	for _, s := range got {
		for i, l := range s.Lines {
			if l.Position != i+1 || l.Amount.Currency() != s.Currency || l.Amount.IsZero() && s.Currency.Code == "" {
				t.Fatalf("line %d: %+v in a statement in %s", i+1, l, s.Currency.Code)
			}
			for _, f := range []string{l.Reference, l.Counterparty, l.Description} {
				if len([]rune(f)) > lim.MaxField {
					t.Fatalf("a field of %d characters", len([]rune(f)))
				}
			}
		}
	}
}

func fuzzFormat(f *testing.F, format string, m *bankfile.Mapping, seeds ...[]byte) {
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := bankfile.Parse(format, data, m, sgd, bankfile.DefaultLimits)
		if err != nil {
			if bankfile.CodeOf(err) == "" {
				t.Fatalf("a refusal without a code: %v", err)
			}
			return
		}
		checkModel(t, got, bankfile.DefaultLimits)
	})
}

// The readers never panic on untrusted input, and what they return is always the model or a coded refusal.
// req: FR-BNK-02
// asvs: V5.2.1
func FuzzMT940(f *testing.F) {
	fuzzFormat(f, bankfile.FormatMT940SAP, nil, fixture(f, "swift.mt940"), fixture(f, "sap.mt940"))
}

func FuzzCAMT053(f *testing.F) {
	fuzzFormat(f, bankfile.FormatCAMT053, nil, fixture(f, "statement.camt053.xml"))
}

func FuzzOFX(f *testing.F) {
	fuzzFormat(f, bankfile.FormatOFX, nil, fixture(f, "statement.ofx"), fixture(f, "statement2.ofx"))
}

func FuzzCSV(f *testing.F) {
	fuzzFormat(f, bankfile.FormatCSV, &dbsMapping, fixture(f, "dbs.csv"))
}

func FuzzXLSX(f *testing.F) {
	fuzzFormat(f, bankfile.FormatXLSX, &xlsxMapping, workbook(f, xlsxRows))
}
