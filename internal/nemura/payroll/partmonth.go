package payroll

import (
	"time"

	"github.com/cockroachdb/apd/v3"
)

// PartMonthMethod is how a part month is counted (NM-PAY-12, owner decision of 21 Nov 2026): the working days of the person's working pattern the pay record covers over the working days of the month,
// which is MOM's method for an incomplete month: "Monthly gross rate of pay / Total number of working days in that month x Total number of days the employee actually worked in that month", working days
// excluding rest days and non-working days and including public holidays (https://www.mom.gov.sg/employment-practices/salary/monthly-and-daily-salary, retrieved 21 Nov 2026). It replaces the calendar
// days of the PRD's first text of NM-PAY-12, which the PRD keeps marked superseded. It is a named parameter so that one place says which method is in force.
const PartMonthMethod = "working_days"

// WorkingDays counts the days from one day to another, both included, on which a person works by their working pattern: a rest day or a non-working day is not counted and a public holiday is (MOM).
func WorkingDays(works func(time.Weekday) bool, from, to time.Time) int {
	n := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if works(d.Weekday()) {
			n++
		}
	}
	return n
}

// PartMonthPay is an amount for the part of a month a person is employed: amount × the working days from `from` to `to` ÷ the working days of the month, worked out exactly and rounded once to the cent
// (RND-PAY-RATIO). A span with no working day, or a month with none, is nothing.
func PartMonthPay(amount *apd.Decimal, works func(time.Weekday) bool, month, from, to time.Time) *apd.Decimal {
	first := firstOf(month)
	last := first.AddDate(0, 1, -1)
	if from.Before(first) {
		from = first
	}
	if to.After(last) {
		to = last
	}
	den := WorkingDays(works, first, last)
	n := 0
	if !to.Before(from) {
		n = WorkingDays(works, from, to)
	}
	if den == 0 || n == 0 {
		return roundCents(zero())
	}
	return Ratio(amount, n, den)
}
