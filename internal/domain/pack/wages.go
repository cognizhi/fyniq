package pack

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// The sg-cpf and sg-sdl packs (PRD-001 §10.6; p1-planning design D1): the CPF contribution rates and wage ceilings,
// and the Skills Development Levy, each chosen by the month the wages are earned in. The figures are exact; payroll's
// own rounding (the total CPF to the dollar, the employee's share down, the SDL total down) is p1-payroll-link's.

// GroupSCSPR3 is Singapore citizens and permanent residents from the third year of permanent residence.
const GroupSCSPR3 = "sc_spr3"

// The CPF wage ceilings.
const (
	CeilingOWMonthly    = "ow_monthly"    // the Ordinary Wage ceiling a month
	CeilingAnnualSalary = "annual_salary" // the annual salary ceiling
)

// ErrNotHeld is the answer for a month, a group, an age or a wage the version holds no figure for: wages earned
// before its rows begin, or in the graduated range it leaves to a later version.
var ErrNotHeld = errors.New("the pack holds no figure for the month, the group, the age and the wage")

// CPFRates are an employee's contribution rates for a month, as fractions of the wage.
type CPFRates struct {
	Employer, Employee *apd.Decimal
}

// monthFormat is how a pinned case names a month.
const monthFormat = "2006-01"

// inDates reports whether d falls from from to to, a blank to having no end.
func inDates(d time.Time, from, to string) bool {
	f, err := ParseDate(from)
	if err != nil || d.Before(f) {
		return false
	}
	if to == "" {
		return true
	}
	t, err := ParseDate(to)
	return err == nil && !d.After(t)
}

// inAges reports whether age is above above (blank: no lower limit) and up to to (blank: no upper limit).
func inAges(age int, above, to string) bool {
	if above != "" {
		a, err := strconv.Atoi(above)
		if err != nil || age <= a {
			return false
		}
	}
	if to != "" {
		t, err := strconv.Atoi(to)
		if err != nil || age > t {
			return false
		}
	}
	return true
}

// CPFRatesOn is the employer's and the employee's rates for wages earned in month by an employee of the group and age
// (as the CPF Board counts it for that month) whose total wages for the month are wage.
func CPFRatesOn(v Version, month time.Time, group string, age int, wage *apd.Decimal) (CPFRates, error) {
	for _, row := range v.Tables["rates"].Rows {
		if row[0] != group || !inAges(age, row[1], row[2]) || !inDates(month, row[4], row[5]) {
			continue
		}
		floor, _, err := apd.NewFromString(row[3])
		if err != nil {
			return CPFRates{}, fmt.Errorf("sg-cpf rates: wage_above %q does not read", row[3])
		}
		if wage.Cmp(floor) <= 0 {
			continue
		}
		er, _, err1 := apd.NewFromString(row[6])
		ee, _, err2 := apd.NewFromString(row[7])
		if err1 != nil || err2 != nil {
			return CPFRates{}, fmt.Errorf("sg-cpf rates: the rates of %s above %s do not read", row[0], row[1])
		}
		return CPFRates{Employer: er, Employee: ee}, nil
	}
	return CPFRates{}, ErrNotHeld
}

// CPFCeiling is the ceiling in force for wages earned in month.
func CPFCeiling(v Version, ceiling string, month time.Time) (*apd.Decimal, error) {
	for _, row := range v.Tables["ceilings"].Rows {
		if row[0] == ceiling && inDates(month, row[1], row[2]) {
			d, _, err := apd.NewFromString(row[3])
			return d, err
		}
	}
	return nil, ErrNotHeld
}

// SDLOn is the Skills Development Levy on one employee's total wages for month: the rate of the wages, at least the
// minimum and at most the maximum; no wages, no levy.
func SDLOn(v Version, month time.Time, wages *apd.Decimal) (*apd.Decimal, error) {
	for _, row := range v.Tables["levy"].Rows {
		if !inDates(month, row[0], row[1]) {
			continue
		}
		rate, _, err1 := apd.NewFromString(row[2])
		lo, _, err2 := apd.NewFromString(row[3])
		hi, _, err3 := apd.NewFromString(row[4])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("sg-sdl levy: the row from %s does not read", row[0])
		}
		if wages.Sign() <= 0 {
			return apd.New(0, 0), nil
		}
		levy := new(apd.Decimal)
		if _, err := exact.Mul(levy, wages, rate); err != nil {
			return nil, err
		}
		switch {
		case levy.Cmp(lo) < 0:
			return new(apd.Decimal).Set(lo), nil
		case levy.Cmp(hi) > 0:
			return new(apd.Decimal).Set(hi), nil
		}
		return levy, nil
	}
	return nil, ErrNotHeld
}

