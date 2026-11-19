package pack

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// sg-cpf@2026.2 (p1-statutory-packs 4.1): the CPF contribution rates of the first two years of permanent residence, the
// graduated range of monthly total wages from $50 to $750, and the Ordinary Wage maximum a band implies. The figures
// are exact; payroll's own rounding (the total to the nearest dollar, the employee's share down, the employer's the
// difference) is p1-payroll-link's.

// The residency groups of the rates table. SC and SPR from the third year (GroupSCSPR3) contribute at the full rates.
// A first- or second-year permanent resident contributes at the graduated rates of both the employer and the employee
// (_gg) and, once the CPF Board approves a joint application, at full employer and graduated employee rates (_fg); at
// full rates of both the employer and the employee a permanent resident reads the sc_spr3 rows.
const (
	GroupSPR1GG = "spr1_gg"
	GroupSPR2GG = "spr2_gg"
	GroupSPR1FG = "spr1_fg"
	GroupSPR2FG = "spr2_fg"
)

// cpfGroups are the residency groups sg-cpf reads, and the version that began to hold each (sc_spr3 from the first).
var cpfGroups = map[string][2]int{
	GroupSCSPR3: {2026, 1}, GroupSPR1GG: {2026, 2}, GroupSPR2GG: {2026, 2}, GroupSPR1FG: {2026, 2}, GroupSPR2FG: {2026, 2},
}

// CPFAmounts are the contributions on one month's total wages in the graduated range, exact and not yet rounded: the
// total contribution (the employer's and the employee's shares) and the employee's share. The employer's share is the
// difference.
type CPFAmounts struct {
	Total, Employee *apd.Decimal
}

// CPFLowWagesOn is the contribution on total wages tw (Ordinary and Additional Wages together) of month, up to the
// graduated range's top (S$750): nothing up to its first limit (S$50); the flat rate of the wages, all of it the
// employer's, up to the second (S$500); then the flat rate of the wages plus the slope of the wages above the second
// limit, the slope being the employee's share. Wages above the graduated range are read with CPFRatesOn, so ErrNotHeld
// says this version has no graduated figure for them, for an age or group it does not hold, or for an earlier month.
func CPFLowWagesOn(v Version, month time.Time, group string, age int, tw *apd.Decimal) (CPFAmounts, error) {
	for _, row := range v.Tables["low_wages"].Rows {
		if row[0] != group || !inAges(age, row[1], row[2]) || !inDates(month, row[3], row[4]) {
			continue
		}
		var lim [3]*apd.Decimal
		for i := range lim {
			d, _, err := apd.NewFromString(row[5+i])
			if err != nil {
				return CPFAmounts{}, fmt.Errorf("sg-cpf low_wages: the limit %q does not read", row[5+i])
			}
			lim[i] = d
		}
		flat, _, err1 := apd.NewFromString(row[8])
		slope, _, err2 := apd.NewFromString(row[9])
		if err1 != nil || err2 != nil {
			return CPFAmounts{}, fmt.Errorf("sg-cpf low_wages: the rates of %s above %s do not read", row[0], row[1])
		}
		zero := func() CPFAmounts { return CPFAmounts{Total: apd.New(0, 0), Employee: apd.New(0, 0)} }
		switch {
		case tw.Cmp(lim[0]) <= 0:
			return zero(), nil
		case tw.Cmp(lim[2]) > 0:
			return CPFAmounts{}, ErrNotHeld
		}
		total := new(apd.Decimal)
		if _, err := exact.Mul(total, tw, flat); err != nil {
			return CPFAmounts{}, err
		}
		if tw.Cmp(lim[1]) <= 0 {
			return CPFAmounts{Total: total, Employee: apd.New(0, 0)}, nil
		}
		over, ee := new(apd.Decimal), new(apd.Decimal)
		if _, err := exact.Sub(over, tw, lim[1]); err != nil {
			return CPFAmounts{}, err
		}
		if _, err := exact.Mul(ee, over, slope); err != nil {
			return CPFAmounts{}, err
		}
		if _, err := exact.Add(total, total, ee); err != nil {
			return CPFAmounts{}, err
		}
		return CPFAmounts{Total: total, Employee: ee}, nil
	}
	return CPFAmounts{}, ErrNotHeld
}

