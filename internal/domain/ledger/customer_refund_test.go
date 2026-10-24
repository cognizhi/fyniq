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

var customerRefundID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000ef120")

// refunding is the chart a customer refund posts against: the sales chart and the two money accounts.
func refunding() Settings {
	s := selling()
	s.Accounts[current.ID], s.Accounts[wallet.ID] = current, wallet
	return s
}

func customerRefund(t testing.TB, from Account, amount string, cur money.Currency, credit, rate *money.Rate) CustomerRefund {
	r := CustomerRefund{ID: customerRefundID, Version: 1, Date: day, Memo: "Acme Analytics, refund of CN-0001", Account: from.ID, Currency: cur, Rate: rate,
		Credit: PaidBill{BillID: creditNoteID, Amount: amt(t, amount, cur), StoredRate: money.Identity(sgd)}}
	if credit != nil {
		r.Credit.StoredRate = *credit
	}
	return r
}

// Worked by hand (FR-AR-04, FR-AR-18, ENG-41): the company pays a customer back a credit.
//   - S$109.00 of an SGD credit note from the current account: Dr 1100 109.00, Cr 1011 109.00;
//   - USD 100.00 of a credit note booked at 1.36 (S$136.00) from the USD wallet at 1.30: Dr 1100 S$136.00, Cr 1021
//     S$130.00, Cr 7030 S$6.00: a gain, as less went out than the credit carried;
//   - USD 100.00 of a credit booked at 1.30 from the SGD current account, which paid S$128.00: Dr 1100 130.00, Cr 1011
//     128.00, Cr 7030 2.00.
//
// A refusal names the refund and its fields.
// req: FR-AR-04
// req: FR-AR-18
// req: ENG-41
// req: ledger/posting-rules Customer refunds and deposits
func TestPostCustomerRefund(t *testing.T) {
	s := refunding()
	for _, c := range []struct {
		name    string
		r       CustomerRefund
		want    string
		settled string
	}{
		{"in SGD", customerRefund(t, current, "109.00", sgd, nil, nil), "1100 109.00 SGD = 109.00; 1011 -109.00 SGD = -109.00", "c2110 109.00 = 109.00 fx 0.00"},
		{"a gain", customerRefund(t, wallet, "100.00", usd, usdRate(t, "1.36"), usdRate(t, "1.30")),
			"1100 100.00 USD = 136.00; 1021 -100.00 USD = -130.00; 7030 -6.00 SGD = -6.00", "c2110 100.00 = 136.00 fx 6.00"},
	} {
		e, x, err := PostCustomerRefund(c.r, s)
		if err != nil || legs(s, e) != c.want || settlements([]Settlement{x}) != c.settled {
			t.Errorf("%s: %s; %s; %v", c.name, legs(s, e), settlements([]Settlement{x}), err)
		}
	}
	fromSGD := customerRefund(t, current, "100.00", usd, usdRate(t, "1.30"), nil)
	paid := amt(t, "128.00", sgd)
	fromSGD.Paid = &paid
	e, x, err := PostCustomerRefund(fromSGD, s)
	if err != nil || legs(s, e) != "1100 100.00 USD = 130.00; 1011 -128.00 SGD = -128.00; 7030 -2.00 SGD = -2.00" || x.RealisedFX.String() != "2.00" ||
		e.RuleVersion != RuleCustomerRefund || e.Source.Type != SourceCustomerRefund || e.PostingIdentity != CustomerRefundIdentity(customerRefundID, 1, RuleCustomerRefund) {
		t.Errorf("from the SGD account: %s; %s; %v", legs(s, e), settlements([]Settlement{x}), err)
	}
	_, _, err = PostCustomerRefund(CustomerRefund{ID: customerRefundID, Version: 1, Currency: sgd, Account: software.ID,
		Credit: PaidBill{BillID: creditNoteID, Amount: money.Zero(sgd), StoredRate: money.Identity(sgd)}}, s)
	probs, _ := err.(Problems)
	if !slices.Equal(codes(err), sortedCopy([]string{CodeNoDate, CodeNotMoneyAccount, CodePaymentAmount})) ||
		!slices.ContainsFunc(probs, func(p Problem) bool {
			return p.Message == "the refund has no date" && p.Fix == "Enter the date the money left the account"
		}) ||
		!slices.ContainsFunc(probs, func(p Problem) bool { return p.Field == "amount" && strings.Contains(p.Message, "the amount refunded") }) {
		t.Errorf("a refusal: %+v", err)
	}
	noPaid := customerRefund(t, current, "100.00", usd, usdRate(t, "1.30"), nil)
	if _, _, err := PostCustomerRefund(noPaid, s); !slices.ContainsFunc(err.(Problems), func(p Problem) bool {
		return p.Code == CodePaidAmount && p.Field == "paid" && strings.Contains(p.Message, "what it paid in SGD")
	}) {
		t.Errorf("a USD credit from an SGD account without what it paid: %+v", err)
	}
}

