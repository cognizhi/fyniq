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
	suspenseAcc = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002999"), Code: "2999", Active: true, Type: "liability", Role: RoleSuspense}
	bankTxnID   = uuid.MustParse("0192f1a0-0000-7000-8000-0000000bb110")
)

// banking is the chart a bank line posts against: the sales and purchase charts, the two money accounts and 2999.
func banking() Settings {
	s := refunding()
	for _, a := range []Account{payables, prepaid, hosting, suspenseAcc} {
		s.Accounts[a.ID] = a
	}
	return s
}

func part(t testing.TB, acc Account, amount string, cur money.Currency, gst string, months int) BankPart {
	return BankPart{AccountID: acc.ID, Amount: amt(t, amount, cur), GSTRate: gst, Months: months, Description: "Part " + acc.Code}
}

func bankTxn(t testing.TB, into Account, amount string, cur money.Currency, rate *money.Rate, parts ...BankPart) BankTransaction {
	return BankTransaction{ID: bankTxnID, Version: 1, Date: day, Memo: "DBS Current: GIRO ACME ANALYTICS", Account: into.ID, Amount: amt(t, amount, cur),
		Rate: rate, Parts: parts}
}

// Worked by hand (Appendix B.1; RND-GST-INCL on the signed gross, RND-FX):
//   - money in, S$1,090.00 for consulting, registered, SR: Dr 1011 1,090.00; Cr 4020 1,000.00; Cr 2200 90.00
//     (1,090 × 0.09 ÷ 1.09);
//   - money in, S$500.00 before the organisation registered for GST: Dr 1011 500.00; Cr 4020 500.00, no GST;
//   - money out, S$109.00 of software, TX: GST = −109 × 0.09 ÷ 1.09 = −9.00, so Dr 6120 100.00; Dr 2200 9.00; Cr 1011
//     109.00;
//   - money out, S$1,308.00 of software spread over 12 months, TX: Dr 1210 1,200.00; Dr 2200 108.00; Cr 1011 1,308.00;
//   - money in, S$1,200.00 of subscriptions spread over 12 months, no GST: Dr 1011 1,200.00; Cr 2400 1,200.00;
//   - money out, USD 100.00 from the USD wallet at 1.35, split 33.33 hosting and 33.33 and 33.34 software, TX: the
//     line is S$135.00; the parts S$45.00 (−44.9955), S$45.00 and S$45.01 (−45.009), GST S$3.72 each
//     (45.00 × 0.09 ÷ 1.09 = 3.7156; 45.01 × 0.09 ÷ 1.09 = 3.7164), so Dr 5010 41.28; Dr 6120 41.28; Dr 6120 41.29;
//     Dr 2200 11.16; and the cent RND-FX leaves between the parts (S$135.01) and the line, Cr 7030 0.01;
//   - money out, S$250.00 to suspense: Dr 2999 250.00; Cr 1011 250.00.
//
// req: ledger/posting-rules Posting a bank transaction, money in
// req: ledger/posting-rules Posting a bank transaction, money out
// req: ledger/posting-rules Spreading a bank transaction over N months
// req: ledger/posting-rules Amounts of a bank, card or cash transaction
// req: FR-BNK-06
// req: RND-GST-INCL
func TestPostBankTransaction(t *testing.T) {
	s := banking()
	for _, c := range []struct {
		name string
		t    BankTransaction
		want string
	}{
		{"money in, SR", bankTxn(t, current, "1090.00", sgd, nil, part(t, consulting, "1090.00", sgd, "0.09", 0)),
			"1011 1090.00 SGD = 1090.00; 4020 -1000.00 SGD = -1000.00; 2200 -90.00 SGD = -90.00"},
		{"money in, not registered", bankTxn(t, current, "500.00", sgd, nil, part(t, consulting, "500.00", sgd, "", 0)),
			"1011 500.00 SGD = 500.00; 4020 -500.00 SGD = -500.00"},
		{"money out, TX", bankTxn(t, current, "-109.00", sgd, nil, part(t, software, "-109.00", sgd, "0.09", 0)),
			"1011 -109.00 SGD = -109.00; 6120 100.00 SGD = 100.00; 2200 9.00 SGD = 9.00"},
		{"money out, spread", bankTxn(t, current, "-1308.00", sgd, nil, part(t, software, "-1308.00", sgd, "0.09", 12)),
			"1011 -1308.00 SGD = -1308.00; 1210 1200.00 SGD = 1200.00; 2200 108.00 SGD = 108.00"},
		{"money in, spread", bankTxn(t, current, "1200.00", sgd, nil, part(t, subscribe, "1200.00", sgd, "", 12)),
			"1011 1200.00 SGD = 1200.00; 2400 -1200.00 SGD = -1200.00"},
		{"USD out, in three parts", bankTxn(t, wallet, "-100.00", usd, usdRate(t, "1.35"), part(t, hosting, "-33.33", usd, "0.09", 0),
			part(t, software, "-33.33", usd, "0.09", 0), part(t, software, "-33.34", usd, "0.09", 0)),
			"1021 -100.00 USD = -135.00; 5010 41.28 SGD = 41.28; 6120 41.28 SGD = 41.28; 6120 41.29 SGD = 41.29; 2200 11.16 SGD = 11.16; 7030 -0.01 SGD = -0.01"},
		{"to suspense", bankTxn(t, current, "-250.00", sgd, nil, part(t, suspenseAcc, "-250.00", sgd, "", 0)),
			"1011 -250.00 SGD = -250.00; 2999 250.00 SGD = 250.00"},
	} {
		e, err := PostBankTransaction(c.t, s)
		if err != nil || legs(s, e) != c.want {
			t.Errorf("%s: %s %v", c.name, legs(s, e), err)
		}
		if err == nil && (e.RuleVersion != RuleBankTransaction || e.Source != (Source{SourceBankTransaction, bankTxnID.String(), 1}) ||
			e.PostingIdentity != BankTransactionIdentity(bankTxnID, 1, RuleBankTransaction) || !e.PostingDate.Equal(day) || e.Memo != c.t.Memo) {
			t.Errorf("%s: the entry's identity %+v", c.name, e)
		}
	}
}

