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

var refundID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000ef110")

func refund(t testing.TB, from Account, amount string, cur money.Currency, credit, rate *money.Rate) SupplierRefund {
	r := SupplierRefund{ID: refundID, Version: 1, Date: day, Memo: "Vercel, refund of CN-3", Account: from.ID, Currency: cur, Rate: rate,
		Credit: PaidBill{BillID: creditID, Amount: amt(t, amount, cur), StoredRate: money.Identity(sgd)}}
	if credit != nil {
		r.Credit.StoredRate = *credit
	}
	return r
}

// Worked by hand (FR-AP-03, ENG-41): a supplier pays back a credit.
//   - S$218.00 of an SGD credit into the current account: Dr 1011 218.00, Cr 2010 218.00;
//   - USD 100.00 of a credit booked at 1.30 (S$130.00) into the USD wallet at 1.34: Dr 1021 S$134.00, Cr 2010 S$130.00,
//     Cr 7030 S$4.00: a gain, as more came in than the credit carried;
//   - the same into the SGD current account, which received S$128.00: Dr 1011 128.00, Cr 2010 130.00, Dr 7030 2.00.
//
// A refusal names the refund and its fields.
// req: FR-AP-03
// req: ENG-41
func TestPostSupplierRefund(t *testing.T) {
	s := paying()
	for _, c := range []struct {
		name    string
		r       SupplierRefund
		want    string
		settled string
	}{
		{"in SGD", refund(t, current, "218.00", sgd, nil, nil), "2010 -218.00 SGD = -218.00; 1011 218.00 SGD = 218.00", "c1110 218.00 = 218.00 fx 0.00"},
		{"a gain", refund(t, wallet, "100.00", usd, usdRate(t, "1.30"), usdRate(t, "1.34")),
			"2010 -100.00 USD = -130.00; 1021 100.00 USD = 134.00; 7030 -4.00 SGD = -4.00", "c1110 100.00 = 130.00 fx 4.00"},
	} {
		e, x, err := PostSupplierRefund(c.r, s)
		if err != nil || legs(s, e) != c.want || settlements([]Settlement{x}) != c.settled {
			t.Errorf("%s: %s; %s; %v", c.name, legs(s, e), settlements([]Settlement{x}), err)
		}
	}
	intoSGD := refund(t, current, "100.00", usd, usdRate(t, "1.30"), nil)
	got := amt(t, "128.00", sgd)
	intoSGD.Received = &got
	e, x, err := PostSupplierRefund(intoSGD, s)
	if err != nil || legs(s, e) != "2010 -100.00 USD = -130.00; 1011 128.00 SGD = 128.00; 7030 2.00 SGD = 2.00" || x.RealisedFX.String() != "-2.00" ||
		e.RuleVersion != RuleSupplierRefund || e.PostingIdentity != SupplierRefundIdentity(refundID, 1, RuleSupplierRefund) {
		t.Errorf("into the SGD account: %s; %s; %v", legs(s, e), settlements([]Settlement{x}), err)
	}
	_, _, err = PostSupplierRefund(SupplierRefund{ID: refundID, Version: 1, Currency: sgd, Account: software.ID, Credit: PaidBill{BillID: creditID, Amount: money.Zero(sgd), StoredRate: money.Identity(sgd)}}, s)
	probs, _ := err.(Problems)
	if !slices.Equal(codes(err), sortedCopy([]string{CodeNoDate, CodeNotMoneyAccount, CodePaymentAmount})) ||
		!slices.ContainsFunc(probs, func(p Problem) bool { return p.Message == "the refund has no date" && p.Fix == "Enter the date the money came into the account" }) ||
		!slices.ContainsFunc(probs, func(p Problem) bool { return p.Field == "amount" && strings.Contains(p.Message, "the amount refunded") }) {
		t.Errorf("a refusal: %+v", err)
	}
	usdNoReceived := refund(t, current, "100.00", usd, usdRate(t, "1.30"), nil)
	if _, _, err := PostSupplierRefund(usdNoReceived, s); !slices.ContainsFunc(err.(Problems), func(p Problem) bool {
		return p.Code == CodePaidAmount && p.Field == "received" && strings.Contains(p.Message, "what it received in SGD")
	}) {
		t.Errorf("a USD credit into an SGD account without what it received: %+v", err)
	}
}

// Any refund the rule accepts posts exactly the legs a payment of the same amount of a bill carried as the credit is
// posts, negated, with the realised FX the other way.
// req: QA-ENG-03
// req: FR-AP-03
func TestASupplierRefundIsAPaymentNegated(t *testing.T) {
	s := paying()
	rates := []string{"1.3472", "1.5", "1.34", "0.7"}
	rapid.Check(t, func(t *rapid.T) {
		amount := money.FromMinor(rapid.Int64Range(1, 900_000_000).Draw(t, "amount"), usd)
		credit, _ := money.ParseRate(rapid.SampledFrom(rates).Draw(t, "credit"), usd, sgd, "test")
		rate, _ := money.ParseRate(rapid.SampledFrom(rates).Draw(t, "rate"), usd, sgd, "test")
		r := SupplierRefund{ID: refundID, Version: 1, Date: day, Account: wallet.ID, Currency: usd, Rate: &rate, Credit: PaidBill{BillID: creditID, Amount: amount, StoredRate: credit}}
		e, x, err := PostSupplierRefund(r, s)
		if err != nil {
			t.Fatal(err)
		}
		p, settled, err := PostBillPayment(BillPayment{ID: refundID, Version: 1, Date: day, Account: wallet.ID, Currency: usd, Rate: &rate,
			Bills: []PaidBill{r.Credit}, Unapplied: money.Zero(usd)}, s)
		if err != nil || len(p.Lines) != len(e.Lines) || !x.RealisedFX.Equal(settled[0].RealisedFX.Neg()) {
			t.Fatalf("%v: %d lines against %d; fx %s against %s", err, len(e.Lines), len(p.Lines), x.RealisedFX, settled[0].RealisedFX)
		}
		for i, l := range e.Lines {
			if !l.Amount.Equal(p.Lines[i].Amount.Neg()) || !l.Base.Equal(p.Lines[i].Base.Neg()) {
				t.Fatalf("line %d: %s = %s, the payment's %s = %s", i, l.Amount, l.Base, p.Lines[i].Amount, p.Lines[i].Base)
			}
		}
	})
}

// Each supplier refund rule version reproduces exactly what it produced over the same refunds (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEverySupplierRefundRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	intoSGD := refund(t, current, "100.00", usd, usdRate(t, "1.30"), nil)
	got := amt(t, "128.00", sgd)
	intoSGD.Received = &got
	cases := map[string][]struct {
		name string
		r    SupplierRefund
	}{
		RuleSupplierRefund: {
			{"an SGD credit into the current account", refund(t, current, "218.00", sgd, nil, nil)},
			{"a USD credit into the wallet, a gain", refund(t, wallet, "100.00", usd, usdRate(t, "1.30"), usdRate(t, "1.34"))},
			{"a USD credit into the SGD account, a loss", intoSGD},
		},
	}
	for rule := range SupplierRefundRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEverySupplierRefundRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, x, err := ReplaySupplierRefund(rule, c.r, paying())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
			fmt.Fprintf(&b, "settles %s %s = %s, realised %s\n", x.BillID, x.Amount, x.Base, x.RealisedFX)
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
	if _, _, err := ReplaySupplierRefund("supplier_refund@0", refund(t, current, "1.00", sgd, nil, nil), paying()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
