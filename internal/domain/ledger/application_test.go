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

var applicationID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000a9110")

func application(t testing.TB, amount string, cur money.Currency, bill, credit *money.Rate) CreditApplication {
	a := CreditApplication{ID: applicationID, Version: 1, Date: day, Memo: "Atlassian, CN-12 to AU-1182", Amount: amt(t, amount, cur),
		BillRate: money.Identity(sgd), CreditRate: money.Identity(sgd)}
	if bill != nil {
		a.BillRate = *bill
	}
	if credit != nil {
		a.CreditRate = *credit
	}
	return a
}

func applied(x Applied) string {
	return fmt.Sprintf("bill %s credit %s fx %s", x.Bill, x.Credit, x.RealisedFX)
}

// Worked by hand (FR-AP-03, ENG-41):
//   - S$100.00 of an SGD credit applied to an SGD bill posts nothing: an allocation alone;
//   - USD 100.00 of a credit booked at 1.30 (S$130.00) applied to a bill booked at 1.34 (S$134.00): Dr 2010 S$134.00,
//     Cr 2010 S$130.00, Cr 7030 S$4.00, a gain: the bill carried more than the credit that settles it;
//   - the same bill settled by what a payment at 1.40 left unapplied (S$140.00): Dr 7030 S$6.00, a loss;
//   - USD 0.01 settling the last of a bill at 1.5 whose carrying amount left is S$0.01, from a credit at 1.5: the bill's
//     line is S$0.02 by RND-FX and its residual Cr S$0.01; the credit's S$0.02; Dr 7030 S$0.01.
//
// req: FR-AP-03
// req: ENG-41
// req: FR-AR-18
func TestPostCreditApplication(t *testing.T) {
	s := purchasing()
	e, x, err := PostCreditApplication(application(t, "100.00", sgd, nil, nil), s)
	if err != nil || len(e.Lines) != 0 || applied(x) != "bill 100.00 credit 100.00 fx 0.00" || e.PostingIdentity != CreditApplicationIdentity(applicationID, 1, RuleCreditApplication) {
		t.Errorf("in SGD: %s %s %v", legs(s, e), applied(x), err)
	}
	for _, c := range []struct {
		name, credit, want, applied string
	}{
		{"a gain", "1.30", "2010 100.00 USD = 134.00; 2010 -100.00 USD = -130.00; 7030 -4.00 SGD = -4.00", "bill 134.00 credit 130.00 fx 4.00"},
		{"a loss", "1.40", "2010 100.00 USD = 134.00; 2010 -100.00 USD = -140.00; 7030 6.00 SGD = 6.00", "bill 134.00 credit 140.00 fx -6.00"},
	} {
		e, x, err := PostCreditApplication(application(t, "100.00", usd, usdRate(t, "1.34"), usdRate(t, c.credit)), s)
		if err != nil || legs(s, e) != c.want || applied(x) != c.applied {
			t.Errorf("%s: %s; %s; %v", c.name, legs(s, e), applied(x), err)
		}
	}
	last := application(t, "0.01", usd, usdRate(t, "1.5"), usdRate(t, "1.5"))
	left := amt(t, "0.01", sgd)
	last.BillClears = &left
	if e, x, err := PostCreditApplication(last, s); err != nil || applied(x) != "bill 0.01 credit 0.02 fx -0.01" ||
		legs(s, e) != "2010 0.01 USD = 0.02; 2010 0.00 USD = -0.01; 2010 -0.01 USD = -0.02; 7030 0.01 SGD = 0.01" {
		t.Errorf("the last of a bill: %s; %s; %v", legs(s, e), applied(x), err)
	}
}

