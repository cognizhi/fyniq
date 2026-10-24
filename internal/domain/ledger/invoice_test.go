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
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

var (
	receivables = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001100"), Code: "1100", Active: true, Type: "asset", Role: RoleAccountsReceivable}
	deferredRev = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002400"), Code: "2400", Active: true, Type: "liability", Role: RoleDeferredRevenue}
	subscribe   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000004010"), Code: "4010", Active: true, Type: "revenue"}
	consulting  = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000004020"), Code: "4020", Active: true, Type: "revenue"}
	invoiceID   = uuid.MustParse("0192f1a0-0000-7000-8000-0000000a1110")
)

// selling is the chart an invoice posts against: the system accounts by role, two revenue accounts, and settings' own.
func selling() Settings {
	s := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}}
	for _, a := range []Account{receivables, deferredRev, gstControl, fxRounding, subscribe, consulting, software, bank, group, closed} {
		s.Accounts[a.ID] = a
	}
	return s
}

func invoiceLine(t testing.TB, acc Account, net string, cur money.Currency, code, rate string) InvoiceLine {
	return InvoiceLine{AccountID: acc.ID, Net: amt(t, net, cur), GSTCode: code, GSTRate: rate, Description: "Line " + acc.Code}
}

func sgdInvoice(lines ...InvoiceLine) Invoice {
	return Invoice{ID: invoiceID, Version: 1, Date: day, Memo: "Acme Analytics INV-0140", Currency: sgd, Lines: lines}
}

func spreadInvoiceLine(l InvoiceLine, from time.Time, months int) InvoiceLine {
	l.Spread = &Spread{From: from, Months: months}
	return l
}

// Worked by hand: an SGD invoice not GST-registered posts Dr 1100 1,000.00, Cr 4020 1,000.00. Registered, at 9%: Dr
// 1100 1,090.00, Cr 4020 1,000.00, Cr 2200 90.00. Two lines of one code round once on their subtotal (RND-GST-EXCL):
// 333.33 and 333.34 at 9% is 60.00 on 666.67 (30.00 and 30.00 per line, which the setting chooses instead); a
// zero-rated line bears none.
// req: ledger/posting-rules Posting an invoice
// req: ledger/posting-rules Amounts of a posted invoice
// req: FR-AR-03
func TestPostInvoiceInTheBaseCurrency(t *testing.T) {
	s := selling()
	perLine := sgdInvoice(invoiceLine(t, consulting, "333.33", sgd, "SR", "0.09"), invoiceLine(t, consulting, "333.34", sgd, "SR", "0.09"))
	perLine.GSTMethod = rounding.GSTPerLine
	for _, c := range []struct {
		name string
		i    Invoice
		want string
	}{
		{"not registered", sgdInvoice(invoiceLine(t, consulting, "1000.00", sgd, "SR", "")),
			"1100 1000.00 SGD = 1000.00; 4020 -1000.00 SGD = -1000.00"},
		{"at 9%", sgdInvoice(invoiceLine(t, consulting, "1000.00", sgd, "SR", "0.09")),
			"1100 1090.00 SGD = 1090.00; 4020 -1000.00 SGD = -1000.00; 2200 -90.00 SGD = -90.00"},
		{"one subtotal", sgdInvoice(invoiceLine(t, consulting, "333.33", sgd, "SR", "0.09"), invoiceLine(t, consulting, "333.34", sgd, "SR", "0.09")),
			"1100 726.67 SGD = 726.67; 4020 -333.33 SGD = -333.33; 4020 -333.34 SGD = -333.34; 2200 -60.00 SGD = -60.00"},
		{"per line", perLine,
			"1100 726.67 SGD = 726.67; 4020 -333.33 SGD = -333.33; 4020 -333.34 SGD = -333.34; 2200 -60.00 SGD = -60.00"},
		{"zero-rated beside standard", sgdInvoice(invoiceLine(t, subscribe, "500.00", sgd, "ZR", "0"), invoiceLine(t, consulting, "200.00", sgd, "SR", "0.09")),
			"1100 718.00 SGD = 718.00; 4010 -500.00 SGD = -500.00; 4020 -200.00 SGD = -200.00; 2200 -18.00 SGD = -18.00"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, err := PostInvoice(c.i, s)
			if err != nil {
				t.Fatal(err)
			}
			if got := legs(s, e); got != c.want {
				t.Errorf("\n got %s\nwant %s", got, c.want)
			}
			if e.RuleVersion != RuleInvoice || e.Source.Type != SourceInvoice || e.PostingIdentity != InvoiceIdentity(invoiceID, 1, RuleInvoice) {
				t.Errorf("source %+v rule %s identity %s", e.Source, e.RuleVersion, e.PostingIdentity)
			}
		})
	}
}