// Every refusal of the bank rule is reported together, with its field (ENG-22).
// req: ENG-22
// req: FR-BNK-06
// req: QA-RPT-02
func TestPostBankTransactionRefuses(t *testing.T) {
	s := banking()
	in := func(x BankTransaction, f func(*BankTransaction)) BankTransaction { f(&x); return x }
	shareCap := Account{ID: uuid.New(), Code: "3010", Active: true, Type: "equity", ShareCapital: true}
	usdLoan := Account{ID: uuid.New(), Code: "2510", Active: true, Type: "liability", Currency: "USD"}
	withMore := banking()
	withMore.Accounts[shareCap.ID], withMore.Accounts[usdLoan.ID] = shareCap, usdLoan
	sgdOut := func(acc Account, gst string, months int) BankTransaction {
		return bankTxn(t, current, "-10.00", sgd, nil, part(t, acc, "-10.00", sgd, gst, months))
	}
	for name, c := range map[string]struct {
		t    BankTransaction
		s    Settings
		want []string
	}{
		"nothing": {BankTransaction{ID: bankTxnID, Version: 1, Account: software.ID, Amount: money.Zero(sgd)}, s,
			[]string{CodeNoDate, CodeNotMoneyAccount, CodeZeroLine, CodeTooFewLines}},
		"no account":      {in(sgdOut(software, "", 0), func(x *BankTransaction) { x.Account = uuid.New() }), s, []string{CodeUnknownAccount}},
		"USD into SGD":    {bankTxn(t, current, "-10.00", usd, usdRate(t, "1.35"), part(t, software, "-10.00", usd, "", 0)), s, []string{CodeLineCurrency}},
		"no rate":         {bankTxn(t, wallet, "-10.00", usd, nil, part(t, software, "-10.00", usd, "", 0)), s, []string{CodeMissingRate}},
		"a rate on SGD":   {in(sgdOut(software, "", 0), func(x *BankTransaction) { x.Rate = usdRate(t, "1.35") }), s, []string{CodeRateOnBase}},
		"a long memo":     {in(sgdOut(software, "", 0), func(x *BankTransaction) { x.Memo = strings.Repeat("m", MaxMemo+1) }), s, []string{CodeTooLong}},
		"no part account": {sgdOut(Account{ID: uuid.New()}, "", 0), s, []string{CodeUnknownAccount}},
		"a group":         {sgdOut(group, "", 0), s, []string{CodeGroupAccount}},
		"inactive":        {sgdOut(closed, "", 0), s, []string{CodeInactiveAccount}},
		"share capital":   {sgdOut(shareCap, "", 0), withMore, []string{CodeShareCapital}},
		"a money account": {sgdOut(wallet, "", 0), s, []string{CodeBankPart}},
		"receivables":     {sgdOut(receivables, "", 0), s, []string{CodeBankPart}},
		"payables":        {sgdOut(payables, "", 0), s, []string{CodeBankPart}},
		"GST on 2200":     {sgdOut(gstControl, "0.09", 0), s, []string{CodeRuleAccount}},
		"a USD category":  {sgdOut(usdLoan, "", 0), withMore, []string{CodeLineCurrency}},
		"a long description": {in(sgdOut(software, "", 0), func(x *BankTransaction) { x.Parts[0].Description = strings.Repeat("d", MaxDescription+1) }), s,
			[]string{CodeTooLong}},
		"a part in USD": {bankTxn(t, current, "-10.00", sgd, nil, part(t, software, "-10.00", usd, "", 0)), s, []string{CodeLineCurrency, CodeBankPart}},
		"the other way": {bankTxn(t, current, "-10.00", sgd, nil, part(t, software, "-12.00", sgd, "", 0), part(t, hosting, "2.00", sgd, "", 0)), s,
			[]string{CodeBankPart}},
		"not adding up":           {bankTxn(t, current, "-10.00", sgd, nil, part(t, software, "-9.99", sgd, "", 0)), s, []string{CodeBankPart}},
		"a bad GST rate":          {sgdOut(software, "9", 0), s, []string{CodeGSTRate}},
		"GST, no 2200":            {sgdOut(software, "0.09", 0), without(s, gstControl), []string{CodeSystemRole}},
		"too long a spread":       {sgdOut(software, "", MaxSpreadMonths+1), s, []string{CodeSpread}},
		"a spread of a liability": {sgdOut(suspenseAcc, "", 12), s, []string{CodeSpread}},
		"revenue spread out":      {sgdOut(subscribe, "", 12), s, []string{CodeSpread}},
		"expense spread in":       {bankTxn(t, current, "10.00", sgd, nil, part(t, software, "10.00", sgd, "", 12)), s, []string{CodeSpread}},
		"a spread, and a part on 1210": {bankTxn(t, current, "-10.00", sgd, nil, part(t, software, "-5.00", sgd, "", 12), part(t, prepaid, "-5.00", sgd, "", 0)), s,
			[]string{CodeSpread}},
		"a spread, no 1210": {sgdOut(software, "", 12), without(s, prepaid), []string{CodeSystemRole}},
	} {
		_, err := PostBankTransaction(c.t, c.s)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) {
			t.Errorf("%s: got %v, want %v (%v)", name, got, sortedCopy(c.want), err)
		}
	}
	longest := sgdOut(software, "", 0) // the memo and the description at their limits
	longest.Memo, longest.Parts[0].Description = strings.Repeat("m", MaxMemo), strings.Repeat("d", MaxDescription)
	if _, err := PostBankTransaction(longest, s); err != nil {
		t.Errorf("the longest memo and description: %v", err)
	}
	if _, err := PostBankTransaction(in(sgdOut(software, "", 0), func(x *BankTransaction) { x.Parts = nil }), s); !slices.Equal(codes(err), []string{CodeTooFewLines}) {
		t.Errorf("a line of money with no parts: %v", err)
	}
	second := bankTxn(t, current, "-10.00", sgd, nil, part(t, software, "-12.00", sgd, "", 0), part(t, hosting, "2.00", sgd, "", 0))
	if _, err := PostBankTransaction(second, s); err == nil || !slices.ContainsFunc(err.(Problems), func(p Problem) bool { return p.Field == "parts[2].amount" }) {
		t.Errorf("the second part named: %v", err)
	}
	_, err := PostBankTransaction(BankTransaction{ID: bankTxnID, Version: 1, Account: current.ID, Amount: money.Zero(sgd)}, s)
	if p, _ := err.(Problems); !slices.ContainsFunc(p, func(p Problem) bool {
		return p.Field == "parts" && p.Message == "the line has no category" && p.Fix == "Choose the account the money is for"
	}) {
		t.Errorf("the wording of a refusal: %v", err)
	}
	if _, err := PostBankTransaction(bankTxn(t, current, "-10.00", sgd, nil, part(t, software, "-9.99", sgd, "", 0)), s); !strings.Contains(err.Error(),
		"the parts add up to -9.99 SGD; the line is -10.00 SGD") {
		t.Errorf("the parts' sum named: %v", err)
	}
}

