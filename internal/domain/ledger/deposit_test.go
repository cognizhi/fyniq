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

var (
	depositsAcc = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002410"), Code: "2410", Active: true, Type: "liability", Role: RoleCustomerDeposits}
	depositID   = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0110")
)

// depositing is the chart a customer deposit posts against: the sales chart, the money accounts and 2410.
func depositing() Settings {
	s := refunding()
	s.Accounts[depositsAcc.ID] = depositsAcc
	return s
}

func deposit(t testing.TB, into Account, amount string, cur money.Currency, rate *money.Rate, gst string) CustomerDeposit {
	return CustomerDeposit{ID: depositID, Version: 1, Date: day, Memo: "Acme Analytics DP-0001", Account: into.ID, Currency: cur, Rate: rate,
		Amount: amt(t, amount, cur), GSTRate: gst}
}

// Worked by hand (FR-AR-09, RND-GST-INCL, ENG-45):
//   - a security deposit of S$500.00 into the current account: Dr 1011 500.00, Cr 2410 500.00, no GST;
//   - a part payment of S$1,090.00 at 9%: Dr 1011 1,090.00, Cr 2410 1,000.00, Cr 2200 90.00 (1,090 × 0.09 ÷ 1.09);
//   - a part payment of USD 1,000.00 into the USD wallet at 1.35 (S$1,350.00) at 9%: GST 1,350 × 0.09 ÷ 1.09 =
//     111.4679…, so Cr 2200 S$111.47 and Cr 2410 S$1,238.53, in the base currency;
//   - a security deposit of USD 1,000.00 into the SGD current account, which received S$1,348.00: Dr 1011 1,348.00, Cr
//     2410 1,348.00.
//
// req: FR-AR-09
// req: ledger/posting-rules Customer refunds and deposits
// req: RND-GST-INCL
func TestPostCustomerDeposit(t *testing.T) {
	s := depositing()
	intoSGD := deposit(t, current, "1000.00", usd, nil, "")
	got := amt(t, "1348.00", sgd)
	intoSGD.Received = &got
	for _, c := range []struct {
		name string
		d    CustomerDeposit
		want string
	}{
		{"a security deposit", deposit(t, current, "500.00", sgd, nil, ""), "1011 500.00 SGD = 500.00; 2410 -500.00 SGD = -500.00"},
		{"a part payment at 9%", deposit(t, current, "1090.00", sgd, nil, "0.09"), "1011 1090.00 SGD = 1090.00; 2410 -1000.00 SGD = -1000.00; 2200 -90.00 SGD = -90.00"},
		{"a USD part payment", deposit(t, wallet, "1000.00", usd, usdRate(t, "1.35"), "0.09"),
			"1021 1000.00 USD = 1350.00; 2410 -1238.53 SGD = -1238.53; 2200 -111.47 SGD = -111.47"},
		{"USD into the SGD account", intoSGD, "1011 1348.00 SGD = 1348.00; 2410 -1348.00 SGD = -1348.00"},
	} {
		e, err := PostCustomerDeposit(c.d, s)
		if err != nil || legs(s, e) != c.want {
			t.Errorf("%s: %s %v", c.name, legs(s, e), err)
		}
		if err == nil && (e.RuleVersion != RuleCustomerDeposit || e.Source.Type != SourceCustomerDeposit || e.PostingIdentity != CustomerDepositIdentity(depositID, 1, RuleCustomerDeposit)) {
			t.Errorf("%s: the entry's identity %+v", c.name, e)
		}
	}
}

