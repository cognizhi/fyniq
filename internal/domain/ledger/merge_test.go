package ledger

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// Worked by hand (Appendix B.8, rev 0.174): 6510 Local travel is merged into 6520 Overseas travel on 30 Nov 2026. It
// holds EUR 10.00 recorded at −S$0.49, whose signs imply no rate, so the euros move at 1 and the S$10.49 that leaves in
// SGD; S$300.00; USD 300,000,000.00 recorded at S$400,000,000.01, which implies 1.3333333334 (to 10 places), at which
// the dollars are worth S$400,000,000.02, so S$0.01 moves back in SGD; USD 100.00 tagged Engineering, recorded at
// S$135.01, which implies 1.3501 and moves exactly; and USD 0.00 tagged Project P1 still recorded at S$0.50, which moves
// in SGD alone. 6510 then holds nothing in any currency or set, and 6520 gains exactly what it held.
// req: FR-COA-04
// req: ledger/posting-rules Account merge
func TestPostAccountMerge(t *testing.T) {
	s := claiming()
	eur := money.MustLookup("EUR")
	eng, p1 := uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0e01"), uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0f01")
	on := time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)
	merge := AccountMerge{ID: claimID, Version: 1, Date: on, Memo: "Merge 6510 into 6520", From: localTravel.ID, Into: overseas.ID,
		Holdings: []MergeHolding{
			{Amount: amt(t, "0.00", usd), Base: amt(t, "0.50", sgd), Values: []uuid.UUID{p1}},
			{Amount: amt(t, "100.00", usd), Base: amt(t, "135.01", sgd), Values: []uuid.UUID{eng}},
			{Amount: amt(t, "300000000.00", usd), Base: amt(t, "400000000.01", sgd)},
			{Amount: amt(t, "300.00", sgd), Base: amt(t, "300.00", sgd)},
			{Amount: amt(t, "10.00", eur), Base: amt(t, "-0.49", sgd)},
		}}
	e, err := PostAccountMerge(merge, s)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"6510 -10.00 EUR = -10.00", "6520 10.00 EUR = 10.00", "6510 10.49 SGD = 10.49", "6520 -10.49 SGD = -10.49",
		"6510 -300.00 SGD = -300.00", "6520 300.00 SGD = 300.00",
		"6510 -300000000.00 USD = -400000000.02", "6520 300000000.00 USD = 400000000.02", "6510 0.01 SGD = 0.01", "6520 -0.01 SGD = -0.01",
		"6510 -100.00 USD = -135.01", "6520 100.00 USD = 135.01",
		"6510 -0.50 SGD = -0.50", "6520 0.50 SGD = 0.50",
	}, "; ")
	if got := legs(s, e); got != want {
		t.Errorf("the lines:\n got %s\nwant %s", got, want)
	}
	if !e.PostingDate.Equal(on) || e.RuleVersion != RuleAccountMerge || e.Source.Type != SourceAccountMerge ||
		e.PostingIdentity != AccountMergeIdentity(claimID, 1, RuleAccountMerge) {
		t.Errorf("on %s under %s from %v as %s", e.PostingDate, e.RuleVersion, e.Source, e.PostingIdentity)
	}
	for i, l := range e.Lines {
		var values []uuid.UUID
		switch i {
		case 10, 11:
			values = []uuid.UUID{eng}
		case 12, 13:
			values = []uuid.UUID{p1}
		}
		if !slices.Equal(l.Values, values) {
			t.Errorf("line %d's values %v, want %v", i+1, l.Values, values)
		}
		if rate := map[int]string{0: "1", 1: "1", 6: "1.3333333334", 7: "1.3333333334", 10: "1.3501", 11: "1.3501"}[i]; rate != "" &&
			(l.Rate.String() != rate || l.Rate.Source != MergeRateSource) {
			t.Errorf("line %d at %s from %q, want %s", i+1, l.Rate, l.Rate.Source, rate)
		}
	}
	// What 6510 gives up, in base: 300.00 − 0.49 + 400,000,000.01 + 135.01 + 0.50.
	given := amt(t, "0.00", sgd)
	for _, l := range e.Lines {
		if l.AccountID == localTravel.ID {
			given, _ = given.Add(l.Base)
		}
	}
	if given.String() != "-400000435.03" {
		t.Errorf("6510 gives up %s in base", given)
	}
	if again, err := ReplayAccountMerge(RuleAccountMerge, merge, s); err != nil || len(Differences(e, again)) > 0 {
		t.Errorf("the replay: %v %v", err, Differences(e, again))
	}

	// An account holding nothing posts no line.
	if e, err := PostAccountMerge(AccountMerge{ID: claimID, Version: 1, Date: on, From: localTravel.ID, Into: overseas.ID,
		Holdings: []MergeHolding{{Amount: amt(t, "0.00", sgd), Base: amt(t, "0.00", sgd)}}}, s); err != nil || len(e.Lines) != 0 {
		t.Errorf("nothing to move: %v %v", e.Lines, err)
	}

	bank := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001020"), Code: "1020", Active: true, Type: "asset", Money: true}
	s.Accounts[bank.ID] = bank
	refused := func(name, code string, change func(*AccountMerge)) {
		t.Helper()
		x := merge
		change(&x)
		if _, err := PostAccountMerge(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("into itself", CodeAccountMerge, func(x *AccountMerge) { x.Into = x.From })
	refused("into another type", CodeAccountMerge, func(x *AccountMerge) { x.Into = advances.ID })
	refused("a system account", CodeAccountMerge, func(x *AccountMerge) { x.From = gstControl.ID })
	refused("a money account", CodeAccountMerge, func(x *AccountMerge) { x.From = bank.ID })
	refused("a group", CodeAccountMerge, func(x *AccountMerge) { x.Into = group.ID })
	refused("an inactive target", CodeInactiveAccount, func(x *AccountMerge) { x.Into = closed.ID })
	refused("an unknown account", CodeUnknownAccount, func(x *AccountMerge) { x.Into = uuid.MustParse("0192f1a0-0000-7000-8000-000000009999") })
	refused("no date", CodeNoDate, func(x *AccountMerge) { x.Date = time.Time{} })
	refused("an SGD holding whose base differs", CodeAccountMerge, func(x *AccountMerge) {
		x.Holdings = []MergeHolding{{Amount: amt(t, "1.00", sgd), Base: amt(t, "2.00", sgd)}}
	})
}