// Any bank transaction the rule accepts balances in the base currency (INV-01); its money account takes RND-FX of the
// line, 2200 the sum of RND-GST-INCL over the parts' base amounts, and 7030 nothing for a line in the base currency.
// req: QA-ENG-03
// req: INV-01
// req: ledger/posting-rules Amounts of a bank, card or cash transaction
func TestPostBankTransactionProperties(t *testing.T) {
	s := banking()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		sign := rapid.SampledFrom([]int64{1, -1}).Draw(t, "sign")
		n := rapid.IntRange(1, 4).Draw(t, "parts")
		x := BankTransaction{ID: bankTxnID, Version: 1, Date: day, Account: current.ID}
		total := money.Zero(cur)
		cat := software
		if sign > 0 {
			cat = consulting
		}
		for i := range n {
			p := BankPart{AccountID: cat.ID, Amount: money.FromMinor(sign*rapid.Int64Range(1, 90_000_000).Draw(t, fmt.Sprintf("amount%d", i)), cur),
				GSTRate: rapid.SampledFrom([]string{"", "0", "0.09"}).Draw(t, fmt.Sprintf("gst%d", i)), Months: rapid.SampledFrom([]int{0, 0, 1, 7, 12}).Draw(t, fmt.Sprintf("months%d", i))}
			x.Parts = append(x.Parts, p)
			total, _ = total.Add(p.Amount)
		}
		x.Amount = total
		rate := money.Identity(sgd)
		if cur == usd {
			r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.5", "0.7"}).Draw(t, "rate"), usd, sgd, "test")
			x.Account, x.Rate, rate = wallet.ID, &r, r
		}
		e, err := PostBankTransaction(x, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, tax, want := money.Zero(sgd), money.Zero(sgd), money.Zero(sgd)
		for _, p := range x.Parts {
			if p.GSTRate != "" {
				b, _ := rounding.FX(p.Amount, rate)
				g, _ := rounding.GSTIncl(b, p.GSTRate)
				want, _ = want.Add(g)
			}
		}
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			switch l.AccountID {
			case gstControl.ID:
				tax, _ = tax.Add(l.Base.Neg())
			case fxRounding.ID:
				if cur == sgd {
					t.Fatalf("a 7030 line for a line in SGD: %s", legs(s, e))
				}
			}
		}
		gross, _ := rounding.FX(x.Amount, rate)
		if !sum.IsZero() || !e.Lines[0].Base.Equal(gross) || !tax.Equal(want) {
			t.Fatalf("%s: off by %s; money %s of %s; GST %s of %s", legs(s, e), sum, e.Lines[0].Base, gross, tax, want)
		}
	})
}

