package planning

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The feasibility calculator (FR-PLN-06; p1-planning design D8): one idea's cash flows over its evaluation period, their
// NPV, IRR and paybacks, its break-even and margin of safety, the sensitivity grid and the NPV at other rates, and the
// verdict. The model is the workbook's (zh-sheet-accounting tests/ref_planning.py), worked in exact decimals under
// RND-PLAN: each line of a year is rounded once from its drivers and the lines it is a percentage of as shown; each
// year's present value is rounded once from its exact quotient; the totals, the running totals and the NPV add the
// rounded lines. A rule (the verdict, the IRR's root, break-even) compares exact values.

// The study's limits (design D8; the guard, FY302).
const (
	MinStudyYears = 1
	MaxStudyYears = 10
)

// The sensitivity grid's multipliers of sales volume (rows) and fixed costs (columns), and the rates of the NPV table.
var (
	Multipliers = []string{"0.8", "0.9", "1", "1.1", "1.2"}
	NPVRates    = []string{"0.08", "0.1", "0.12", "0.15", "0.2", "0.25", "0.3", "0.4"}
)

// The verdicts.
const (
	VerdictFeasible    = "feasible"     // ✔ a positive NPV and payback within the longest acceptable
	VerdictMarginal    = "marginal"     // ⚠ a positive NPV but payback longer, or none
	VerdictNotFeasible = "not_feasible" // ✖ an NPV at or below zero
	VerdictNoFigures   = "no_figures"   // nothing to judge yet: no investment, units or price
)

// Study is one idea's inputs, as the workbook's sheet. Rates and growths are fractions (0.18 for 18%).
type Study struct {
	Currency   money.Currency
	Years      int          // the evaluation period, 1 to 10
	Rate       *apd.Decimal // the discount rate
	MaxPayback *apd.Decimal // the longest acceptable payback, in years
	Investment *apd.Decimal // spent at the start, year 0
	Life       int          // years over which it is depreciated, straight line to its salvage value
	Salvage    *apd.Decimal // what it fetches at the end of the period
	WorkingCap *apd.Decimal // working capital kept, as a fraction of each year's revenue
	Units      *apd.Decimal // sold in the first year
	UnitGrowth *apd.Decimal
	Price      *apd.Decimal // per unit, the first year
	PriceGrowth,
	VarCost, VarCostGrowth, // per unit
	Fixed, FixedGrowth *apd.Decimal // a year
	TaxRate *apd.Decimal // sg-cit's headline rate at the study's date, fixed for its years
}

// StudyYear is one year of the study: year 0 holds the investment alone.
type StudyYear struct {
	Year                                                 int
	Units                                                *apd.Decimal // exact, shown to 1 decimal
	Revenue, Variable, Fixed, Depreciation, EBIT, Tax    *apd.Decimal
	NOPAT, WorkingCap, WorkingCapChange, Salvage, Relief *apd.Decimal // Salvage: after tax on its gain, the last year
	CashFlow, PV, Cumulative, CumulativePV               *apd.Decimal
}

// StudyResult is what the calculator gives.
type StudyResult struct {
	Years []StudyYear
	NPV   *apd.Decimal
	// IRR is the root of the NPV nearest 10% on the grid from −99% to +1,000% in steps of 1%, to 4 decimals of a
	// fraction; nil when there is none.
	IRR *apd.Decimal
	// Payback and DiscountedPayback are the years until the running total, or the discounted one, is no longer below
	// zero, interpolated; nil when it never is in the period.
	Payback, DiscountedPayback *Fraction
	PI                         *apd.Decimal // 1 + NPV ÷ investment, to 2 decimals; nil without an investment
	BreakEvenUnits             *Fraction    // fixed costs ÷ (price − variable cost), the first year's; nil when that margin is not above zero
	BreakEvenRevenue           *apd.Decimal
	Safety                     *Fraction // (units − break-even units) ÷ units, the first year's; nil without units or a margin
	Grid                       [5][5]*apd.Decimal
	NPVAt                      []*apd.Decimal // at NPVRates
	Verdict                    string
}

// flows is one run of the model: the years, at a discount rate, with the units and fixed costs scaled.
type flows struct {
	years []StudyYear
	npv   *apd.Decimal
	poly  []*apd.Decimal // the cash flows, year 0 first, for the IRR and the exact NPV's sign
}

