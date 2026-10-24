package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var october = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// spreadLine is l spread over months from the month of from.
func spreadLine(t testing.TB, l BillLine, from time.Time, months int) BillLine {
	t.Helper()
	l.Spread = &Spread{From: from, Months: months}
	return l
}

// month is the first day of a month of 2026 or later, as "2026-08".
func month(t testing.TB, s string) time.Time {
	t.Helper()
	m, err := time.Parse("2006-01", s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// schedule renders releases as "l<line> <month> <part>/<of> <date> <code> <amount>", one a line.
func schedule(s Settings, rs []Release) string {
	var b strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&b, "l%d %s %d/%d %s %s %s\n", r.Line, r.Month.Format("2006-01"), r.Part, r.Of, r.Date.Format(time.DateOnly), s.Accounts[r.AccountID].Code, r.Amount)
	}
	return b.String()
}

// Worked by hand (Appendix B.2, "Spread"): a spread line debits 1210 with its cost in the base currency, and the rest
// of the bill posts as it would. A year's subscription of 13,080.00 + 1,177.20 GST, claimable, puts 13,080.00 on 1210
// and all 1,177.20 of GST on 2200 at once (G4-022's bill half); not claimable, the GST stays in the cost and is spread
// with it. A USD line spread goes to 1210 at its base amount, S$1,340.00 at 1.34, in SGD: a prepayment is not monetary
// and is never revalued.
// req: FR-GL-03
// req: G4-022
// req: ledger/posting-rules Spreading an invoice or bill
func TestPostBillSpreadsALineToPrepayments(t *testing.T) {
	s := purchasing()
	stated := amt(t, "121.50", sgd)
	g4023 := billLine(t, software, "1000.00", "90.00", usd, true)
	g4023.StatedBase = &stated
	for _, c := range []struct {
		name string
		b    Bill
		want string
	}{
		{"claimable, beside an unspread line", sgdBill(spreadLine(t, billLine(t, software, "13080.00", "1177.20", sgd, true), october, 12),
			billLine(t, hosting, "50.00", "4.50", sgd, false)),
			"1210 13080.00 SGD = 13080.00; 5010 54.50 SGD = 54.50; 2200 1177.20 SGD = 1177.20; 2010 -14311.70 SGD = -14311.70"},
		{"not claimable", sgdBill(spreadLine(t, billLine(t, software, "1200.00", "108.00", sgd, false), october, 6)),
			"1210 1308.00 SGD = 1308.00; 2010 -1308.00 SGD = -1308.00"},
		{"in US dollars", Bill{ID: billID, Version: 1, Date: day, Memo: "Vercel INV-77", Currency: usd, Rate: usdRate(t, "1.34"),
			Lines: []BillLine{spreadLine(t, g4023, october, 6)}},
			"1210 1340.00 SGD = 1340.00; 2200 121.50 SGD = 121.50; 2010 -1090.00 USD = -1460.60; 7030 -0.90 SGD = -0.90"},
	} {
		e, err := PostBill(c.b, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
	credit := sgdBill(spreadLine(t, billLine(t, software, "100.00", "9.00", sgd, true), october, 2), spreadLine(t, billLine(t, hosting, "10.00", "0.00", sgd, true), october, 2))
	_, err := PostVendorCredit(credit, s)
	if probs, _ := err.(Problems); len(probs) != 2 || probs[0].Code != CodeSpread || probs[0].Field != "lines[1].spread_months" || probs[1].Field != "lines[2].spread_months" {
		t.Errorf("a credit note spread, line by line: %v", err)
	}
	bad := sgdBill(billLine(t, software, "1.00", "0.00", sgd, true), spreadLine(t, billLine(t, software, "1.00", "0.00", sgd, true), october, 0))
	if _, err := PostBill(bad, s); err == nil || err.(Problems)[0].Field != "lines[2].spread_months" {
		t.Errorf("a bill's spread refused on its own line: %v", err)
	}
	credit.Lines = []BillLine{spreadLine(t, billLine(t, software, "100.00", "9.00", sgd, true), october, 2), billLine(t, payables, "1.00", "0.00", sgd, true)}
	if _, err := PostVendorCredit(credit, s); !slices.Equal(codes(err), []string{CodeRuleAccount, CodeSpread}) {
		t.Errorf("a credit note spread with another refusal: %v", err)
	}
}

// Worked by hand: a bill dated 14 October 2026 spreads 13,080.00 over 12 months from August, a service that started
// before the bill: 1,090.00 a month, the parts of August, September and October dated on the bill's date and the rest
// on the first of their months. S$1.00 spread over 6 months from November is G4-015: 0.17, 0.16, 0.17, 0.17, 0.16,
// 0.17, which sum to 1.00 (ALLOC-EVEN). The unspread line between them has no releases, and keeps the third line's
// number.
// req: FR-GL-03
// req: ENG-05
// req: G4-015
// req: ledger/posting-rules Spreading an invoice or bill
func TestBillReleasesAreTheSpreadSplitByMonth(t *testing.T) {
	s := purchasing()
	b := sgdBill(spreadLine(t, billLine(t, software, "13080.00", "0.00", sgd, true), month(t, "2026-08"), 12), billLine(t, hosting, "5.00", "0.00", sgd, true),
		spreadLine(t, billLine(t, hosting, "1.00", "0.00", sgd, true), month(t, "2026-11").AddDate(0, 0, 17), 6))
	e, err := PostBill(b, s)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := BillReleases(b, e, s)
	if err != nil {
		t.Fatal(err)
	}
	want := `l1 2026-08 1/12 2026-10-14 6120 1090.00
l1 2026-09 2/12 2026-10-14 6120 1090.00
l1 2026-10 3/12 2026-10-14 6120 1090.00
l1 2026-11 4/12 2026-11-01 6120 1090.00
l1 2026-12 5/12 2026-12-01 6120 1090.00
l1 2027-01 6/12 2027-01-01 6120 1090.00
l1 2027-02 7/12 2027-02-01 6120 1090.00
l1 2027-03 8/12 2027-03-01 6120 1090.00
l1 2027-04 9/12 2027-04-01 6120 1090.00
l1 2027-05 10/12 2027-05-01 6120 1090.00
l1 2027-06 11/12 2027-06-01 6120 1090.00
l1 2027-07 12/12 2027-07-01 6120 1090.00
l3 2026-11 1/6 2026-11-01 5010 0.17
l3 2026-12 2/6 2026-12-01 5010 0.16
l3 2027-01 3/6 2027-01-01 5010 0.17
l3 2027-02 4/6 2027-02-01 5010 0.17
l3 2027-03 5/6 2027-03-01 5010 0.16
l3 2027-04 6/6 2027-04-01 5010 0.17
`
	if got := schedule(s, rs); got != want {
		t.Errorf("the schedule:\n%s\nwant\n%s", got, want)
	}
	for _, r := range rs {
		if r.DocumentID != billID || r.Version != 1 || r.Memo != "Atlassian AU-1182" || r.Description != "Line "+s.Accounts[r.AccountID].Code {
			t.Fatalf("a release names its bill: %+v", r)
		}
	}
	if rs, err := BillReleases(sgdBill(billLine(t, software, "1.00", "0.00", sgd, true)), e, s); err == nil || rs != nil {
		t.Errorf("an entry with more prepayment lines than the bill's spreads: %v %v", rs, err)
	}
	short := e
	short.Lines = slices.DeleteFunc(slices.Clone(e.Lines), func(l Line) bool { return l.AccountID == prepaid.ID && l.Base.String() == "1.00" })
	if _, err := BillReleases(b, short, s); err == nil {
		t.Error("an entry with fewer prepayment lines than the bill's spreads")
	}
	unspread := sgdBill(billLine(t, software, "1.00", "0.00", sgd, true))
	plain, err := PostBill(unspread, s)
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := BillReleases(unspread, plain, s); err != nil || len(rs) != 0 {
		t.Errorf("a bill with no spread has no releases: %v %v", rs, err)
	}
}

// A release posts Dr the line's account and Cr 1210 with its part, in SGD on its date, under its own identity: one per
// bill version, line and month. Every refusal comes back together.
// req: FR-GL-03
// req: ENG-05
// req: ENG-22
func TestPostBillRelease(t *testing.T) {
	s := purchasing()
	r := Release{Source: SourceBillRelease, DocumentID: billID, Version: 2, Line: 1, Month: month(t, "2026-08"), Part: 1, Of: 12, Date: day, AccountID: software.ID,
		Description: "Jira, a year", Memo: "Atlassian AU-1182", Amount: amt(t, "1090.00", sgd)}
	e, err := PostBillRelease(r, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "6120 1090.00 SGD = 1090.00; 1210 -1090.00 SGD = -1090.00" {
		t.Errorf("legs: %s", got)
	}
	if e.PostingIdentity != "bill_release:"+billID.String()+":v2:l1:2026-08:bill_release@1" || e.RuleVersion != RuleBillRelease ||
		e.Source != (Source{SourceBillRelease, billID.String(), 2}) || !e.PostingDate.Equal(day) ||
		e.Memo != "Atlassian AU-1182: cost spread for August 2026 (1 of 12)" || e.Lines[0].Description != "Jira, a year" || e.Lines[1].Description != "Jira, a year" {
		t.Errorf("the entry: %+v", e)
	}
	zero := r
	zero.Amount = money.Zero(sgd)
	if _, err := PostBillRelease(zero, s); !slices.Equal(codes(err), []string{CodeZeroLine}) {
		t.Errorf("a zero part: %v", err)
	}
	longest := r // the memo at its limit, with the month and the part after the bill's
	longest.Memo = strings.Repeat("m", MaxMemo-len(": cost spread for August 2026 (1 of 12)"))
	if e, err := PostBillRelease(longest, s); err != nil || len(e.Memo) != MaxMemo {
		t.Errorf("the longest memo: %d %v", len(e.Memo), err)
	}
	bad := r
	bad.Date, bad.Amount, bad.Memo, bad.AccountID = time.Time{}, amt(t, "1.00", usd), strings.Repeat("m", MaxMemo), uuid.New()
	if _, err := PostBillRelease(bad, without(s, prepaid)); !slices.Equal(codes(err), []string{CodeNoDate, CodeUnknownAccount, CodeTooLong, CodeSystemRole, CodeLineCurrency}) {
		t.Errorf("every refusal: %v", err)
	}
	for _, c := range []struct {
		acc  Account
		code string
	}{{group, CodeGroupAccount}, {closed, CodeInactiveAccount}} {
		x := r
		x.AccountID = c.acc.ID
		if _, err := PostBillRelease(x, s); !slices.Equal(codes(err), []string{c.code}) {
			t.Errorf("account %s: %v", c.acc.Code, err)
		}
	}
}

// Any spread bill's releases move exactly what it put on 1210 to each spread line's account (ENG-05, ALLOC-EVEN): the
// parts of a line sum to its 1210 amount, each within a cent of the even share, dated in order, never before the bill
// or the first of their month; each release balances, and the bill with all its releases leaves 1210 at zero.
// req: QA-ENG-03
// req: FR-GL-03
// req: INV-01
func TestBillReleasesProperties(t *testing.T) {
	s := purchasing()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		date := day.AddDate(0, 0, rapid.IntRange(-40, 40).Draw(t, "date"))
		b := Bill{ID: billID, Version: 1, Date: date, Memo: "A supplier", Currency: cur}
		if cur == usd {
			r, err := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7", "1.34"}).Draw(t, "rate"), usd, sgd, "test")
			if err != nil {
				t.Fatal(err)
			}
			b.Rate = &r
		}
		for i := range rapid.IntRange(1, 4).Draw(t, "lines") {
			l := BillLine{AccountID: rapid.SampledFrom([]uuid.UUID{software.ID, hosting.ID}).Draw(t, "account"),
				Net: money.FromMinor(rapid.Int64Range(1, 50_000_000).Draw(t, fmt.Sprintf("net %d", i)), cur), GST: money.Zero(cur)}
			if rapid.Bool().Draw(t, "spread") {
				l.Spread = &Spread{From: date.AddDate(0, rapid.IntRange(-6, 6).Draw(t, "from"), 0), Months: rapid.IntRange(1, MaxSpreadMonths).Draw(t, "months")}
			}
			b.Lines = append(b.Lines, l)
		}
		e, err := PostBill(b, s)
		if err != nil {
			t.Fatal(err)
		}
		rs, err := BillReleases(b, e, s)
		if err != nil {
			t.Fatal(err)
		}
		var spread []money.Money // what each spread line put on 1210, in order
		prepay := money.Zero(sgd)
		for _, l := range e.Lines {
			if l.AccountID == prepaid.ID {
				spread = append(spread, l.Base)
				prepay, _ = prepay.Add(l.Base)
			}
		}
		released := map[int]money.Money{}
		for i, r := range rs {
			if r.Date.Before(b.Date) || r.Date.Before(r.Month) || r.Month.Day() != 1 || (i > 0 && rs[i-1].Line == r.Line && r.Date.Before(rs[i-1].Date)) {
				t.Fatalf("release %d is dated %s for %s", i, r.Date, r.Month)
			}
			sum, ok := released[r.Line]
			if !ok {
				sum = money.Zero(sgd)
			}
			released[r.Line], _ = sum.Add(r.Amount)
			if r.Amount.IsZero() {
				continue
			}
			x, err := PostBillRelease(r, s)
			if err != nil {
				t.Fatal(err)
			}
			net, _ := x.Lines[0].Base.Add(x.Lines[1].Base)
			if !net.IsZero() || x.Lines[0].AccountID != b.Lines[r.Line-1].AccountID || x.Lines[1].AccountID != prepaid.ID {
				t.Fatalf("release %d: %s", i, legs(s, x))
			}
			prepay, _ = prepay.Add(x.Lines[1].Base)
		}
		if !prepay.IsZero() {
			t.Fatalf("1210 is left at %s", prepay)
		}
		k := 0
		for i, l := range b.Lines {
			if l.Spread == nil {
				if _, ok := released[i+1]; ok {
					t.Fatalf("line %d is not spread but releases", i+1)
				}
				continue
			}
			if got := released[i+1]; !got.Equal(spread[k]) {
				t.Fatalf("line %d releases %s of the %s it put on 1210", i+1, got, spread[k])
			}
			k++
		}
	})
}

// Each release rule version reproduces exactly what it produced over the same releases (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryBillReleaseRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	cases := map[string][]struct {
		name string
		r    Release
	}{
		RuleBillRelease: {
			{"the first part of a year, dated on the bill", Release{Source: SourceBillRelease, DocumentID: billID, Version: 1, Line: 1, Month: month(t, "2026-08"), Part: 1, Of: 12, Date: day,
				AccountID: software.ID, Description: "Jira, a year", Memo: "Atlassian AU-1182", Amount: amt(t, "1090.00", sgd)}},
			{"G4-015's second part, on the first of its month", Release{Source: SourceBillRelease, DocumentID: billID, Version: 3, Line: 2, Month: month(t, "2026-12"), Part: 2, Of: 6,
				Date: month(t, "2026-12"), AccountID: hosting.ID, Memo: "Vercel INV-78", Amount: amt(t, "0.16", sgd)}},
		},
	}
	for rule := range BillReleaseRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryBillReleaseRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayRelease(rule, c.r, purchasing())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
		}
		path := filepath.Join("testdata", "replay", rule+".golden")
		if *update {
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run `go test ./internal/domain/ledger -run %s -update` for a new rule version, and review it)", err, t.Name())
		}
		if string(want) != b.String() {
			t.Errorf("%s no longer reproduces its entries (ENG-06): make the change a new rule version.\n--- want\n%s\n--- got\n%s", rule, want, b.String())
		}
	}
	if _, err := ReplayRelease("bill_release@0", Release{Source: SourceBillRelease}, purchasing()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
