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
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

var (
	current   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001011"), Code: "1011", Active: true, Money: true}
	wallet    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001021"), Code: "1021", Active: true, Money: true, Currency: "USD"}
	paymentID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000ba110")
	otherBill = uuid.MustParse("0192f1a0-0000-7000-8000-0000000b1111")
)

// paying is the chart a payment posts against: purchasing's, with an SGD current account and a USD wallet.
func paying() Settings {
	s := purchasing()
	s.Accounts[current.ID], s.Accounts[wallet.ID] = current, wallet
	return s
}

func paid(t testing.TB, id uuid.UUID, amount string, cur money.Currency, stored *money.Rate) PaidBill {
	r := money.Identity(sgd)
	if stored != nil {
		r = *stored
	}
	return PaidBill{BillID: id, Amount: amt(t, amount, cur), StoredRate: r}
}

func sgdPayment(from Account, unapplied string, bills ...PaidBill) BillPayment {
	u, _ := money.Parse(unapplied, sgd)
	return BillPayment{ID: paymentID, Version: 1, Date: day, Memo: "Atlassian, FAST 88231", Account: from.ID, Currency: sgd, Bills: bills, Unapplied: u}
}

func usdPayment(t testing.TB, from Account, rate string, bills ...PaidBill) BillPayment {
	return BillPayment{ID: paymentID, Version: 1, Date: day, Memo: "Vercel, Wise 4471", Account: from.ID, Currency: usd, Rate: usdRate(t, rate),
		Bills: bills, Unapplied: money.Zero(usd)}
}

func settlements(ss []Settlement) string {
	var parts []string
	for _, x := range ss {
		parts = append(parts, fmt.Sprintf("%s %s = %s fx %s", x.BillID.String()[31:], x.Amount, x.Base, x.RealisedFX))
	}
	return strings.Join(parts, "; ")
}

