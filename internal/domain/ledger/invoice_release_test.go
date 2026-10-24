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

// Worked by hand (Appendix B.2, "Spread"; rev 0.118): an invoice dated 14 October 2026 spreads 12,000.00 of
// subscription over 12 months from August, a service that started before the invoice: 1,000.00 a month, the parts of
// August, September and October dated on the invoice's date and the rest on the first of their months. The unspread
// line between keeps the third line's number. In US dollars, US$1,000.00 at 1.3456 defers S$1,345.60, released
// 448.53, 448.54 and 448.53 (ALLOC-EVEN) from October: deferred revenue is not monetary and is released at the rate it
// was deferred at, never revalued.
// req: FR-GL-03
// req: ENG-05
// req: ledger/posting-rules Spreading an invoice or bill
func TestInvoiceReleasesAreTheSpreadSplitByMonth(t *testing.T) {
	s := selling()
	i := sgdInvoice(spreadInvoiceLine(invoiceLine(t, subscribe, "12000.00", sgd, "SR", "0.09"), month(t, "2026-08"), 12),
		invoiceLine(t, consulting, "500.00", sgd, "SR", "0.09"), spreadInvoiceLine(invoiceLine(t, consulting, "1.00", sgd, "ZR", "0"), month(t, "2026-11"), 6))
	e, err := PostInvoice(i, s)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := InvoiceReleases(i, e, s)
	if err != nil {
		t.Fatal(err)
	}
	want := `l1 2026-08 1/12 2026-10-14 4010 1000.00
l1 2026-09 2/12 2026-10-14 4010 1000.00
l1 2026-10 3/12 2026-10-14 4010 1000.00
l1 2026-11 4/12 2026-11-01 4010 1000.00
l1 2026-12 5/12 2026-12-01 4010 1000.00
l1 2027-01 6/12 2027-01-01 4010 1000.00
l1 2027-02 7/12 2027-02-01 4010 1000.00
l1 2027-03 8/12 2027-03-01 4010 1000.00
l1 2027-04 9/12 2027-04-01 4010 1000.00
l1 2027-05 10/12 2027-05-01 4010 1000.00
l1 2027-06 11/12 2027-06-01 4010 1000.00
l1 2027-07 12/12 2027-07-01 4010 1000.00
l3 2026-11 1/6 2026-11-01 4020 0.17
l3 2026-12 2/6 2026-12-01 4020 0.16
l3 2027-01 3/6 2027-01-01 4020 0.17
l3 2027-02 4/6 2027-02-01 4020 0.17
l3 2027-03 5/6 2027-03-01 4020 0.16
l3 2027-04 6/6 2027-04-01 4020 0.17
`
	if got := schedule(s, rs); got != want {
		t.Errorf("the schedule:\n%s\nwant\n%s", got, want)
	}
	for _, r := range rs {
		if r.Source != SourceInvoiceRelease || r.DocumentID != invoiceID || r.Version != 1 || r.Memo != "Acme Analytics INV-0140" ||
			r.Description != "Line "+s.Accounts[r.AccountID].Code {
			t.Fatalf("a release names its invoice: %+v", r)
		}
	}

	foreign := Invoice{ID: invoiceID, Version: 1, Date: day, Memo: "Globex INV-0141", Currency: usd, Rate: usdRate(t, "1.3456"),
		Lines: []InvoiceLine{spreadInvoiceLine(invoiceLine(t, subscribe, "1000.00", usd, "ZR", "0"), october, 3)}}
	e, err = PostInvoice(foreign, s)
	if err != nil {
		t.Fatal(err)
	}
	if rs, err = InvoiceReleases(foreign, e, s); err != nil {
		t.Fatal(err)
	}
	if got := schedule(s, rs); got != "l1 2026-10 1/3 2026-10-14 4010 448.53\nl1 2026-11 2/3 2026-11-01 4010 448.54\nl1 2026-12 3/3 2026-12-01 4010 448.53\n" {
		t.Errorf("a US dollar spread:\n%s", got)
	}
	if rs, err := InvoiceReleases(sgdInvoice(invoiceLine(t, consulting, "1.00", sgd, "SR", "")), e, s); err == nil || rs != nil {
		t.Errorf("an entry with more deferred revenue lines than the invoice's spreads: %v %v", rs, err)
	}
	plain := sgdInvoice(invoiceLine(t, consulting, "1.00", sgd, "SR", ""))
	pe, err := PostInvoice(plain, s)
	if err != nil {
		t.Fatal(err)
	}
	if rs, err := InvoiceReleases(plain, pe, s); err != nil || len(rs) != 0 {
		t.Errorf("an invoice with no spread has no releases: %v %v", rs, err)
	}
}