// Every refusal of the application rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-AP-03
func TestPostCreditApplicationRefuses(t *testing.T) {
	s := purchasing()
	jpyRate, _ := money.ParseRate("0.009", jpy, sgd, "test")
	far := amt(t, "3.00", sgd)
	zero := money.Zero(sgd)
	in := func(a CreditApplication, f func(*CreditApplication)) CreditApplication { f(&a); return a }
	for name, c := range map[string]struct {
		a    CreditApplication
		s    Settings
		want []string
	}{
		"nothing": {CreditApplication{ID: applicationID, Version: 1, Amount: money.Zero(sgd), BillRate: money.Identity(sgd), CreditRate: money.Identity(sgd)}, s,
			[]string{CodeNoDate, CodePaymentAmount}},
		"a credit of another currency": {application(t, "1.00", usd, usdRate(t, "1.34"), &jpyRate), s, []string{CodeWrongRate}},
		"a residual above S$1.00": {in(application(t, "0.02", usd, usdRate(t, "1.5"), usdRate(t, "1.5")), func(a *CreditApplication) { a.BillClears = &far }), s,
			[]string{CodePaymentAmount}},
		"a carrying amount of zero": {in(application(t, "0.02", usd, usdRate(t, "1.5"), usdRate(t, "1.5")), func(a *CreditApplication) { a.CreditClears = &zero }), s,
			[]string{CodePaymentAmount}},
		"a residual in SGD": {in(application(t, "1.00", sgd, nil, nil), func(a *CreditApplication) { x := amt(t, "1.01", sgd); a.BillClears = &x }), s,
			[]string{CodePaymentAmount}},
		"a long memo":         {in(application(t, "1.00", sgd, nil, nil), func(a *CreditApplication) { a.Memo = strings.Repeat("m", MaxMemo+1) }), s, []string{CodeTooLong}},
		"no accounts payable": {application(t, "1.00", sgd, nil, nil), without(s, payables), []string{CodeSystemRole}},
	} {
		_, _, err := PostCreditApplication(c.a, c.s)
		probs, _ := err.(Problems)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) {
			t.Errorf("%s: got %v, want %v (%v)", name, got, sortedCopy(c.want), err)
		}
		if name == "a credit of another currency" && (len(probs) != 1 || probs[0].Field != "credit") {
			t.Errorf("the field of the credit's rate: %v", err)
		}
	}
	if _, _, err := PostCreditApplication(application(t, "1.00", usd, usdRate(t, "1.34"), usdRate(t, "1.30")), without(s, fxRounding)); !slices.Equal(codes(err), []string{CodeSystemRole}) {
		t.Errorf("a realised difference with no 7030: %v", err)
	}
	if _, _, err := PostCreditApplication(in(application(t, "1.00", sgd, nil, nil), func(a *CreditApplication) { a.Memo = strings.Repeat("m", MaxMemo) }), s); err != nil {
		t.Errorf("the longest memo: %v", err)
	}
	// USD 0.02 at 1.5 carries S$0.03: a carrying amount left of S$1.03 leaves a residual of S$1.00, the most there is.
	most := amt(t, "1.03", sgd)
	if _, x, err := PostCreditApplication(in(application(t, "0.02", usd, usdRate(t, "1.5"), usdRate(t, "1.5")), func(a *CreditApplication) { a.BillClears = &most }), s); err != nil ||
		x.Bill.String() != "1.03" {
		t.Errorf("a residual of S$1.00: %+v %v", x, err)
	}
}

// Any application the rule accepts balances in the base currency (INV-01), leaves 2010's balance in the credit's
// currency where it was, and books on 7030 exactly the realised FX: the bill's carrying amount less the credit's.
// req: QA-ENG-03
// req: INV-01
// req: ENG-41
func TestPostCreditApplicationProperties(t *testing.T) {
	s := purchasing()
	rates := []string{"1.3472", "1.5", "1.34", "0.7", "1.2999999999"}
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		a := CreditApplication{ID: applicationID, Version: 1, Date: day, Amount: money.FromMinor(rapid.Int64Range(1, 900_000_000).Draw(t, "amount"), cur),
			BillRate: money.Identity(sgd), CreditRate: money.Identity(sgd)}
		if cur == usd {
			a.BillRate, _ = money.ParseRate(rapid.SampledFrom(rates).Draw(t, "bill"), usd, sgd, "test")
			a.CreditRate, _ = money.ParseRate(rapid.SampledFrom(rates).Draw(t, "credit"), usd, sgd, "test")
		}
		e, x, err := PostCreditApplication(a, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, inCur, fx := money.Zero(sgd), money.Zero(cur), money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			if l.AccountID == payables.ID && l.Amount.Currency() == cur {
				inCur, _ = inCur.Add(l.Amount)
			}
			if l.AccountID == fxRounding.ID {
				fx = l.Base
			}
		}
		billBase, _ := rounding.FX(a.Amount, a.BillRate)
		creditBase, _ := rounding.FX(a.Amount, a.CreditRate)
		gain, _ := billBase.Sub(creditBase)
		if !sum.IsZero() || !inCur.IsZero() || !fx.Equal(gain.Neg()) || !x.RealisedFX.Equal(gain) || (gain.IsZero() != (len(e.Lines) == 0)) {
			t.Fatalf("%s: off by %s, %s %s on 2010, 7030 %s, gain %s", legs(s, e), sum, inCur, cur, fx, gain)
		}
	})
}

// Each credit application rule version reproduces exactly what it produced over the same applications (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryCreditApplicationRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	last := application(t, "0.01", usd, usdRate(t, "1.5"), usdRate(t, "1.5"))
	left := amt(t, "0.01", sgd)
	last.BillClears = &left
	cases := map[string][]struct {
		name string
		a    CreditApplication
	}{
		RuleCreditApplication: {
			{"SGD, an allocation alone", application(t, "100.00", sgd, nil, nil)},
			{"USD across rates, a gain", application(t, "100.00", usd, usdRate(t, "1.34"), usdRate(t, "1.30"))},
			{"the last of a bill", last},
		},
	}
	for rule := range CreditApplicationRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryCreditApplicationRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, x, err := ReplayCreditApplication(rule, c.a, purchasing())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
			fmt.Fprintf(&b, "clears the bill %s, the credit %s, realised %s\n", x.Bill, x.Credit, x.RealisedFX)
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
	if _, _, err := ReplayCreditApplication("credit_application@0", application(t, "1.00", sgd, nil, nil), purchasing()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
