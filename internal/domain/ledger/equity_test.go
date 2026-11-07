package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Worked by hand (G4-043, B.7): an investor's S$600,000.00 waited in share application monies; on 12 Mar 2027, the day
// ACRA's register shows the allotment, the register posts Dr 2040 600,000.00 naming the investor · Cr 3010 600,000.00.
// A line on 2040 names its subscriber, a shareholder or investor, and only that account takes one.
// req: FR-EQ-17
// req: INV-13
// req: ledger/posting-rules Built as share_issue_cash@1, never unpaid (rev 0.205)
func TestPostCashIssue(t *testing.T) {
	s := claiming()
	monies := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002040"), Code: "2040", Active: true, Type: "liability", Role: RoleShareApplicationMonies}
	class := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003010"), Code: "3010", Active: true, Type: "equity", ShareCapital: true}
	s.Accounts[monies.ID], s.Accounts[class.ID] = monies, class
	fund := uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0042")
	s.Subscribers = map[uuid.UUID]bool{fund: true}
	on := time.Date(2027, time.March, 12, 0, 0, 0, 0, time.UTC)
	c := CashIssue{ID: claimID, PostOn: on, Memo: "EQ-0001 issue cash", Party: fund, Monies: monies.ID, Class: class.ID, Amount: amt(t, "600000.00", sgd)}
	e, err := PostCashIssue(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "2040 600000.00 SGD = 600000.00; 3010 -600000.00 SGD = -600000.00"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if e.Lines[0].Party != fund || e.Lines[1].Party != uuid.Nil || e.Source.Type != SourceShareIssue || e.RuleVersion != RuleCashIssue ||
		e.PostingIdentity != CashIssueIdentity(claimID, RuleCashIssue) || !e.PostingDate.Equal(on) {
		t.Errorf("the entry: %+v", e)
	}
	if again, err := ReplayCashIssue(RuleCashIssue, c, s); err != nil || legs(s, again) != legs(s, e) {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	if _, err := ReplayCashIssue("share_issue_cash@0", c, s); !hasCode(err, CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}
	refused := func(name, code string, change func(*CashIssue)) {
		t.Helper()
		x := c
		change(&x)
		if _, err := PostCashIssue(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("no subscriber", CodeParty, func(x *CashIssue) { x.Party = uuid.Nil })
	refused("a subscriber who is not a shareholder or investor", CodeParty, func(x *CashIssue) { x.Party = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0099") })
	refused("monies on an account that does not hold them", CodeEquity, func(x *CashIssue) { x.Monies = class.ID })
	refused("a class that is not share capital", CodeEquity, func(x *CashIssue) { x.Class = monies.ID })
	refused("nothing issued", CodeEquity, func(x *CashIssue) { x.Amount = amt(t, "0.00", sgd) })
	refused("no date", CodeNoDate, func(x *CashIssue) { x.PostOn = time.Time{} })

	// Only share application monies take a subscriber.
	if p := partyProblem(s, class, fund, "party"); p == nil || p.Code != CodeParty {
		t.Errorf("a subscriber on share capital: %v", p)
	}
	if p := partyProblem(s, monies, fund, "party"); p != nil {
		t.Errorf("a subscriber on 2040: %v", p)
	}
}

// Worked by hand (FR-EQ-17): 100,000 ordinary shares convert into preference shares, their carrying amount S$240,000.00
// moving from 3010 to 3020 on the effective date; classes on one account move nothing.
// req: FR-EQ-17
// req: equity/cap-table-and-investors A conversion between accounts moves its carrying amount (rev 0.205)
func TestPostClassConversion(t *testing.T) {
	s := claiming()
	ordinary := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003010"), Code: "3010", Active: true, Type: "equity", ShareCapital: true}
	preference := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003020"), Code: "3020", Active: true, Type: "equity", ShareCapital: true}
	s.Accounts[ordinary.ID], s.Accounts[preference.ID] = ordinary, preference
	on := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
	c := ClassConversion{ID: claimID, Version: 2, PostOn: on, Memo: "EQ-0002 class conversion", From: ordinary.ID, To: preference.ID, Amount: amt(t, "240000.00", sgd)}
	e, err := PostClassConversion(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "3010 240000.00 SGD = 240000.00; 3020 -240000.00 SGD = -240000.00"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if e.Source != (Source{Type: SourceEquityTransaction, ID: claimID.String(), Version: 2}) || e.PostingIdentity != ClassConversionIdentity(claimID, 2, RuleClassConversion) {
		t.Errorf("the entry: %+v", e)
	}
	if again, err := ReplayClassConversion(RuleClassConversion, c, s); err != nil || legs(s, again) != legs(s, e) {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	for name, change := range map[string]func(*ClassConversion){
		"classes on one account": func(x *ClassConversion) { x.To = x.From },
		"nothing moved":          func(x *ClassConversion) { x.Amount = amt(t, "0.00", sgd) },
	} {
		x := c
		change(&x)
		if _, err := PostClassConversion(x, s); err == nil || !err.(Problems).Has(CodeEquity) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