func (c *calc) study(s Study, rate, unitsBy, fixedBy *apd.Decimal) flows {
	var out flows
	zero := c.round(apd.New(0, 0)) // at the currency's minor units, as every line is shown
	out.years = append(out.years, StudyYear{Year: 0, Units: zero, Revenue: zero, Variable: zero, Fixed: zero, Depreciation: zero, EBIT: zero,
		Tax: zero, NOPAT: zero, WorkingCap: zero, WorkingCapChange: zero, Salvage: zero, Relief: zero, CashFlow: c.neg(s.Investment)})
	dep := c.roundFrac(c.over(c.sub(s.Investment, s.Salvage), d(int64(s.Life))))
	depreciated, wcBefore := apd.New(0, 0), apd.New(0, 0)
	for t := 1; t <= s.Years; t++ {
		y := StudyYear{Year: t, Salvage: zero, Relief: zero}
		y.Units = c.mul(s.Units, unitsBy, c.pow(c.grow(s.UnitGrowth), t-1))
		y.Revenue = c.round(c.mul(y.Units, s.Price, c.pow(c.grow(s.PriceGrowth), t-1)))
		y.Variable = c.round(c.mul(y.Units, s.VarCost, c.pow(c.grow(s.VarCostGrowth), t-1)))
		y.Fixed = c.round(c.mul(s.Fixed, fixedBy, c.pow(c.grow(s.FixedGrowth), t-1)))
		y.Depreciation = zero
		if t <= s.Life {
			y.Depreciation = dep
		}
		depreciated = c.add(depreciated, y.Depreciation)
		y.EBIT = c.sub(c.sub(c.sub(y.Revenue, y.Variable), y.Fixed), y.Depreciation)
		y.Tax = c.round(c.mul(y.EBIT, s.TaxRate)) // a loss saves tax
		y.NOPAT = c.sub(y.EBIT, y.Tax)
		y.WorkingCap = c.round(c.mul(s.WorkingCap, y.Revenue))
		y.WorkingCapChange = c.sub(y.WorkingCap, wcBefore)
		wcBefore = y.WorkingCap
		y.CashFlow = c.sub(c.add(y.NOPAT, y.Depreciation), y.WorkingCapChange)
		if t == s.Years {
			// The end of the period: the salvage, after tax on its gain over the book value (a loss saves tax), and the
			// working capital released.
			book := c.sub(s.Investment, depreciated)
			y.Salvage = c.sub(s.Salvage, c.round(c.mul(c.sub(s.Salvage, book), s.TaxRate)))
			y.Relief = y.WorkingCap
			y.CashFlow = c.add(y.CashFlow, y.Salvage, y.Relief)
		}
		out.years = append(out.years, y)
	}
	out.npv = apd.New(0, 0)
	cum, cumPV := apd.New(0, 0), apd.New(0, 0)
	for t := range out.years {
		y := &out.years[t]
		y.PV = c.roundFrac(c.over(y.CashFlow, c.pow(c.grow(rate), t)))
		cum, cumPV = c.add(cum, y.CashFlow), c.add(cumPV, y.PV)
		y.Cumulative, y.CumulativePV = cum, cumPV
		out.npv = c.add(out.npv, y.PV)
		out.poly = append(out.poly, y.CashFlow)
	}
	return out
}

// polynomial is Σ cash flow × (1 + r)^(years − t): the NPV at r times (1 + r)^years, so of the NPV's sign for r > −1, and
// exact in decimals.
func (c *calc) polynomial(cash []*apd.Decimal, r *apd.Decimal) *apd.Decimal {
	out := apd.New(0, 0)
	n := len(cash) - 1
	for t, x := range cash {
		out = c.add(out, c.mul(x, c.pow(c.grow(r), n-t)))
	}
	return out
}

// irr is the root of the NPV nearest 10% (design D8): the grid from −99% to +1,000% in steps of 1% finds where the NPV
// changes sign, or is zero; the interval nearest 10% is bisected exactly until the ends round alike to 4 decimals. Cash
// flows that are all zero have none: every rate would do.
func (c *calc) irr(cash []*apd.Decimal) *apd.Decimal {
	nothing := true
	for _, x := range cash {
		nothing = nothing && x.IsZero()
	}
	if nothing {
		return nil
	}
	ten := apd.New(1, -1)
	at := func(k int) *apd.Decimal { return apd.New(int64(k-99), -2) } // −0.99 + k × 0.01
	distance := func(a, b *apd.Decimal) *apd.Decimal {
		if a.Cmp(ten) <= 0 && b.Cmp(ten) >= 0 {
			return apd.New(0, 0)
		}
		x, y := new(apd.Decimal).Abs(c.sub(a, ten)), new(apd.Decimal).Abs(c.sub(b, ten))
		return minOf(x, y)
	}
	var lo, hi, best *apd.Decimal
	prev := c.polynomial(cash, at(0))
	if prev.IsZero() {
		lo, hi, best = at(0), at(0), distance(at(0), at(0))
	}
	for k := 1; k <= 1099; k++ {
		r := at(k)
		p := c.polynomial(cash, r)
		var a, b *apd.Decimal
		switch {
		case p.IsZero():
			a, b = r, r
		case prev.Sign()*p.Sign() < 0:
			a, b = at(k-1), r
		}
		if a != nil {
			if dd := distance(a, b); best == nil || dd.Cmp(best) < 0 {
				lo, hi, best = a, b, dd
			}
		}
		prev = p
	}
	if lo == nil || c.err != nil {
		return nil
	}
	four := func(x *apd.Decimal) *apd.Decimal {
		q, err := rounding.PlanQuotient(x, d(1), 4) // RND-PLAN's IRR: 4 decimals of a fraction
		if err != nil {
			c.fail(err)
			return apd.New(0, 0)
		}
		return q
	}
	signLo := c.polynomial(cash, lo).Sign()
	for i := 0; i < 400 && four(lo).Cmp(four(hi)) != 0; i++ {
		mid := new(apd.Decimal)
		c.check(exact.Quo(mid, c.add(lo, hi), d(2)))
		p := c.polynomial(cash, mid)
		switch {
		case p.IsZero():
			lo, hi = mid, mid
		case p.Sign() == signLo:
			lo = mid
		default:
			hi = mid
		}
	}
	return four(lo)
}