// Worked by hand, ENG-42 for sales: a USD 1,000.00 invoice at 1.3456 with 9% GST charges USD 90.00, total 1,090.00 =
// SGD 1,466.70 (RND-FX); the net is SGD 1,345.60 and its GST SGD 121.10 (RND-GST-EXCL on the SGD net), 1,466.70 in all,
// so nothing goes to 7030. At 1.3457 the total is 1,466.81 and the parts 1,345.70 + 121.11 = 1,466.81 again; at 1.33335
// with two lines of 0.05, the cents RND-FX leaves go to 7030.
// req: ledger/posting-rules Posting an invoice
// req: ENG-42
func TestPostInvoiceInAnotherCurrency(t *testing.T) {
	s := selling()
	usdInvoice := func(rate string, lines ...InvoiceLine) Invoice {
		return Invoice{ID: invoiceID, Version: 1, Date: day, Memo: "Globex INV-0141", Currency: usd, Rate: usdRate(t, rate), Lines: lines}
	}
	for _, c := range []struct {
		name string
		i    Invoice
		want string
	}{
		{"at 1.3456", usdInvoice("1.3456", invoiceLine(t, consulting, "1000.00", usd, "SR", "0.09")),
			"1100 1090.00 USD = 1466.70; 4020 -1000.00 USD = -1345.60; 2200 -121.10 SGD = -121.10"},
		{"two cents of rounding", usdInvoice("1.33335", invoiceLine(t, consulting, "0.05", usd, "OS", ""), invoiceLine(t, consulting, "0.05", usd, "OS", "")),
			"1100 0.10 USD = 0.13; 4020 -0.05 USD = -0.07; 4020 -0.05 USD = -0.07; 7030 0.01 SGD = 0.01"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, err := PostInvoice(c.i, s)
			if err != nil {
				t.Fatal(err)
			}
			if got := legs(s, e); got != c.want {
				t.Errorf("\n got %s\nwant %s", got, c.want)
			}
		})
	}
}

