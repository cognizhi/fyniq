// Package capallow is the tax capital allowance schedule of the registered assets (PRD-001 FR-FA-04; p2-reporting-planning task 5.2 and design D11): for a year of assessment,
// each asset's allowance under the method the company elected for it (section 19, 19A(1), 19A(2) or 19A(10A)), its tax written down value brought and carried forward, the
// balancing allowance or charge of one sold in the year, and the assets the
// schedule cannot place, listed apart with the reason and never guessed. The values come from the pack sg-capital-allowances and the sums are whole minor units; the schedule
// is a pure function of the assets, the elections, the pack and the organisation's financial years, so every figure can be worked by hand (and the oracle's half does).
package capallow

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
)

// The reasons an asset is listed apart.
const (
	ReasonNoElection = "no_election" // the company has not elected a method for it
	ReasonBeforePack = "before_pack" // acquired in a basis period the pack's rules do not cover (before YA 2023)
	ReasonDisposed   = "disposed"    // a disposal of part of it stands, or one whose proceeds are not known: its balancing allowance or charge is the tax agent's [accountant to confirm]
	ReasonOverCap    = "over_cap"    // elected under section 19A(10A) but above S$5,000, or past the S$30,000 of the year
	ReasonBadLife    = "bad_life"    // the working life elected is not one the pack allows for the prescribed life
	ReasonNotSGD     = "not_sgd"     // the base currency is not Singapore dollars, the pack's currency
	ReasonNotInPack  = "not_in_pack" // the method elected is not one the pack version in force works (section 19A(2) before sg-capital-allowances@2026.2)
)

// Asset is one registered asset as the schedule reads it. Cost is the cost it was bought for, in the base currency.
type Asset struct {
	ID          uuid.UUID
	Number      string
	Name        string
	PurchasedOn time.Time
	Cost        money.Money
	Election    *asset.TaxElection
	Disposed    bool
	// Disposal is the sale of the whole asset, with its proceeds net of GST in the base currency; nil for an asset not sold. An asset with Disposed set and no Disposal had
	// a part of it disposed of, or has proceeds the schedule cannot read: it is listed apart.
	Disposal *Disposal
}

// Disposal is the sale of a whole asset: its day and the proceeds, net of GST, in the base currency (a write-off is proceeds of nothing).
type Disposal struct {
	On       time.Time
	Proceeds money.Money
}

// Row is one asset's line in a year of assessment.
type Row struct {
	Asset     Asset
	Method    string
	Years     int         // the years of a section 19 election, else 0
	FirstYA   int         // the year of assessment of its first claim
	BroughtFw money.Money // the tax written down value brought forward: the cost less the allowances of the years before
	Initial   money.Money // the initial allowance, in the first year under section 19
	Annual    money.Money // the annual allowance (the whole of the year's allowance, but the initial allowance)
	Allowance money.Money // Initial + Annual
	CarriedFw money.Money // BroughtFw − Allowance
	// Sold is the sale the row is the year of: no allowance is given in it (BroughtFw is the tax written down value, CarriedFw nothing), and the balancing allowance or charge is
	// the proceeds' difference to it, a charge at most the allowances allowed before (pack.Balancing). Nil for any other row.
	Sold            *Disposal
	BalancingAllow  money.Money
	BalancingCharge money.Money
}

// Apart is an asset the schedule lists apart, with why.
type Apart struct {
	Asset  Asset
	Reason string
	YA     int // its year of assessment, when it is known
}

// Schedule is the capital allowance schedule of one year of assessment.
type Schedule struct {
	YA     int
	Pack   string
	Rows   []Row
	Apart  []Apart
	Totals Row // the columns' sums, with no asset
}