// Every refusal of the deposit rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-AR-09
// req: QA-RPT-02
func TestPostCustomerDepositRefuses(t *testing.T) {
	s := depositing()
	in := func(d CustomerDeposit, f func(*CustomerDeposit)) CustomerDeposit { f(&d); return d }
	got := amt(t, "1.00", sgd)
	for name, c := range map[string]struct {
		d    CustomerDeposit
		s    Settings
		want []string
	}{
		"nothing": {CustomerDeposit{ID: depositID, Version: 1, Currency: sgd, Account: software.ID, Amount: money.Zero(sgd)}, s,
			[]string{CodeNoDate, CodeNotMoneyAccount, CodePaymentAmount}},
		"no account":       {in(deposit(t, current, "1.00", sgd, nil, ""), func(d *CustomerDeposit) { d.Account = uuid.New() }), s, []string{CodeUnknownAccount}},
		"no rate":          {deposit(t, wallet, "1.00", usd, nil, ""), s, []string{CodeMissingRate}},
		"a rate on SGD":    {deposit(t, current, "1.00", sgd, usdRate(t, "1.35"), ""), s, []string{CodeRateOnBase}},
		"a rate into SGD":  {deposit(t, current, "1.00", usd, usdRate(t, "1.35"), ""), s, []string{CodeWrongRate, CodePaidAmount}},
		"SGD into USD":     {deposit(t, wallet, "1.00", sgd, nil, ""), s, []string{CodeLineCurrency}},
		"received on SGD":  {in(deposit(t, current, "1.00", sgd, nil, ""), func(d *CustomerDeposit) { d.Received = &got }), s, []string{CodePaidAmount}},
		"a long memo":      {in(deposit(t, current, "1.00", sgd, nil, ""), func(d *CustomerDeposit) { d.Memo = strings.Repeat("m", MaxMemo+1) }), s, []string{CodeTooLong}},
		"no 2410":          {deposit(t, current, "1.00", sgd, nil, ""), without(s, depositsAcc), []string{CodeSystemRole}},
		"GST with no 2200": {deposit(t, current, "1.00", sgd, nil, "0.09"), without(s, gstControl), []string{CodeSystemRole}},
	} {
		_, err := PostCustomerDeposit(c.d, c.s)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) {
			t.Errorf("%s: got %v, want %v (%v)", name, got, sortedCopy(c.want), err)
		}
	}
	probs, _ := func() (Problems, bool) {
		_, err := PostCustomerDeposit(CustomerDeposit{ID: depositID, Version: 1, Currency: sgd, Account: current.ID, Amount: money.Zero(sgd)}, s)
		p, ok := err.(Problems)
		return p, ok
	}()
	if !slices.ContainsFunc(probs, func(p Problem) bool {
		return p.Message == "the deposit has no date" && p.Fix == "Enter the date the money came into the account"
	}) {
		t.Errorf("the wording of a refusal: %v", probs)
	}
	if _, err := PostCustomerDeposit(deposit(t, current, "1.00", sgd, nil, "9"), s); !slices.Equal(codes(err), []string{CodeGSTRate}) {
		t.Errorf("a GST rate that is not a fraction: %v", err)
	}
}

// Any deposit the rule accepts balances in the base currency (INV-01), credits 2410 and 2200 together with exactly the
// base amount the money account took, and puts on 2200 RND-GST-INCL of it.
// req: QA-ENG-03
// req: INV-01
// req: FR-AR-09
func TestPostCustomerDepositProperties(t *testing.T) {
	s := depositing()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		d := CustomerDeposit{ID: depositID, Version: 1, Date: day, Account: current.ID, Currency: cur,
			Amount: money.FromMinor(rapid.Int64Range(1, 900_000_000).Draw(t, "amount"), cur), GSTRate: rapid.SampledFrom([]string{"", "0", "0.09"}).Draw(t, "gst")}
		if cur == usd {
			r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7"}).Draw(t, "rate"), usd, sgd, "test")
			d.Account, d.Rate = wallet.ID, &r
		}
		e, err := PostCustomerDeposit(d, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, held := money.Zero(sgd), money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			if l.AccountID == depositsAcc.ID || l.AccountID == gstControl.ID {
				held, _ = held.Add(l.Base.Neg())
			}
		}
		if !sum.IsZero() || !held.Equal(e.Lines[0].Base) {
			t.Fatalf("%s: off by %s; 2410 and 2200 hold %s of %s", legs(s, e), sum, held, e.Lines[0].Base)
		}
	})
}

