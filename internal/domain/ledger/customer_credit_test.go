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

var customerApplicationID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000a9120")

func customerApplication(t testing.TB, amount string, cur money.Currency, inv, credit *money.Rate) CustomerCreditApplication {
	a := CustomerCreditApplication{ID: customerApplicationID, Version: 1, Date: day, Memo: "CN-0001 applied to INV-0140", Amount: amt(t, amount, cur),
		InvoiceRate: money.Identity(sgd), CreditRate: money.Identity(sgd)}
	if inv != nil {
		a.InvoiceRate = *inv
	}
	if credit != nil {
		a.CreditRate = *credit
	}
	return a
}

func customerApplied(x CustomerApplied) string {
	return fmt.Sprintf("invoice %s credit %s fx %s", x.Invoice, x.Credit, x.RealisedFX)
}

// Worked by hand (FR-AR-04, FR-AR-18, ENG-41):
//   - S$100.00 of an SGD credit note applied to an SGD invoice posts nothing: an allocation alone;
//   - USD 100.00 of a credit note booked at 1.36 (S$136.00) applied to an invoice booked at 1.34 (S$134.00): Cr 1100
//     S$134.00, Dr 1100 S$136.00, Cr 7030 S$2.00, a gain: the credit settled a debt carried for less than it;
//   - the same invoice settled by what a receipt at 1.30 left unapplied (S$130.00): Dr 7030 S$4.00, a loss;
//   - USD 0.01 settling the last of an invoice at 1.5 whose carrying amount left is S$0.01, from a credit at 1.5: the
//     invoice's line is S$0.02 by RND-FX and its residual Dr S$0.01; the credit's S$0.02; Cr 7030 S$0.01.
//
// req: FR-AR-04
// req: FR-AR-18
// req: ENG-41
func TestPostCustomerCreditApplication(t *testing.T) {
	s := selling()
	e, x, err := PostCustomerCreditApplication(customerApplication(t, "100.00", sgd, nil, nil), s)
	if err != nil || len(e.Lines) != 0 || customerApplied(x) != "invoice 100.00 credit 100.00 fx 0.00" ||
		e.PostingIdentity != CustomerCreditApplicationIdentity(customerApplicationID, 1, RuleCustomerCreditApplication) || e.Source.Type != SourceCustomerCreditApplication {
		t.Errorf("in SGD: %s %s %v", legs(s, e), customerApplied(x), err)
	}
	for _, c := range []struct {
		name, credit, want, applied string
	}{
		{"a gain", "1.36", "1100 -100.00 USD = -134.00; 1100 100.00 USD = 136.00; 7030 -2.00 SGD = -2.00", "invoice 134.00 credit 136.00 fx 2.00"},
		{"a loss", "1.30", "1100 -100.00 USD = -134.00; 1100 100.00 USD = 130.00; 7030 4.00 SGD = 4.00", "invoice 134.00 credit 130.00 fx -4.00"},
	} {
		e, x, err := PostCustomerCreditApplication(customerApplication(t, "100.00", usd, usdRate(t, "1.34"), usdRate(t, c.credit)), s)
		if err != nil || legs(s, e) != c.want || customerApplied(x) != c.applied {
			t.Errorf("%s: %s; %s; %v", c.name, legs(s, e), customerApplied(x), err)
		}
	}
	last := customerApplication(t, "0.01", usd, usdRate(t, "1.5"), usdRate(t, "1.5"))
	left := amt(t, "0.01", sgd)
	last.InvoiceClears = &left
	if e, x, err := PostCustomerCreditApplication(last, s); err != nil || customerApplied(x) != "invoice 0.01 credit 0.02 fx 0.01" ||
		legs(s, e) != "1100 -0.01 USD = -0.02; 1100 0.00 USD = 0.01; 1100 0.01 USD = 0.02; 7030 -0.01 SGD = -0.01" {
		t.Errorf("the last of an invoice: %s; %s; %v", legs(s, e), customerApplied(x), err)
	}
}