// YearOfAssessment is the year of assessment of a day: the one after the year its financial year (the basis period) ends in. A day before the books start belongs to the
// years before the first, taken as twelve months each ending on the first year's end's anniversary.
func YearOfAssessment(cal period.Calendar, day time.Time) int {
	if n := cal.Number(day); n > 0 {
		return cal.Year(n).EndsOn.Year() + 1
	}
	end := cal.FirstYearEnd
	for !day.After(end.AddDate(-1, 0, 0)) {
		end = end.AddDate(-1, 0, 0)
	}
	return end.Year() + 1
}

// Work is the capital allowance schedule of the year of assessment ya for the assets, in the order given (the purchase date, then the number). An asset claimed under section 19A(10A)
// is taken in that order until the year's cap is met; one over S$5,000 or past the cap is listed apart as over_cap (the company elects another method for it). An asset whose first
// claim is after ya is not in the schedule; one fully written off before ya has no row.
func Work(v pack.Version, cal period.Calendar, base money.Currency, ya int, assets []Asset) (Schedule, error) {
	out := Schedule{YA: ya, Pack: v.ID}
	zero := money.Zero(base)
	out.Totals = Row{BroughtFw: zero, Initial: zero, Annual: zero, Allowance: zero, CarriedFw: zero, BalancingAllow: zero, BalancingCharge: zero}
	type claim struct {
		a      Asset
		method string
		years  int
		first  int
		series []int64
		sale   *Disposal
		saleYA int
	}
	var claims []claim
	minor := func(m money.Money) int64 { n, _ := m.Minor(); return n }
	for _, a := range assets {
		first := YearOfAssessment(cal, a.PurchasedOn)
		apart := func(reason string) { out.Apart = append(out.Apart, Apart{Asset: a, Reason: reason, YA: first}) }
		switch {
		case base.Code != "SGD":
			apart(ReasonNotSGD)
			continue
		case a.Disposed && (a.Disposal == nil || !pack.HoldsBalancing(v)): // a version that works no balancing allowance lists a sold asset apart
			apart(ReasonDisposed)
			continue
		case a.Election == nil:
			apart(ReasonNoElection)
			continue
		}
		rules, err := pack.CapitalAllowanceRules(v, first)
		if err != nil {
			apart(ReasonBeforePack)
			continue
		}
		years := 0
		if a.Election.Method == asset.TaxS19 {
			if !slices.Contains(pack.WorkingLifeElections(v, a.Election.PrescribedYears, first), a.Election.Years) {
				apart(ReasonBadLife)
				continue
			}
			years = a.Election.Years
		}
		if a.Election.Method == asset.TaxS19A10A && minor(a.Cost) > rules.LowValueMax {
			apart(ReasonOverCap)
			continue
		}
		if a.Election.Method == asset.TaxS19A2 && rules.S19A2Years != 1 {
			apart(ReasonNotInPack)
			continue
		}
		series, err := pack.CapitalAllowanceSeries(v, a.Election.Method, years, minor(a.Cost), first)
		if err != nil {
			return Schedule{}, fmt.Errorf("%s: %w", a.Number, err)
		}
		c := claim{a: a, method: a.Election.Method, years: years, first: first, series: series}
		if a.Disposal != nil {
			c.sale, c.saleYA = a.Disposal, YearOfAssessment(cal, a.Disposal.On)
		}
		claims = append(claims, c)
	}
	// The one-year write-off of low-value assets, year by year: the cap takes whole assets in the order given.
	byYA := map[int][]int{}
	for i, c := range claims {
		if c.method == asset.TaxS19A10A && !(c.sale != nil && c.saleYA <= c.first) { // one sold in the year it was bought is claimed nothing: it takes no part of the cap
			byYA[c.first] = append(byYA[c.first], i)
		}
	}
	dropped := map[int]bool{}
	for first, idx := range byYA {
		costs := make([]int64, len(idx))
		for k, i := range idx {
			costs[k] = minor(claims[i].a.Cost)
		}
		taken, err := pack.LowValueClaims(v, first, costs)
		if err != nil {
			return Schedule{}, err
		}
		for k, i := range idx {
			if !taken[k] {
				dropped[i] = true
				out.Apart = append(out.Apart, Apart{Asset: claims[i].a, Reason: ReasonOverCap, YA: first})
			}
		}
	}
	for i, c := range claims {
		k := ya - c.first
		if dropped[i] || k < 0 {
			continue
		}
		if c.sale != nil && ya >= c.saleYA {
			if ya > c.saleYA {
				continue // sold in an earlier year: nothing more to say
			}
			row, err := soldRow(c.a, c.method, c.years, c.first, c.series, k, c.sale, base)
			if err != nil {
				return Schedule{}, err
			}
			out.Rows = append(out.Rows, row)
			if err := out.Totals.add(row); err != nil {
				return Schedule{}, err
			}
			continue
		}
		if k >= len(c.series) {
			continue
		}
		var before int64
		for _, x := range c.series[:k] {
			before += x
		}
		cost := minor(c.a.Cost)
		this := c.series[k]
		row := Row{Asset: c.a, Method: c.method, Years: c.years, FirstYA: c.first, BroughtFw: money.FromMinor(cost-before, base),
			Initial: zero, Annual: money.FromMinor(this, base), Allowance: money.FromMinor(this, base), CarriedFw: money.FromMinor(cost-before-this, base)}
		if c.method == asset.TaxS19 && k == 0 {
			ia, err := pack.CapitalAllowanceRules(v, c.first)
			if err != nil {
				return Schedule{}, err
			}
			initial := (2*cost*int64(ia.InitialPercent) + 100) / 200
			row.Initial = money.FromMinor(initial, base)
			row.Annual = money.FromMinor(this-initial, base)
		}
		row.BalancingAllow, row.BalancingCharge = zero, zero
		out.Rows = append(out.Rows, row)
		if err := out.Totals.add(row); err != nil {
			return Schedule{}, err
		}
	}
	out.Apart = slices.DeleteFunc(out.Apart, func(x Apart) bool { return x.YA > ya }) // an asset first claimed in a later year has nothing to say yet
	slices.SortStableFunc(out.Apart, func(a, b Apart) int {
		if c := a.Asset.PurchasedOn.Compare(b.Asset.PurchasedOn); c != 0 {
			return c
		}
		return compareString(a.Asset.Number, b.Asset.Number)
	})
	return out, nil
}

