package pack

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// The sg-tax-filings pack (PRD-001 FR-AR-11, FR-TAX-06; p2-tax-filings design D2 and D3): the periods of GST bad debt relief and the revenue limits that decide
// which form a company files for corporate income tax. A pure library: no clock, no I/O.

// The statuses of a bad debt's relief.
const (
	ReliefNotYet   = "not_yet"  // the twelve months have not passed and the debtor is not insolvent: no claim yet
	ReliefEligible = "eligible" // a claim may be made, until ClaimBy
	ReliefExpired  = "expired"  // the claim is out of time
)

func init() {
	Schemas["sg-tax-filings"] = Schema{
		Tables: map[string][]string{
			"relief": {"rule", "value", "effective_from", "effective_to", "source"},
			"forms":  {"form", "max_revenue", "source"},
		},
		Pinned: map[string][]string{
			"relief": {"case", "supply", "written_off", "insolvent", "today", "want_status", "want_eligible_from", "want_claim_by", "why", "source"},
			"forms":  {"case", "revenue", "want_form", "want_optional", "why", "source"},
		},
	}
}

// Relief is a written-off debt's GST bad debt relief by the dates: the day a claim may be made from, the last day to make it, and where it stands.
type Relief struct {
	Status       string
	EligibleFrom time.Time
	ClaimBy      time.Time
}

// addMonths moves a day forward by whole months, keeping a month's end a month's end (31 Jan and one month on is 28 or 29 Feb).
func addMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(first.Year(), first.Month(), min(d, last), 0, 0, 0, 0, time.UTC)
}

func reliefRule(v Version, rule string, on time.Time) (int, bool) {
	for _, r := range v.Tables["relief"].Rows {
		if r[0] != rule {
			continue
		}
		from, err := ParseDate(r[2])
		if err != nil || on.Before(from) {
			continue
		}
		if r[3] != "" {
			if to, err := ParseDate(r[3]); err != nil || on.After(to) {
				continue
			}
		}
		n, err := strconv.Atoi(r[1])
		return n, err == nil
	}
	return 0, false
}

// BadDebtRelief says when GST bad debt relief on a supply written off on writtenOff may be claimed, and until when, and where it stands on today (FR-AR-11). A claim may be made
// when twelve months beginning with the date of supply have elapsed, or the debtor became insolvent before that, and the debt has been written off; it must be made within five
// years from the date of supply (seven for a supply made before 1 January 2007). The five years end the day before the anniversary [accountant to confirm]. The other
// conditions of the claim (reasonable steps to recover the debt, open market value, goods whose ownership passed) are the person's declaration, not a date.
func BadDebtRelief(v Version, supply, writtenOff time.Time, insolvent *time.Time, today time.Time) (Relief, error) {
	months, ok := reliefRule(v, "months_after_supply", supply)
	if !ok {
		return Relief{}, fmt.Errorf("%s holds no months for bad debt relief on a supply of %s", v.ID, supply.Format(time.DateOnly))
	}
	years, ok := reliefRule(v, "claim_years", supply)
	if !ok {
		return Relief{}, fmt.Errorf("%s holds no claim period for bad debt relief on a supply of %s", v.ID, supply.Format(time.DateOnly))
	}
	twelve := addMonths(supply, months)
	from := twelve
	if insolvent != nil && insolvent.Before(twelve) {
		from = writtenOff // insolvency before the twelve months have elapsed: no wait
	}
	if writtenOff.After(from) {
		from = writtenOff
	}
	by := addMonths(supply, 12*years).AddDate(0, 0, -1)
	r := Relief{EligibleFrom: from, ClaimBy: by}
	switch {
	case today.After(by):
		r.Status = ReliefExpired
	case today.Before(from):
		r.Status = ReliefNotYet
	default:
		r.Status = ReliefEligible
	}
	return r, nil
}

// FilingForm is the form a company with the annual revenue files for corporate income tax, and the one it may choose instead: a company of S$200,000 or less revenue may file
// Form C-S (Lite) or Form C-S, one of S$5 million or less Form C-S, a bigger one Form C. Other conditions, such as the rate its income is taxed at and a relief that bars Form C-S,
// are the tax agent's to confirm (FR-TAX-06).
func FilingForm(v Version, revenue string) (form, optional string, err error) {
	rev, _, perr := apd.NewFromString(revenue)
	if perr != nil || rev.Sign() < 0 {
		return "", "", fmt.Errorf("the revenue %q is not an amount", revenue)
	}
	rows := v.Tables["forms"].Rows
	for i, r := range rows {
		max, _, err := apd.NewFromString(r[1])
		if err != nil {
			return "", "", fmt.Errorf("%s: the limit of %s does not read", v.ID, r[0])
		}
		if rev.Cmp(max) <= 0 {
			form = r[0]
			if i+1 < len(rows) {
				optional = rows[i+1][0] // the next form up, which a company within the Lite limit may also file
			}
			if i != 0 {
				optional = ""
			}
			return form, optional, nil
		}
	}
	return "C", "", nil
}

