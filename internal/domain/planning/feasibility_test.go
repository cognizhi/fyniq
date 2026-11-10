package planning_test

import (
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/planning"
)

// study is a one-year idea worked by hand: 100.00 invested and depreciated over the year, one unit sold for 300.00, no
// costs and no tax, at 10%, and a payback of a year is acceptable; edit changes it.
func study(t *testing.T, edit func(*planning.Study)) planning.Study {
	s := planning.Study{Currency: sgd, Years: 1, Rate: dec(t, "0.1"), MaxPayback: dec(t, "1"), Investment: dec(t, "100"),
		Life: 1, Salvage: dec(t, "0"), WorkingCap: dec(t, "0"), Units: dec(t, "1"), UnitGrowth: dec(t, "0"), Price: dec(t, "300"),
		PriceGrowth: dec(t, "0"), VarCost: dec(t, "0"), VarCostGrowth: dec(t, "0"), Fixed: dec(t, "0"), FixedGrowth: dec(t, "0"),
		TaxRate: dec(t, "0")}
	if edit != nil {
		edit(&s)
	}
	return s
}

func text(x *apd.Decimal) string {
	if x == nil {
		return "none"
	}
	return x.Text('f')
}

func shown(t *testing.T, f *planning.Fraction, places int) string {
	t.Helper()
	if f == nil {
		return "none"
	}
	x, err := f.Round(places)
	if err != nil {
		t.Fatal(err)
	}
	return x.Text('f')
}