// add adds a row's columns to the totals.
func (t *Row) add(row Row) error {
	for _, f := range []struct {
		into *money.Money
		x    money.Money
	}{{&t.BroughtFw, row.BroughtFw}, {&t.Initial, row.Initial}, {&t.Annual, row.Annual}, {&t.Allowance, row.Allowance}, {&t.CarriedFw, row.CarriedFw},
		{&t.BalancingAllow, row.BalancingAllow}, {&t.BalancingCharge, row.BalancingCharge}} {
		s, err := f.into.Add(f.x)
		if err != nil {
			return err
		}
		*f.into = s
	}
	return nil
}

// soldRow is the row of the year of assessment an asset was sold in: the tax written down value is the cost less the allowances of the years before (the first k years of
// its series, all of them when it was written off already), no allowance is given in the year, and the balancing allowance or charge is pack.Balancing's.
func soldRow(a Asset, method string, years, first int, series []int64, k int, sale *Disposal, base money.Currency) (Row, error) {
	cost, _ := a.Cost.Minor()
	var before int64
	for _, x := range series[:min(max(k, 0), len(series))] {
		before += x
	}
	twdv := cost - before
	proceeds, _ := sale.Proceeds.Minor()
	ba, bc := pack.Balancing(cost, twdv, proceeds)
	zero := money.Zero(base)
	return Row{Asset: a, Method: method, Years: years, FirstYA: first, BroughtFw: money.FromMinor(twdv, base), Initial: zero, Annual: zero, Allowance: zero, CarriedFw: zero,
		Sold: sale, BalancingAllow: money.FromMinor(ba, base), BalancingCharge: money.FromMinor(bc, base)}, nil
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