// Worked by hand (Appendix B.1, "Category (spread over n months)"): a bank line has no service period, so its spread
// releases from the posting month. S$1,308.00 of software spread over 12 months, TX, put S$1,200.00 on 1210: S$100.00
// a month from October 2026, the first on the line's date, 14 October, the others on the first of their months. S$1.00
// of subscriptions in spread over 6 months releases from 2400 under ALLOC-EVEN: 0.17, 0.16, 0.17, 0.17, 0.16, 0.17.
// req: ledger/posting-rules Spreading a bank transaction over N months
// req: FR-GL-03
// req: ALLOC-EVEN
func TestBankTransactionReleasesStartInThePostingMonth(t *testing.T) {
	s := banking()
	out := bankTxn(t, current, "-1308.00", sgd, nil, part(t, software, "-1308.00", sgd, "0.09", 12))
	e, err := PostBankTransaction(out, s)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := BankTransactionReleases(out, e, s)
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for i := range 12 {
		on := month(t, "2026-10").AddDate(0, i, 0)
		date := on
		if i == 0 {
			date = day
		}
		fmt.Fprintf(&want, "l1 %s %d/12 %s 6120 100.00\n", on.Format("2006-01"), i+1, date.Format(time.DateOnly))
	}
	if got := schedule(s, rs); got != want.String() {
		t.Errorf("the schedule:\n%s\nwant\n%s", got, want.String())
	}
	for _, r := range rs {
		if r.Source != SourceBankOutRelease || r.DocumentID != bankTxnID || r.Memo != out.Memo || r.Description != "Part 6120" {
			t.Fatalf("a release names its transaction: %+v", r)
		}
	}
	in := bankTxn(t, current, "1.00", sgd, nil, part(t, subscribe, "1.00", sgd, "", 6))
	e, err = PostBankTransaction(in, s)
	if err != nil {
		t.Fatal(err)
	}
	if rs, err = BankTransactionReleases(in, e, s); err != nil {
		t.Fatal(err)
	}
	want.Reset()
	for i, a := range []string{"0.17", "0.16", "0.17", "0.17", "0.16", "0.17"} {
		on := month(t, "2026-10").AddDate(0, i, 0)
		date := on
		if i == 0 {
			date = day
		}
		fmt.Fprintf(&want, "l1 %s %d/6 %s 4010 %s\n", on.Format("2006-01"), i+1, date.Format(time.DateOnly), a)
	}
	if got := schedule(s, rs); got != want.String() || rs[0].Source != SourceBankInRelease {
		t.Errorf("the schedule of money in:\n%s\nwant\n%s", got, want.String())
	}
	if rs, err := BankTransactionReleases(bankTxn(t, current, "-1.00", sgd, nil, part(t, software, "-1.00", sgd, "", 0)), e, s); err != nil || rs != nil {
		t.Errorf("a transaction without a spread: %v %v", rs, err)
	}
}