// A release posts Dr 2400 and Cr the line's revenue account with its part, in SGD on its date, under its own identity:
// one per invoice version, line and month. Every refusal comes back together; a bill's release is not an invoice's.
// req: FR-GL-03
// req: ENG-05
// req: ENG-22
func TestPostInvoiceRelease(t *testing.T) {
	s := selling()
	r := Release{Source: SourceInvoiceRelease, DocumentID: invoiceID, Version: 2, Line: 1, Month: month(t, "2026-08"), Part: 1, Of: 12, Date: day,
		AccountID: subscribe.ID, Description: "Analytics, a year", Memo: "Acme Analytics INV-0140", Amount: amt(t, "1000.00", sgd)}
	e, err := PostInvoiceRelease(r, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "2400 1000.00 SGD = 1000.00; 4010 -1000.00 SGD = -1000.00" {
		t.Errorf("legs: %s", got)
	}
	if e.PostingIdentity != "invoice_release:"+invoiceID.String()+":v2:l1:2026-08:invoice_release@1" || e.RuleVersion != RuleInvoiceRelease ||
		e.Source != (Source{SourceInvoiceRelease, invoiceID.String(), 2}) || !e.PostingDate.Equal(day) ||
		e.Memo != "Acme Analytics INV-0140: revenue spread for August 2026 (1 of 12)" || e.Lines[1].Description != "Analytics, a year" {
		t.Errorf("the entry: %+v", e)
	}
	zero := r
	zero.Amount = money.Zero(sgd)
	if _, err := PostInvoiceRelease(zero, s); !slices.Equal(codes(err), []string{CodeZeroLine}) {
		t.Errorf("a zero part: %v", err)
	}
	bad := r
	bad.Date, bad.Amount, bad.Memo, bad.AccountID = time.Time{}, amt(t, "1.00", usd), strings.Repeat("m", MaxMemo), uuid.New()
	if _, err := PostInvoiceRelease(bad, without(s, deferredRev)); !slices.Equal(codes(err), []string{CodeNoDate, CodeUnknownAccount, CodeTooLong, CodeSystemRole, CodeLineCurrency}) {
		t.Errorf("every refusal: %v", err)
	}
	for _, c := range []struct {
		acc  Account
		code string
	}{{group, CodeGroupAccount}, {closed, CodeInactiveAccount}} {
		x := r
		x.AccountID = c.acc.ID
		if _, err := PostInvoiceRelease(x, s); !slices.Equal(codes(err), []string{c.code}) {
			t.Errorf("account %s: %v", c.acc.Code, err)
		}
	}
	other := r
	other.Source = SourceBillRelease
	if _, err := PostInvoiceRelease(other, s); !slices.Equal(codes(err), []string{CodeUnknownRule}) {
		t.Errorf("a bill's release under the invoice's rule: %v", err)
	}
	if _, err := PostBillRelease(r, purchasing()); !slices.Equal(codes(err), []string{CodeUnknownRule}) {
		t.Errorf("an invoice's release under the bill's rule: %v", err)
	}
}

// Any spread invoice's releases move exactly what it put on 2400 to each spread line's account (ENG-05, ALLOC-EVEN): the
// parts of a line sum to its 2400 amount, dated in order, never before the invoice or the first of their month; each
// release balances, and the invoice with all its releases leaves 2400 at zero.
// req: QA-ENG-03
// req: FR-GL-03
// req: INV-01
func TestInvoiceReleasesProperties(t *testing.T) {
	s := selling()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		date := day.AddDate(0, 0, rapid.IntRange(-40, 40).Draw(t, "date"))
		i := Invoice{ID: invoiceID, Version: 1, Date: date, Memo: "A customer", Currency: cur}
		if cur == usd {
			r, err := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7", "1.34"}).Draw(t, "rate"), usd, sgd, "test")
			if err != nil {
				t.Fatal(err)
			}
			i.Rate = &r
		}
		for k := range rapid.IntRange(1, 4).Draw(t, "lines") {
			l := InvoiceLine{AccountID: rapid.SampledFrom([]uuid.UUID{subscribe.ID, consulting.ID}).Draw(t, "account"),
				Net: money.FromMinor(rapid.Int64Range(1, 50_000_000).Draw(t, fmt.Sprintf("net %d", k)), cur), GSTCode: "SR"}
			if rapid.Bool().Draw(t, "taxed") {
				l.GSTRate = "0.09"
			}
			if rapid.Bool().Draw(t, "spread") {
				l.Spread = &Spread{From: date.AddDate(0, rapid.IntRange(-6, 6).Draw(t, "from"), 0), Months: rapid.IntRange(1, MaxSpreadMonths).Draw(t, "months")}
			}
			i.Lines = append(i.Lines, l)
		}
		e, err := PostInvoice(i, s)
		if err != nil {
			t.Fatal(err)
		}
		rs, err := InvoiceReleases(i, e, s)
		if err != nil {
			t.Fatal(err)
		}
		var spread []money.Money // what each spread line put on 2400, in order, as a positive amount
		held := money.Zero(sgd)
		for _, l := range e.Lines {
			if l.AccountID == deferredRev.ID {
				spread = append(spread, l.Base.Neg())
				held, _ = held.Add(l.Base)
			}
		}
		released := map[int]money.Money{}
		for k, r := range rs {
			if r.Date.Before(i.Date) || r.Date.Before(r.Month) || r.Month.Day() != 1 || (k > 0 && rs[k-1].Line == r.Line && r.Date.Before(rs[k-1].Date)) {
				t.Fatalf("release %d is dated %s for %s", k, r.Date, r.Month)
			}
			sum, ok := released[r.Line]
			if !ok {
				sum = money.Zero(sgd)
			}
			released[r.Line], _ = sum.Add(r.Amount)
			if r.Amount.IsZero() {
				continue
			}
			x, err := PostInvoiceRelease(r, s)
			if err != nil {
				t.Fatal(err)
			}
			net, _ := x.Lines[0].Base.Add(x.Lines[1].Base)
			if !net.IsZero() || x.Lines[0].AccountID != deferredRev.ID || x.Lines[1].AccountID != i.Lines[r.Line-1].AccountID {
				t.Fatalf("release %d: %s", k, legs(s, x))
			}
			held, _ = held.Add(x.Lines[0].Base)
		}
		if !held.IsZero() {
			t.Fatalf("2400 is left at %s", held)
		}
		n := 0
		for k, l := range i.Lines {
			if l.Spread == nil {
				if _, ok := released[k+1]; ok {
					t.Fatalf("line %d is not spread but releases", k+1)
				}
				continue
			}
			if got := released[k+1]; !got.Equal(spread[n]) {
				t.Fatalf("line %d releases %s of the %s it put on 2400", k+1, got, spread[n])
			}
			n++
		}
	})
}

// Each invoice release rule version reproduces exactly what it produced over the same releases (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryInvoiceReleaseRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	cases := map[string][]struct {
		name string
		r    Release
	}{
		RuleInvoiceRelease: {
			{"the first part of a year, dated on the invoice", Release{Source: SourceInvoiceRelease, DocumentID: invoiceID, Version: 1, Line: 1,
				Month: month(t, "2026-08"), Part: 1, Of: 12, Date: day, AccountID: subscribe.ID, Description: "Analytics, a year",
				Memo: "Acme Analytics INV-0140", Amount: amt(t, "1000.00", sgd)}},
			{"a cent-sized part, on the first of its month", Release{Source: SourceInvoiceRelease, DocumentID: invoiceID, Version: 3, Line: 3,
				Month: month(t, "2026-12"), Part: 2, Of: 6, Date: month(t, "2026-12"), AccountID: consulting.ID, Memo: "Globex INV-0141",
				Amount: amt(t, "0.16", sgd)}},
		},
	}
	for rule := range InvoiceReleaseRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryInvoiceReleaseRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayRelease(rule, c.r, selling())
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
	if _, err := ReplayRelease("invoice_release@0", Release{Source: SourceInvoiceRelease}, selling()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
