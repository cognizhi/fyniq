package ledger

import (
	"errors"
	"fmt"
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
	claimsPayable = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002150"), Code: "2150", Active: true, Type: "liability",
		Role: RoleExpenseClaimsPayable}
	advances    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001250"), Code: "1250", Active: true, Type: "asset"}
	localTravel = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006510"), Code: "6510", Active: true, Type: "expense"}
	overseas    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006520"), Code: "6520", Active: true, Type: "expense"}
	meals       = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006530"), Code: "6530", Active: true, Type: "expense"}
	claimID     = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c1a10")
)

// claiming is the chart a claim posts against: the system accounts by role, the advances account and three expense
// accounts.
func claiming() Settings {
	s := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}}
	for _, a := range []Account{claimsPayable, gstControl, fxRounding, advances, localTravel, overseas, meals, group, closed} {
		s.Accounts[a.ID] = a
	}
	return s
}

func claimLine(t testing.TB, acc Account, amount, gst string, cur money.Currency, claimable bool) ClaimLine {
	return ClaimLine{AccountID: acc.ID, Amount: amt(t, amount, cur), GST: amt(t, gst, cur), Claimable: claimable, Description: "Line " + acc.Code}
}

func idrLine(t testing.TB, acc Account, amount string) ClaimLine {
	t.Helper()
	l := claimLine(t, acc, amount, "0", idr, false)
	r, err := money.ParseRate("0.0000850", idr, sgd, "daily")
	if err != nil {
		t.Fatal(err)
	}
	l.Rate = &r
	return l
}

func aClaim(lines ...ClaimLine) Claim {
	return Claim{ID: claimID, Version: 1, Date: day, Memo: "CLM-0001 Mei Ling Tan", Lines: lines, Advances: advances.ID}
}

