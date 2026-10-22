package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var creditID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c1110")

// Worked by hand: a supplier's credit note for S$200.00 net and S$18.00 claimable GST posts Dr 2010 218.00, Cr the
// expense 200.00, Cr 2200 18.00: the bill's legs the other way. In US dollars (G4-023's figures, credited back) it posts
// Dr 2010 USD 1,090.00 = S$1,460.60, Cr the expense USD 1,000.00 = S$1,340.00, Cr 2200 S$121.50 as stated and Dr 7030
// S$0.90. A refusal names the credit note.
// req: FR-AP-03
// req: ledger/posting-rules Posting a bill
func TestPostVendorCreditReversesABillsLegs(t *testing.T) {
	s := purchasing()
	sgdCredit := Bill{ID: creditID, Version: 1, Date: day, Memo: "Atlassian CN-12", Currency: sgd, Lines: []BillLine{billLine(t, software, "200.00", "18.00", sgd, true)}}
	e, err := PostVendorCredit(sgdCredit, s)
	if err != nil || legs(s, e) != "6120 -200.00 SGD = -200.00; 2200 -18.00 SGD = -18.00; 2010 218.00 SGD = 218.00" {
		t.Fatalf("an SGD credit: %s %v", legs(s, e), err)
	}
	if e.RuleVersion != RuleVendorCredit || e.Source != (Source{SourceVendorCredit, creditID.String(), 1}) || e.PostingIdentity != VendorCreditIdentity(creditID, 1, RuleVendorCredit) {
		t.Errorf("the entry's identity: %+v", e)
	}
	stated := amt(t, "121.50", sgd)
	line := billLine(t, software, "1000.00", "90.00", usd, true)
	line.StatedBase = &stated
	usdCredit := Bill{ID: creditID, Version: 1, Date: day, Memo: "Vercel CN-3", Currency: usd, Rate: usdRate(t, "1.34"), Lines: []BillLine{line}}
	if e, err := PostVendorCredit(usdCredit, s); err != nil || legs(s, e) != "6120 -1000.00 USD = -1340.00; 2200 -121.50 SGD = -121.50; 2010 1090.00 USD = 1460.60; 7030 0.90 SGD = 0.90" {
		t.Errorf("a USD credit: %s %v", legs(s, e), err)
	}
	_, err = PostVendorCredit(Bill{ID: creditID, Version: 1, Currency: sgd}, s)
	probs, _ := err.(Problems)
	if !slices.Equal(codes(err), []string{CodeNoDate, CodeTooFewLines}) || !slices.ContainsFunc(probs, func(p Problem) bool { return p.Message == "the credit note has no date" }) {
		t.Errorf("a refusal: %v", err)
	}
	if _, err := PostVendorCredit(Bill{ID: creditID, Version: 1, Date: day, Currency: sgd, Lines: []BillLine{billLine(t, software, "-5.00", "0.00", sgd, true)}}, s); !strings.Contains(fmt.Sprint(err), "A credit note's total is more than zero") {
		t.Errorf("a credit of a negative total: %v", err)
	}
}

// Any credit note the rule accepts posts exactly the legs the same content posts as a bill, negated.
// req: QA-ENG-03
// req: FR-AP-03
func TestAVendorCreditIsABillNegated(t *testing.T) {
	s := purchasing()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		b := Bill{ID: creditID, Version: 1, Date: day, Currency: cur}
		if cur == usd {
			r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7"}).Draw(t, "rate"), usd, sgd, "test")
			b.Rate = &r
		}
		for i := range rapid.IntRange(1, 4).Draw(t, "lines") {
			b.Lines = append(b.Lines, BillLine{AccountID: software.ID, Net: money.FromMinor(rapid.Int64Range(1, 9_999_999).Draw(t, fmt.Sprintf("net %d", i)), cur),
				GST: money.FromMinor(rapid.Int64Range(0, 999_999).Draw(t, fmt.Sprintf("gst %d", i)), cur), Claimable: rapid.Bool().Draw(t, "claimable")})
		}
		asBill, err := PostBill(b, s)
		if err != nil {
			t.Fatal(err)
		}
		credit, err := PostVendorCredit(b, s)
		if err != nil || len(credit.Lines) != len(asBill.Lines) {
			t.Fatalf("%v: %d lines against %d", err, len(credit.Lines), len(asBill.Lines))
		}
		for i, l := range credit.Lines {
			if !l.Amount.Equal(asBill.Lines[i].Amount.Neg()) || !l.Base.Equal(asBill.Lines[i].Base.Neg()) || l.AccountID != asBill.Lines[i].AccountID {
				t.Fatalf("line %d: %s = %s, the bill's %s = %s", i, l.Amount, l.Base, asBill.Lines[i].Amount, asBill.Lines[i].Base)
			}
		}
	})
}

// Each vendor credit rule version reproduces exactly what it produced over the same credit notes (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryVendorCreditRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	stated := amt(t, "121.50", sgd)
	line := billLine(t, software, "1000.00", "90.00", usd, true)
	line.StatedBase = &stated
	cases := map[string][]struct {
		name string
		b    Bill
	}{
		RuleVendorCredit: {
			{"an SGD credit, claimable and not", Bill{ID: creditID, Version: 1, Date: day, Memo: "Atlassian CN-12", Currency: sgd,
				Lines: []BillLine{billLine(t, software, "200.00", "18.00", sgd, true), billLine(t, hosting, "50.00", "4.50", sgd, false)}}},
			{"a USD credit with the GST stated in SGD", Bill{ID: creditID, Version: 2, Date: day, Memo: "Vercel CN-3", Currency: usd, Rate: usdRate(t, "1.34"),
				Lines: []BillLine{line}}},
		},
	}
	for rule := range VendorCreditRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryVendorCreditRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayVendorCredit(rule, c.b, purchasing())
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
	if _, err := ReplayVendorCredit("vendor_credit@0", Bill{ID: creditID, Version: 1}, purchasing()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
