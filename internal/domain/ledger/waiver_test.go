package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Worked by hand (FR-EQ-07, B.7; design D10): Mei Ling waives S$400.00 of her loan as a shareholder on 3 Nov 2026: Dr 2500
// 400.00 naming her (waived), Cr 3300 Capital contribution 400.00.
// req: FR-EQ-07
// req: ledger/posting-rules Waiver legs
func TestPostDirectorWaiver(t *testing.T) {
	s := claiming()
	loan := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002500"), Code: "2500", Active: true, Type: "liability", Role: RoleDirectorLoan}
	capital := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003300"), Code: "3300", Active: true, Type: "equity"}
	s.Accounts[loan.ID], s.Accounts[capital.ID] = loan, capital
	meiLing := uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0011")
	s.People = map[uuid.UUID]bool{meiLing: true}
	on := time.Date(2026, time.November, 3, 0, 0, 0, 0, time.UTC)
	w := DirectorWaiver{ID: claimID, Version: 2, PostOn: on, Memo: "DS-0001 Waived ahead of the seed round", Party: meiLing, Account: loan.ID, Credit: capital.ID,
		Amount: amt(t, "400.00", sgd)}
	e, err := PostDirectorWaiver(w, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "2500 400.00 SGD = 400.00; 3300 -400.00 SGD = -400.00"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if e.Lines[0].Party != meiLing || e.Lines[1].Party != uuid.Nil || !e.PostingDate.Equal(on) || e.RuleVersion != RuleDirectorWaiver {
		t.Errorf("the entry: %+v", e)
	}
	refused := func(name, code string, change func(*DirectorWaiver)) {
		t.Helper()
		x := w
		change(&x)
		if _, err := PostDirectorWaiver(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("crediting a director's account", CodeWaiver, func(x *DirectorWaiver) { x.Credit = loan.ID })
	refused("crediting GST control", CodeWaiver, func(x *DirectorWaiver) { x.Credit = gstControl.ID })
	refused("on an account that is not a director's", CodeWaiver, func(x *DirectorWaiver) { x.Account = capital.ID })
	refused("no amount", CodeWaiver, func(x *DirectorWaiver) { x.Amount = amt(t, "0.00", sgd) })
	refused("someone who is not a director", CodeParty, func(x *DirectorWaiver) { x.Party = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0099") })
}

// Worked by hand (FR-EQ-05, B.7; design D10): payment batch PB-0007 repays Mei Ling S$600.00 of her loan and S$250.00 of
// her deferred pay from DBS on 30 Nov 2026: Dr 2500 600.00 and Dr 2120 250.00, both naming her (paid out), Cr 1020 850.00.
// req: FR-EQ-05
// req: FR-BNK-12
// req: ledger/posting-rules Repaying a director
func TestPostDirectorRepayment(t *testing.T) {
	s := claiming()
	loan := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002500"), Code: "2500", Active: true, Type: "liability", Role: RoleDirectorLoan}
	deferred := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002120"), Code: "2120", Active: true, Type: "liability", Role: RoleDeferredRemuneration}
	s.Accounts[loan.ID], s.Accounts[deferred.ID], s.Accounts[dbs.ID], s.Accounts[wiseUSD.ID] = loan, deferred, dbs, wiseUSD
	meiLing := uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0011")
	s.People = map[uuid.UUID]bool{meiLing: true}
	on := time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)
	first, second := uuid.MustParse("0192f1a0-0000-7000-8000-0000000d5001"), uuid.MustParse("0192f1a0-0000-7000-8000-0000000d5002")
	r := DirectorRepayment{ID: claimID, Version: 1, Date: on, Memo: "PB-0007 DS-0001 DS-0002", Party: meiLing, Account: dbs.ID, Settlements: []RepaidSettlement{
		{SettlementID: first, Account: loan.ID, Amount: amt(t, "600.00", sgd)}, {SettlementID: second, Account: deferred.ID, Amount: amt(t, "250.00", sgd)}}}
	e, settled, err := PostDirectorRepayment(r, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "2500 600.00 SGD = 600.00; 2120 250.00 SGD = 250.00; 1020 -850.00 SGD = -850.00"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if e.Lines[0].Party != meiLing || e.Lines[1].Party != meiLing || e.Lines[2].Party != uuid.Nil || !e.PostingDate.Equal(on) ||
		e.PostingIdentity != DirectorRepaymentIdentity(claimID, 1, RuleDirectorRepayment) || e.Source.Type != SourceDirectorRepayment {
		t.Errorf("the entry: %+v", e)
	}
	if len(settled) != 2 || settled[1].DocumentID != second || settled[1].Amount.String() != "250.00" {
		t.Errorf("the settlements: %+v", settled)
	}
	if again, err := ReplayDirectorRepayment(RuleDirectorRepayment, r, s); err != nil || legs(s, again) != legs(s, e) {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	if _, err := ReplayDirectorRepayment("director_repayment@0", r, s); !hasCode(err, CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}
	refused := func(name, code string, change func(*DirectorRepayment)) {
		t.Helper()
		x := r
		x.Settlements = append([]RepaidSettlement(nil), r.Settlements...)
		change(&x)
		if _, _, err := PostDirectorRepayment(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("from a USD wallet", CodePaymentAmount, func(x *DirectorRepayment) { x.Account = wiseUSD.ID })
	refused("from an account that is not money", CodeNotMoneyAccount, func(x *DirectorRepayment) { x.Account = loan.ID })
	refused("a settlement on an account that is not a director's", CodeRepayment, func(x *DirectorRepayment) { x.Settlements[0].Account = meals.ID })
	refused("no amount", CodeRepayment, func(x *DirectorRepayment) { x.Settlements[1].Amount = amt(t, "0.00", sgd) })
	refused("nothing repaid", CodeTooFewLines, func(x *DirectorRepayment) { x.Settlements = nil })
	refused("no date", CodeNoDate, func(x *DirectorRepayment) { x.Date = time.Time{} })
	refused("someone who is not a director", CodeParty, func(x *DirectorRepayment) { x.Party = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0099") })
}