// Any refund the rule accepts posts exactly the legs a customer receipt of the same amount of an invoice carried as the
// credit is posts, negated, with the realised FX the other way.
// req: QA-ENG-03
// req: FR-AR-04
func TestACustomerRefundIsAReceiptNegated(t *testing.T) {
	s := refunding()
	rates := []string{"1.3472", "1.5", "1.34", "0.7"}
	rapid.Check(t, func(t *rapid.T) {
		amount := money.FromMinor(rapid.Int64Range(1, 900_000_000).Draw(t, "amount"), usd)
		credit, _ := money.ParseRate(rapid.SampledFrom(rates).Draw(t, "credit"), usd, sgd, "test")
		rate, _ := money.ParseRate(rapid.SampledFrom(rates).Draw(t, "rate"), usd, sgd, "test")
		r := CustomerRefund{ID: customerRefundID, Version: 1, Date: day, Account: wallet.ID, Currency: usd, Rate: &rate,
			Credit: PaidBill{BillID: creditNoteID, Amount: amount, StoredRate: credit}}
		e, x, err := PostCustomerRefund(r, s)
		if err != nil {
			t.Fatal(err)
		}
		p, settled, err := PostCustomerReceipt(CustomerReceipt{ID: customerRefundID, Version: 1, Date: day, Account: wallet.ID, Currency: usd, Rate: &rate,
			Invoices: []ReceivedInvoice{{InvoiceID: creditNoteID, Amount: amount, StoredRate: credit}}, Unapplied: money.Zero(usd)}, s)
		if err != nil || len(p.Lines) != len(e.Lines) || !x.RealisedFX.Equal(settled[0].RealisedFX.Neg()) {
			t.Fatalf("%v: %d lines against %d; fx %s against %s", err, len(e.Lines), len(p.Lines), x.RealisedFX, settled[0].RealisedFX)
		}
		for i, l := range e.Lines {
			if !l.Amount.Equal(p.Lines[i].Amount.Neg()) || !l.Base.Equal(p.Lines[i].Base.Neg()) {
				t.Fatalf("line %d: %s = %s, the receipt's %s = %s", i, l.Amount, l.Base, p.Lines[i].Amount, p.Lines[i].Base)
			}
		}
	})
}

// Each customer refund rule version reproduces exactly what it produced over the same refunds (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryCustomerRefundRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	fromSGD := customerRefund(t, current, "100.00", usd, usdRate(t, "1.30"), nil)
	paid := amt(t, "128.00", sgd)
	fromSGD.Paid = &paid
	cases := map[string][]struct {
		name string
		r    CustomerRefund
	}{
		RuleCustomerRefund: {
			{"an SGD credit from the current account", customerRefund(t, current, "109.00", sgd, nil, nil)},
			{"a USD credit from the wallet, a gain", customerRefund(t, wallet, "100.00", usd, usdRate(t, "1.36"), usdRate(t, "1.30"))},
			{"a USD credit from the SGD account", fromSGD},
		},
	}
	for rule := range CustomerRefundRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryCustomerRefundRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, x, err := ReplayCustomerRefund(rule, c.r, refunding())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
			fmt.Fprintf(&b, "settles %s %s = %s, realised %s\n", x.DocumentID, x.Amount, x.Base, x.RealisedFX)
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
	if _, _, err := ReplayCustomerRefund("customer_refund@0", customerRefund(t, current, "1.00", sgd, nil, nil), refunding()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