// periods checks that a table's rows of one key have dates in order that do not overlap.
func periods(add func(code, field, message, fix string), file string, rows [][]string, key func([]string) string, from, to int) {
	type span struct {
		from time.Time
		to   *time.Time
	}
	seen := map[string][]span{}
	for i, row := range rows {
		at := fmt.Sprintf("%s row %d", file, i+2)
		f, err := ParseDate(row[from])
		if err != nil {
			add(CodeDates, at, fmt.Sprintf("effective_from %q is not a date", row[from]), "Enter the first day it applies (YYYY-MM-DD)")
			continue
		}
		s := span{from: f}
		if row[to] != "" {
			t, err := ParseDate(row[to])
			if err != nil || t.Before(f) {
				add(CodeDates, at, fmt.Sprintf("effective_to %q is not a date on or after effective_from", row[to]), "Enter the last day it applies, or leave it blank")
				continue
			}
			s.to = &t
		}
		k := key(row)
		for _, o := range seen[k] {
			if (o.to == nil || !s.from.After(*o.to)) && (s.to == nil || !o.from.After(*s.to)) {
				add(CodeDates, at, fmt.Sprintf("overlaps another row of %s", k), "A value's periods follow one another without an overlap")
			}
		}
		seen[k] = append(seen[k], s)
	}
}

// checkCPF checks the sg-cpf tables: a known group, whole ages with the band's top above its bottom, a wage floor in
// cents, rates as fractions and periods without an overlap per band; ceilings Fyniq reads, in cents, per period.
func checkCPF(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	rates := v.Tables["rates"].Rows
	for i, row := range rates {
		at := fmt.Sprintf("rates.csv row %d", i+2)
		if row[0] != GroupSCSPR3 {
			add(CodeValue, at, fmt.Sprintf("group %q is not one Fyniq reads", row[0]), "Enter sc_spr3")
		}
		lo, hi := -1, 1<<30
		var err1, err2 error
		if row[1] != "" {
			lo, err1 = strconv.Atoi(row[1])
		}
		if row[2] != "" {
			hi, err2 = strconv.Atoi(row[2])
		}
		if err1 != nil || err2 != nil || hi <= lo {
			add(CodeValue, at, fmt.Sprintf("ages above %q up to %q are not whole years with the top above the bottom", row[1], row[2]),
				"Enter the band as the CPF Board's table states it, such as above 55 up to 60")
		}
		if !amountTwoDP.MatchString(row[3]) {
			add(CodeValue, at, fmt.Sprintf("wage_above %q is not an amount with cents", row[3]), "Enter the wage the rates apply above, such as 750.00")
		}
		for c, name := range map[int]string{6: "employer", 7: "employee"} {
			if _, err := fraction(row[c]); err != nil {
				add(CodeValue, at, fmt.Sprintf("%s %q %v", name, row[c], err), "Write the rate as a fraction, such as 0.17 for 17%")
			}
		}
	}
	if len(rates) == 0 {
		add(CodeTable, "rates.csv", "has no rates", "Add the rates")
	}
	periods(add, "rates.csv", rates, func(r []string) string { return r[0] + " above " + r[1] + " to " + r[2] + " over " + r[3] }, 4, 5)
	for i, row := range v.Tables["ceilings"].Rows {
		at := fmt.Sprintf("ceilings.csv row %d", i+2)
		if row[0] != CeilingOWMonthly && row[0] != CeilingAnnualSalary {
			add(CodeValue, at, fmt.Sprintf("ceiling %q is not one Fyniq reads", row[0]), "Enter ow_monthly or annual_salary")
		}
		if !amountTwoDP.MatchString(row[3]) || row[3] == "0.00" {
			add(CodeValue, at, fmt.Sprintf("amount %q is not an amount above zero with cents", row[3]), "Enter the ceiling, such as 8000.00")
		}
	}
	periods(add, "ceilings.csv", v.Tables["ceilings"].Rows, func(r []string) string { return r[0] }, 1, 2)
	return out
}