// The calculator worked by hand (FR-PLN-06, RND-PLAN; p1-planning design D8).
//   - One year: the year's cash flow is 300.00 (a profit of 200.00 after 100.00 of depreciation, added back), worth
//     272.73 at 10%, so the NPV is 172.73; the IRR is 200% exactly (−100 × 3 + 300 = 0); the payback is 100 ÷ 300,
//     0.3 years, and the discounted one 100 ÷ 272.73, 0.4; the PI (100 + 172.73) ÷ 100 = 2.73; with no fixed costs
//     break-even is 0 units, and the margin of safety 100%. At 80% of the volume the year brings 240.00, worth 218.18:
//     an NPV of 118.18. At 8% it is worth 277.78, at 40% 214.29. Feasible, a payback within a year; marginal when only
//     0.2 years are acceptable.
//   - Nothing sold: the year's cash flow is nothing (the loss of 100.00 is all depreciation), so the NPV is −100.00:
//     not feasible, with no IRR, no payback and no margin.
//   - Nothing entered: no figures to judge.
//   - Two years at 0% with 20% tax: 1,000.00 invested, 200.00 of salvage over 4 years, 10 units at 50.00 and 600.00 of
//     fixed costs a year. Each year loses 300.00 after 200.00 of depreciation, which saves 60.00 of tax: a cash flow of
//     −40.00. At the end the book value is 600.00, so selling for 200.00 loses 400.00, which saves 80.00: 280.00 more.
//     The flows −1,000.00, −40.00 and 240.00 make an NPV of −800.00.
//
// req: FR-PLN-06
// req: RND-PLAN
func TestTheFeasibilityCalculator(t *testing.T) {
	r, err := planning.Feasible(study(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	y := r.Years[1]
	if text(y.Revenue) != "300.00" || text(y.Depreciation) != "100.00" || text(y.EBIT) != "200.00" || text(y.CashFlow) != "300.00" ||
		text(y.PV) != "272.73" || text(r.NPV) != "172.73" || text(r.IRR) != "2.0000" || shown(t, r.Payback, 1) != "0.3" ||
		shown(t, r.DiscountedPayback, 1) != "0.4" || text(r.PI) != "2.73" || shown(t, r.BreakEvenUnits, 1) != "0.0" ||
		text(r.BreakEvenRevenue) != "0.00" || r.Verdict != planning.VerdictFeasible {
		t.Errorf("one year: %+v, NPV %s, IRR %s, PI %s, %s", y, text(r.NPV), text(r.IRR), text(r.PI), r.Verdict)
	}
	if p, err := r.Safety.Percent(); err != nil || p.Text('f') != "100.0" {
		t.Errorf("the margin of safety: %v %v", p, err)
	}
	if text(r.Grid[0][2]) != "118.18" || text(r.Grid[2][2]) != "172.73" || text(r.NPVAt[0]) != "177.78" || text(r.NPVAt[7]) != "114.29" {
		t.Errorf("the grid's 80%% volume %s and base %s; at 8%% %s, at 40%% %s", text(r.Grid[0][2]), text(r.Grid[2][2]), text(r.NPVAt[0]), text(r.NPVAt[7]))
	}

	m, err := planning.Feasible(study(t, func(s *planning.Study) { s.MaxPayback = dec(t, "0.2") }))
	if err != nil || m.Verdict != planning.VerdictMarginal {
		t.Errorf("a payback longer than acceptable: %s %v", m.Verdict, err)
	}

	n, err := planning.Feasible(study(t, func(s *planning.Study) { s.Units, s.Price = dec(t, "0"), dec(t, "0") }))
	if err != nil || text(n.Years[1].CashFlow) != "0.00" || text(n.NPV) != "-100.00" || n.IRR != nil || n.Payback != nil ||
		n.DiscountedPayback != nil || n.BreakEvenUnits != nil || n.BreakEvenRevenue != nil || n.Safety != nil || text(n.PI) != "0.00" ||
		n.Verdict != planning.VerdictNotFeasible {
		t.Errorf("nothing sold: NPV %s, IRR %s, %s, %v", text(n.NPV), text(n.IRR), n.Verdict, err)
	}

	e, err := planning.Feasible(study(t, func(s *planning.Study) { s.Investment, s.Units, s.Price = dec(t, "0"), dec(t, "0"), dec(t, "0") }))
	if err != nil || e.Verdict != planning.VerdictNoFigures || e.IRR != nil || e.PI != nil {
		t.Errorf("nothing entered: %s, IRR %s, PI %s, %v", e.Verdict, text(e.IRR), text(e.PI), err)
	}

	l, err := planning.Feasible(study(t, func(s *planning.Study) {
		s.Years, s.Rate, s.Investment, s.Life, s.Salvage, s.TaxRate = 2, dec(t, "0"), dec(t, "1000"), 4, dec(t, "200"), dec(t, "0.2")
		s.Units, s.Price, s.Fixed = dec(t, "10"), dec(t, "50"), dec(t, "600")
	}))
	if err != nil || text(l.Years[1].Tax) != "-60.00" || text(l.Years[1].CashFlow) != "-40.00" || text(l.Years[2].Salvage) != "280.00" ||
		text(l.Years[2].CashFlow) != "240.00" || text(l.NPV) != "-800.00" || l.Verdict != planning.VerdictNotFeasible {
		t.Errorf("a loss saves tax: %+v %+v, NPV %s, %v", l.Years[1], l.Years[2], text(l.NPV), err)
	}

	if _, err := planning.Feasible(study(t, func(s *planning.Study) { s.Years = 11 })); err == nil {
		t.Error("11 years is beyond the evaluation period's limit")
	}
}

// The IRR (design D8): the root nearest 10% on the grid, bisected exactly. −100, +230, −132 has roots at 10% and 20%:
// 10%. −100, +245, −147 has them at 5% and 40%: 5%. −100, 0, +150 has one at √1.5 − 1 = 0.22474…: 0.2247. Flows that
// never change sign have none; flows that are all zero have none either.
// req: FR-PLN-06
func TestTheIRR(t *testing.T) {
	flows := func(xs ...string) []*apd.Decimal {
		var out []*apd.Decimal
		for _, x := range xs {
			out = append(out, dec(t, x))
		}
		return out
	}
	for _, c := range []struct {
		flows []*apd.Decimal
		want  string
	}{
		{flows("-100", "230", "-132"), "0.1000"},
		{flows("-100", "245", "-147"), "0.0500"},
		{flows("-100", "0", "150"), "0.2247"},
		{flows("-100", "0"), "none"},
		{flows("100", "50"), "none"},
		{flows("0", "0", "0"), "none"},
	} {
		got, err := planning.IRR(c.flows)
		if err != nil || text(got) != c.want {
			t.Errorf("%v: %s, want %s (%v)", c.flows, text(got), c.want, err)
		}
	}
}