// CPFOWMax is the most the contributions on Ordinary Wages can be for an employee of the group and age in month,
// whose total wages are above the graduated range: the Ordinary Wage ceiling at the total rate (the employer's and the
// employee's), and at the employee's rate. These are the "Max." figures the CPF Board prints under each band.
func CPFOWMax(v Version, month time.Time, group string, age int) (CPFAmounts, error) {
	r, err := CPFRatesOn(v, month, group, age, apd.New(75001, -2))
	if err != nil {
		return CPFAmounts{}, err
	}
	ceiling, err := CPFCeiling(v, CeilingOWMonthly, month)
	if err != nil {
		return CPFAmounts{}, err
	}
	rate := new(apd.Decimal)
	if _, err := exact.Add(rate, r.Employer, r.Employee); err != nil {
		return CPFAmounts{}, err
	}
	total, ee := new(apd.Decimal), new(apd.Decimal)
	if _, err := exact.Mul(total, rate, ceiling); err != nil {
		return CPFAmounts{}, err
	}
	if _, err := exact.Mul(ee, r.Employee, ceiling); err != nil {
		return CPFAmounts{}, err
	}
	return CPFAmounts{Total: total, Employee: ee}, nil
}

// sinceVersion reports whether v is version {year, n} or later.
func sinceVersion(v Version, since [2]int) bool {
	return v.Year > since[0] || (v.Year == since[0] && v.N >= since[1])
}

// checkCPFLowWages checks sg-cpf's low_wages table: a known group, bands and limits as sg-cpf's rates states them, the
// limits in cents in rising order, and rates as fractions. Each band's graduated formula must also meet its full rate
// where the range ends: the flat rate of the top limit plus the slope of what lies above the second limit is the
// total rate (the employer's and the employee's) of the same band in the rates table, read at the same dates, as the
// CPF Board's tables are built; a rate mistyped in either table fails here.
func checkCPFLowWages(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	rows := v.Tables["low_wages"].Rows
	if len(rows) == 0 {
		add(CodeTable, "low_wages.csv", "has no rates", "Add the graduated rates")
	}
	for i, row := range rows {
		at := fmt.Sprintf("low_wages.csv row %d", i+2)
		if since, ok := cpfGroups[row[0]]; !ok || !sinceVersion(v, since) {
			add(CodeValue, at, fmt.Sprintf("group %q is not one Fyniq reads", row[0]), "Enter sc_spr3, spr1_gg, spr2_gg, spr1_fg or spr2_fg")
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
		var lim [3]*apd.Decimal
		for c := 0; c < 3; c++ {
			d, _, err := apd.NewFromString(row[5+c])
			if err != nil || !amountTwoDP.MatchString(row[5+c]) {
				add(CodeValue, at, fmt.Sprintf("limit %q is not an amount with cents", row[5+c]), "Enter the wage limit with cents, such as 500.00")
				continue
			}
			lim[c] = d
		}
		if lim[0] != nil && lim[1] != nil && lim[2] != nil && (lim[0].Cmp(lim[1]) >= 0 || lim[1].Cmp(lim[2]) >= 0) {
			add(CodeValue, at, "the limits do not rise (nil_to, flat_to, graduated_to)", "Enter 50.00, 500.00 and 750.00 as the CPF Board's table states them")
		}
		flat, err1 := fraction(row[8])
		slope, err2 := fraction(row[9])
		if err1 != nil || err2 != nil {
			add(CodeValue, at, fmt.Sprintf("flat_rate %q or slope %q is not a fraction", row[8], row[9]), "Write each rate as a fraction, such as 0.17 and 0.6")
			continue
		}
		if lim[1] == nil || lim[2] == nil {
			continue
		}
		// The formula at the top limit, against the full rate of the same band and dates.
		top, over, atTop := new(apd.Decimal), new(apd.Decimal), new(apd.Decimal)
		_, e1 := exact.Mul(top, lim[2], flat)
		_, e2 := exact.Sub(over, lim[2], lim[1])
		_, e3 := exact.Mul(over, over, slope)
		_, e4 := exact.Add(atTop, top, over)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			add(CodeValue, at, "the formula at the top limit is not exact", "Use rates the CPF Board states")
			continue
		}
		found := false
		for _, r := range v.Tables["rates"].Rows {
			if r[0] != row[0] || r[1] != row[1] || r[2] != row[2] || r[4] != row[3] || r[5] != row[4] {
				continue
			}
			found = true
			er, _, err1 := apd.NewFromString(r[6])
			ee, _, err2 := apd.NewFromString(r[7])
			if err1 != nil || err2 != nil {
				break
			}
			rate, full := new(apd.Decimal), new(apd.Decimal)
			if _, err := exact.Add(rate, er, ee); err != nil {
				break
			}
			if _, err := exact.Mul(full, rate, lim[2]); err != nil || full.Cmp(atTop) != 0 {
				add(CodeValue, at, fmt.Sprintf("the graduated formula gives %s at wages of %s, but the full rate of the same band gives %s", atTop.Text('f'), lim[2].Text('f'), full.Text('f')),
					"Check the flat rate and the slope against the CPF Board's table, and the rates table's rates for the same band")
			}
		}
		if !found {
			add(CodeValue, at, "has no band of the same group, ages and dates in rates.csv", "Add the full rate of the band, or correct the dates")
		}
	}
	return out
}