// checkSDL checks the sg-sdl table: a rate as a fraction, a minimum and a maximum in cents with the minimum not above
// the maximum, and periods without an overlap.
func checkSDL(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	rows := v.Tables["levy"].Rows
	for i, row := range rows {
		at := fmt.Sprintf("levy.csv row %d", i+2)
		if _, err := fraction(row[2]); err != nil {
			add(CodeValue, at, fmt.Sprintf("rate %q %v", row[2], err), "Write the rate as a fraction, such as 0.0025 for 0.25%")
		}
		lo, _, err1 := apd.NewFromString(row[3])
		hi, _, err2 := apd.NewFromString(row[4])
		if !amountTwoDP.MatchString(row[3]) || !amountTwoDP.MatchString(row[4]) || err1 != nil || err2 != nil || lo.Cmp(hi) > 0 {
			add(CodeValue, at, fmt.Sprintf("minimum %q and maximum %q are not amounts with cents, the minimum not above the maximum", row[3], row[4]),
				"Enter both with cents, such as 2.00 and 11.25")
		}
	}
	if len(rows) == 0 {
		add(CodeTable, "levy.csv", "has no levy", "Add the levy")
	}
	periods(add, "levy.csv", rows, func([]string) string { return "the levy" }, 0, 1)
	return out
}

// runWages runs the sg-cpf and sg-sdl pinned cases; "none" where the version must not hold a figure.
func runWages(v Version) []Result {
	if v.Pack != "sg-cpf" && v.Pack != "sg-sdl" {
		return nil
	}
	var out []Result
	month := func(r *Result, s string) (time.Time, bool) {
		m, err := time.ParseInLocation(monthFormat, s, time.UTC)
		if err != nil {
			r.Got = fmt.Sprintf("the month %q does not parse", s)
		}
		return m, err == nil
	}
	amount := func(r *Result, s string) (*apd.Decimal, bool) {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			r.Got = fmt.Sprintf("the amount %q does not parse", s)
		}
		return d, err == nil
	}
	none := func(r *Result, err error) {
		r.Got, r.Pass = "none", r.Want == "none"
		if !errors.Is(err, ErrNotHeld) {
			r.Got, r.Pass = err.Error(), false
		}
	}
	for _, row := range v.Pinned["rates"].Rows {
		r := Result{Table: "rates", Case: row[0] + " " + row[1] + " age " + row[3], Why: row[7]}
		r.Want = row[5] + " / " + row[6]
		if row[5] == "none" {
			r.Want = "none"
		}
		age, err := strconv.Atoi(row[3])
		if err != nil {
			r.Got = fmt.Sprintf("the age %q does not parse", row[3])
		} else if m, ok := month(&r, row[1]); ok {
			if w, ok := amount(&r, row[4]); ok {
				rates, err := CPFRatesOn(v, m, row[2], age, w)
				if err != nil {
					none(&r, err)
				} else {
					r.Got = rates.Employer.Text('f') + " / " + rates.Employee.Text('f')
					r.Pass = sameDecimal(rates.Employer.Text('f'), row[5]) && sameDecimal(rates.Employee.Text('f'), row[6])
				}
			}
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["ceilings"].Rows {
		r := Result{Table: "ceilings", Case: row[0] + " " + row[1] + " " + row[2], Why: row[4], Want: row[3]}
		if m, ok := month(&r, row[2]); ok {
			if d, err := CPFCeiling(v, row[1], m); err != nil {
				none(&r, err)
			} else {
				r.Got = d.Text('f')
				r.Pass = sameDecimal(r.Got, row[3])
			}
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["levy"].Rows {
		r := Result{Table: "levy", Case: row[0] + " " + row[1] + " " + row[2], Why: row[4], Want: row[3]}
		if m, ok := month(&r, row[1]); ok {
			if w, ok := amount(&r, row[2]); ok {
				if d, err := SDLOn(v, m, w); err != nil {
					none(&r, err)
				} else {
					r.Got = d.Text('f')
					r.Pass = sameDecimal(r.Got, row[3])
				}
			}
		}
		out = append(out, r)
	}
	return out
}
