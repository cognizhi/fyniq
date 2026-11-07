package pack

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/cockroachdb/apd/v3"
)

// The sg-cit pack (PRD-001 §10.6, FR-PLN-03, FR-PLN-06; p1-planning design D1): the corporate income tax rate, the
// start-up and partial tax exemptions and each Year of Assessment's rebate, all chosen by YA. The figures are exact:
// the caller rounds the tax it shows once (RND-PLAN).

// The exemption schemes.
const (
	SchemeStartUp = "sute" // the tax exemption scheme for new start-up companies, a qualifying company's first 3 YAs
	SchemePartial = "pte"  // the partial tax exemption, every other company
)

// ErrNoYA is the answer for a Year of Assessment the version holds no figure for: a YA before its rows begin, or a YA
// without an announced rebate.
var ErrNoYA = errors.New("the pack holds no figure for the Year of Assessment")

// exact multiplies decimals without losing a digit: a product that would need rounding is an error, never rounded.
var exact = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(100)
	c.Traps |= apd.Inexact
	return c
}()

var (
	yearFormat  = regexp.MustCompile(`^20[0-9]{2}$`)
	amountTwoDP = regexp.MustCompile(`^[0-9]{1,12}\.[0-9]{2}$`)
)

// inYA reports whether ya falls between from and to, a blank to having no end.
func inYA(ya int, from, to string) bool {
	f, err := strconv.Atoi(from)
	if err != nil || ya < f {
		return false
	}
	if to == "" {
		return true
	}
	t, err := strconv.Atoi(to)
	return err == nil && ya <= t
}

// CITRate is the rate on normal chargeable income for the YA, as a fraction.
func CITRate(v Version, ya int) (*apd.Decimal, error) {
	for _, row := range v.Tables["rates"].Rows {
		if inYA(ya, row[0], row[1]) {
			d, _, err := apd.NewFromString(row[2])
			return d, err
		}
	}
	return nil, ErrNoYA
}

// CITExemption is the amount of chargeable income the scheme exempts in the YA: each tier's percentage of the band
// that follows the tiers before it, exactly.
func CITExemption(v Version, scheme string, ya int, chargeable *apd.Decimal) (*apd.Decimal, error) {
	left := new(apd.Decimal).Set(chargeable)
	if left.Negative {
		left.SetInt64(0)
	}
	out := apd.New(0, 0)
	found := false
	for _, row := range v.Tables["exemptions"].Rows {
		if row[0] != scheme || !inYA(ya, row[1], row[2]) {
			continue
		}
		found = true
		band, _, err1 := apd.NewFromString(row[4])
		pct, _, err2 := apd.NewFromString(row[5])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("sg-cit exemptions: %s tier %s does not read", scheme, row[3])
		}
		part := band
		if left.Cmp(band) < 0 {
			part = new(apd.Decimal).Set(left)
		}
		var exempt apd.Decimal
		if _, err := exact.Mul(&exempt, part, pct); err != nil {
			return nil, err
		}
		if _, err := exact.Add(out, out, &exempt); err != nil {
			return nil, err
		}
		if _, err := exact.Sub(left, left, part); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, ErrNoYA
	}
	return out, nil
}