// A spread line credits 2400 Deferred revenue with its base amount, at rate 1, for its releases to move month by month
// (FR-GL-03, Appendix B.2): G4-022's invoice half.
// req: ledger/posting-rules Spreading an invoice or bill
func TestASpreadInvoiceLineDefersItsRevenue(t *testing.T) {
	s := selling()
	e, err := PostInvoice(sgdInvoice(spreadInvoiceLine(invoiceLine(t, subscribe, "12000.00", sgd, "SR", "0.09"), october, 12)), s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "1100 13080.00 SGD = 13080.00; 2400 -12000.00 SGD = -12000.00; 2200 -1080.00 SGD = -1080.00"; got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// What the rule refuses, all together (ENG-22), each with its code.
// req: ledger/posting-rules Posting an invoice
// req: QA-RPT-02
func TestTheInvoiceRuleRefusesWhatCannotPost(t *testing.T) {
	s := selling()
	noAR := selling()
	delete(noAR.Accounts, receivables.ID)
	for _, c := range []struct {
		name string
		i    Invoice
		s    Settings
		want string
	}{
		{"a total of zero or less", sgdInvoice(invoiceLine(t, consulting, "-10.00", sgd, "SR", "")), s, CodeInvoiceTotal},
		{"a line on accounts receivable", sgdInvoice(invoiceLine(t, receivables, "10.00", sgd, "SR", "")), s, CodeRuleAccount},
		{"a spread on an expense account", sgdInvoice(spreadInvoiceLine(invoiceLine(t, software, "10.00", sgd, "SR", ""), october, 3)), s, CodeSpread},
		{"a foreign invoice with no rate", Invoice{ID: invoiceID, Version: 1, Date: day, Currency: usd, Lines: []InvoiceLine{invoiceLine(t, consulting, "10.00", usd, "SR", "")}}, s, CodeMissingRate},
		{"no accounts receivable account", sgdInvoice(invoiceLine(t, consulting, "10.00", sgd, "SR", "")), noAR, CodeSystemRole},
		{"a GST rate that is not one", sgdInvoice(invoiceLine(t, consulting, "10.00", sgd, "SR", "9%")), s, CodeGSTRate},
		{"no lines", sgdInvoice(), s, CodeTooFewLines},
		{"no date", Invoice{ID: invoiceID, Version: 1, Currency: sgd, Lines: []InvoiceLine{invoiceLine(t, consulting, "10.00", sgd, "SR", "")}}, s, CodeNoDate},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := PostInvoice(c.i, c.s); !slices.Contains(codes(err), c.want) {
				t.Errorf("want %s, got %v", c.want, err)
			}
		})
	}
}

// The rule's limits are exact: a memo of MaxMemo characters and a description of MaxDescription post, one more does
// not; a total of exactly zero, a spread line below zero, and a refusal on the second line name that line.
// req: ledger/posting-rules Posting an invoice
// req: QA-RPT-02
func TestTheInvoiceRuleLimitsAreExact(t *testing.T) {
	s := selling()
	ok := sgdInvoice(invoiceLine(t, consulting, "10.00", sgd, "SR", ""))
	ok.Memo = strings.Repeat("m", MaxMemo)
	ok.Lines[0].Description = strings.Repeat("d", MaxDescription)
	if _, err := PostInvoice(ok, s); err != nil {
		t.Fatalf("at the limits: %v", err)
	}
	long := ok
	long.Memo += "m"
	long.Lines = []InvoiceLine{ok.Lines[0]}
	long.Lines[0].Description += "d"
	err := refusalOf(long, s)
	if fields(err)["memo"] != CodeTooLong || fields(err)["lines[1].description"] != CodeTooLong {
		t.Errorf("one over the limits: %v", err)
	}
	zero := sgdInvoice(invoiceLine(t, consulting, "10.00", sgd, "SR", ""), invoiceLine(t, subscribe, "-10.00", sgd, "SR", ""))
	if !slices.Contains(codes(refusalOf(zero, s)), CodeInvoiceTotal) {
		t.Errorf("a total of zero: %v", refusalOf(zero, s))
	}
	below := sgdInvoice(invoiceLine(t, consulting, "100.00", sgd, "SR", ""), spreadInvoiceLine(invoiceLine(t, subscribe, "-10.00", sgd, "SR", ""), october, 3))
	if fields(refusalOf(below, s))["lines[2].spread_months"] != CodeSpread {
		t.Errorf("a spread line below zero: %v", refusalOf(below, s))
	}
}

// refusalOf is what PostInvoice refuses of i, nil when it posts.
func refusalOf(i Invoice, s Settings) error {
	_, err := PostInvoice(i, s)
	return err
}

// fields are a refusal's codes by field.
func fields(err error) map[string]string {
	out := map[string]string{}
	if ps, ok := err.(Problems); ok {
		for _, p := range ps {
			out[p.Field] = p.Code
		}
	}
	return out
}

