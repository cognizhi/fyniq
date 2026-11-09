package equity

import (
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// A round modelled by hand (FR-EQ-21, Appendix G.3): 1,000,000 founder shares; G.3's SAFE of S$125,000 with a
// S$2,500,000 pre-money cap and a 20% discount; a round at S$3.00 a share with a new investor's S$600,000. The SAFE
// converts at min(2.50, 2.40) = S$2.40 into 52,083 shares, the investor takes 200,000, of 1,252,083: the founder 79.87%,
// diluted by 20.13 points, the investor 15.97%, the SAFE 4.16%. Pre-money S$3,000,000.00, post-money S$3,756,249.00. A
// pre-money valuation of S$3,000,000 prices the same round; with a top-up of 200,000 before the money the price is
// S$2.50 and the investor takes 240,000.
// req: FR-EQ-21
// req: RND-SHARE
func TestARoundIsModelledByHand(t *testing.T) {
	sgd, err := money.Lookup("SGD")
	if err != nil {
		t.Fatal(err)
	}
	founder, investor, holder := uuid.MustParse("00000000-0000-7000-8000-00000000000f"), uuid.MustParse("00000000-0000-7000-8000-00000000001a"),
		uuid.MustParse("00000000-0000-7000-8000-00000000002b")
	account := uuid.MustParse("00000000-0000-7000-8000-000000003010")
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	shares := Move{ID: uuid.New(), Key: HoldingKey{Holder: founder, Account: account}, On: day("2025-01-02")}
	shares.Shares.SetInt64(1000000)
	one := func(HoldingKey) (apd.Decimal, bool, error) { return *apd.New(1, 0), false, nil }
	before, err := TableAt(day("2027-03-11"), []Move{shares}, one)
	if err != nil {
		t.Fatal(err)
	}
	amount := func(s string) money.Money {
		m, err := money.ParseField(s, sgd, "amount")
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	price := func(s string) *money.UnitPrice {
		p, err := money.ParseUnitPrice(s, sgd, "price")
		if err != nil {
			t.Fatal(err)
		}
		return &p
	}
	safe := Converting{ID: uuid.New(), Instrument: Instrument{Form: FormPreMoneySAFE, HolderID: holder, Amount: amount("125000.00"), ValuationCap: "2500000",
		Discount: "20", Trigger: "priced_round", Denominator: "pre_money_shares", PriceDecimals: 4}}
	m := RoundModel{On: day("2027-03-12"), Price: price("3.00"), Investors: []RoundInvestor{{HolderID: investor, Amount: amount("600000.00")}}}
	out, err := ModelRound(m, before, []Converting{safe}, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if out.Conversions[0].Price.String() != "2.40" || out.Conversions[0].Shares.Text('f') != "52083" || out.Conversions[0].Left.String() != "0.80" {
		t.Errorf("the SAFE's conversion: %+v", out.Conversions[0])
	}
	if out.Issues[0].Shares.Text('f') != "200000" || out.IssuedAfter.Text('f') != "1252083" || out.PreMoney.String() != "3000000.00" ||
		out.PostMoney.String() != "3756249.00" {
		t.Errorf("the round: %+v", out)
	}
	want := map[uuid.UUID][3]string{founder: {"100.00", "79.87", "20.13"}, investor: {"", "15.97", "-15.97"}, holder: {"", "4.16", "-4.16"}}
	for _, h := range out.Holders {
		before := h.OwnershipBefore
		if h.IssuedBefore.IsZero() {
			before = ""
		}
		if got := [3]string{before, h.OwnershipAfter, h.Drop}; got != want[h.HolderID] {
			t.Errorf("holder %v: %v, want %v", h.HolderID, got, want[h.HolderID])
		}
	}
	if out.Holders[0].HolderID != founder || out.Holders[0].ProRata.Text('f') != "252083" {
		t.Errorf("the founder's pro-rata entitlement, all of the new shares: %+v", out.Holders[0])
	}

	byValuation := m
	byValuation.Price, byValuation.PreMoney, byValuation.PriceDecimals = nil, func() *money.Money { v := amount("3000000.00"); return &v }(), 4
	if v, err := ModelRound(byValuation, before, nil, nil); err != nil || v.Price.String() != "3.00" || v.Issues[0].Shares.Text('f') != "200000" {
		t.Errorf("by a pre-money valuation of S$3,000,000: %+v %v", v, err)
	}
	topped := byValuation
	topped.TopUp = &TopUp{Before: true}
	topped.TopUp.Shares.SetInt64(200000)
	if v, err := ModelRound(topped, before, nil, nil); err != nil || v.Price.String() != "2.50" || v.Issues[0].Shares.Text('f') != "240000" ||
		v.UnallocatedAfter.Text('f') != "200000" || v.DilutedAfter.Text('f') != "1440000" {
		t.Errorf("with a top-up of 200,000 before the money: %+v %v", v, err)
	}
	if _, err := ModelRound(RoundModel{On: m.On}, before, nil, nil); err == nil {
		t.Error("a round with no price and no valuation was modelled")
	}
}
