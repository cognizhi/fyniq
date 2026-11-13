package migration

import (
	"archive/zip"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/sheet"
)

const fixture = "../../test/migration/workbook-sample.xlsx"

func sample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func read(t *testing.T, b []byte) *Workbook {
	t.Helper()
	w, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// sum adds the amounts; a missing one is zero.
func sum(xs ...*apd.Decimal) string {
	total := new(apd.Decimal)
	for _, x := range xs {
		if x != nil {
			if _, err := apd.BaseContext.WithPrecision(40).Add(total, total, x); err != nil {
				panic(err)
			}
		}
	}
	return total.Text('f')
}

func decimal(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func equal(a, b *apd.Decimal) bool {
	if a == nil {
		a = new(apd.Decimal)
	}
	if b == nil {
		b = new(apd.Decimal)
	}
	return a.Cmp(b) == 0
}

// The fixture is the file its record names: written by scripts/workbook_migration_fixture.py from a pinned commit of
// the workbook, never by hand (design D12).
func TestTheFixtureIsTheRecordedOne(t *testing.T) {
	var meta struct {
		Source  struct{ Commit, SHA256 string }
		Sheets  []string
		Fixture struct{ File, SHA256 string }
	}
	raw, err := os.ReadFile("../../test/migration/workbook-sample.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sample(t))
	if got := hex.EncodeToString(sum[:]); got != meta.Fixture.SHA256 || meta.Fixture.File != "workbook-sample.xlsx" {
		t.Errorf("the fixture's SHA-256 is %s; its record says %s: rerun the script, never edit the file", got, meta.Fixture.SHA256)
	}
	if len(meta.Source.Commit) != 40 || len(meta.Source.SHA256) != 64 {
		t.Errorf("the record names no source commit and hash: %+v", meta.Source)
	}
	for _, s := range sheets {
		if !slices.Contains(meta.Sheets, s) {
			t.Errorf("the import reads %q, which the fixture does not copy", s)
		}
	}
}

// The sample workbook reads into the model with nothing to fix, and its counts and totals are the sheets' own: the
// counts and the Transactions, Invoices and Journal totals were counted from the fixture by a separate reader, and the
// opening and carried-forward balances agree with the workbook's own trial balance.
func TestTheSampleWorkbookReadsIntoTheModel(t *testing.T) {
	// req: FR-MIG-01, RND-IMPORT
	w := read(t, sample(t))
	for _, p := range w.Problems {
		if p.Severity == Error {
			t.Errorf("a problem on the sample: %+v", p)
		}
	}
	if w.Errors() || len(w.Problems) != 1 || w.Problems[0].Code != CodeArchived || w.Timesheets != 9 {
		t.Errorf("the sample's problems: %+v", w.Problems)
	}

	s := w.Settings
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	if s.BaseCurrency != "SGD" || !s.BooksStart.Equal(day(2026, 1, 1)) || !s.YearEnd.Equal(day(2026, 12, 31)) ||
		!s.AsOf.Equal(day(2026, 9, 30)) || !s.ClosedTo.Equal(day(2026, 8, 31)) || !s.GSTRegistered || s.PaymentTerms.Text('f') != "30" {
		t.Errorf("the settings: %+v", s)
	}
	if len(s.SystemAccounts) != 16 || s.SystemAccounts["Accounts receivable"] != "1100" || s.SystemAccounts["FX & rounding differences"] != "7030" {
		t.Errorf("the system accounts: %v", s.SystemAccounts)
	}

	counts := map[string][2]int{
		"Accounts": {len(w.Accounts), 83}, "Items": {len(w.Items), 5}, "Contacts": {len(w.Contacts), 16},
		"Employees": {len(w.Employees), 5}, "Departments": {len(w.Departments), 5}, "GST codes": {len(w.GSTCodes), 12},
		"Currencies": {len(w.Currencies), 11}, "FX rates": {len(w.Rates), 9}, "Bank rules": {len(w.BankRules), 29},
		"Transactions": {len(w.Transactions), 178}, "Invoices": {len(w.Invoices), 25}, "Bills": {len(w.Bills), 17},
		"Quotes & Orders": {len(w.QuotesOrders), 4}, "Doc Lines": {len(w.DocLines), 7}, "Payroll": {len(w.Payroll), 39},
		"Assets": {len(w.Assets), 3}, "Journal": {len(w.Journal), 4}, "Recurring": {len(w.Recurring), 5},
		"Bank Rec": {len(w.BankRecs), 6}, "Budget": {len(w.Budget), 22}, "Holdings": {len(w.Holdings), 5},
		"SAFEs": {len(w.Instruments), 1}, "Trial Balance": {len(w.TrialBalance), 83}, "Checks": {len(w.Checks), 37},
		"Carry Forward balances": {len(w.CarryForward.Balances), 83}, "Carry Forward invoices": {len(w.CarryForward.Invoices), 6},
		"Carry Forward bills": {len(w.CarryForward.Bills), 3},
	}
	for name, c := range counts {
		if c[0] != c[1] {
			t.Errorf("%s: read %d rows, the sheet has %d", name, c[0], c[1])
		}
	}

	var in, out, net, dr, cr []*apd.Decimal
	for _, x := range w.Transactions {
		in, out = append(in, x.In), append(out, x.Out)
	}
	for _, d := range w.Invoices {
		net = append(net, d.Net)
	}
	for _, l := range w.Journal {
		dr, cr = append(dr, l.Debit), append(cr, l.Credit)
	}
	// Money out holds 98.09999999999999 nine times: the 98.1 the sheet shows (RND-IMPORT's 15 significant digits).
	if sum(in...) != "988205" || sum(out...) != "671695.05" || sum(net...) != "230000" || sum(dr...) != "3800" || sum(cr...) != "3800" {
		t.Errorf("the totals: in %s, out %s, invoices %s, journal %s / %s", sum(in...), sum(out...), sum(net...), sum(dr...), sum(cr...))
	}

	// The workbook's trial balance opens each account at the balance its Accounts row gives, and Carry Forward carries
	// each balance-sheet account at the "as of" month's end, with the year's profit or loss in retained earnings and the
	// P&L accounts at zero. Accounts and Carry Forward give a balance on the account's own side, the trial balance with
	// debits positive.
	credit := map[string]bool{"Liability": true, "Equity": true, "Revenue": true, "Other Income": true}
	balanceSheet := map[string]bool{"Asset": true, "Liability": true, "Equity": true}
	tb, kind := map[string]TrialBalance{}, map[string]string{}
	for _, r := range w.TrialBalance {
		tb[r.Code] = r
	}
	for _, a := range w.Accounts {
		kind[a.Code] = a.Type
	}
	debit := func(code string, x *apd.Decimal) *apd.Decimal {
		out := new(apd.Decimal)
		if x != nil {
			out.Set(x)
		}
		if credit[kind[code]] {
			out.Neg(out)
		}
		return out
	}
	asOf := -1
	for i, m := range w.TrialBalance[0].Months {
		if m.Equal(s.AsOf) {
			asOf = i
		}
	}
	if len(w.TrialBalance[0].Months) != 12 || asOf != 8 {
		t.Fatalf("the trial balance's months: %v", w.TrialBalance[0].Months)
	}
	for _, a := range w.Accounts {
		if r, ok := tb[a.Code]; !ok || !equal(debit(a.Code, a.Opening), r.Opening) {
			t.Errorf("account %s opens at %v; the trial balance says %v", a.Code, a.Opening, r.Opening)
		}
	}
	retained := s.SystemAccounts["Retained earnings"]
	var profit []*apd.Decimal
	for _, r := range w.TrialBalance {
		if !balanceSheet[kind[r.Code]] {
			profit = append(profit, r.Closing[asOf])
		}
	}
	for _, b := range w.CarryForward.Balances {
		want, carried := sum(tb[b.Code].Closing[asOf]), debit(b.Code, b.Balance).Text('f')
		switch {
		case b.Code == retained:
			want = sum(append(profit, tb[b.Code].Closing[asOf])...)
		case !balanceSheet[kind[b.Code]]:
			want = "0"
		}
		if !equal(debit(b.Code, b.Balance), decimal(t, want)) {
			t.Errorf("Carry Forward carries %s at %s; the trial balance gives %s", b.Code, carried, want)
		}
	}

	// The Checks sheet's own summary: "⚠ 2 item(s) to review".
	review := 0
	for _, c := range w.Checks {
		if c.Status == Review {
			review++
		}
	}
	if review != 2 || w.Checks[0].Number != 1 || w.Checks[0].Name != "Debits equal credits (trial balance)" || w.Checks[0].Status != Fine {
		t.Errorf("the checks: %d to review, the first %+v", review, w.Checks[0])
	}

	// Labels become codes; calculated figures are RND-IMPORT's; Carry Forward's open items keep their currency.
	p := w.Payroll[0]
	if p.Gross.Text('f') != "6000.00" || p.EmployerCPF.Text('f') != "1020.00" || p.SDL.Text('f') != "11.25" || p.Net.Text('f') != "4798.00" {
		t.Errorf("the first pay row: %+v", p)
	}
	if b := w.CarryForward.Bills[0]; b.Currency != "USD" || b.Net.Text('f') != "1450.00" || b.Category != "5010" || !b.OpeningItem {
		t.Errorf("the first open bill: %+v", b)
	}
	if d := w.Departments[0]; d.Name != "Product & Engineering" || d.SalaryAccount != "6011" {
		t.Errorf("the first department: %+v", d)
	}
	if e := w.Employees[0]; e.Department != "General & Admin" || e.SalaryAccount != "6013" {
		t.Errorf("the first employee: %+v", e)
	}
}

// mutate rewrites one part of a workbook.
func mutate(t *testing.T, b []byte, part string, f func(string) string) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	found := false
	for _, file := range r.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == part {
			found = true
			changed := f(string(data))
			if changed == string(data) {
				t.Fatalf("the change to %s changed nothing", part)
			}
			data = []byte(changed)
		}
		fw, err := zw.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("the workbook has no part %s", part)
	}
	return buf.Bytes()
}

// Each thing wrong with a workbook is a problem the report names, with where it is and how to fix it, and all of them
// are found at once (ENG-23).
func TestEachProblemNamesWhereItIsAndHowToFixIt(t *testing.T) {
	// req: FR-MIG-01
	b := sample(t)
	cases := []struct {
		name, part string
		change     func(string) string
		want       Problem
	}{
		{"a missing sheet", "xl/workbook.xml", func(s string) string {
			return strings.Replace(s, `<sheet name="Budget"`, `<sheet name="Budget 2"`, 1)
		}, Problem{Sheet: "Budget", Severity: Error, Code: CodeSheet}},
		{"a missing column", "xl/worksheets/sheet7.xml", func(s string) string {
			return strings.Replace(s, `>Money in</t>`, `>Cash in</t>`, 1)
		}, Problem{Sheet: "Transactions", Row: headerRow, Column: "Money in", Severity: Error, Code: CodeColumn}},
		{"too many decimals", "xl/worksheets/sheet7.xml", func(s string) string {
			return strings.Replace(s, `<v>98.09999999999999</v>`, `<v>98.125</v>`, 1)
		}, Problem{Sheet: "Transactions", Row: 8, Column: "Money out", Severity: Error, Code: CodeValue}},
		{"not a date", "xl/worksheets/sheet8.xml", func(s string) string {
			i := strings.Index(s, `<c r="B6">`)
			j := strings.Index(s[i:], `</c>`)
			return s[:i] + `<c r="B6" t="inlineStr"><is><t>soon</t></is></c>` + s[i+j+len(`</c>`):]
		}, Problem{Sheet: "Invoices", Row: 6, Column: "Invoice date", Severity: Error, Code: CodeValue}},
		{"saved without its values", "xl/worksheets/sheet2.xml", func(s string) string {
			return strings.Replace(s, `<row r="6">`, `<row r="6"><c r="Z6"><f>1+1</f></c>`, 1)
		}, Problem{Sheet: "Accounts", Row: 6, Severity: Error, Code: CodeUncalculated}},
		{"a row the workbook did not post", "xl/worksheets/sheet12.xml", func(s string) string {
			i := strings.Index(s, `<row r="6">`)
			j := strings.Index(s[i:], `✔`)
			return s[:i+j] + `✖` + s[i+j+len(`✔`):]
		}, Problem{Sheet: "Payroll", Row: 6, Column: "Status", Severity: Warning, Code: CodeBlocked}},
		{"a department without a salary account", "xl/worksheets/sheet5.xml", func(s string) string {
			return strings.Replace(s, `>General &amp; Admin</t>`, `>Admin</t>`, 1)
		}, Problem{Sheet: "Employees", Row: 6, Column: "Department", Severity: Error, Code: CodeValue}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := read(t, mutate(t, b, c.part, c.change))
			found := false
			for _, p := range w.Problems {
				if p.Sheet == c.want.Sheet && p.Row == c.want.Row && p.Column == c.want.Column && p.Severity == c.want.Severity && p.Code == c.want.Code {
					found = true
					if p.Message == "" || p.Fix == "" {
						t.Errorf("the problem says no message or fix: %+v", p)
					}
				}
			}
			if !found {
				t.Errorf("no problem %+v among %+v", c.want, w.Problems)
			}
		})
	}
	// A row the workbook did not post is kept out.
	w := read(t, mutate(t, b, "xl/worksheets/sheet12.xml", cases[5].change))
	if len(w.Payroll) != 38 || w.Errors() {
		t.Errorf("the blocked pay row: %d rows, errors %v", len(w.Payroll), w.Errors())
	}
}

