package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Worked by hand (FR-EQ-04, §10.2; design D8): the shareholders vote Mei Ling a S$6,000.00 fee for 2026 at the AGM of
// 30 Nov 2026. It posts on that day: Dr 6050 Directors' fees 6,000.00, Cr 2120 Deferred remuneration payable 6,000.00
// naming her, owed to her until it is paid.
// req: FR-EQ-04
// req: equity/founders-money A director's fee posts to the person (rev 0.167)
func TestPostDirectorFee(t *testing.T) {
	s := claiming()
	owed := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002120"), Code: "2120", Active: true, Type: "liability", Role: RoleDeferredRemuneration}
	fees := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006050"), Code: "6050", Active: true, Type: "expense"}
	s.Accounts[owed.ID], s.Accounts[fees.ID] = owed, fees
	meiLing := uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0011")
	s.People = map[uuid.UUID]bool{meiLing: true}
	agm := time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)
	fee := DirectorFee{ID: claimID, Version: 1, EntitledOn: agm, Memo: "DF-0001 Directors' fee 2026", Party: meiLing, Account: fees.ID,
		Amount: amt(t, "6000.00", sgd)}
	e, err := PostDirectorFee(fee, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "6050 6000.00 SGD = 6000.00; 2120 -6000.00 SGD = -6000.00"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if !e.PostingDate.Equal(agm) || e.RuleVersion != RuleDirectorFee || e.Lines[1].Party != meiLing || e.Lines[0].Party != uuid.Nil {
		t.Errorf("on %s under %s, the persons %v", e.PostingDate, e.RuleVersion, e.Lines)
	}

	refused := func(name, code string, change func(*DirectorFee)) {
		t.Helper()
		x := fee
		change(&x)
		if _, err := PostDirectorFee(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("no amount", CodeDirectorFee, func(x *DirectorFee) { x.Amount = amt(t, "0.00", sgd) })
	refused("another currency", CodeDirectorFee, func(x *DirectorFee) { x.Amount = amt(t, "6000.00", idr) })
	refused("to the director's account", CodeDirectorFee, func(x *DirectorFee) { x.Account = owed.ID })
	refused("to GST control", CodeDirectorFee, func(x *DirectorFee) { x.Account = gstControl.ID })
	refused("someone who is not a director", CodeParty, func(x *DirectorFee) { x.Party = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0099") })
	refused("no day", CodeNoDate, func(x *DirectorFee) { x.EntitledOn = time.Time{} })
	delete(s.Accounts, owed.ID)
	refused("no 2120", CodeSystemRole, func(*DirectorFee) {})
}
