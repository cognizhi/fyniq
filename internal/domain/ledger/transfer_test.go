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
	savings    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001012"), Code: "1012", Active: true, Money: true}
	charges    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006460"), Code: "6460", Active: true, Type: "expense"}
	transferID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000cc110")
)

// transferring is the chart a transfer posts against: the money accounts, 6460 and 7030.
func transferring() Settings {
	s := banking()
	s.Accounts[savings.ID], s.Accounts[charges.ID] = savings, charges
	return s
}

func transfer(t testing.TB, from Account, paid string, pc money.Currency, fromRate *money.Rate, to Account, received string, rc money.Currency, toRate *money.Rate, fee string) BankTransfer {
	x := BankTransfer{ID: transferID, Date: day, Memo: "Wise: SGD to USD", From: from.ID, Paid: amt(t, paid, pc), FromRate: fromRate, To: to.ID,
		Received: amt(t, received, rc), ToRate: toRate, Charges: charges.ID}
	if fee != "" {
		f := amt(t, fee, pc)
		x.Fee = &f
	}
	return x
}

// Worked by hand (posting rule "Transfer between money accounts", rev 0.140; the owner's decision of 25 Oct 2026):
//   - S$5,000.00 from the current account to savings: Dr 1012 5,000.00; Cr 1011 5,000.00;
//   - S$1,005.00 out, S$1,000.00 in, a S$5.00 fee: Dr 1012 1,000.00; Cr 1011 1,005.00; Dr 6460 5.00;
//   - a Wise conversion, S$1,355.00 out of the current account for USD 1,000.00 into the USD wallet at the day's 1.3472,
//     with a S$5.00 fee: the USD leg is S$1,347.20; Dr 6460 5.00; the S$2.80 left is the conversion's cost, Dr 7030;
//   - USD 1,000.00 out of the wallet at 1.35 (S$1,350.00) for S$1,340.00: Dr 7030 S$10.00;
//   - S$1,300.00 for USD 1,000.00 at 1.3472 (S$1,347.20): a gain, Cr 7030 S$47.20.
//
// req: FR-BNK-09
// req: ledger/posting-rules Transfer between money accounts
// req: ENG-40
func TestPostBankTransfer(t *testing.T) {
	s := transferring()
	r := usdRate(t, "1.3472")
	for _, c := range []struct {
		name string
		x    BankTransfer
		want string
	}{
		{"one currency", transfer(t, current, "5000.00", sgd, nil, savings, "5000.00", sgd, nil, ""), "1012 5000.00 SGD = 5000.00; 1011 -5000.00 SGD = -5000.00"},
		{"one currency, a fee", transfer(t, current, "1005.00", sgd, nil, savings, "1000.00", sgd, nil, "5.00"),
			"1012 1000.00 SGD = 1000.00; 1011 -1005.00 SGD = -1005.00; 6460 5.00 SGD = 5.00"},
		{"Wise, SGD to USD", transfer(t, current, "1355.00", sgd, nil, wallet, "1000.00", usd, r, "5.00"),
			"1021 1000.00 USD = 1347.20; 1011 -1355.00 SGD = -1355.00; 6460 5.00 SGD = 5.00; 7030 2.80 SGD = 2.80"},
		{"USD to SGD", transfer(t, wallet, "1000.00", usd, usdRate(t, "1.35"), current, "1340.00", sgd, nil, ""),
			"1011 1340.00 SGD = 1340.00; 1021 -1000.00 USD = -1350.00; 7030 10.00 SGD = 10.00"},
		{"a gain", transfer(t, current, "1300.00", sgd, nil, wallet, "1000.00", usd, r, ""),
			"1021 1000.00 USD = 1347.20; 1011 -1300.00 SGD = -1300.00; 7030 -47.20 SGD = -47.20"},
	} {
		e, err := PostBankTransfer(c.x, s)
		if err != nil || legs(s, e) != c.want {
			t.Errorf("%s: %s %v", c.name, legs(s, e), err)
		}
		if err == nil && (e.Source != (Source{SourceBankTransfer, transferID.String(), 1}) || e.RuleVersion != RuleBankTransfer ||
			e.PostingIdentity != "bank_transfer:"+transferID.String()+":v1:bank_transfer@1" || !e.PostingDate.Equal(day)) {
			t.Errorf("%s: the entry's identity %+v", c.name, e)
		}
	}
}

