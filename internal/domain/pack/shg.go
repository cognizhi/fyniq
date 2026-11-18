package pack

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// The sg-shg pack (PRD-001 §10.6; p1-statutory-packs 4.1): the monthly contributions to the self-help group funds,
// CDAC, ECF, MBMF and SINDA, by the employee's monthly total wages, chosen by the month the wages are earned in.
// Who contributes to which fund is the employee's race or religion, which is personal data: this package holds the
// amounts and the eligibility the CPF Board states, and payroll decides who is in a fund.

// The self-help group funds.
const (
	FundCDAC  = "CDAC"
	FundECF   = "ECF"
	FundMBMF  = "MBMF"
	FundSINDA = "SINDA"
)

// funds are the funds the CPF Board collects for.
var funds = []string{FundCDAC, FundECF, FundMBMF, FundSINDA}

// SHGOn is the monthly contribution to fund for wages earned in month, whose monthly total wages are wages: the amount
// of the band the wages fall in, a band "up to" an amount including it and a band "above" an amount starting a cent
// over it. The page gives no figure for no wages, a fund it does not list or a month before the version, so
// ErrNotHeld says none is held.
func SHGOn(v Version, month time.Time, fund string, wages *apd.Decimal) (*apd.Decimal, error) {
	if wages.Sign() <= 0 {
		return nil, ErrNotHeld
	}
	for _, row := range v.Tables["bands"].Rows {
		if row[0] != fund || !inDates(month, row[3], row[4]) {
			continue
		}
		if row[1] != "" {
			above, _, err := apd.NewFromString(row[1])
			if err != nil {
				return nil, fmt.Errorf("sg-shg bands: wage_above %q does not read", row[1])
			}
			if wages.Cmp(above) <= 0 {
				continue
			}
		}
		if row[2] != "" {
			to, _, err := apd.NewFromString(row[2])
			if err != nil {
				return nil, fmt.Errorf("sg-shg bands: wage_to %q does not read", row[2])
			}
			if wages.Cmp(to) > 0 {
				continue
			}
		}
		amount, _, err := apd.NewFromString(row[5])
		if err != nil {
			return nil, fmt.Errorf("sg-shg bands: amount %q does not read", row[5])
		}
		return amount, nil
	}
	return nil, ErrNotHeld
}

// checkSHG checks the sg-shg tables: each fund listed once with race or religion deciding it; per fund and period,
// bands in amounts with cents that start from nothing, follow one another without a gap or an overlap and end with one
// without a limit; and periods of one fund that do not overlap.
func checkSHG(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	known := map[string]bool{}
	for _, f := range funds {
		known[f] = false
	}
	for i, row := range v.Tables["funds"].Rows {
		at := fmt.Sprintf("funds.csv row %d", i+2)
		if seen, ok := known[row[0]]; !ok {
			add(CodeValue, at, fmt.Sprintf("fund %q is not one Fyniq reads", row[0]), "Enter CDAC, ECF, MBMF or SINDA")
		} else if seen {
			add(CodeValue, at, fmt.Sprintf("fund %s is listed twice", row[0]), "List each fund once")
		}
		known[row[0]] = true
		if row[2] != "race" && row[2] != "religion" {
			add(CodeValue, at, fmt.Sprintf("basis %q is not race or religion", row[2]), "Enter what decides the fund, as the CPF Board's page states it")
		}
	}
	for _, f := range funds {
		if !known[f] {
			add(CodeTable, "funds.csv", fmt.Sprintf("does not list the fund %s", f), "Add the fund")
		}
	}
	rows := v.Tables["bands"].Rows
	if len(rows) == 0 {
		add(CodeTable, "bands.csv", "has no bands", "Add the bands")
	}
	type edge struct{ above, to string }
	byPeriod := map[string][]edge{}
	for i, row := range rows {
		at := fmt.Sprintf("bands.csv row %d", i+2)
		if !known[row[0]] {
			add(CodeValue, at, fmt.Sprintf("fund %q is not listed in funds.csv", row[0]), "Add the fund to funds.csv, or correct its name")
		}
		for c, name := range map[int]string{1: "wage_above", 2: "wage_to"} {
			if row[c] != "" && !amountTwoDP.MatchString(row[c]) {
				add(CodeValue, at, fmt.Sprintf("%s %q is not an amount with cents", name, row[c]), "Enter the wage with cents, such as 2000.00, or leave it blank")
			}
		}
		if row[1] != "" && row[2] != "" {
			lo, _, err1 := apd.NewFromString(row[1])
			hi, _, err2 := apd.NewFromString(row[2])
			if err1 != nil || err2 != nil || lo.Cmp(hi) >= 0 {
				add(CodeValue, at, fmt.Sprintf("the band above %s up to %s is empty", row[1], row[2]), "Enter the top above the bottom")
			}
		}
		if !amountTwoDP.MatchString(row[5]) || row[5] == "0.00" {
			add(CodeValue, at, fmt.Sprintf("amount %q is not an amount above zero with cents", row[5]), "Enter the monthly contribution, such as 1.50")
		}
		key := row[0] + " " + row[3] + " " + row[4]
		byPeriod[key] = append(byPeriod[key], edge{row[1], row[2]})
	}
	periods(add, "bands.csv", rows, func(r []string) string { return r[0] + " above " + r[1] + " to " + r[2] }, 3, 4)
	// Bands of one fund and period chain: the first from nothing, each starting where the last ended, the last unlimited.
	for key, bands := range byPeriod {
		next, first, last := "", true, false
		for _, b := range bands {
			switch {
			case last:
				add(CodeValue, "bands.csv "+key, "has a band after the one without a limit", "End the fund's bands with the band without an upper limit")
			case b.above != next && !(first && b.above == ""):
				add(CodeValue, "bands.csv "+key, fmt.Sprintf("the band above %q does not start where the one before ended (%q)", b.above, next),
					"List the bands from the lowest wages up, each starting where the one before ended")
			}
			first, next, last = false, b.to, b.to == ""
		}
		if !last {
			add(CodeValue, "bands.csv "+key, "has no band without an upper limit", "End the fund's bands with the band for wages above the last limit")
		}
	}
	return out
}

// runSHG runs the sg-shg pinned cases; "none" where the version must not hold a figure.
func runSHG(v Version) []Result {
	if v.Pack != "sg-shg" {
		return nil
	}
	var out []Result
	for _, row := range v.Pinned["bands"].Rows {
		r := Result{Table: "bands", Case: row[0] + " " + row[1] + " " + row[2] + " " + row[3], Why: row[5], Want: row[4]}
		m, err1 := time.ParseInLocation(monthFormat, row[1], time.UTC)
		w, _, err2 := apd.NewFromString(row[3])
		if err1 != nil || err2 != nil {
			r.Got = fmt.Sprintf("the month %q or the wages %q do not parse", row[1], row[3])
			out = append(out, r)
			continue
		}
		d, err := SHGOn(v, m, row[2], w)
		switch {
		case errors.Is(err, ErrNotHeld):
			r.Got, r.Pass = "none", row[4] == "none"
		case err != nil:
			r.Got = err.Error()
		default:
			r.Got = d.Text('f')
			r.Pass = sameDecimal(r.Got, row[4])
		}
		out = append(out, r)
	}
	return out
}
