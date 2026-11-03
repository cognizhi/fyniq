package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Worked by hand (FR-EQ-03, B.7; design D7): Mei Ling paid S$1,090.00, GST S$90.00 shown, for the lawyer's
// incorporation work before the company existed. The accountant decides it is capital and posts it on the books start,
// 1 Jan 2026: Dr 1600 1,090.00 (the GST stays in the cost while the company is not GST-registered), Cr 2500 1,090.00
// naming her. Registered on that date and claiming the GST: Dr 1600 1,000.00, Dr 2200 90.00, Cr 2500 1,090.00.
// req: FR-EQ-03
// req: ledger/posting-rules Legs when input tax is claimable
// req: ledger/posting-rules Legs when input tax is not claimable
func TestPostPreIncorporationCost(t *testing.T) {
	s := claiming()
	loan := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002500"), Code: "2500", Active: true, Type: "liability", Role: RoleDirectorLoan}
	capital := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001600"), Code: "1600", Active: true, Type: "asset"}
	s.Accounts[loan.ID], s.Accounts[capital.ID] = loan, capital
	meiLing := uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0011")
	s.People = map[uuid.UUID]bool{meiLing: true}
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cost := PreIncorporationCost{ID: claimID, Version: 1, PostOn: start, Memo: "PIC-0001 Lawyer: incorporation", Party: meiLing, Account: capital.ID,
		Gross: amt(t, "1090.00", sgd), GST: amt(t, "90.00", sgd)}
	for _, c := range []struct {
		name              string
		claim, registered bool
		want              string
	}{
		{"not registered", false, false, "1600 1090.00 SGD = 1090.00; 2500 -1090.00 SGD = -1090.00"},
		{"registered, not claimed", false, true, "1600 1090.00 SGD = 1090.00; 2500 -1090.00 SGD = -1090.00"},
		{"registered and claimed", true, true, "1600 1000.00 SGD = 1000.00; 2200 90.00 SGD = 90.00; 2500 -1090.00 SGD = -1090.00"},
	} {
		x := cost
		x.ClaimGST, x.Registered = c.claim, c.registered
		e, err := PostPreIncorporationCost(x, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
		last := e.Lines[len(e.Lines)-1]
		if !e.PostingDate.Equal(start) || e.RuleVersion != RulePreIncorporationCost || last.Party != meiLing {
			t.Errorf("%s: on %s under %s, the loan line's person %v", c.name, e.PostingDate, e.RuleVersion, last.Party)
		}
	}

	refused := func(name, code string, change func(*PreIncorporationCost)) {
		t.Helper()
		x := cost
		change(&x)
		if _, err := PostPreIncorporationCost(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("GST claimed while not registered", CodePreIncorporationCost, func(x *PreIncorporationCost) { x.ClaimGST = true })
	refused("GST claimed when none shown", CodePreIncorporationCost, func(x *PreIncorporationCost) {
		x.ClaimGST, x.Registered, x.GST = true, true, amt(t, "0.00", sgd)
	})
	refused("GST as large as the amount", CodePreIncorporationCost, func(x *PreIncorporationCost) { x.GST = amt(t, "1090.00", sgd) })
	refused("an amount of nothing", CodePreIncorporationCost, func(x *PreIncorporationCost) { x.Gross = amt(t, "0.00", sgd) })
	refused("an amount in another currency", CodePreIncorporationCost, func(x *PreIncorporationCost) { x.Gross = amt(t, "1090.00", idr) })
	refused("posted to the director's account", CodePreIncorporationCost, func(x *PreIncorporationCost) { x.Account = loan.ID })
	refused("posted to GST control", CodePreIncorporationCost, func(x *PreIncorporationCost) { x.Account = gstControl.ID })
	refused("a person who is not a director", CodeParty, func(x *PreIncorporationCost) {
		x.Party = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0099")
	})
	refused("no person", CodeParty, func(x *PreIncorporationCost) { x.Party = uuid.Nil })
	refused("no posting date", CodeNoDate, func(x *PreIncorporationCost) { x.PostOn = time.Time{} })
	delete(s.Accounts, loan.ID)
	refused("no director's loan account", CodeSystemRole, func(*PreIncorporationCost) {})
}