// CITRebate is the rebate the YA gives on the tax payable after tax set-offs, as IRAS computes it: the percentage of
// the tax, at most the cap; for a company eligible for the cash grant, nothing when that is not more than the grant,
// else the capped rebate less the grant, since the grant is paid instead of the rebate's first part.
func CITRebate(v Version, ya int, taxPayable *apd.Decimal, cashGrantEligible bool) (*apd.Decimal, error) {
	for _, row := range v.Tables["rebates"].Rows {
		if row[0] != strconv.Itoa(ya) {
			continue
		}
		pct, _, err1 := apd.NewFromString(row[1])
		capped, _, err2 := apd.NewFromString(row[2])
		grant, _, err3 := apd.NewFromString(row[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("sg-cit rebates: YA %d does not read", ya)
		}
		rebate := new(apd.Decimal)
		if taxPayable.Sign() > 0 {
			if _, err := exact.Mul(rebate, taxPayable, pct); err != nil {
				return nil, err
			}
		}
		atMost := rebate
		if rebate.Cmp(capped) > 0 {
			atMost = capped
		}
		if !cashGrantEligible || grant.Sign() == 0 {
			return new(apd.Decimal).Set(atMost), nil
		}
		if rebate.Cmp(grant) <= 0 {
			return apd.New(0, 0), nil
		}
		out := new(apd.Decimal)
		if _, err := exact.Sub(out, atMost, grant); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, ErrNoYA
}

// checkCIT checks the sg-cit tables: YAs as years in order, rates and percentages as fractions, each scheme's tiers
// numbered from 1 for its YAs with bands of whole cents, and rebates once per YA with a cap and a grant in cents.
func checkCIT(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	years := func(at, from, to string) {
		if !yearFormat.MatchString(from) {
			add(CodeDates, at, fmt.Sprintf("ya_from %q is not a year", from), "Enter the first Year of Assessment, such as 2020")
		}
		if to != "" && (!yearFormat.MatchString(to) || to < from) {
			add(CodeDates, at, fmt.Sprintf("ya_to %q is not a year on or after ya_from", to), "Enter the last Year of Assessment, or leave it blank")
		}
	}
	for i, row := range v.Tables["rates"].Rows {
		at := fmt.Sprintf("rates.csv row %d", i+2)
		years(at, row[0], row[1])
		if _, err := fraction(row[2]); err != nil {
			add(CodeValue, at, fmt.Sprintf("rate %q %v", row[2], err), "Write the rate as a fraction, such as 0.17 for 17%")
		}
	}
	if len(v.Tables["rates"].Rows) == 0 {
		add(CodeTable, "rates.csv", "has no rates", "Add the rate")
	}
	next := map[string]int{}
	for i, row := range v.Tables["exemptions"].Rows {
		at := fmt.Sprintf("exemptions.csv row %d", i+2)
		if row[0] != SchemeStartUp && row[0] != SchemePartial {
			add(CodeValue, at, fmt.Sprintf("scheme %q is not sute or pte", row[0]), "Enter sute for the start-up exemption or pte for the partial exemption")
		}
		years(at, row[1], row[2])
		key := row[0] + " " + row[1]
		if row[3] != strconv.Itoa(next[key]+1) {
			add(CodeValue, at, fmt.Sprintf("tier %q does not follow tier %d", row[3], next[key]), "Number each scheme's tiers from 1, in order")
		}
		next[key]++
		if !amountTwoDP.MatchString(row[4]) || row[4] == "0.00" {
			add(CodeValue, at, fmt.Sprintf("band %q is not an amount above zero with cents", row[4]), "Enter the band, such as 100000.00")
		}
		if _, err := fraction(row[5]); err != nil {
			add(CodeValue, at, fmt.Sprintf("percent %q %v", row[5], err), "Write the percentage exempted as a fraction, such as 0.75")
		}
	}
	seen := map[string]bool{}
	for i, row := range v.Tables["rebates"].Rows {
		at := fmt.Sprintf("rebates.csv row %d", i+2)
		if !yearFormat.MatchString(row[0]) || seen[row[0]] {
			add(CodeDates, at, fmt.Sprintf("ya %q is not a year, or is repeated", row[0]), "Give each Year of Assessment one row")
		}
		seen[row[0]] = true
		if _, err := fraction(row[1]); err != nil {
			add(CodeValue, at, fmt.Sprintf("percent %q %v", row[1], err), "Write the rebate as a fraction of the tax, such as 0.50")
		}
		if !amountTwoDP.MatchString(row[2]) || !amountTwoDP.MatchString(row[3]) {
			add(CodeValue, at, fmt.Sprintf("cap %q or cash_grant %q is not an amount with cents", row[2], row[3]), "Enter both with cents, such as 40000.00 and 2000.00")
		}
	}
	return out
}

// runCIT runs the sg-cit pinned cases: each YA's rate, each scheme's exempt amount and each rebate; "none" where the
// version must not hold one.
func runCIT(v Version) []Result {
	if v.Pack != "sg-cit" {
		return nil
	}
	var out []Result
	answer := func(r *Result, d *apd.Decimal, err error) {
		if err != nil {
			r.Got, r.Pass = "none", r.Want == "none"
			if !errors.Is(err, ErrNoYA) {
				r.Got = err.Error()
			}
			return
		}
		r.Got = d.Text('f')
		r.Pass = sameDecimal(r.Got, r.Want)
	}
	ya := func(r *Result, s string) (int, bool) {
		n, err := strconv.Atoi(s)
		if err != nil {
			r.Got = fmt.Sprintf("the YA %q does not parse", s)
		}
		return n, err == nil
	}
	amount := func(r *Result, s string) (*apd.Decimal, bool) {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			r.Got = fmt.Sprintf("the amount %q does not parse", s)
		}
		return d, err == nil
	}
	for _, row := range v.Pinned["rates"].Rows {
		r := Result{Table: "rates", Case: row[0] + " YA " + row[1], Why: row[3], Want: row[2]}
		if y, ok := ya(&r, row[1]); ok {
			d, err := CITRate(v, y)
			answer(&r, d, err)
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["exemptions"].Rows {
		r := Result{Table: "exemptions", Case: row[0] + " " + row[1] + " YA " + row[2], Why: row[5], Want: row[4]}
		if y, ok := ya(&r, row[2]); ok {
			if c, ok := amount(&r, row[3]); ok {
				d, err := CITExemption(v, row[1], y, c)
				answer(&r, d, err)
			}
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["rebates"].Rows {
		r := Result{Table: "rebates", Case: row[0] + " YA " + row[1], Why: row[5], Want: row[4]}
		if y, ok := ya(&r, row[1]); ok {
			if t, ok := amount(&r, row[2]); ok {
				d, err := CITRebate(v, y, t, row[3] == "true")
				answer(&r, d, err)
			}
		}
		out = append(out, r)
	}
	return out
}
