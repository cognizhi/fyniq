package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Worked by hand (G4-044, Appendix G.3): the post-money SAFE's S$125,000.00 waits on 2700 naming the instrument; at the
// priced round it converts into 52,631 shares at S$2.375, so the register posts Dr 2700 124,998.63 naming the instrument
// · Cr 3010 124,998.63 (52,631 × 2.375 = 124,998.625 under RND-SHARE). The S$1.37 it leaves stays on 2700 until it is
// settled under the terms.
// req: FR-EQ-18
// req: RND-SHARE
func TestPostInstrumentConversion(t *testing.T) {
	s := claiming()
	holding := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002700"), Code: "2700", Active: true, Type: "liability", Role: RoleConvertibleInstruments}
	class := Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003010"), Code: "3010", Active: true, Type: "equity", ShareCapital: true}
	s.Accounts[holding.ID], s.Accounts[class.ID] = holding, class
	safe := uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0044")
	s.Instruments = map[uuid.UUID]bool{safe: true}
	on := time.Date(2027, time.March, 12, 0, 0, 0, 0, time.UTC)
	c := InstrumentConversion{ID: claimID, PostOn: on, Memo: "EQ-0003 SAFE conversion", Instrument: safe, Holding: holding.ID, Class: class.ID,
		Amount: amt(t, "124998.63", sgd)}
	e, err := PostInstrumentConversion(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e), "2700 124998.63 SGD = 124998.63; 3010 -124998.63 SGD = -124998.63"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	if e.Lines[0].Instrument != safe || e.Lines[1].Instrument != uuid.Nil || e.Source.Type != SourceShareIssue || e.RuleVersion != RuleInstrumentConversion ||
		e.PostingIdentity != InstrumentConversionIdentity(claimID, RuleInstrumentConversion) || !e.PostingDate.Equal(on) {
		t.Errorf("the entry: %+v", e)
	}
	if again, err := ReplayInstrumentConversion(RuleInstrumentConversion, c, s); err != nil || legs(s, again) != legs(s, e) {
		t.Errorf("replay: %s %v", legs(s, again), err)
	}
	if _, err := ReplayInstrumentConversion("instrument_conversion@0", c, s); !hasCode(err, CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}
	refused := func(name, code string, change func(*InstrumentConversion)) {
		t.Helper()
		x := c
		change(&x)
		if _, err := PostInstrumentConversion(x, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("no instrument", CodeInstrument, func(x *InstrumentConversion) { x.Instrument = uuid.Nil })
	refused("an instrument the organisation has not recorded", CodeInstrument, func(x *InstrumentConversion) {
		x.Instrument = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0099")
	})
	refused("money held on an account that does not hold instruments", CodeEquity, func(x *InstrumentConversion) { x.Holding = class.ID })
	refused("a class that is not share capital", CodeEquity, func(x *InstrumentConversion) { x.Class = holding.ID })
	refused("nothing converted", CodeEquity, func(x *InstrumentConversion) { x.Amount = amt(t, "0.00", sgd) })
	refused("no date", CodeNoDate, func(x *InstrumentConversion) { x.PostOn = time.Time{} })
}