// checkTaxFilings checks the sg-tax-filings tables: relief rules with a whole number of months or years, each rule's periods without an overlap, and form limits in
// cents rising from one form to the next.
func checkTaxFilings(dir string, v Version) Problems {
	var out Problems
	add := func(field, message, fix string) {
		out = append(out, Problem{CodeValue, dir + ": " + field, message, fix})
	}
	type span struct{ from, to time.Time }
	spans := map[string][]span{}
	for i, r := range v.Tables["relief"].Rows {
		at := fmt.Sprintf("relief.csv row %d", i+2)
		if r[0] != "months_after_supply" && r[0] != "claim_years" {
			add(at, fmt.Sprintf("the rule %q is not months_after_supply or claim_years", r[0]), "Use a rule the table's description names")
		}
		if n, err := strconv.Atoi(r[1]); err != nil || n < 1 || n > 100 {
			add(at, fmt.Sprintf("the value %q is not a whole number from 1 to 100", r[1]), "Write whole months or years")
		}
		from, err := ParseDate(r[2])
		if err != nil {
			add(at, fmt.Sprintf("the day %q is not YYYY-MM-DD", r[2]), "Write the first day a rule applies from")
			continue
		}
		to := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		if r[3] != "" {
			if to, err = ParseDate(r[3]); err != nil || to.Before(from) {
				add(at, fmt.Sprintf("the last day %q is not a day on or after the first", r[3]), "Leave it blank while the rule is current")
				continue
			}
		}
		for _, o := range spans[r[0]] {
			if !from.After(o.to) && !to.Before(o.from) {
				add(at, "its period overlaps another period of the rule "+r[0], "Give each rule one value on any day")
			}
		}
		spans[r[0]] = append(spans[r[0]], span{from, to})
	}
	var last *apd.Decimal
	for i, r := range v.Tables["forms"].Rows {
		at := fmt.Sprintf("forms.csv row %d", i+2)
		d, _, err := apd.NewFromString(r[1])
		if err != nil || !amountTwoDP.MatchString(strings.TrimSpace(r[1])) {
			add(at, fmt.Sprintf("the limit %q is not an amount in dollars and cents", r[1]), "Write it with two decimals")
			continue
		}
		if last != nil && d.Cmp(last) <= 0 {
			add(at, "the limits rise from one form to the next", "List the forms from the smallest limit up")
		}
		last = d
	}
	return out
}

// runTaxFilings runs the sg-tax-filings pinned cases.
func runTaxFilings(v Version) []Result {
	if v.Pack != "sg-tax-filings" {
		return nil
	}
	var out []Result
	for _, row := range v.Pinned["relief"].Rows {
		want := fmt.Sprintf("%s from %s by %s", row[5], row[6], row[7])
		r := Result{Table: "relief", Case: row[0], Why: row[8], Want: want}
		supply, e1 := ParseDate(row[1])
		wo, e2 := ParseDate(row[2])
		today, e3 := ParseDate(row[4])
		var ins *time.Time
		if row[3] != "" {
			d, e := ParseDate(row[3])
			if e != nil {
				e1 = e
			}
			ins = &d
		}
		if e1 != nil || e2 != nil || e3 != nil {
			r.Got = "a date does not parse"
			out = append(out, r)
			continue
		}
		rel, err := BadDebtRelief(v, supply, wo, ins, today)
		if err != nil {
			r.Got = err.Error()
		} else {
			r.Got = fmt.Sprintf("%s from %s by %s", rel.Status, rel.EligibleFrom.Format(time.DateOnly), rel.ClaimBy.Format(time.DateOnly))
			r.Pass = r.Got == want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["forms"].Rows {
		r := Result{Table: "forms", Case: row[0], Why: row[4], Want: strings.TrimSpace(row[2] + " " + row[3])}
		form, opt, err := FilingForm(v, row[1])
		if err != nil {
			r.Got = err.Error()
		} else {
			r.Got = strings.TrimSpace(form + " " + opt)
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	return out
}