// Every invoice the rule posts balances in the base currency, debits 1100 with RND-FX of its total, and credits 2200
// with RND-GST-EXCL on its nets in the base currency.
// req: INV-01
// req: ledger/posting-rules Amounts of a posted invoice
func TestEveryPostedInvoiceBalances(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := selling()
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		i := Invoice{ID: invoiceID, Version: 1, Date: day, Memo: "Property", Currency: cur}
		if cur == usd {
			r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7", "1.33335"}).Draw(t, "rate"), usd, sgd, "test")
			i.Rate = &r
		}
		n := rapid.IntRange(1, 5).Draw(t, "lines")
		for k := range n {
			cents := rapid.Int64Range(1, 5_000_000).Draw(t, fmt.Sprintf("cents%d", k))
			rate := rapid.SampledFrom([]string{"", "0", "0.09", "0.08"}).Draw(t, fmt.Sprintf("rate%d", k))
			code := map[string]string{"": "OS", "0": "ZR", "0.09": "SR", "0.08": "SR8"}[rate]
			i.Lines = append(i.Lines, InvoiceLine{AccountID: consulting.ID, Net: money.FromMinor(cents, cur), GSTCode: code, GSTRate: rate})
		}
		e, err := PostInvoice(i, s)
		if err != nil {
			t.Fatal(err)
		}
		sum := money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
		}
		if !sum.IsZero() {
			t.Fatalf("the entry does not balance by %s: %s", sum, legs(s, e))
		}
		totals, err := TotalsOf(i)
		if err != nil {
			t.Fatal(err)
		}
		rate := money.Identity(sgd)
		if i.Rate != nil {
			rate = *i.Rate
		}
		want, _ := rounding.FX(totals.Total, rate)
		if !e.Lines[0].Base.Equal(want) || e.Lines[0].AccountID != receivables.ID {
			t.Fatalf("1100 takes %s, want %s: %s", e.Lines[0].Base, want, legs(s, e))
		}
	})
}

// Each invoice rule version reproduces exactly what it produced over the same invoices (ENG-06): its golden file
// changes only by review, and a changed rule is a new version.
// req: ENG-06
// req: QA-ENG-06
func TestEveryInvoiceRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	cases := map[string][]struct {
		name string
		i    Invoice
	}{
		RuleInvoice: {
			{"an SGD invoice, standard and zero-rated", sgdInvoice(invoiceLine(t, subscribe, "500.00", sgd, "ZR", "0"), invoiceLine(t, consulting, "200.00", sgd, "SR", "0.09"))},
			{"not registered", sgdInvoice(invoiceLine(t, consulting, "1000.00", sgd, "SR", ""))},
			{"ENG-42: USD with its GST on the SGD net", Invoice{ID: invoiceID, Version: 2, Date: day, Memo: "Globex INV-0141", Currency: usd, Rate: usdRate(t, "1.3456"),
				Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", usd, "SR", "0.09")}}},
			{"G4-022's invoice half: a year's subscription deferred", sgdInvoice(spreadInvoiceLine(invoiceLine(t, subscribe, "12000.00", sgd, "SR", "0.09"), october, 12))},
			{"rev 0.127: a part payment deducted", withDeposits(sgdInvoice(invoiceLine(t, consulting, "2000.00", sgd, "SR", "0.09")),
				deducting(t, "1090.00", "1090.00", "90.00", sgd))},
			{"rev 0.127: a USD deposit at another rate", withDeposits(Invoice{ID: invoiceID, Version: 3, Date: day, Memo: "Globex INV-0142", Currency: usd,
				Rate: usdRate(t, "1.30"), Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", usd, "OS", "")}}, deducting(t, "100.00", "140.00", "0.00", usd))},
		},
	}
	for rule := range InvoiceRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryInvoiceRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayInvoice(rule, c.i, depositing())
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
	if _, err := ReplayInvoice("invoice@0", sgdInvoice(invoiceLine(t, consulting, "1.00", sgd, "SR", "")), selling()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}

func withDeposits(i Invoice, ds ...InvoiceDeposit) Invoice {
	i.Deposits = ds
	return i
}