// Worked by hand (UC-EXP-01, FR-AP-20): a S$27.30 taxi e-receipt shows no GST number, so no GST was charged: Dr 6510
// 27.30, Cr 2150 27.30. A S$109.00 meal on a simplified tax invoice claims its S$9.00 GST: Dr 6530 100.00, Dr 2200 9.00,
// Cr 2150 109.00; not claimable (the organisation not GST-registered), the GST stays in the cost: Dr 6530 109.00. A
// mileage line of S$11.04 posts with no GST. One claim of all three posts line by line with one 2150 line.
// req: FR-AP-07
// req: ledger/posting-rules Posting an expense claim
// req: purchases/expense-claims Out-of-pocket receipt claim
func TestPostExpenseClaimInTheBaseCurrency(t *testing.T) {
	s := claiming()
	mileage := claimLine(t, localTravel, "11.04", "0.00", sgd, false)
	mileage.Mileage = true
	for _, c := range []struct {
		name string
		c    Claim
		want string
	}{
		{"UC-EXP-01's taxi", aClaim(claimLine(t, localTravel, "27.30", "0.00", sgd, false)), "6510 27.30 SGD = 27.30; 2150 -27.30 SGD = -27.30"},
		{"a simplified tax invoice", aClaim(claimLine(t, meals, "109.00", "9.00", sgd, true)),
			"6530 100.00 SGD = 100.00; 2200 9.00 SGD = 9.00; 2150 -109.00 SGD = -109.00"},
		{"GST not claimable", aClaim(claimLine(t, meals, "109.00", "9.00", sgd, false)), "6530 109.00 SGD = 109.00; 2150 -109.00 SGD = -109.00"},
		{"mileage", aClaim(mileage), "6510 11.04 SGD = 11.04; 2150 -11.04 SGD = -11.04"},
		{"all three", aClaim(claimLine(t, localTravel, "27.30", "0.00", sgd, false), claimLine(t, meals, "109.00", "9.00", sgd, true), mileage),
			"6510 27.30 SGD = 27.30; 6530 100.00 SGD = 100.00; 6510 11.04 SGD = 11.04; 2200 9.00 SGD = 9.00; 2150 -147.34 SGD = -147.34"},
	} {
		e, err := PostExpenseClaim(c.c, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if e.RuleVersion != RuleExpenseClaim || e.Source.Type != SourceExpenseClaim || e.PostingIdentity != ExpenseClaimIdentity(claimID, 1, RuleExpenseClaim) ||
			!e.PostingDate.Equal(day) {
			t.Errorf("%s: %+v", c.name, e)
		}
		if err := checkBalanced(e); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// Worked by hand (FR-AP-17, ENG-40, ENG-42): IDR 1,500,000.00 at the cascade's 0.0000850 is S$127.50, what the company
// owes: Dr 6520 IDR 1,500,000.00 = 127.50, Cr 2150 127.50. When the claimant states their card took S$129.10, that is
// what is owed and the S$1.60 against the rate goes to 7030. A USD 109.00 receipt with USD 9.00 GST at 1.34 whose tax
// invoice states S$12.10 GST claims the stated GST: Dr 6520 USD 100.00 = 134.00, Dr 2200 12.10, Cr 2150 146.06 (USD
// 109.00 at 1.34), Cr 7030 0.04; with no SGD GST stated, the GST stays in the cost.
// req: FR-AP-07
// req: purchases/expense-claims FR-AP-17 Employee claims processing
// req: ledger/posting-rules Posting an expense claim
func TestPostExpenseClaimInAnotherCurrency(t *testing.T) {
	s := claiming()
	jakarta := idrLine(t, overseas, "1500000.00")
	paid := jakarta
	card := amt(t, "129.10", sgd)
	paid.Paid = &card
	usdLine := claimLine(t, overseas, "109.00", "9.00", usd, true)
	usdLine.Rate = usdRate(t, "1.34")
	stated := usdLine
	gst := amt(t, "12.10", sgd)
	stated.StatedBase = &gst
	for _, c := range []struct {
		name string
		c    Claim
		want string
	}{
		{"at the rate", aClaim(jakarta), "6520 1500000.00 IDR = 127.50; 2150 -127.50 SGD = -127.50"},
		{"what the card took", aClaim(paid), "6520 1500000.00 IDR = 127.50; 2150 -129.10 SGD = -129.10; 7030 1.60 SGD = 1.60"},
		{"SGD GST stated", aClaim(stated), "6520 100.00 USD = 134.00; 2200 12.10 SGD = 12.10; 2150 -146.06 SGD = -146.06; 7030 -0.04 SGD = -0.04"},
		{"no SGD GST stated", aClaim(usdLine), "6520 109.00 USD = 146.06; 2150 -146.06 SGD = -146.06"},
	} {
		e, err := PostExpenseClaim(c.c, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if err := checkBalanced(e); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// Worked by hand (UC-EXP-03): S$800 advanced for Jakarta; the IDR receipts convert at 0.0000850 to S$425.00, S$170.00
// and S$147.15, S$742.15 in all, which settles the advance: Cr 1250 742.15 and nothing to 2150; S$57.85 stays on 1250.
// Claims of S$900.00 against S$800.00 open settle the S$800.00 and owe the S$100.00 excess on 2150.
// req: purchases/expense-claims Cash advance settled by foreign-currency claims
// req: ledger/posting-rules Posting an expense claim
func TestPostExpenseClaimSettlingAnAdvance(t *testing.T) {
	s := claiming()
	c := aClaim(idrLine(t, overseas, "5000000.00"), idrLine(t, localTravel, "2000000.00"), idrLine(t, meals, "1731176.47"))
	all := amt(t, "742.15", sgd)
	c.Settles = &all
	e, err := PostExpenseClaim(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "6520 5000000.00 IDR = 425.00; 6510 2000000.00 IDR = 170.00; 6530 1731176.47 IDR = 147.15; 1250 -742.15 SGD = -742.15" {
		t.Errorf("UC-EXP-03: %s", got)
	}
	over := aClaim(claimLine(t, overseas, "900.00", "0.00", sgd, false))
	open := amt(t, "800.00", sgd)
	over.Settles = &open
	if e, err := PostExpenseClaim(over, s); err != nil || legs(s, e) != "6520 900.00 SGD = 900.00; 2150 -100.00 SGD = -100.00; 1250 -800.00 SGD = -800.00" {
		t.Errorf("claims above the advance: %s %v", legs(s, e), err)
	}
	zero := amt(t, "0.00", sgd)
	none := aClaim(claimLine(t, overseas, "900.00", "0.00", sgd, false))
	none.Settles, none.Advances = &zero, uuid.Nil
	if e, err := PostExpenseClaim(none, s); err != nil || legs(s, e) != "6520 900.00 SGD = 900.00; 2150 -900.00 SGD = -900.00" {
		t.Errorf("settling nothing: %s %v", legs(s, e), err)
	}
}

// Every problem comes back at once, each with its field and code (ENG-22, ENG-23).
// req: FR-AP-07
// req: ENG-22
func TestPostExpenseClaimRefusesWhatDoesNotFit(t *testing.T) {
	s := claiming()
	onBase := claimLine(t, meals, "10.00", "0.00", sgd, false)
	onBase.Rate = usdRate(t, "1.34")
	noRate := claimLine(t, overseas, "10.00", "0.00", usd, false)
	paidOnBase := claimLine(t, meals, "10.00", "0.00", sgd, false)
	card := amt(t, "10.00", sgd)
	paidOnBase.Paid = &card
	mileage := claimLine(t, localTravel, "11.04", "0.99", sgd, false)
	mileage.Mileage = true
	tooMuch := amt(t, "1000.00", sgd)
	c := Claim{ID: claimID, Version: 1, Lines: []ClaimLine{
		claimLine(t, meals, "-5.00", "0.00", sgd, false),
		claimLine(t, meals, "10.00", "11.00", sgd, true),
		claimLine(t, meals, "0.00", "0.00", sgd, false),
		onBase, noRate, paidOnBase, mileage,
		claimLine(t, claimsPayable, "10.00", "0.00", sgd, false),
		claimLine(t, group, "10.00", "0.00", sgd, false),
		claimLine(t, closed, "10.00", "0.00", sgd, false),
	}, Settles: &tooMuch, Advances: advances.ID}
	_, err := PostExpenseClaim(c, s)
	var probs Problems
	if !errors.As(err, &probs) {
		t.Fatalf("want problems, got %v", err)
	}
	want := map[string]string{
		"date": CodeNoDate, "lines[1].amount": CodeClaimLine, "lines[2].gst": CodeClaimLine, "lines[3].amount": CodeZeroLine,
		"lines[4].fx_rate": CodeRateOnBase, "lines[5].fx_rate": CodeMissingRate, "lines[6].paid": CodeClaimPaid, "lines[7].gst": CodeClaimLine,
		"lines[8].category_id": CodeRuleAccount, "lines[9].category_id": CodeGroupAccount, "lines[10].category_id": CodeInactiveAccount,
	}
	got := map[string]string{}
	for _, p := range probs {
		got[p.Field] = p.Code
		if p.Message == "" || p.Fix == "" {
			t.Errorf("%s: no message or fix", p.Field)
		}
	}
	for f, code := range want {
		if got[f] != code {
			t.Errorf("%s: %q, want %q (all: %v)", f, got[f], code, got)
		}
	}

	// What it settles is at most its total, and needs the advances account.
	over := aClaim(claimLine(t, meals, "10.00", "0.00", sgd, false))
	over.Settles = &tooMuch
	if _, err := PostExpenseClaim(over, s); !hasCode(err, CodeClaimPaid) {
		t.Errorf("settles more than the claim: %v", err)
	}
	some := amt(t, "5.00", sgd)
	noAccount := aClaim(claimLine(t, meals, "10.00", "0.00", sgd, false))
	noAccount.Settles, noAccount.Advances = &some, uuid.Nil
	if _, err := PostExpenseClaim(noAccount, s); !hasCode(err, CodeClaimPaid) {
		t.Errorf("no advances account: %v", err)
	}
	// The memo and a line's description are at most the database's lengths.
	long := aClaim(claimLine(t, meals, "10.00", "0.00", sgd, false))
	long.Memo, long.Lines[0].Description = strings.Repeat("m", MaxMemo), strings.Repeat("d", MaxDescription)
	if _, err := PostExpenseClaim(long, s); err != nil {
		t.Errorf("the longest memo and description: %v", err)
	}
	long.Memo += "m"
	if _, err := PostExpenseClaim(long, s); !hasCode(err, CodeTooLong) {
		t.Errorf("a memo too long: %v", err)
	}
	long.Memo, long.Lines[0].Description = "", long.Lines[0].Description+"d"
	if _, err := PostExpenseClaim(long, s); !hasCode(err, CodeTooLong) {
		t.Errorf("a description too long: %v", err)
	}
	// The chart needs 2150.
	bare := claiming()
	delete(bare.Accounts, claimsPayable.ID)
	if _, err := PostExpenseClaim(aClaim(claimLine(t, meals, "10.00", "0.00", sgd, false)), bare); !hasCode(err, CodeSystemRole) {
		t.Errorf("no 2150: %v", err)
	}
}

// A replay re-runs the rule version an entry names, and refuses one the engine does not have (ENG-06).
// req: ENG-06
func TestReplayExpenseClaim(t *testing.T) {
	s := claiming()
	c := aClaim(claimLine(t, meals, "109.00", "9.00", sgd, true))
	first, err := PostExpenseClaim(c, s)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ReplayExpenseClaim(RuleExpenseClaim, c, s)
	if err != nil || legs(s, again) != legs(s, first) || again.PostingIdentity != first.PostingIdentity {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	if _, err := ReplayExpenseClaim("expense_claim@0", c, s); !hasCode(err, CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}
}

// Property: any claim of SGD, USD and IDR lines, claimable or not, with or without what was paid, posts an entry that
// balances in the base currency, whose credits to 2150 and 1250 are what the company owes: each line's amount, at its
// rate, or what was paid.
// req: ledger/posting-rules Posting an expense claim
// req: INV-01
func TestPostExpenseClaimBalancesProperty(t *testing.T) {
	s := claiming()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(rt, "lines")
		c := aClaim()
		owed := money.Zero(sgd)
		for i := range n {
			cents := rapid.Int64Range(1, 10_000_000).Draw(rt, "cents")
			gstCents := rapid.Int64Range(0, cents).Draw(rt, "gst")
			cur := []money.Currency{sgd, usd, idr}[rapid.IntRange(0, 2).Draw(rt, "currency")]
			l := ClaimLine{AccountID: []uuid.UUID{localTravel.ID, overseas.ID, meals.ID}[i%3], Amount: money.FromMinor(cents, cur),
				GST: money.FromMinor(gstCents, cur), Claimable: rapid.Bool().Draw(rt, "claimable")}
			due := l.Amount
			if cur != sgd {
				text := "0.0000850"
				if cur == usd {
					text = "1.3421"
				}
				r, err := money.ParseRate(text, cur, sgd, "daily")
				if err != nil {
					rt.Fatal(err)
				}
				l.Rate = &r
				if due, err = rounding.FX(l.Amount, r); err != nil {
					rt.Fatal(err)
				}
				if rapid.Bool().Draw(rt, "stated") {
					g := money.FromMinor(rapid.Int64Range(0, 1_000_000).Draw(rt, "stated_gst"), sgd)
					l.StatedBase = &g
				}
				if rapid.Bool().Draw(rt, "paid") {
					p := money.FromMinor(rapid.Int64Range(1, 20_000_000).Draw(rt, "paid_cents"), sgd)
					l.Paid, due = &p, p
				}
			}
			owed, _ = owed.Add(due)
			c.Lines = append(c.Lines, l)
		}
		var settles money.Money
		if rapid.Bool().Draw(rt, "settles") {
			minor, _ := owed.Minor()
			settles = money.FromMinor(rapid.Int64Range(0, minor).Draw(rt, "settles_cents"), sgd)
			c.Settles = &settles
		}
		e, err := PostExpenseClaim(c, s)
		if err != nil {
			rt.Fatal(err)
		}
		if err := checkBalanced(e); err != nil {
			rt.Fatal(err)
		}
		credited := money.Zero(sgd)
		for _, l := range e.Lines {
			if l.AccountID == claimsPayable.ID || l.AccountID == advances.ID {
				credited, _ = credited.Add(l.Base.Neg())
			}
		}
		if !credited.Equal(owed) {
			rt.Fatalf("credited %s, owed %s", credited, owed)
		}
	})
}

// checkBalanced says whether an entry's lines net to zero in the base currency (INV-01).
func checkBalanced(e Entry) error {
	sum := money.Zero(sgd)
	for _, l := range e.Lines {
		var err error
		if sum, err = sum.Add(l.Base); err != nil {
			return err
		}
	}
	if !sum.IsZero() {
		return fmt.Errorf("the entry does not balance by %s", sum)
	}
	return nil
}

// hasCode says whether err is problems with one of code.
func hasCode(err error, code string) bool {
	return slices.Contains(codes(err), code)
}

var (
	dbs       = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001020"), Code: "1020", Active: true, Type: "asset", Money: true}
	wiseUSD   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001030"), Code: "1030", Active: true, Type: "asset", Money: true, Currency: "USD"}
	reimburse = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c1b10")
	claimTwo  = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c1a20")
)

// Worked by hand (FR-AP-07, the owner's decision of 26 Oct 2026): S$92.10 paid back from DBS settles two claims, S$27.30
// and S$64.80: Dr 2150 92.10, Cr 1020 92.10, with a settlement of each claim in SGD and no FX.
// req: FR-AP-07
// req: purchases/expense-claims FR-AP-07 Expense claims
func TestPostClaimReimbursement(t *testing.T) {
	s := claiming()
	s.Accounts[dbs.ID], s.Accounts[wiseUSD.ID] = dbs, wiseUSD
	r := ClaimReimbursement{ID: reimburse, Version: 1, Date: day, Memo: "Ah Kow FAST 1234", Account: dbs.ID, Claims: []PaidClaim{
		{ClaimID: claimID, Amount: amt(t, "27.30", sgd)}, {ClaimID: claimTwo, Amount: amt(t, "64.80", sgd)}}}
	e, settled, err := PostClaimReimbursement(r, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "2150 92.10 SGD = 92.10; 1020 -92.10 SGD = -92.10" {
		t.Errorf("the entry: %s", got)
	}
	if len(settled) != 2 || settled[1].DocumentID != claimTwo || settled[1].Base.String() != "64.80" || !settled[1].RealisedFX.IsZero() {
		t.Errorf("the settlements: %+v", settled)
	}
	if e.PostingIdentity != ClaimReimbursementIdentity(reimburse, 1, RuleClaimReimbursement) || e.Source.Type != SourceClaimReimbursement {
		t.Errorf("the identity: %+v", e)
	}
	if again, err := ReplayClaimReimbursement(RuleClaimReimbursement, r, s); err != nil || legs(s, again) != legs(s, e) {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	if _, err := ReplayClaimReimbursement("claim_reimbursement@0", r, s); !hasCode(err, CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}

	// Refused whole: from a USD wallet, from an account that is not a money account, a zero amount, no date.
	bad := r
	bad.Date, bad.Account = time.Time{}, wiseUSD.ID
	bad.Claims = []PaidClaim{{ClaimID: claimID, Amount: amt(t, "0.00", sgd)}}
	_, _, err = PostClaimReimbursement(bad, s)
	if got := codes(err); !slices.Equal(got, []string{CodeNoDate, CodePaymentAmount, CodePaymentAmount}) {
		t.Errorf("refused: %v", got)
	}
	if probs, _ := err.(Problems); len(probs) != 3 || probs[2].Field != "claims[1].amount" {
		t.Errorf("the zero amount's field: %+v", probs)
	}
	long := r
	long.Memo = strings.Repeat("m", MaxMemo)
	if _, _, err := PostClaimReimbursement(long, s); err != nil {
		t.Errorf("the longest memo: %v", err)
	}
	long.Memo += "m"
	if _, _, err := PostClaimReimbursement(long, s); !hasCode(err, CodeTooLong) {
		t.Errorf("a memo too long: %v", err)
	}
	bad = r
	bad.Account, bad.Claims = meals.ID, nil
	if _, _, err := PostClaimReimbursement(bad, s); !hasCode(err, CodeNotMoneyAccount) || !hasCode(err, CodeTooFewLines) {
		t.Errorf("a cost account and no claims: %v", err)
	}
	bare := claiming()
	bare.Accounts[dbs.ID] = dbs
	delete(bare.Accounts, claimsPayable.ID)
	if _, _, err := PostClaimReimbursement(r, bare); !hasCode(err, CodeSystemRole) {
		t.Errorf("no 2150: %v", err)
	}
}
