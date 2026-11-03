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