// A file that is not a workbook is refused before anything is read.
func TestAFileThatIsNoWorkbookIsRefused(t *testing.T) {
	// req: FR-MIG-01
	var re *sheet.ReadError
	if _, err := Read([]byte("PK not really")); !errors.As(err, &re) {
		t.Errorf("not a workbook: %v", err)
	}
}

// The parallel month's fixture is the sample at the end of October, as scripts/workbook_parallel_fixture.py derives it
// and its record says: its "as of" 31 October, the membership's and the spreads' October on the Trial Balance, every
// other figure the sample's.
// req: data/migration Parallel run (M5)
func TestTheParallelFixtureIsTheSampleAtOctobersEnd(t *testing.T) {
	var meta struct {
		Base, Fixture struct{ File, SHA256 string }
		AsOf          string `json:"as_of"`
		October       []struct{ Account, Movement string }
	}
	raw, err := os.ReadFile("../../test/migration/workbook-parallel.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../test/migration/workbook-parallel.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	base := sha256.Sum256(sample(t))
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != meta.Fixture.SHA256 || hex.EncodeToString(base[:]) != meta.Base.SHA256 {
		t.Fatalf("the fixture or its base differs from the record: rerun the script, never edit the file")
	}
	w, s := read(t, b), read(t, sample(t))
	if w.Settings.AsOf.Format(time.DateOnly) != meta.AsOf || len(meta.October) != 7 {
		t.Fatalf("the parallel month: as of %s, %d October movements", w.Settings.AsOf, len(meta.October))
	}
	moved := map[string]string{}
	for _, m := range meta.October {
		moved[m.Account] = m.Movement
	}
	for i, r := range w.TrialBalance {
		want := s.TrialBalance[i].Movements[9]
		if m, ok := moved[r.Code]; ok {
			d, _, _ := apd.NewFromString(m)
			want = add(want, d)
		}
		if r.Movements[9].Cmp(cmp.Or(want, new(apd.Decimal))) != 0 {
			t.Errorf("account %s's October: %s, not %s", r.Code, r.Movements[9].Text('f'), want.Text('f'))
		}
	}
}