// A month's release of a bank transaction's spread moves its part between the holding account and the part's account:
// money out from 1210 to the expense (bank_out_release@1), money in from 2400 to the revenue (bank_in_release@1).
// req: ledger/posting-rules Spreading a bank transaction over N months
// req: ENG-05
func TestPostBankReleases(t *testing.T) {
	s := banking()
	out := Release{Source: SourceBankOutRelease, DocumentID: bankTxnID, Version: 2, Line: 1, Month: month(t, "2026-10"), Part: 1, Of: 12, Date: day,
		AccountID: software.ID, Description: "Jira, a year", Memo: "DBS Current: ATLASSIAN", Amount: amt(t, "100.00", sgd)}
	e, err := PostBankOutRelease(out, s)
	if err != nil || legs(s, e) != "6120 100.00 SGD = 100.00; 1210 -100.00 SGD = -100.00" ||
		e.PostingIdentity != "bank_out_release:"+bankTxnID.String()+":v2:l1:2026-10:bank_out_release@1" || e.RuleVersion != RuleBankOutRelease ||
		e.Memo != "DBS Current: ATLASSIAN: cost spread for October 2026 (1 of 12)" {
		t.Errorf("money out: %s %+v %v", legs(s, e), e, err)
	}
	in := out
	in.Source, in.AccountID, in.Memo = SourceBankInRelease, subscribe.ID, "DBS Current: ACME"
	e, err = PostBankInRelease(in, s)
	if err != nil || legs(s, e) != "2400 100.00 SGD = 100.00; 4010 -100.00 SGD = -100.00" ||
		e.PostingIdentity != "bank_in_release:"+bankTxnID.String()+":v2:l1:2026-10:bank_in_release@1" || e.RuleVersion != RuleBankInRelease ||
		e.Memo != "DBS Current: ACME: revenue spread for October 2026 (1 of 12)" {
		t.Errorf("money in: %s %+v %v", legs(s, e), e, err)
	}
	if _, err := PostBankOutRelease(in, s); !slices.Equal(codes(err), []string{CodeUnknownRule}) {
		t.Errorf("money in under the rule for money out: %v", err)
	}
	if _, err := PostBankInRelease(out, s); !slices.Equal(codes(err), []string{CodeUnknownRule}) {
		t.Errorf("money out under the rule for money in: %v", err)
	}
	for _, src := range []string{SourceBankOutRelease, SourceBankInRelease} {
		if !IsRelease(src) || len(ReleaseRules(src)) != 1 || ReleaseRules(src)[CurrentRelease(src)] == nil {
			t.Errorf("%s is not wired as a release", src)
		}
	}
	if IsRelease(SourceBankTransaction) {
		t.Error("a bank transaction is not a release")
	}
}

