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
	payables   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002010"), Code: "2010", Active: true, Role: RoleAccountsPayable}
	gstControl = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002200"), Code: "2200", Active: true, Role: RoleGSTControl}
	fxRounding = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000007030"), Code: "7030", Active: true, Role: RoleFXRounding}
	software   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006120"), Code: "6120", Active: true, Type: "expense"}
	hosting    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000005010"), Code: "5010", Active: true, Type: "expense"}
	prepaid    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001210"), Code: "1210", Active: true, Type: "asset", Role: RolePrepayments}
	billID     = uuid.MustParse("0192f1a0-0000-7000-8000-0000000b1110")
)

// purchasing is the chart a bill posts against: the system accounts by role, two expense accounts, and settings' own.
func purchasing() Settings {
	s := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}}
	for _, a := range []Account{payables, gstControl, fxRounding, prepaid, software, hosting, bank, group, closed} {
		s.Accounts[a.ID] = a
	}
	return s
}

func billLine(t testing.TB, acc Account, net, gst string, cur money.Currency, claimable bool) BillLine {
	return BillLine{AccountID: acc.ID, Net: amt(t, net, cur), GST: amt(t, gst, cur), Claimable: claimable, Description: "Line " + acc.Code}
}

// shared is l with the shares of its GST that are claimed.
func shared(l BillLine, shares ...string) BillLine {
	l.Shares = shares
	return l
}

func sgdBill(lines ...BillLine) Bill {
	return Bill{ID: billID, Version: 1, Date: day, Memo: "Atlassian AU-1182", Currency: sgd, Lines: lines}
}

// legs renders an entry's lines as "code amount currency = base", in order.
func legs(s Settings, e Entry) string {
	var parts []string
	for _, l := range e.Lines {
		parts = append(parts, fmt.Sprintf("%s %s %s = %s", s.Accounts[l.AccountID].Code, l.Amount, l.Amount.Currency().Code, l.Base))
	}
	return strings.Join(parts, "; ")
}