// Every refusal of the transfer rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-BNK-09
// req: QA-RPT-02
func TestPostBankTransferRefuses(t *testing.T) {
	s := transferring()
	r := usdRate(t, "1.3472")
	in := func(x BankTransfer, f func(*BankTransfer)) BankTransfer { f(&x); return x }
	ok := transfer(t, current, "1355.00", sgd, nil, wallet, "1000.00", usd, r, "5.00")
	for name, c := range map[string]struct {
		x    BankTransfer
		s    Settings
		want []string
	}{
		"no date":              {in(ok, func(x *BankTransfer) { x.Date = time.Time{} }), s, []string{CodeNoDate}},
		"one account":          {transfer(t, current, "1.00", sgd, nil, current, "1.00", sgd, nil, ""), s, []string{CodeTransfer}},
		"not money":            {in(ok, func(x *BankTransfer) { x.From = software.ID }), s, []string{CodeNotMoneyAccount}},
		"no account":           {in(ok, func(x *BankTransfer) { x.To = uuid.New() }), s, []string{CodeUnknownAccount}},
		"a zero amount":        {transfer(t, current, "0.00", sgd, nil, savings, "0.00", sgd, nil, ""), s, []string{CodeTransfer, CodeTransfer}},
		"the wrong currency":   {transfer(t, current, "1.00", usd, r, savings, "1.00", sgd, nil, ""), s, []string{CodeTransfer}},
		"no rate":              {in(ok, func(x *BankTransfer) { x.ToRate = nil }), s, []string{CodeMissingRate}},
		"a rate on SGD":        {in(ok, func(x *BankTransfer) { x.FromRate = r }), s, []string{CodeRateOnBase}},
		"a fee in USD":         {in(ok, func(x *BankTransfer) { f := amt(t, "5.00", usd); x.Fee = &f }), s, []string{CodeTransfer}},
		"a negative fee":       {in(ok, func(x *BankTransfer) { f := amt(t, "-5.00", sgd); x.Fee = &f }), s, []string{CodeTransfer}},
		"a fee of it all":      {in(ok, func(x *BankTransfer) { f := amt(t, "1355.00", sgd); x.Fee = &f }), s, []string{CodeTransfer}},
		"one currency, uneven": {transfer(t, current, "1005.00", sgd, nil, savings, "1000.00", sgd, nil, ""), s, []string{CodeTransfer}},
		"no charges account":   {ok, without(s, charges), []string{CodeSystemRole}},
		"a long memo":          {in(ok, func(x *BankTransfer) { x.Memo = strings.Repeat("m", MaxMemo+1) }), s, []string{CodeTooLong}},
	} {
		_, err := PostBankTransfer(c.x, c.s)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) {
			t.Errorf("%s: got %v, want %v (%v)", name, got, sortedCopy(c.want), err)
		}
	}
	if _, err := PostBankTransfer(transfer(t, current, "1.00", sgd, nil, wallet, "1.00", usd, r, ""), without(s, fxRounding)); !slices.Equal(codes(err), []string{CodeSystemRole}) {
		t.Errorf("a difference with no 7030: %v", err)
	}
	if _, err := PostBankTransfer(transfer(t, current, "1.00", sgd, nil, savings, "1.00", sgd, nil, ""), without(s, charges)); err != nil {
		t.Errorf("no fee needs no charges account: %v", err)
	}
}

// Any transfer the rule accepts balances in the base currency (INV-01): the receiving leg is RND-FX of what arrived at
// its rate, the paying leg of what left at its own, and the fee and 7030 take exactly the rest.
// req: QA-ENG-03
// req: INV-01
// req: FR-BNK-09
func TestPostBankTransferProperties(t *testing.T) {
	s := transferring()
	rapid.Check(t, func(t *rapid.T) {
		paid := money.FromMinor(rapid.Int64Range(2, 90_000_000).Draw(t, "paid"), sgd)
		received := money.FromMinor(rapid.Int64Range(1, 90_000_000).Draw(t, "received"), usd)
		r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7"}).Draw(t, "rate"), usd, sgd, "test")
		fee := money.FromMinor(rapid.Int64Range(0, 1).Draw(t, "fee"), sgd)
		x := BankTransfer{ID: transferID, Date: day, From: current.ID, Paid: paid, To: wallet.ID, Received: received, ToRate: &r, Fee: &fee, Charges: charges.ID}
		e, err := PostBankTransfer(x, s)
		if err != nil {
			t.Fatal(err)
		}
		sum := money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
		}
		want, _ := rounding.FX(received, r)
		if !sum.IsZero() || !e.Lines[0].Base.Equal(want) || !e.Lines[1].Base.Equal(paid.Neg()) {
			t.Fatalf("%s: off by %s", legs(s, e), sum)
		}
	})
}

// Each transfer rule version reproduces exactly what it produced over the same transfers (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryBankTransferRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	r := usdRate(t, "1.3472")
	cases := map[string][]struct {
		name string
		x    BankTransfer
	}{
		RuleBankTransfer: {
			{"one currency", transfer(t, current, "5000.00", sgd, nil, savings, "5000.00", sgd, nil, "")},
			{"Wise, SGD to USD with a fee", transfer(t, current, "1355.00", sgd, nil, wallet, "1000.00", usd, r, "5.00")},
			{"USD to SGD", transfer(t, wallet, "1000.00", usd, usdRate(t, "1.35"), current, "1340.00", sgd, nil, "")},
		},
	}
	for rule := range BankTransferRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryBankTransferRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayBankTransfer(rule, c.x, transferring())
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
	if _, err := ReplayBankTransfer("bank_transfer@0", transfer(t, current, "1.00", sgd, nil, savings, "1.00", sgd, nil, ""), transferring()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