// Worked by hand: an SGD bill of 1,090.00 paid in full from the current account posts Dr 2010 1,090.00, Cr 1011
// 1,090.00, with no realised FX. Two bills and S$25.00 left unapplied (a credit with the supplier) post one 2010 line a
// bill and one for the credit, and Cr the account with the total, S$175.00.
// req: FR-AP-02
// req: FR-AR-18
func TestPostBillPaymentInTheBaseCurrency(t *testing.T) {
	s := paying()
	for _, c := range []struct {
		name         string
		p            BillPayment
		want, settle string
	}{
		{"in full", sgdPayment(current, "0", paid(t, billID, "1090.00", sgd, nil)),
			"2010 1090.00 SGD = 1090.00; 1011 -1090.00 SGD = -1090.00", "b1110 1090.00 = 1090.00 fx 0.00"},
		{"two bills and a credit", sgdPayment(current, "25.00", paid(t, billID, "100.00", sgd, nil), paid(t, otherBill, "50.00", sgd, nil)),
			"2010 100.00 SGD = 100.00; 2010 50.00 SGD = 50.00; 2010 25.00 SGD = 25.00; 1011 -175.00 SGD = -175.00",
			"b1110 100.00 = 100.00 fx 0.00; b1111 50.00 = 50.00 fx 0.00"},
		{"all of it a credit", sgdPayment(current, "40.00"), "2010 40.00 SGD = 40.00; 1011 -40.00 SGD = -40.00", ""},
	} {
		e, ss, err := PostBillPayment(c.p, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if got := settlements(ss); got != c.settle {
			t.Errorf("%s: settled %s, want %s", c.name, got, c.settle)
		}
		if e.RuleVersion != RuleBillPayment || e.Source != (Source{SourceBillPayment, paymentID.String(), 1}) ||
			e.PostingIdentity != BillPaymentIdentity(paymentID, 1, RuleBillPayment) || e.Memo != "Atlassian, FAST 88231" || !e.PostingDate.Equal(day) {
			t.Errorf("%s: the entry's identity: %+v", c.name, e)
		}
	}
}

// ENG-41, worked by hand. A USD bill of 1,090.00 booked at 1.34 carries S$1,460.60 on 2010.
//   - Paid from the USD wallet at 1.30: Dr 2010 USD 1,090.00 = S$1,460.60 (its carrying amount), Cr 1021 USD 1,090.00 =
//     S$1,417.00, and Cr 7030 S$43.60, a realised gain: it cost less than it carried.
//   - Paid from the SGD current account, which paid S$1,420.00: the payment implies 1,420.00 ÷ 1,090.00 =
//     1.3027522936 (RND-RATE), at which the bill cost S$1,420.00: Cr 1011 S$1,420.00 and Cr 7030 S$40.60.
//   - Paid at 1.40 from the wallet: it cost S$1,526.00, S$65.40 more, a realised loss: Dr 7030 S$65.40.
//
// req: ENG-41
// req: FR-AP-02
func TestPostBillPaymentRealisesFX(t *testing.T) {
	s := paying()
	stored := usdRate(t, "1.34")
	fromSGD := usdPayment(t, current, "1.34", paid(t, billID, "1090.00", usd, stored))
	fromSGD.Rate = nil
	x := amt(t, "1420.00", sgd)
	fromSGD.Paid = &x
	for _, c := range []struct {
		name         string
		p            BillPayment
		want, settle string
	}{
		{"a gain, from the wallet", usdPayment(t, wallet, "1.30", paid(t, billID, "1090.00", usd, stored)),
			"2010 1090.00 USD = 1460.60; 1021 -1090.00 USD = -1417.00; 7030 -43.60 SGD = -43.60", "b1110 1090.00 = 1460.60 fx 43.60"},
		{"a gain, from the current account", fromSGD,
			"2010 1090.00 USD = 1460.60; 1011 -1420.00 SGD = -1420.00; 7030 -40.60 SGD = -40.60", "b1110 1090.00 = 1460.60 fx 40.60"},
		{"a loss", usdPayment(t, wallet, "1.40", paid(t, billID, "1090.00", usd, stored)),
			"2010 1090.00 USD = 1460.60; 1021 -1090.00 USD = -1526.00; 7030 65.40 SGD = 65.40", "b1110 1090.00 = 1460.60 fx -65.40"},
	} {
		e, ss, err := PostBillPayment(c.p, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if got := settlements(ss); got != c.settle {
			t.Errorf("%s: settled %s, want %s", c.name, got, c.settle)
		}
	}
	if r := fromSGD; true {
		e, _, _ := PostBillPayment(r, s)
		if e.Lines[1].Rate.String() != "1" {
			t.Errorf("what the current account paid is at rate 1: %s", e.Lines[1].Rate)
		}
	}
}

// RND-FX of the parts need not sum to the whole. A USD bill of 0.03 at 1.5 carries S$0.05 (0.045 rounds away from
// zero); two parts of 0.01 at that rate carry S$0.02 each (0.015 rounds to 0.02), so S$0.01 is left for the last part,
// which RND-FX makes S$0.02 too: Dr 2010 USD 0.01 = S$0.02 at the bill's rate (INV-05), and a base-only line Cr 2010
// S$0.01, the residual, so 2010 holds nothing of the bill (Appendix B, "Settlement difference"). Paid at 1.5 the part
// cost S$0.02 and carried S$0.02: no realised FX (ENG-41); the allocation clears S$0.01. Two bills of 0.01 paid together
// carry S$0.02 each but the wallet paid S$0.03 for 0.02: neither part has a gain, and the cent RND-FX leaves between the
// lines goes to 7030 too. A residual above S$1.00 is a write-off, not a rounding.
// req: ENG-41
// req: RND-FX
func TestTheLastPartOfABillClearsWhatIsLeft(t *testing.T) {
	s := paying()
	stored := usdRate(t, "1.5")
	last := paid(t, billID, "0.01", usd, stored)
	left := amt(t, "0.01", sgd)
	last.Clears = &left
	e, ss, err := PostBillPayment(usdPayment(t, wallet, "1.5", last), s)
	if err != nil || legs(s, e) != "2010 0.01 USD = 0.02; 2010 0.00 USD = -0.01; 1021 -0.01 USD = -0.02; 7030 0.01 SGD = 0.01" || settlements(ss) != "b1110 0.01 = 0.01 fx 0.00" {
		t.Errorf("the last part: %s; %s; %v", legs(s, e), settlements(ss), err)
	}
	exact := paid(t, billID, "0.02", usd, stored)
	three := amt(t, "0.03", sgd)
	exact.Clears = &three
	if e, ss, err := PostBillPayment(usdPayment(t, wallet, "1.5", exact), s); err != nil || legs(s, e) != "2010 0.02 USD = 0.03; 1021 -0.02 USD = -0.03" ||
		settlements(ss) != "b1110 0.02 = 0.03 fx 0.00" {
		t.Errorf("a last part with nothing left over: %s; %s; %v", legs(s, e), settlements(ss), err)
	}
	// USD 0.02 carries S$0.03: what is left may differ from it by S$1.00, not by S$2.99.
	for clears, ok := range map[string]bool{"1.03": true, "0.02": true, "3.02": false} {
		part := paid(t, billID, "0.02", usd, stored)
		x := amt(t, clears, sgd)
		part.Clears = &x
		if _, _, err := PostBillPayment(usdPayment(t, wallet, "1.5", part), s); ok != (err == nil) || (!ok && !slices.Equal(codes(err), []string{CodePaymentAmount})) {
			t.Errorf("a carrying amount of S$%s left for S$0.03: %v", clears, err)
		}
	}
}

// Every refusal of the payment rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-AP-02
func TestPostBillPaymentRefuses(t *testing.T) {
	s := paying()
	stored := usdRate(t, "1.34")
	in := func(p BillPayment, f func(*BillPayment)) BillPayment { f(&p); return p }
	usdFromSGD := amt(t, "100.00", usd)
	sgdPaid := amt(t, "100.00", sgd)
	negative, zero := amt(t, "-1.00", sgd), money.Zero(sgd)
	jpyRate, _ := money.ParseRate("0.009", jpy, sgd, "test")
	for name, c := range map[string]struct {
		p    BillPayment
		s    Settings
		want []string
	}{
		"nothing":             {BillPayment{ID: paymentID, Version: 1, Currency: sgd, Unapplied: money.Zero(sgd)}, s, []string{CodeNoDate, CodeUnknownAccount}},
		"not a money account": {sgdPayment(software, "0", paid(t, billID, "1.00", sgd, nil)), s, []string{CodeNotMoneyAccount}},
		"a group or closed money account": {sgdPayment(Account{ID: group.ID}, "0", paid(t, billID, "1.00", sgd, nil)),
			s, []string{CodeNotMoneyAccount}},
		"SGD bills from the USD wallet": {sgdPayment(wallet, "0", paid(t, billID, "1.00", sgd, nil)), s, []string{CodeLineCurrency}},
		"USD bills, no rate": {in(usdPayment(t, wallet, "1.3", paid(t, billID, "1.00", usd, stored)), func(p *BillPayment) { p.Rate = nil }), s,
			[]string{CodeMissingRate}},
		"USD bills, a yen rate": {in(usdPayment(t, wallet, "1.3", paid(t, billID, "1.00", usd, stored)), func(p *BillPayment) { p.Rate = &jpyRate }), s,
			[]string{CodeWrongRate}},
		"USD bills from SGD, nothing paid named, and a rate": {usdPayment(t, current, "1.3", paid(t, billID, "1.00", usd, stored)), s,
			[]string{CodePaidAmount, CodeWrongRate}},
		"SGD bills, a rate": {in(sgdPayment(current, "0", paid(t, billID, "1.00", sgd, nil)), func(p *BillPayment) { p.Rate = stored }), s,
			[]string{CodeRateOnBase}},
		"what was paid, for SGD bills": {in(sgdPayment(current, "0", paid(t, billID, "1.00", sgd, nil)), func(p *BillPayment) { p.Paid = &sgdPaid }), s,
			[]string{CodePaidAmount}},
		"what was paid, in dollars": {in(usdPayment(t, current, "1.3", paid(t, billID, "1.00", usd, stored)), func(p *BillPayment) { p.Paid, p.Rate = &usdFromSGD, nil }), s,
			[]string{CodePaidAmount}},
		"amounts": {in(sgdPayment(current, "-5.00", paid(t, billID, "0.00", sgd, nil), paid(t, otherBill, "1.00", usd, nil)), func(*BillPayment) {}), s,
			[]string{CodePaymentAmount, CodePaymentAmount, CodePaymentAmount}},
		"a bill of another currency": {usdPayment(t, wallet, "1.3", paid(t, billID, "1.00", usd, &jpyRate)), s, []string{CodeWrongRate}},
		"a carrying amount below zero": {in(sgdPayment(current, "0", paid(t, billID, "1.00", sgd, nil)), func(p *BillPayment) { p.Bills[0].Clears = &negative }), s,
			[]string{CodePaymentAmount}},
		"a carrying amount of zero": {in(usdPayment(t, wallet, "1.3", paid(t, billID, "0.02", usd, stored)), func(p *BillPayment) { p.Bills[0].Clears = &zero }), s,
			[]string{CodePaymentAmount}},
		"nothing paid":        {sgdPayment(current, "0"), s, []string{CodePaymentAmount}},
		"a long reference":    {in(sgdPayment(current, "1.00"), func(p *BillPayment) { p.Memo = strings.Repeat("m", MaxMemo+1) }), s, []string{CodeTooLong}},
		"no accounts payable": {sgdPayment(current, "1.00"), without(s, payables), []string{CodeSystemRole}},
	} {
		_, ss, err := PostBillPayment(c.p, c.s)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) || ss != nil {
			t.Errorf("%s: got %v, want %v (%v)", name, got, sortedCopy(c.want), err)
		}
	}
	// Each problem names its field, counting the bills from one.
	_, _, err := PostBillPayment(sgdPayment(current, "0", paid(t, billID, "1.00", sgd, nil), paid(t, otherBill, "0.00", sgd, nil)), s)
	if probs, _ := err.(Problems); len(probs) != 1 || probs[0].Field != "bills[2].amount" {
		t.Errorf("the second bill's amount: %v", err)
	}
	if _, _, err := PostBillPayment(usdPayment(t, wallet, "1.30", paid(t, billID, "1.00", usd, stored)), without(s, fxRounding)); !slices.Equal(codes(err), []string{CodeSystemRole}) {
		t.Errorf("a realised difference with no 7030: %v", err)
	}
	if _, _, err := PostBillPayment(in(sgdPayment(current, "1.00"), func(p *BillPayment) { p.Memo = strings.Repeat("m", MaxMemo) }), s); err != nil {
		t.Errorf("the longest reference: %v", err)
	}
}

// Any payment the rule accepts balances in the base currency (INV-01); costs each 2010 line at RND-FX of its amount at
// the bill's stored rate; credits the money account with RND-FX of the total at the payment's rate, or with what it
// paid; and books on 7030 the settlements' realised FX but for the cents RND-FX leaves between the parts, at most one
// a line.
// req: QA-ENG-03
// req: INV-01
// req: ENG-41
func TestPostBillPaymentProperties(t *testing.T) {
	s := paying()
	rates := []string{"1.3472", "1.5", "1.3549", "0.7", "1.34", "1.2999999999"}
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		p := BillPayment{ID: paymentID, Version: 1, Date: day, Currency: cur, Account: current.ID,
			Unapplied: money.FromMinor(rapid.SampledFrom([]int64{0, 0, 1, 2_500, 99_999_999}).Draw(t, "unapplied"), cur)}
		stored := money.Identity(sgd)
		if cur == usd {
			r, err := money.ParseRate(rapid.SampledFrom(rates).Draw(t, "rate"), usd, sgd, "test")
			if err != nil {
				t.Fatal(err)
			}
			p.Rate = &r
			if stored, err = money.ParseRate(rapid.SampledFrom(rates).Draw(t, "stored"), usd, sgd, "test"); err != nil {
				t.Fatal(err)
			}
			if rapid.Bool().Draw(t, "from the current account") {
				x := money.FromMinor(rapid.Int64Range(1, 900_000_000).Draw(t, "paid"), sgd)
				p.Paid, p.Rate = &x, nil
			} else {
				p.Account = wallet.ID
			}
		}
		n := rapid.IntRange(0, 5).Draw(t, "bills")
		if n == 0 && p.Unapplied.IsZero() {
			n = 1
		}
		for i := range n {
			p.Bills = append(p.Bills, PaidBill{BillID: uuid.New(), Amount: money.FromMinor(rapid.Int64Range(1, 500_000_000).Draw(t, fmt.Sprintf("amount %d", i)), cur),
				StoredRate: stored})
		}
		e, ss, err := PostBillPayment(p, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, fx, gains := money.Zero(sgd), money.Zero(sgd), money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			if l.AccountID == fxRounding.ID {
				fx = l.Base
			}
		}
		if !sum.IsZero() {
			t.Fatalf("the entry is off by %s: %s", sum, legs(s, e))
		}
		for i, x := range ss {
			if want, _ := rounding.FX(p.Bills[i].Amount, stored); !x.Base.Equal(want) || !e.Lines[i].Base.Equal(want) || e.Lines[i].AccountID != payables.ID {
				t.Fatalf("bill %d carries %s, RND-FX gives %s: %s", i, x.Base, want, legs(s, e))
			}
			gains, _ = gains.Add(x.RealisedFX)
		}
		bankLine := e.Lines[len(ss)]
		if !p.Unapplied.IsZero() {
			bankLine = e.Lines[len(ss)+1]
		}
		total := p.Unapplied
		for _, b := range p.Bills {
			total, _ = total.Add(b.Amount)
		}
		want := money.Money{}
		if p.Paid != nil {
			want = p.Paid.Neg()
		} else {
			r := money.Identity(sgd)
			if p.Rate != nil {
				r = *p.Rate
			}
			want, _ = rounding.FX(total.Neg(), r)
		}
		if !bankLine.Base.Equal(want) {
			t.Fatalf("the money account takes %s, want %s: %s", bankLine.Base, want, legs(s, e))
		}
		// 7030 is the gains, negated, and the cents RND-FX leaves between the parts.
		rest, _ := fx.Add(gains)
		if minor, _ := rest.Minor(); minor < -int64(len(e.Lines)) || minor > int64(len(e.Lines)) {
			t.Fatalf("7030 takes %s for gains of %s: %s", fx, gains, legs(s, e))
		}
	})
}