// Worked by hand: an SGD bill whose input tax is claimable posts Dr the expense 1,000.00, Dr 2200 90.00, Cr 2010
// 1,090.00. Not claimable (code BL, or an organisation not GST-registered), the GST stays in the cost: Dr 1,090.00, Cr
// 1,090.00. Lines of both kinds and a discount line post line by line, with one 2200 line and one 2010 line.
// req: ledger/posting-rules Posting a bill
// req: ledger/posting-rules Amounts of a posted bill
// req: FR-AP-02
func TestPostBillInTheBaseCurrency(t *testing.T) {
	s := purchasing()
	for _, c := range []struct {
		name string
		b    Bill
		want string
	}{
		{"claimable", sgdBill(billLine(t, software, "1000.00", "90.00", sgd, true)),
			"6120 1000.00 SGD = 1000.00; 2200 90.00 SGD = 90.00; 2010 -1090.00 SGD = -1090.00"},
		{"not claimable", sgdBill(billLine(t, software, "1000.00", "90.00", sgd, false)),
			"6120 1090.00 SGD = 1090.00; 2010 -1090.00 SGD = -1090.00"},
		{"both, and a discount", sgdBill(billLine(t, software, "200.00", "18.00", sgd, true), billLine(t, hosting, "50.00", "4.50", sgd, false),
			billLine(t, software, "-20.00", "-1.80", sgd, true)),
			"6120 200.00 SGD = 200.00; 5010 54.50 SGD = 54.50; 6120 -20.00 SGD = -20.00; 2200 16.20 SGD = 16.20; 2010 -250.70 SGD = -250.70"},
		{"no GST", sgdBill(billLine(t, hosting, "120.00", "0.00", sgd, true)), "5010 120.00 SGD = 120.00; 2010 -120.00 SGD = -120.00"},
	} {
		e, err := PostBill(c.b, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if e.RuleVersion != RuleBill || e.Source != (Source{SourceBill, billID.String(), 1}) || e.PostingIdentity != BillIdentity(billID, 1, RuleBill) ||
			e.Memo != "Atlassian AU-1182" || !e.PostingDate.Equal(day) {
			t.Errorf("%s: the entry's identity: %+v", c.name, e)
		}
	}
}

// G4-023, worked by hand: a USD bill of 1,000.00 net + 90.00 GST at the book rate 1.34, whose tax invoice states
// S$121.50 of GST, posts Dr expense USD 1,000.00 = S$1,340.00, Dr 2200 S$121.50 as stated, Cr 2010 USD 1,090.00 =
// S$1,460.60, and Cr 7030 S$0.90: the same split as ForeignInputTax. Without a stated SGD GST the input tax is not
// claimed and stays in the cost. Two cents of US dollars at 1.5 are 0.02 + 0.02 a line but 0.03 together: the cent goes
// to 7030 too, so the entry balances.
// req: ENG-42
// req: ledger/posting-rules Amounts of a posted bill
func TestPostBillInAnotherCurrency(t *testing.T) {
	s := purchasing()
	stated := amt(t, "121.50", sgd)
	usdBill := func(rate string, lines ...BillLine) Bill {
		return Bill{ID: billID, Version: 1, Date: day, Memo: "Vercel INV-77", Currency: usd, Rate: usdRate(t, rate), Lines: lines}
	}
	claimed := billLine(t, software, "1000.00", "90.00", usd, true)
	claimed.StatedBase = &stated
	e, err := PostBill(usdBill("1.34", claimed), s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "6120 1000.00 USD = 1340.00; 2200 121.50 SGD = 121.50; 2010 -1090.00 USD = -1460.60; 7030 -0.90 SGD = -0.90" {
		t.Errorf("G4-023: %s", got)
	}
	split, err := ForeignInputTax(amt(t, "1000.00", usd), amt(t, "90.00", usd), *usdRate(t, "1.34"), false, &stated)
	if err != nil || !split.Net.Equal(e.Lines[0].Base) || !split.GST.Equal(e.Lines[1].Base) || !split.Gross.Equal(e.Lines[2].Base.Neg()) ||
		!split.Difference.Equal(e.Lines[3].Base.Neg()) {
		t.Errorf("the bill rule and ForeignInputTax disagree: %+v %v", split, err)
	}
	if e, err := PostBill(usdBill("1.34", billLine(t, software, "1000.00", "90.00", usd, true)), s); err != nil ||
		legs(s, e) != "6120 1090.00 USD = 1460.60; 2010 -1090.00 USD = -1460.60" {
		t.Errorf("no SGD GST stated: %s %v", legs(s, e), err)
	}
	if e, err := PostBill(usdBill("1.5", billLine(t, software, "0.01", "0.00", usd, true), billLine(t, hosting, "0.01", "0.00", usd, true)), s); err != nil ||
		legs(s, e) != "6120 0.01 USD = 0.02; 5010 0.01 USD = 0.02; 2010 -0.02 USD = -0.03; 7030 -0.01 SGD = -0.01" {
		t.Errorf("a cent of RND-FX: %s %v", legs(s, e), err)
	}
}

// Worked by hand (FR-AP-20, RND-GST-SHARE; rev 0.89): a claimable line used 60% for the business claims 60% of its GST,
// 90.00 × 0.6 = 54.00, and keeps the other 36.00 in its cost; 50% of 7.65 is 3.825, claimed as 3.83, half away from
// zero, with 3.82 kept. In US dollars the share applies to the GST stated in SGD, 121.50 × 0.6 = 72.90, while the cost
// keeps USD 90.00 − 54.00 = 36.00 at the book rate: USD 1,036.00 = S$1,388.24, and 7030 takes the S$0.54 between the
// stated and the book GST. A share of 0 claims nothing and posts no 2200 line, and a share that is not a percentage is
// refused.
// req: FR-AP-20
// req: RND-GST-SHARE
// req: ledger/posting-rules Amounts of a posted bill
func TestPostBillClaimsAShareOfTheGST(t *testing.T) {
	s := purchasing()
	share := shared
	stated := amt(t, "121.50", sgd)
	usdLine := share(billLine(t, software, "1000.00", "90.00", usd, true), "60")
	usdLine.StatedBase = &stated
	for _, c := range []struct {
		name string
		b    Bill
		want string
	}{
		{"60% for the business", sgdBill(share(billLine(t, software, "1000.00", "90.00", sgd, true), "60")),
			"6120 1036.00 SGD = 1036.00; 2200 54.00 SGD = 54.00; 2010 -1090.00 SGD = -1090.00"},
		{"half of 7.65", sgdBill(share(billLine(t, software, "85.00", "7.65", sgd, true), "50"), billLine(t, hosting, "10.00", "0.90", sgd, true)),
			"6120 88.82 SGD = 88.82; 5010 10.00 SGD = 10.00; 2200 4.73 SGD = 4.73; 2010 -103.55 SGD = -103.55"},
		{"100% is the whole", sgdBill(share(billLine(t, software, "1000.00", "90.00", sgd, true), "100.00")),
			"6120 1000.00 SGD = 1000.00; 2200 90.00 SGD = 90.00; 2010 -1090.00 SGD = -1090.00"},
		{"0% claims nothing", sgdBill(share(billLine(t, software, "1000.00", "90.00", sgd, true), "0")),
			"6120 1090.00 SGD = 1090.00; 2010 -1090.00 SGD = -1090.00"},
		{"not claimable ignores its share", sgdBill(share(billLine(t, software, "1000.00", "90.00", sgd, false), "60")),
			"6120 1090.00 SGD = 1090.00; 2010 -1090.00 SGD = -1090.00"},
		{"in US dollars", Bill{ID: billID, Version: 1, Date: day, Memo: "Vercel INV-77", Currency: usd, Rate: usdRate(t, "1.34"), Lines: []BillLine{usdLine}},
			"6120 1036.00 USD = 1388.24; 2200 72.90 SGD = 72.90; 2010 -1090.00 USD = -1460.60; 7030 -0.54 SGD = -0.54"},
	} {
		e, err := PostBill(c.b, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
	for _, bad := range []string{"60%", "101", "-5", ""} {
		_, err := PostBill(sgdBill(share(billLine(t, software, "1000.00", "90.00", sgd, true), bad)), s)
		if !slices.Contains(codes(err), CodeClaimShare) {
			t.Errorf("share %q: %v; want %s", bad, err, CodeClaimShare)
		}
	}
	if cost, err := share(billLine(t, software, "85.00", "7.65", sgd, true), "50").ClaimedCost(); err != nil || cost.String() != "88.82" {
		t.Errorf("ClaimedCost: %s %v; want 88.82", cost, err)
	}
	if cost, err := billLine(t, software, "85.00", "7.65", sgd, true).ClaimedCost(); err != nil || cost.String() != "85.00" {
		t.Errorf("ClaimedCost without a share: %s %v; want 85.00", cost, err)
	}
}

// Every refusal of the bill rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-AP-02
func TestPostBillRefuses(t *testing.T) {
	s := purchasing()
	shares := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003010"), Code: "3010", Active: true, ShareCapital: true}
	s.Accounts[shares.ID] = shares
	stated := amt(t, "1.00", usd)
	inUSD := billLine(t, software, "10.00", "0.90", usd, true)
	inUSD.StatedBase = &stated
	for name, c := range map[string]struct {
		b    Bill
		s    Settings
		want []string
	}{
		"nothing": {Bill{ID: billID, Version: 1, Currency: sgd}, s, []string{CodeNoDate, CodeTooFewLines}},
		"accounts": {sgdBill(billLine(t, Account{ID: uuid.New()}, "1.00", "0.00", sgd, true), billLine(t, group, "1.00", "0.00", sgd, true),
			billLine(t, closed, "1.00", "0.00", sgd, true), billLine(t, shares, "1.00", "0.00", sgd, true), billLine(t, payables, "1.00", "0.00", sgd, true),
			billLine(t, gstControl, "1.00", "0.00", sgd, true)), s,
			[]string{CodeUnknownAccount, CodeGroupAccount, CodeInactiveAccount, CodeShareCapital, CodeRuleAccount, CodeRuleAccount}},
		"a zero line and a credit total": {sgdBill(billLine(t, software, "0.00", "0.00", sgd, true), billLine(t, hosting, "-5.00", "0.00", sgd, true)), s,
			[]string{CodeZeroLine, CodeBillTotal}},
		"a line in another currency": {sgdBill(billLine(t, software, "5.00", "0.00", usd, true)), s, []string{CodeBillTotal, CodeLineCurrency}},
		"no rate": {Bill{ID: billID, Version: 1, Date: day, Currency: usd, Lines: []BillLine{billLine(t, software, "5.00", "0.00", usd, false)}}, s,
			[]string{CodeMissingRate}},
		"a rate on SGD": {Bill{ID: billID, Version: 1, Date: day, Currency: sgd, Rate: usdRate(t, "1.3"), Lines: []BillLine{billLine(t, software, "5.00", "0.00", sgd, false)}}, s,
			[]string{CodeRateOnBase}},
		"a rate of another currency": {Bill{ID: billID, Version: 1, Date: day, Currency: jpy, Rate: usdRate(t, "1.3"), Lines: []BillLine{billLine(t, software, "5", "0", jpy, false)}}, s,
			[]string{CodeWrongRate}},
		"a stated GST not in SGD": {Bill{ID: billID, Version: 1, Date: day, Currency: usd, Rate: usdRate(t, "1.3"), Lines: []BillLine{inUSD}}, s,
			[]string{CodeLineCurrency}},
		"long text": {Bill{ID: billID, Version: 1, Date: day, Memo: strings.Repeat("m", MaxMemo+1), Currency: sgd,
			Lines: []BillLine{{AccountID: software.ID, Net: amt(t, "1.00", sgd), GST: amt(t, "0.00", sgd), Description: strings.Repeat("d", MaxDescription+1)}}}, s,
			[]string{CodeTooLong, CodeTooLong}},
		"no accounts payable": {sgdBill(billLine(t, software, "1.00", "0.09", sgd, true)), without(s, payables, gstControl), []string{CodeSystemRole, CodeSystemRole}},
		"spreads": {sgdBill(spreadLine(t, billLine(t, software, "1.00", "0.00", sgd, true), october, 0), spreadLine(t, billLine(t, software, "1.00", "0.00", sgd, true), october, MaxSpreadMonths+1),
			spreadLine(t, billLine(t, software, "1.00", "0.00", sgd, true), time.Time{}, 2), spreadLine(t, billLine(t, prepaid, "1.00", "0.00", sgd, true), october, 2),
			spreadLine(t, billLine(t, software, "0.00", "0.09", sgd, true), october, 2)), s,
			[]string{CodeSpread, CodeSpread, CodeSpread, CodeSpread, CodeSpread}},
		"no prepayments": {sgdBill(spreadLine(t, billLine(t, software, "1.00", "0.00", sgd, true), october, 2)), without(s, prepaid), []string{CodeSystemRole}},
	} {
		_, err := PostBill(c.b, c.s)
		if got := codes(err); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v (%v)", name, got, c.want, err)
		}
	}
	longest := Bill{ID: billID, Version: 1, Date: day, Memo: strings.Repeat("m", MaxMemo), Currency: sgd,
		Lines: []BillLine{{AccountID: software.ID, Net: amt(t, "1.00", sgd), GST: amt(t, "0.00", sgd), Description: strings.Repeat("d", MaxDescription)}}}
	if _, err := PostBill(longest, s); err != nil {
		t.Errorf("the longest memo and description: %v", err)
	}
	if _, err := PostBill(Bill{ID: billID, Version: 1, Date: day, Currency: usd, Rate: usdRate(t, "1.5"),
		Lines: []BillLine{billLine(t, software, "0.01", "0.00", usd, true), billLine(t, hosting, "0.01", "0.00", usd, true)}}, without(s, fxRounding)); !slices.Equal(codes(err), []string{CodeSystemRole}) {
		t.Errorf("a difference with no 7030: %v", err)
	}
}

func without(s Settings, gone ...Account) Settings {
	out := Settings{Base: s.Base, Accounts: map[uuid.UUID]Account{}}
	for id, a := range s.Accounts {
		if !slices.ContainsFunc(gone, func(g Account) bool { return g.ID == id }) {
			out.Accounts[id] = a
		}
	}
	return out
}

// Any bill the rule accepts balances in the base currency (INV-01), credits 2010 with RND-FX of its total, and costs
// each line at RND-FX of its amount, in its own currency, at the bill's rate.
// req: QA-ENG-03
// req: INV-01
func TestPostBillProperties(t *testing.T) {
	s := purchasing()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		b := Bill{ID: billID, Version: 1, Date: day, Currency: cur}
		if cur == usd {
			r, err := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "1.3549", "0.7", "1.34"}).Draw(t, "rate"), usd, sgd, "test")
			if err != nil {
				t.Fatal(err)
			}
			b.Rate = &r
		}
		n := rapid.IntRange(1, 6).Draw(t, "lines")
		for i := range n {
			net := money.FromMinor(rapid.Int64Range(1, 50_000_000).Draw(t, fmt.Sprintf("net %d", i)), cur)
			gst := money.FromMinor(rapid.Int64Range(0, 5_000_000).Draw(t, fmt.Sprintf("gst %d", i)), cur)
			l := BillLine{AccountID: rapid.SampledFrom([]uuid.UUID{software.ID, hosting.ID}).Draw(t, "account"), Net: net, GST: gst,
				Claimable: rapid.Bool().Draw(t, "claimable")}
			if cur == usd && rapid.Bool().Draw(t, "stated") {
				x := money.FromMinor(rapid.Int64Range(0, 7_000_000).Draw(t, "stated"), sgd)
				l.StatedBase = &x
			}
			b.Lines = append(b.Lines, l)
		}
		e, err := PostBill(b, s)
		if err != nil {
			t.Fatal(err)
		}
		total, sum := money.Zero(cur), money.Zero(sgd)
		for _, l := range b.Lines {
			total, _ = total.Add(l.Net)
			total, _ = total.Add(l.GST)
		}
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			if want, err := rounding.FX(l.Amount, l.Rate); err != nil || !want.Equal(l.Base) {
				t.Fatalf("line %s: base %s, RND-FX gives %s", s.Accounts[l.AccountID].Code, l.Base, want)
			}
		}
		if !sum.IsZero() {
			t.Fatalf("the entry is off by %s: %s", sum, legs(s, e))
		}
		ap := e.Lines[len(e.Lines)-1]
		if ap.AccountID == fxRounding.ID {
			ap = e.Lines[len(e.Lines)-2]
		}
		if ap.AccountID != payables.ID || !ap.Amount.Equal(total.Neg()) {
			t.Fatalf("2010 takes %s, want −%s: %s", ap.Amount, total, legs(s, e))
		}
	})
}

