package pack

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The sg-capital-allowances pack (PRD-001 FR-FA-04; p2-reporting-planning task 5.2): the capital allowance a company claims on an asset under section 19 (an initial
// allowance and annual allowances over an elected working life), section 19A(1) (a third a year over 3 years) and section 19A(10A) (the whole cost in one year, for an asset
// of at most S$5,000, within S$30,000 a year of assessment). A pure library over exact whole minor units (cents): no clock, no I/O, no floating point.

// The methods of a capital allowance claim.
const (
	MethodS19     = "s19"     // the initial allowance, then annual allowances over the elected working life
	MethodS19A1   = "s19a1"   // a third of the cost in each of 3 years of assessment
	MethodS19A10A = "s19a10a" // the whole cost in one year, for a low-value asset, within the year's cap
	MethodS19A2   = "s19a2"   // the whole cost in one year, for a computer or prescribed automation equipment, with no cap (sg-capital-allowances@2026.2)
)

func init() {
	Schemas["sg-capital-allowances"] = Schema{
		Tables: map[string][]string{
			"rules":        {"rule", "value", "from_ya", "to_ya", "source"},
			"working_life": {"prescribed", "elect_years", "from_ya", "to_ya", "source"},
		},
		Pinned: map[string][]string{
			"series":    {"case", "method", "years", "cost", "want", "why", "source"},
			"lowvalue":  {"case", "ya", "costs", "want_taken", "why", "source"},
			"elections": {"case", "prescribed", "want", "why", "source"},
			"balancing": {"case", "cost", "twdv", "proceeds", "want", "why", "source"},
		},
		Since: map[string][2]int{"balancing": {2026, 2}}, // 2026.1 was written before the balancing allowance and charge, and holds no such table
	}
}

// CARules are the values of the capital allowance methods for the assets acquired in the basis period of one year of assessment.
type CARules struct {
	InitialPercent, AnnualPercent int   // section 19: of the cost, in whole percents
	S19A1Years                    int   // section 19A(1): the years the cost is written off over
	LowValueMax, LowValueCap      int64 // section 19A(10A): the dearest low-value asset and the most claimed in the year, in cents
}

// yaRow finds the value of a rule for the year of assessment ya, and whether the pack holds one.
func caRule(v Version, rule string, ya int) (string, bool) {
	for _, r := range v.Tables["rules"].Rows {
		if r[0] != rule || !yaIn(r[2], r[3], ya) {
			continue
		}
		return r[1], true
	}
	return "", false
}

// yaIn says whether ya is from from to to (blank: no end).
func yaIn(from, to string, ya int) bool {
	f, err := strconv.Atoi(from)
	if err != nil || ya < f {
		return false
	}
	if to != "" {
		if t, err := strconv.Atoi(to); err != nil || ya > t {
			return false
		}
	}
	return true
}

// cents reads an amount in dollars and cents, such as 5000.00, as whole cents.
func cents(s string) (int64, bool) {
	whole, frac, ok := strings.Cut(strings.TrimSpace(s), ".")
	if !ok || len(frac) != 2 {
		return 0, false
	}
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil || w < 0 || f < 0 {
		return 0, false
	}
	return w*100 + f, true
}

// CapitalAllowanceRules are the rules for an asset acquired in the basis period relating to the year of assessment ya. A year before the pack's first holds none: an asset of
// one is not classified (the schedule lists it apart, never guessed).
func CapitalAllowanceRules(v Version, ya int) (CARules, error) {
	var r CARules
	get := func(rule string) (string, error) {
		s, ok := caRule(v, rule, ya)
		if !ok {
			return "", fmt.Errorf("%s holds no %s for assets acquired in the basis period of YA %d", v.ID, rule, ya)
		}
		return s, nil
	}
	for _, x := range []struct {
		rule string
		into *int
	}{{"s19_initial_percent", &r.InitialPercent}, {"s19_annual_percent", &r.AnnualPercent}, {"s19a1_years", &r.S19A1Years}} {
		s, err := get(x.rule)
		if err != nil {
			return CARules{}, err
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return CARules{}, fmt.Errorf("%s: %s of %q is not a whole number", v.ID, x.rule, s)
		}
		*x.into = n
	}
	for _, x := range []struct {
		rule string
		into *int64
	}{{"s19a10a_max_cost", &r.LowValueMax}, {"s19a10a_cap", &r.LowValueCap}} {
		s, err := get(x.rule)
		if err != nil {
			return CARules{}, err
		}
		c, ok := cents(s)
		if !ok {
			return CARules{}, fmt.Errorf("%s: %s of %q is not an amount in dollars and cents", v.ID, x.rule, s)
		}
		*x.into = c
	}
	return r, nil
}

// WorkingLifeElections are the years a company may elect under section 19 for an asset whose life in the Sixth Schedule is prescribed years, acquired in the basis period of ya:
// 6 or 12 for a life of 12 years or less, 6, 12 or 16 for a life of 16 years. A prescribed life the page does not name (13, 14, 15, or over 16) has none.
func WorkingLifeElections(v Version, prescribed, ya int) []int {
	class := ""
	switch {
	case prescribed >= 1 && prescribed <= 12:
		class = "up_to_12"
	case prescribed == 16:
		class = "16"
	default:
		return nil
	}
	for _, r := range v.Tables["working_life"].Rows {
		if r[0] != class || !yaIn(r[2], r[3], ya) {
			continue
		}
		var out []int
		for _, f := range strings.Fields(r[1]) {
			if n, err := strconv.Atoi(f); err == nil {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}

// roundHalfAway is num / den rounded half away from zero, for a non-negative num and a positive den.
func roundHalfAway(num, den int64) int64 { return (2*num + den) / (2 * den) }

// spreadEven splits total into n parts by cumulative rounding (ALLOC-EVEN): part k = round(total × k / n) − round(total × (k − 1) / n), half away from zero, so the parts add to
// the total exactly.
func spreadEven(total int64, n int) []int64 {
	out := make([]int64, n)
	var done int64
	for k := 1; k <= n; k++ {
		cum := roundHalfAway(total*int64(k), int64(n))
		out[k-1] = cum - done
		done = cum
	}
	return out
}

// CapitalAllowanceSeries are the allowances of an asset that cost cost cents, bought for cash, in each year of assessment from the one of its first claim, under method (FR-FA-04):
//
//   - s19, over years (an election WorkingLifeElections allows): the initial allowance round(cost × 20%, cents, half away from zero) in the first year, and the rest of the cost, the
//     80%, spread over the years by ALLOC-EVEN as the annual allowances, the first of them in the first year too (IRAS: IA = 20% of the cost, AA = 80% of the cost / the years);
//   - s19a1: the cost spread over 3 years by ALLOC-EVEN;
//   - s19a10a: the whole cost in the one year (whether the asset qualifies and fits the year's cap is LowValueClaims's);
//   - s19a2: the whole cost in the one year, a computer or prescribed automation equipment, with no cap and no limit of cost (IRAS: AA = 100% of the cost).
//
// The rounding to cents (IRAS shows whole dollars) is ours [accountant to confirm]. ya is the year of assessment of the asset's acquisition, whose rules apply.
func CapitalAllowanceSeries(v Version, method string, years int, cost int64, ya int) ([]int64, error) {
	if cost <= 0 {
		return nil, fmt.Errorf("the cost %d cents is not above zero", cost)
	}
	rules, err := CapitalAllowanceRules(v, ya)
	if err != nil {
		return nil, err
	}
	switch method {
	case MethodS19:
		if years < 1 {
			return nil, fmt.Errorf("a working life of %d years", years)
		}
		ia := roundHalfAway(cost*int64(rules.InitialPercent), 100)
		out := spreadEven(cost-ia, years)
		out[0] += ia
		return out, nil
	case MethodS19A1:
		return spreadEven(cost, rules.S19A1Years), nil
	case MethodS19A10A, MethodS19A2:
		return []int64{cost}, nil
	}
	return nil, fmt.Errorf("the method %q is not section 19, 19A(1), 19A(2) or 19A(10A)", method)
}

// Balancing is the balancing allowance or charge of a sale (IRAS, "Calculating BA/ BC"): sale proceeds less the tax written down value, all in cents. Proceeds below the written down
// value give a balancing allowance of the difference; proceeds above it a balancing charge of the difference, restricted to the allowances allowed before, which is the cost less
// the written down value. Exactly one of the two is nonzero, or neither.
func Balancing(cost, twdv, proceeds int64) (allowance, charge int64) {
	switch {
	case proceeds < twdv:
		return twdv - proceeds, 0
	case proceeds > twdv:
		return 0, min(proceeds-twdv, cost-twdv)
	}
	return 0, 0
}

// LowValueClaims says which of the assets (their costs in cents, in the order the company's claim takes them) take the one-year write-off of section 19A(10A) in the year of
// assessment ya: an asset that costs more than S$5,000 never does, and one takes it only if the total claimed stays within S$30,000: the cap takes a whole asset or none, and
// a later asset that still fits is taken (IRAS's example leaves out the seventh piece of S$4,400 that would make S$30,800). The order is the company's choice, and the
// rule that the earlier asset comes first is ours [accountant to confirm].
func LowValueClaims(v Version, ya int, costs []int64) ([]bool, error) {
	rules, err := CapitalAllowanceRules(v, ya)
	if err != nil {
		return nil, err
	}
	taken := make([]bool, len(costs))
	var total int64
	for i, c := range costs {
		if c <= 0 || c > rules.LowValueMax || total+c > rules.LowValueCap {
			continue
		}
		taken[i] = true
		total += c
	}
	return taken, nil
}

// checkCapitalAllowances checks the sg-capital-allowances tables: whole-number percents and years, amounts in dollars and cents, years of assessment that are numbers, and a rule's
// years without an overlap.
func checkCapitalAllowances(dir string, v Version) Problems {
	var out Problems
	add := func(field, message, fix string) {
		out = append(out, Problem{CodeValue, dir + ": " + field, message, fix})
	}
	type span struct{ from, to int }
	spans := map[string][]span{}
	span1 := func(at, key, from, to string) {
		f, err := strconv.Atoi(from)
		if err != nil || f < 1990 || f > 2200 {
			add(at, fmt.Sprintf("the year of assessment %q is not a year", from), "Write the first year of assessment the row applies to")
			return
		}
		t := 9999
		if to != "" {
			if t, err = strconv.Atoi(to); err != nil || t < f {
				add(at, fmt.Sprintf("the last year %q is not a year on or after the first", to), "Leave it blank while the row is current")
				return
			}
		}
		for _, o := range spans[key] {
			if f <= o.to && t >= o.from {
				add(at, "its years overlap another row of "+key, "Give each rule one value in any year")
			}
		}
		spans[key] = append(spans[key], span{f, t})
	}
	known := []string{"s19_initial_percent", "s19_annual_percent", "s19a1_years", "s19a10a_max_cost", "s19a10a_cap"}
	for i, r := range v.Tables["rules"].Rows {
		at := fmt.Sprintf("rules.csv row %d", i+2)
		if !slices.Contains(known, r[0]) {
			add(at, fmt.Sprintf("the rule %q is not one the table's description names", r[0]), "Use a rule the table's description names")
			continue
		}
		if strings.HasSuffix(r[0], "_cost") || r[0] == "s19a10a_cap" {
			if _, ok := cents(r[1]); !ok {
				add(at, fmt.Sprintf("the value %q is not an amount in dollars and cents", r[1]), "Write it with two decimals")
			}
		} else if n, err := strconv.Atoi(r[1]); err != nil || n < 1 || n > 100 {
			add(at, fmt.Sprintf("the value %q is not a whole number from 1 to 100", r[1]), "Write whole percents or years")
		}
		span1(at, r[0], r[2], r[3])
	}
	for i, r := range v.Tables["working_life"].Rows {
		at := fmt.Sprintf("working_life.csv row %d", i+2)
		if r[0] != "up_to_12" && r[0] != "16" {
			add(at, fmt.Sprintf("the class %q is not up_to_12 or 16", r[0]), "Use a class the table's description names")
		}
		for _, f := range strings.Fields(r[1]) {
			if n, err := strconv.Atoi(f); err != nil || n < 1 || n > 100 {
				add(at, fmt.Sprintf("the election %q is not a whole number of years", f), "List whole years separated by a space")
			}
		}
		span1(at, "working_life "+r[0], r[2], r[3])
	}
	return out
}

// runCapitalAllowances runs the sg-capital-allowances pinned cases.
func runCapitalAllowances(v Version) []Result {
	if v.Pack != "sg-capital-allowances" {
		return nil
	}
	const ya = 2027 // the year of assessment the cases are worked in
	var out []Result
	for _, row := range v.Pinned["series"].Rows {
		r := Result{Table: "series", Case: row[0], Why: row[5], Want: row[4]}
		cost, ok := cents(row[3])
		years, _ := strconv.Atoi(row[2])
		if !ok {
			r.Got = "the cost does not read"
			out = append(out, r)
			continue
		}
		s, err := CapitalAllowanceSeries(v, row[1], years, cost, ya)
		if err != nil {
			r.Got = err.Error()
			out = append(out, r)
			continue
		}
		parts := make([]string, len(s))
		for i, c := range s {
			parts[i] = fmt.Sprintf("%d.%02d", c/100, c%100)
		}
		r.Got = strings.Join(parts, ";")
		r.Pass = r.Got == r.Want
		out = append(out, r)
	}
	for _, row := range v.Pinned["lowvalue"].Rows {
		r := Result{Table: "lowvalue", Case: row[0], Why: row[4], Want: row[3]}
		yr, _ := strconv.Atoi(row[1])
		var costs []int64
		for _, f := range strings.Split(row[2], ";") {
			c, ok := cents(f)
			if !ok {
				r.Got = fmt.Sprintf("the cost %q does not read", f)
			}
			costs = append(costs, c)
		}
		if r.Got != "" {
			out = append(out, r)
			continue
		}
		taken, err := LowValueClaims(v, yr, costs)
		if err != nil {
			r.Got = err.Error()
			out = append(out, r)
			continue
		}
		var b strings.Builder
		for _, t := range taken {
			if t {
				b.WriteByte('y')
			} else {
				b.WriteByte('n')
			}
		}
		r.Got = b.String()
		r.Pass = r.Got == r.Want
		out = append(out, r)
	}
	for _, row := range v.Pinned["balancing"].Rows {
		r := Result{Table: "balancing", Case: row[0], Why: row[5], Want: row[4]}
		cost, ok1 := cents(row[1])
		twdv, ok2 := cents(row[2])
		proceeds, ok3 := cents(row[3])
		if !ok1 || !ok2 || !ok3 {
			r.Got = "an amount does not read"
			out = append(out, r)
			continue
		}
		ba, bc := Balancing(cost, twdv, proceeds)
		if bc > 0 {
			r.Got = fmt.Sprintf("BC %d.%02d", bc/100, bc%100)
		} else {
			r.Got = fmt.Sprintf("BA %d.%02d", ba/100, ba%100)
		}
		r.Pass = r.Got == r.Want
		out = append(out, r)
	}
	for _, row := range v.Pinned["elections"].Rows {
		r := Result{Table: "elections", Case: row[0], Why: row[3], Want: row[2]}
		p, _ := strconv.Atoi(row[1])
		var parts []string
		for _, n := range WorkingLifeElections(v, p, ya) {
			parts = append(parts, strconv.Itoa(n))
		}
		r.Got = strings.Join(parts, " ")
		if r.Got == "" {
			r.Got = "none"
		}
		r.Pass = r.Got == r.Want
		out = append(out, r)
	}
	return out
}
