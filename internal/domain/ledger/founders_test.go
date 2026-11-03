package ledger

import (
	"testing"

	"github.com/google/uuid"
)

var (
	directorLoan = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002500"), Code: "2500", Active: true, Type: "liability", Role: RoleDirectorLoan}
	deferredPay  = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002120"), Code: "2120", Active: true, Type: "liability",
		Role: RoleDeferredRemuneration}
	meiLin   = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0001")
	customer = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c0002")
)

// founders is the chart with the director's accounts, and Mei Lin as its one director.
func founders() Settings {
	s := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}, People: map[uuid.UUID]bool{meiLin: true}}
	for id, a := range settings.Accounts {
		s.Accounts[id] = a
	}
	s.Accounts[directorLoan.ID], s.Accounts[deferredPay.ID] = directorLoan, deferredPay
	return s
}

func loan(t testing.TB, party uuid.UUID, origin string) Journal {
	return Journal{ID: docID, Version: 1, Date: day, Memo: "Loan from Mei Lin", Lines: []JournalLine{
		{AccountID: bank.ID, Amount: amt(t, "5000.00", sgd)},
		{AccountID: directorLoan.ID, Amount: amt(t, "-5000.00", sgd), Party: party, Origin: origin},
	}}
}

// Every line on a director's account names a director or shareholder and where its amount came from, and no other line
// names either (B.7, FR-EQ-01; LEDGER-41).
// req: FR-EQ-01
// req: ledger/posting-rules Founders' balances by person
func TestADirectorsAccountLineNamesItsPerson(t *testing.T) {
	s := founders()
	e, err := PostJournal(loan(t, meiLin, OriginLent), s)
	if err != nil {
		t.Fatal(err)
	}
	if e.Lines[0].Party != uuid.Nil || e.Lines[1].Party != meiLin {
		t.Errorf("the entry's people: %v, %v", e.Lines[0].Party, e.Lines[1].Party)
	}
	r, err := Reverse(e, day.AddDate(0, 0, 1), "posted twice")
	if err != nil || r.Lines[1].Party != meiLin {
		t.Errorf("the reversal keeps the person: %v, %v", r.Lines[1].Party, err)
	}
	stray := loan(t, meiLin, OriginLent)
	stray.Lines[0].Party, stray.Lines[0].Origin = meiLin, OriginLent
	for name, j := range map[string]Journal{
		"no person":         loan(t, uuid.Nil, OriginLent),
		"a customer":        loan(t, customer, OriginLent),
		"no origin":         loan(t, meiLin, ""),
		"an unknown origin": loan(t, meiLin, "gift"),
		"a person on bank":  stray,
	} {
		_, err := PostJournal(j, s)
		if p, ok := err.(Problems); !ok || !p.Has(CodeParty) {
			t.Errorf("%s: want %s, got %v", name, CodeParty, err)
		}
	}
	// Opening balances too: a founder's balance at the books start names them (question 15).
	o := loan(t, meiLin, OriginPaidPersonally)
	if _, err := PostOpening(o, s); err != nil {
		t.Errorf("an opening balance naming its director: %v", err)
	}
	if _, err := PostOpening(loan(t, uuid.Nil, OriginLent), s); err == nil {
		t.Error("an opening balance on 2500 with no person posted")
	}
}

// A bank line's part on a director's account belongs to the line's contact, who must be a director or shareholder;
// the line's other parts name no one (FR-EQ-01).
// req: FR-EQ-01
// req: FR-BNK-06
func TestABankLineOnADirectorsAccountIsItsContacts(t *testing.T) {
	s := banking()
	s.Accounts[directorLoan.ID] = directorLoan
	s.People = map[uuid.UUID]bool{meiLin: true}
	lent := bankTxn(t, current, "5000.00", sgd, nil, part(t, directorLoan, "4000.00", sgd, "", 0), part(t, consulting, "1000.00", sgd, "", 0))
	lent.Party = meiLin
	e, err := PostBankTransaction(lent, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range e.Lines {
		want := uuid.Nil
		if l.AccountID == directorLoan.ID {
			want = meiLin
		}
		if l.Party != want {
			t.Errorf("line on %s names %v, want %v", s.Accounts[l.AccountID].Code, l.Party, want)
		}
	}
	for name, party := range map[string]uuid.UUID{"no contact": uuid.Nil, "a customer": customer} {
		x := lent
		x.Party = party
		_, err := PostBankTransaction(x, s)
		if p, ok := err.(Problems); !ok || !p.Has(CodeParty) {
			t.Errorf("%s: want %s, got %v", name, CodeParty, err)
		}
	}
}

// A replay that loses or changes a line's person differs from the stored entry (ENG-06).
// req: ENG-06
func TestTheReplayComparesTheLinesPerson(t *testing.T) {
	e, err := PostJournal(loan(t, meiLin, OriginLent), founders())
	if err != nil {
		t.Fatal(err)
	}
	other := e
	other.Lines = append([]Line(nil), e.Lines...)
	other.Lines[1].Party = customer
	if len(Differences(e, other)) == 0 {
		t.Error("an entry naming another person replays the same")
	}
}