// Each customer deposit rule version reproduces exactly what it produced over the same deposits (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryCustomerDepositRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	intoSGD := deposit(t, current, "1000.00", usd, nil, "")
	got := amt(t, "1348.00", sgd)
	intoSGD.Received = &got
	cases := map[string][]struct {
		name string
		d    CustomerDeposit
	}{
		RuleCustomerDeposit: {
			{"a security deposit in SGD", deposit(t, current, "500.00", sgd, nil, "")},
			{"a part payment at 9%", deposit(t, current, "1090.00", sgd, nil, "0.09")},
			{"a USD part payment into the wallet", deposit(t, wallet, "1000.00", usd, usdRate(t, "1.35"), "0.09")},
			{"a USD deposit into the SGD account", intoSGD},
		},
	}
	for rule := range CustomerDepositRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryCustomerDepositRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayCustomerDeposit(rule, c.d, depositing())
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
	if _, err := ReplayCustomerDeposit("customer_deposit@0", deposit(t, current, "1.00", sgd, nil, ""), depositing()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}

func deducting(t testing.TB, amount, base, tax string, cur money.Currency) InvoiceDeposit {
	return InvoiceDeposit{DepositID: depositID, Number: "DP-0001", Amount: amt(t, amount, cur), Base: amt(t, base, sgd), Tax: amt(t, tax, sgd)}
}