// IRR is the IRR of cash flows, year 0 first, as the calculator finds it (design D8): to 4 decimals of a fraction, nil
// when there is none.
func IRR(cash []*apd.Decimal) (*apd.Decimal, error) {
	c := &calc{}
	r := c.irr(cash)
	return r, c.err
}

// payback is the years until a running total is no longer below zero, interpolated within the year it turns: year k − 1
// + what was still below zero ÷ year k's flow; nil when it stays below zero.
func (c *calc) payback(running, flow []*apd.Decimal) *Fraction {
	for k := range running {
		if running[k].Sign() < 0 {
			continue
		}
		if k == 0 {
			f := whole(apd.New(0, 0))
			return &f
		}
		f := c.over(c.sub(c.mul(d(int64(k-1)), flow[k]), running[k-1]), flow[k])
		return &f
	}
	return nil
}

// Feasible works a study out (design D8). It is a pure function of the study.
func Feasible(s Study) (StudyResult, error) {
	if s.Years < MinStudyYears || s.Years > MaxStudyYears {
		return StudyResult{}, &Error{CodeInput, "years", fmt.Sprintf("the evaluation period is %d years, not 1 to 10", s.Years), "Evaluate it over 1 to 10 years"}
	}
	if s.Life < 1 {
		return StudyResult{}, &Error{CodeInput, "life", fmt.Sprintf("the depreciation life is %d years", s.Life), "Depreciate it over a year or more"}
	}
	c := &calc{cur: s.Currency}
	one := d(1)
	base := c.study(s, s.Rate, one, one)
	out := StudyResult{Years: base.years, NPV: base.npv}
	out.IRR = c.irr(base.poly)
	var cash, pvs, cum, cumPV []*apd.Decimal
	for _, y := range base.years {
		cash, pvs, cum, cumPV = append(cash, y.CashFlow), append(pvs, y.PV), append(cum, y.Cumulative), append(cumPV, y.CumulativePV)
	}
	out.Payback, out.DiscountedPayback = c.payback(cum, cash), c.payback(cumPV, pvs)
	if s.Investment.Sign() > 0 {
		pi := c.value(c.over(c.add(s.Investment, base.npv), s.Investment), 2)
		out.PI = pi
	}
	margin := c.sub(s.Price, s.VarCost)
	if margin.Sign() > 0 {
		be := c.over(s.Fixed, margin)
		out.BreakEvenUnits = &be
		out.BreakEvenRevenue = c.roundFrac(c.over(c.mul(s.Fixed, s.Price), margin))
		if s.Units.Sign() > 0 {
			safety := c.over(c.sub(c.mul(s.Units, margin), s.Fixed), c.mul(s.Units, margin))
			out.Safety = &safety
		}
	}
	for i, u := range Multipliers {
		for j, f := range Multipliers {
			out.Grid[i][j] = c.study(s, s.Rate, decimal(u), decimal(f)).npv
		}
	}
	for _, r := range NPVRates {
		out.NPVAt = append(out.NPVAt, c.study(s, decimal(r), one, one).npv)
	}
	switch {
	case s.Investment.IsZero() && s.Units.IsZero() && s.Price.IsZero():
		out.Verdict = VerdictNoFigures
	case c.polynomial(base.poly, s.Rate).Sign() <= 0: // the exact NPV's sign
		out.Verdict = VerdictNotFeasible
	case out.Payback != nil && c.cmp(*out.Payback, whole(s.MaxPayback)) <= 0:
		out.Verdict = VerdictFeasible
	default:
		out.Verdict = VerdictMarginal
	}
	if c.err != nil {
		return StudyResult{}, &Error{CodeInput, "study", c.err.Error(), "Check the study's figures"}
	}
	return out, nil
}

// decimal reads one of the package's own decimal constants.
func decimal(s string) *apd.Decimal {
	x, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return x
}
