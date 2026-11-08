package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Worked by hand (FR-EQ-05, FR-EQ-18): Mei Ling turns S$10,000.00 of her loan into the SAFE she holds on 3 Nov 2026:
// Dr 2500 10,000.00 naming her · Cr 2700 10,000.00 naming the SAFE. No cash moves.
// req: FR-EQ-05
// req: FR-EQ-18
func TestPostDirectorSAFE(t *testing.T) {
	s := claiming()
	loan := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002500"), Code: "2500", Active: true, Type: "liability", Role: RoleDirectorLoan}
	holding := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002700"), Code: "2700", Active: true, Type: "liability", Role: RoleConvertibleInstruments}
	s.Accounts[loan.ID], s.Accounts[holding.ID] = loan, holding
	meiLing := uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0011")
	s.People = map[uuid.UUID]bool{meiLing: true}
	safe := uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0011")
	s.Instruments = map[uuid.UUID]bool{safe: true}
	on := time.Date(2026, time.November, 3, 0, 0, 0, 0, time.UTC)
	w := DirectorSAFE{ID: claimID, Version: 1, PostOn: on, Memo: "DS-0002 into a SAFE or note", Party: meiLing, Account: loan.ID, Holding: holding.ID,
		Instrument: safe, Amount: amt(t, "10000.00", sgd)}
	e, err := PostDirectorSAFE(w, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "2500 10000.00 SGD = 10000.00; 2700 -10000.00 SGD = -10000.00"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if e.Lines[0].Party != meiLing || e.Lines[0].Instrument != uuid.Nil || e.Lines[1].Instrument != safe || e.Lines[1].Party != uuid.Nil ||
		!e.PostingDate.Equal(on) || e.RuleVersion != RuleDirectorSAFE || e.Source.Type != SourceDirectorSettlement ||
		e.PostingIdentity != DirectorWaiverIdentity(claimID, 1, RuleDirectorSAFE) {
		t.Errorf("the entry: %+v", e)
	}
	if again, err := ReplayDirectorSAFE(RuleDirectorSAFE, w, s); err != nil || legs(s, again) != legs(s, e) {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	if _, err := ReplayDirectorSAFE("director_safe@0", w, s); !hasCode(err, CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}
	refused := func(name, code string, change func(*DirectorSAFE)) {
		t.Helper()
		x := w
		change(&x)
		if _, err := PostDirectorSAFE(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("not a director's account", CodeWaiver, func(x *DirectorSAFE) { x.Account = holding.ID })
	refused("no person", CodeParty, func(x *DirectorSAFE) { x.Party = uuid.Nil })
	refused("no instruments account", CodeEquity, func(x *DirectorSAFE) { x.Holding = loan.ID })
	refused("an instrument not recorded", CodeInstrument, func(x *DirectorSAFE) { x.Instrument = uuid.New() })
	refused("nothing settled", CodeWaiver, func(x *DirectorSAFE) { x.Amount = amt(t, "0.00", sgd) })
	refused("no date", CodeNoDate, func(x *DirectorSAFE) { x.PostOn = time.Time{} })
}
