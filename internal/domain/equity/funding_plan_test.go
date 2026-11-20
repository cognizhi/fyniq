package equity

import (
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

func planDay(y int, m time.Month) time.Time { return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC) }

func planMoney(t *testing.T, s string) money.Money {
	t.Helper()
	sgd, _ := money.Lookup("SGD")
	m, err := money.ParseField(s, sgd, "amount")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func planTable(a, b uuid.UUID) CapTable {
	dec := func(s string) apd.Decimal { d, _, _ := apd.NewFromString(s); return *d }
	return CapTable{Issued: dec("1000000"), FullyDiluted: dec("1000000"), Holders: []CapHolder{
		{Holder: a, Issued: dec("700000"), FullyDiluted: dec("700000")}, {Holder: b, Issued: dec("300000"), FullyDiluted: dec("300000")}}}
}

// A funding plan chains its rounds through the round model (FR-PLN-07), worked by hand over a company of 1,000,000 shares, 700,000 of them the founder's. A round of
// S$1,000,000 at a pre-money valuation of S$4,000,000 prices a share at 4.0000 and buys 250,000: 1,250,000 shares, the founder 56.00% (14.00 points lost), the
// new investors 20.00%. A second round of S$2,000,000 at S$10,000,000 over those 1,250,000 shares prices at 8.0000 and buys 250,000 more: 1,500,000 shares, the
// founder 46.67%, the new investors 500,000 of them, 33.33%. A round with no valuation is listed, says so, and leaves the table as it was; a round is
// worked in the order of its days whatever the order of the rows.
// req: FR-PLN-07
func TestAPlanChainsItsRoundsThroughTheRoundModel(t *testing.T) {
	founder, other := uuid.New(), uuid.New()
	pre1, pre2 := planMoney(t, "4000000.00"), planMoney(t, "10000000.00")
	got, err := PlanFunding(planTable(founder, other), []PlannedRound{
		{Row: 2, On: planDay(2028, 3), Amount: planMoney(t, "2000000.00"), PreMoney: &pre2},
		{Row: 0, On: planDay(2027, 6), Amount: planMoney(t, "1000000.00"), PreMoney: &pre1},
		{Row: 1, On: planDay(2027, 9), Amount: planMoney(t, "500000.00")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Round.Row != 0 || got[1].Round.Row != 1 || got[2].Round.Row != 2 {
		t.Fatalf("the rounds are worked in the order of their days: %+v", got)
	}
	holder := func(o RoundOutcome, id uuid.UUID) RoundHolder {
		for _, h := range o.Holders {
			if h.HolderID == id {
				return h
			}
		}
		t.Fatalf("no holder %s", id)
		return RoundHolder{}
	}
	r1, r2 := got[0].Outcome, got[2].Outcome
	if !got[0].Modelled || r1.Price.String() != "4.00" || r1.NewShares.String() != "250000" || got[0].Raised.String() != "1000000.00" {
		t.Errorf("round 1: modelled %v price %s new shares %s raised %s", got[0].Modelled, r1.Price, r1.NewShares.String(), got[0].Raised)
	}
	if f := holder(r1, founder); f.DilutedShareBefore != "70.00" || f.DilutedShareAfter != "56.00" || f.Drop != "14.00" {
		t.Errorf("the founder after round 1: %+v", f)
	}
	if n := holder(r1, NewInvestors); n.DilutedShareAfter != "20.00" {
		t.Errorf("the new investors after round 1: %+v", n)
	}
	if got[1].Modelled || got[1].Why == "" {
		t.Errorf("a round with no valuation: %+v", got[1])
	}
	if !got[2].Modelled || r2.Price.String() != "8.00" || r2.NewShares.String() != "250000" || r2.DilutedAfter.String() != "1500000" {
		t.Errorf("round 2: price %s new shares %s after %s", r2.Price, r2.NewShares.String(), r2.DilutedAfter.String())
	}
	if f := holder(r2, founder); f.DilutedShareBefore != "56.00" || f.DilutedShareAfter != "46.67" {
		t.Errorf("the founder after round 2: %+v", f)
	}
	if n := holder(r2, NewInvestors); n.DilutedBefore.String() != "250000" || n.DilutedAfter.String() != "500000" || n.DilutedShareAfter != "33.33" {
		t.Errorf("the new investors after round 2: %+v", n)
	}
}

// A planned round the model cannot price is shown with why, not an error: an amount that buys no whole share of the price it is planned at.
// req: FR-PLN-07
func TestAPlannedRoundTheModelRefusesIsListedWithWhy(t *testing.T) {
	pre := planMoney(t, "4000000.00")
	got, err := PlanFunding(planTable(uuid.New(), uuid.New()), []PlannedRound{{On: planDay(2027, 6), Amount: planMoney(t, "3.00"), PreMoney: &pre}})
	if err != nil || len(got) != 1 || got[0].Modelled || got[0].Why == "" {
		t.Fatalf("a round buying no whole share: %+v, %v", got, err)
	}
}