// Every refusal of the rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-AR-04
func TestPostCustomerCreditApplicationRefuses(t *testing.T) {
	s := selling()
	jpyRate, _ := money.ParseRate("0.009", jpy, sgd, "test")
	far := amt(t, "3.00", sgd)
	zero := money.Zero(sgd)
	in := func(a CustomerCreditApplication, f func(*CustomerCreditApplication)) CustomerCreditApplication {
		f(&a)
		return a
	}
	for name, c := range map[string]struct {
		a    CustomerCreditApplication
		s    Settings
		want []string
	}{
		"nothing": {CustomerCreditApplication{ID: customerApplicationID, Version: 1, Amount: money.Zero(sgd), InvoiceRate: money.Identity(sgd),
			CreditRate: money.Identity(sgd)}, s, []string{CodeNoDate, CodePaymentAmount}},
		"a credit of another currency": {customerApplication(t, "1.00", usd, usdRate(t, "1.34"), &jpyRate), s, []string{CodeWrongRate}},
		"a residual above S$1.00": {in(customerApplication(t, "0.02", usd, usdRate(t, "1.5"), usdRate(t, "1.5")), func(a *CustomerCreditApplication) { a.InvoiceClears = &far }), s,
			[]string{CodePaymentAmount}},
		"a carrying amount of zero": {in(customerApplication(t, "0.02", usd, usdRate(t, "1.5"), usdRate(t, "1.5")), func(a *CustomerCreditApplication) { a.CreditClears = &zero }), s,
			[]string{CodePaymentAmount}},
		"a residual in SGD": {in(customerApplication(t, "1.00", sgd, nil, nil), func(a *CustomerCreditApplication) { x := amt(t, "1.01", sgd); a.InvoiceClears = &x }), s,
			[]string{CodePaymentAmount}},
		"a long memo": {in(customerApplication(t, "1.00", sgd, nil, nil), func(a *CustomerCreditApplication) { a.Memo = strings.Repeat("m", MaxMemo+1) }), s,
			[]string{CodeTooLong}},
		"no accounts receivable": {customerApplication(t, "1.00", sgd, nil, nil), without(s, receivables), []string{CodeSystemRole}},
	} {
		_, _, err := PostCustomerCreditApplication(c.a, c.s)
		probs, _ := err.(Problems)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) {
			t.Errorf("%s: got %v, want %v (%v)", name, got, sortedCopy(c.want), err)
		}
		if name == "a credit of another currency" && (len(probs) != 1 || probs[0].Field != "credit") {
			t.Errorf("the field of the credit's rate: %v", err)
		}
	}
	if _, _, err := PostCustomerCreditApplication(customerApplication(t, "1.00", usd, usdRate(t, "1.34"), usdRate(t, "1.30")), without(s, fxRounding)); !slices.Equal(codes(err), []string{CodeSystemRole}) {
		t.Errorf("a realised difference with no 7030: %v", err)
	}
	// USD 0.02 at 1.5 carries S$0.03: a carrying amount left of S$1.03 leaves a residual of S$1.00, the most there is.
	most := amt(t, "1.03", sgd)
	if _, x, err := PostCustomerCreditApplication(in(customerApplication(t, "0.02", usd, usdRate(t, "1.5"), usdRate(t, "1.5")), func(a *CustomerCreditApplication) { a.CreditClears = &most }), s); err != nil ||
		x.Credit.String() != "1.03" {
		t.Errorf("a residual of S$1.00: %+v %v", x, err)
	}
}

// Any application the rule accepts balances in the base currency (INV-01), leaves 1100's balance in the credit's
// currency where it was, and books on 7030 exactly the realised FX: the credit's carrying amount less the invoice's,
// a gain credited.
// req: QA-ENG-03
// req: INV-01
// req: ENG-41
func TestPostCustomerCreditApplicationProperties(t *testing.T) {
	s := selling()
	rates := []string{"1.3472", "1.5", "1.34", "0.7", "1.2999999999"}
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		a := CustomerCreditApplication{ID: customerApplicationID, Version: 1, Date: day, Amount: money.FromMinor(rapid.Int64Range(1, 900_000_000).Draw(t, "amount"), cur),
			InvoiceRate: money.Identity(sgd), CreditRate: money.Identity(sgd)}
		if cur == usd {
			a.InvoiceRate, _ = money.ParseRate(rapid.SampledFrom(rates).Draw(t, "invoice"), usd, sgd, "test")
			a.CreditRate, _ = money.ParseRate(rapid.SampledFrom(rates).Draw(t, "credit"), usd, sgd, "test")
		}
		e, x, err := PostCustomerCreditApplication(a, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, inCur, fx := money.Zero(sgd), money.Zero(cur), money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			if l.AccountID == receivables.ID && l.Amount.Currency() == cur {
				inCur, _ = inCur.Add(l.Amount)
			}
			if l.AccountID == fxRounding.ID {
				fx = l.Base
			}
		}
		invoiceBase, _ := rounding.FX(a.Amount, a.InvoiceRate)
		creditBase, _ := rounding.FX(a.Amount, a.CreditRate)
		gain, _ := creditBase.Sub(invoiceBase)
		if !sum.IsZero() || !inCur.IsZero() || !fx.Equal(gain.Neg()) || !x.RealisedFX.Equal(gain) || (gain.IsZero() != (len(e.Lines) == 0)) {
			t.Fatalf("%s: off by %s, %s %s on 1100, 7030 %s, gain %s", legs(s, e), sum, inCur, cur, fx, gain)
		}
	})
}

// Each customer credit application rule version reproduces exactly what it produced over the same applications
// (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryCustomerCreditApplicationRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	last := customerApplication(t, "0.01", usd, usdRate(t, "1.5"), usdRate(t, "1.5"))
	left := amt(t, "0.01", sgd)
	last.InvoiceClears = &left
	cases := map[string][]struct {
		name string
		a    CustomerCreditApplication
	}{
		RuleCustomerCreditApplication: {
			{"SGD, an allocation alone", customerApplication(t, "100.00", sgd, nil, nil)},
			{"USD across rates, a gain", customerApplication(t, "100.00", usd, usdRate(t, "1.34"), usdRate(t, "1.36"))},
			{"the last of an invoice", last},
		},
	}
	for rule := range CustomerCreditApplicationRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryCustomerCreditApplicationRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, x, err := ReplayCustomerCreditApplication(rule, c.a, selling())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
			fmt.Fprintf(&b, "clears the invoice %s, the credit %s, realised %s\n", x.Invoice, x.Credit, x.RealisedFX)
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
	if _, _, err := ReplayCustomerCreditApplication("customer_credit_application@0", customerApplication(t, "1.00", sgd, nil, nil), selling()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