// Each bank transaction and bank release rule version reproduces exactly what it produced over the same input (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryBankRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	type replayCase struct {
		name string
		post func() (Entry, error)
	}
	s := banking()
	txn := func(x BankTransaction) func() (Entry, error) {
		return func() (Entry, error) { return ReplayBankTransaction(RuleBankTransaction, x, s) }
	}
	rel := func(rule string, r Release) func() (Entry, error) {
		return func() (Entry, error) { return ReplayRelease(rule, r, s) }
	}
	cases := map[string][]replayCase{
		RuleBankTransaction: {
			{"money in, SR", txn(bankTxn(t, current, "1090.00", sgd, nil, part(t, consulting, "1090.00", sgd, "0.09", 0)))},
			{"money out, TX", txn(bankTxn(t, current, "-109.00", sgd, nil, part(t, software, "-109.00", sgd, "0.09", 0)))},
			{"money out, spread", txn(bankTxn(t, current, "-1308.00", sgd, nil, part(t, software, "-1308.00", sgd, "0.09", 12)))},
			{"money in, spread", txn(bankTxn(t, current, "1200.00", sgd, nil, part(t, subscribe, "1200.00", sgd, "", 12)))},
			{"USD out, in three parts", txn(bankTxn(t, wallet, "-100.00", usd, usdRate(t, "1.35"), part(t, hosting, "-33.33", usd, "0.09", 0),
				part(t, software, "-33.33", usd, "0.09", 0), part(t, software, "-33.34", usd, "0.09", 0)))},
			{"to suspense", txn(bankTxn(t, current, "-250.00", sgd, nil, part(t, suspenseAcc, "-250.00", sgd, "", 0)))},
		},
		RuleBankOutRelease: {
			{"the first month, on the line's date", rel(RuleBankOutRelease, Release{Source: SourceBankOutRelease, DocumentID: bankTxnID, Version: 1, Line: 1,
				Month: month(t, "2026-10"), Part: 1, Of: 12, Date: day, AccountID: software.ID, Description: "Jira, a year", Memo: "DBS Current: ATLASSIAN",
				Amount: amt(t, "100.00", sgd)})},
		},
		RuleBankInRelease: {
			{"a cent-sized part, on the first of its month", rel(RuleBankInRelease, Release{Source: SourceBankInRelease, DocumentID: bankTxnID, Version: 3, Line: 2,
				Month: month(t, "2026-12"), Part: 3, Of: 6, Date: month(t, "2026-12"), AccountID: subscribe.ID, Memo: "DBS Current: ACME", Amount: amt(t, "0.16", sgd)})},
		},
	}
	for _, rules := range []map[string]ReleaseRule{BankOutReleaseRules, BankInReleaseRules} {
		for rule := range rules {
			if len(cases[rule]) == 0 {
				t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
			}
		}
	}
	for rule := range BankTransactionRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryBankRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := c.post()
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
	if _, err := ReplayBankTransaction("bank_transaction@0", bankTxn(t, current, "1.00", sgd, nil), s); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