// Each bill payment rule version reproduces exactly what it produced over the same payments (ENG-06): its golden file
// changes only by review, and a changed rule is a new version.
// req: ENG-06
// req: QA-ENG-06
func TestEveryBillPaymentRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	stored := usdRate(t, "1.34")
	x := amt(t, "1420.00", sgd)
	fromSGD := usdPayment(t, current, "1.3", paid(t, billID, "1090.00", usd, stored))
	fromSGD.Rate, fromSGD.Paid, fromSGD.Version = nil, &x, 2
	cleared := paid(t, otherBill, "0.01", usd, usdRate(t, "1.5"))
	left := amt(t, "0.01", sgd)
	cleared.Clears = &left
	cases := map[string][]struct {
		name string
		p    BillPayment
	}{
		RuleBillPayment: {
			{"two SGD bills and a credit", sgdPayment(current, "25.00", paid(t, billID, "100.00", sgd, nil), paid(t, otherBill, "50.00", sgd, nil))},
			{"a USD bill from the USD wallet, a gain", usdPayment(t, wallet, "1.30", paid(t, billID, "1090.00", usd, stored))},
			{"a USD bill from the SGD account", fromSGD},
			{"the last part of a bill", usdPayment(t, wallet, "1.5", cleared)},
		},
	}
	for rule := range BillPaymentRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryBillPaymentRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, ss, err := ReplayBillPayment(rule, c.p, paying())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
			for _, x := range ss {
				fmt.Fprintf(&b, "settles %s %s %s = %s %s, realised %s\n", x.BillID, x.Amount, x.Amount.Currency().Code, x.Base, x.Base.Currency().Code, x.RealisedFX)
			}
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
	if _, _, err := ReplayBillPayment("bill_payment@0", sgdPayment(current, "1.00"), paying()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