// Worked by hand (FR-AR-09, the owner's decision of 24 Oct 2026: the final invoice deducts the deposit; IRAS: GST on
// the balance). An invoice of S$2,000.00 at 9% (S$2,180.00):
//   - deducting a part payment of S$1,090.00 that bore S$90.00: Dr 1100 1,090.00, Dr 2410 1,000.00, Cr 4020 2,000.00,
//     Cr 2200 90.00 (180 less the 90 the deposit bore);
//   - deducting a security deposit of S$500.00: Dr 1100 1,680.00, Dr 2410 500.00, Cr 4020 2,000.00, Cr 2200 180.00;
//   - an invoice of S$1,090.00 a part payment of S$1,090.00 covers whole: Dr 2410 1,000.00, Cr 4020 1,000.00, no 1100
//     and no GST line;
//   - a USD invoice of USD 1,000.00 out of scope at 1.30 deducting a security deposit of USD 100.00 that came in at 1.40
//     (S$140.00): Dr 1100 USD 900.00 = S$1,170.00, Dr 2410 140.00, Cr 4020 USD 1,000.00 = S$1,300.00, Cr 7030 S$10.00 of
//     realised FX: the deposit came to more than it takes at the invoice's rate (1,300 − 1,170 = 130).
//
// req: FR-AR-09
// req: ledger/posting-rules Posting an invoice
func TestAnInvoiceDeductsDeposits(t *testing.T) {
	s := depositing()
	with := func(i Invoice, ds ...InvoiceDeposit) Invoice { i.Deposits = ds; return i }
	full := sgdInvoice(invoiceLine(t, consulting, "2000.00", sgd, "SR", "0.09"))
	usdInvoice := Invoice{ID: invoiceID, Version: 1, Date: day, Memo: "Globex INV-0141", Currency: usd, Rate: usdRate(t, "1.30"),
		Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", usd, "OS", "")}}
	for _, c := range []struct {
		name string
		i    Invoice
		want string
	}{
		{"a part payment", with(full, deducting(t, "1090.00", "1090.00", "90.00", sgd)),
			"1100 1090.00 SGD = 1090.00; 2410 1000.00 SGD = 1000.00; 4020 -2000.00 SGD = -2000.00; 2200 -90.00 SGD = -90.00"},
		{"a security deposit", with(full, deducting(t, "500.00", "500.00", "0.00", sgd)),
			"1100 1680.00 SGD = 1680.00; 2410 500.00 SGD = 500.00; 4020 -2000.00 SGD = -2000.00; 2200 -180.00 SGD = -180.00"},
		{"covered whole", with(sgdInvoice(invoiceLine(t, consulting, "1000.00", sgd, "SR", "0.09")), deducting(t, "1090.00", "1090.00", "90.00", sgd)),
			"2410 1000.00 SGD = 1000.00; 4020 -1000.00 SGD = -1000.00"},
		{"in USD, at another rate", with(usdInvoice, deducting(t, "100.00", "140.00", "0.00", usd)),
			"1100 900.00 USD = 1170.00; 2410 140.00 SGD = 140.00; 4020 -1000.00 USD = -1300.00; 7030 -10.00 SGD = -10.00"},
	} {
		e, err := PostInvoice(c.i, s)
		if err != nil || legs(s, e) != c.want {
			t.Errorf("%s:\n got %s %v\nwant %s", c.name, legs(s, e), err, c.want)
		}
	}
	for name, c := range map[string]struct {
		i    Invoice
		s    Settings
		want []string
	}{
		"more than the total":   {with(full, deducting(t, "2180.01", "2180.01", "0.00", sgd)), s, []string{CodeDeposit}},
		"in another currency":   {with(full, deducting(t, "10.00", "13.50", "0.00", usd)), s, []string{CodeDeposit}},
		"tax above its base":    {with(full, deducting(t, "10.00", "10.00", "10.01", sgd)), s, []string{CodeDeposit}},
		"no deposits account":   {with(full, deducting(t, "10.00", "10.00", "0.00", sgd)), without(s, depositsAcc), []string{CodeSystemRole}},
		"nothing of a deposit":  {with(full, deducting(t, "0.00", "0.00", "0.00", sgd)), s, []string{CodeDeposit}},
		"two deposits too many": {with(full, deducting(t, "1500.00", "1500.00", "0.00", sgd), deducting(t, "1000.00", "1000.00", "0.00", sgd)), s, []string{CodeDeposit}},
	} {
		if _, err := PostInvoice(c.i, c.s); !slices.Equal(codes(err), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := PostCreditNote(with(full, deducting(t, "10.00", "10.00", "0.00", sgd)), s); !slices.Contains(codes(err), CodeDeposit) {
		t.Errorf("a credit note deducting a deposit: %v", err)
	}
}

// Any invoice the rule accepts with deposits balances in the base currency (INV-01), debits 1100 with exactly its total
// less what the deposits take in its currency, and accounts for GST once: its output tax line and the deposits' tax
// parts add up to the GST it charges.
// req: QA-ENG-03
// req: INV-01
// req: FR-AR-09
func TestAnInvoiceWithDepositsBalances(t *testing.T) {
	s := depositing()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		i := Invoice{ID: invoiceID, Version: 1, Date: day, Currency: cur, Lines: []InvoiceLine{{AccountID: consulting.ID,
			Net: money.FromMinor(rapid.Int64Range(100, 9_999_999).Draw(t, "net"), cur), GSTCode: "SR", GSTRate: "0.09"}}}
		if cur == usd {
			r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7"}).Draw(t, "rate"), usd, sgd, "test")
			i.Rate = &r
		}
		totals, _ := TotalsOf(i)
		whole, _ := totals.Total.Minor()
		used := rapid.Int64Range(1, whole).Draw(t, "deducted")
		base := money.FromMinor(rapid.Int64Range(0, 2*whole).Draw(t, "base"), sgd)
		tax := money.FromMinor(rapid.Int64Range(0, 1).Draw(t, "taxed")*rapid.Int64Range(0, func() int64 { b, _ := base.Minor(); return b }()).Draw(t, "tax"), sgd)
		i.Deposits = []InvoiceDeposit{{DepositID: depositID, Number: "DP-0001", Amount: money.FromMinor(used, cur), Base: base, Tax: tax}}
		e, err := PostInvoice(i, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, onAR, onGST := money.Zero(sgd), money.Zero(cur), money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			switch l.AccountID {
			case receivables.ID:
				onAR, _ = onAR.Add(l.Amount)
			case gstControl.ID:
				onGST, _ = onGST.Add(l.Base.Neg())
			}
		}
		owed, _ := totals.Total.Sub(i.Deposits[0].Amount)
		charged, _ := onGST.Add(tax)
		plain := i
		plain.Deposits = nil
		p, _ := PostInvoice(plain, s)
		gst := money.Zero(sgd)
		for _, l := range p.Lines {
			if l.AccountID == gstControl.ID {
				gst = l.Base.Neg()
			}
		}
		if !sum.IsZero() || !onAR.Equal(owed) || !charged.Equal(gst) {
			t.Fatalf("%s: off by %s; 1100 %s for %s owed; GST %s and %s of tax against %s", legs(s, e), sum, onAR, owed, onGST, tax, gst)
		}
	})
}