// Each bill rule version reproduces exactly what it produced over the same bills (ENG-06): its golden file changes
// only by review, and a changed rule is a new version.
// req: ENG-06
// req: QA-ENG-06
func TestEveryBillRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	stated := amt(t, "121.50", sgd)
	g4023 := billLine(t, software, "1000.00", "90.00", usd, true)
	g4023.StatedBase = &stated
	cases := map[string][]struct {
		name string
		b    Bill
	}{
		RuleBill: {
			{"an SGD bill, claimable and not", sgdBill(billLine(t, software, "200.00", "18.00", sgd, true), billLine(t, hosting, "50.00", "4.50", sgd, false))},
			{"G4-023, USD with the GST stated in SGD", Bill{ID: billID, Version: 2, Date: day, Memo: "Vercel INV-77", Currency: usd, Rate: usdRate(t, "1.34"),
				Lines: []BillLine{g4023}}},
			// rev 0.83: bill@1 gained the spread line; no entry posted before it has one, so every stored entry replays.
			{"G4-022's bill half: a year's subscription spread, its GST claimed at once", sgdBill(spreadLine(t, billLine(t, software, "13080.00", "1177.20", sgd, true), october, 12),
				billLine(t, hosting, "50.00", "4.50", sgd, false))},
			{"a USD line spread, at its base amount", Bill{ID: billID, Version: 3, Date: day, Memo: "Vercel INV-78", Currency: usd, Rate: usdRate(t, "1.34"),
				Lines: []BillLine{spreadLine(t, g4023, october, 6)}}},
			// rev 0.89: bill@1 gained the business share; no entry posted before it has one, so every stored entry replays.
			{"a line 60% for the business, one at half of 7.65", sgdBill(shared(billLine(t, software, "1000.00", "90.00", sgd, true), "60"),
				shared(billLine(t, hosting, "85.00", "7.65", sgd, true), "50"))},
			{"a USD line 60% for the business", Bill{ID: billID, Version: 4, Date: day, Memo: "Vercel INV-79", Currency: usd, Rate: usdRate(t, "1.34"),
				Lines: []BillLine{shared(g4023, "60")}}},
		},
	}
	for rule := range BillRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryBillRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayBill(rule, c.b, purchasing())
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
	if _, err := ReplayBill("bill@0", sgdBill(billLine(t, software, "1.00", "0.00", sgd, true)), purchasing()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