// runCPFLow runs sg-cpf's graduated and maximum pinned cases; "none" where the version must not hold a figure.
func runCPFLow(v Version) []Result {
	if v.Pack != "sg-cpf" {
		return nil
	}
	var out []Result
	for _, row := range v.Pinned["low_wages"].Rows {
		r := Result{Table: "low_wages", Case: row[0] + " " + row[1] + " " + row[2] + " age " + row[3] + " " + row[4], Why: row[7], Want: row[5] + " / " + row[6]}
		if row[5] == "none" {
			r.Want = "none"
		}
		m, err1 := time.ParseInLocation(monthFormat, row[1], time.UTC)
		age, err2 := strconv.Atoi(row[3])
		w, _, err3 := apd.NewFromString(row[4])
		if err1 != nil || err2 != nil || err3 != nil {
			r.Got = fmt.Sprintf("the month %q, age %q or wages %q do not parse", row[1], row[3], row[4])
			out = append(out, r)
			continue
		}
		a, err := CPFLowWagesOn(v, m, row[2], age, w)
		switch {
		case errors.Is(err, ErrNotHeld):
			r.Got, r.Pass = "none", r.Want == "none"
		case err != nil:
			r.Got = err.Error()
		default:
			r.Got = a.Total.Text('f') + " / " + a.Employee.Text('f')
			r.Pass = sameDecimal(a.Total.Text('f'), row[5]) && sameDecimal(a.Employee.Text('f'), row[6])
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["maxima"].Rows {
		r := Result{Table: "maxima", Case: row[0] + " " + row[1] + " " + row[2] + " age " + row[3], Why: row[6], Want: row[4] + " / " + row[5]}
		if row[4] == "none" {
			r.Want = "none"
		}
		m, err1 := time.ParseInLocation(monthFormat, row[1], time.UTC)
		age, err2 := strconv.Atoi(row[3])
		if err1 != nil || err2 != nil {
			r.Got = fmt.Sprintf("the month %q or age %q do not parse", row[1], row[3])
			out = append(out, r)
			continue
		}
		a, err := CPFOWMax(v, m, row[2], age)
		switch {
		case errors.Is(err, ErrNotHeld):
			r.Got, r.Pass = "none", r.Want == "none"
		case err != nil:
			r.Got = err.Error()
		default:
			r.Got = a.Total.Text('f') + " / " + a.Employee.Text('f')
			r.Pass = sameDecimal(a.Total.Text('f'), row[4]) && sameDecimal(a.Employee.Text('f'), row[5])
		}
		out = append(out, r)
	}
	return out
}
