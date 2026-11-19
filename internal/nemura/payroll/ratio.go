package payroll

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

// RatioName is the policy of PRD-002 NM-PAY-12: an amount worked out from a salary by a ratio of days or hours is computed exactly and rounded once to the cent, half away from zero.
const RatioName = "RND-PAY-RATIO"

// roundCents is RND-PAY-RATIO's one rounding step.
func roundCents(d *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	c := work
	c.Rounding = apd.RoundHalfUp
	if _, err := c.Quantize(out, d, -2); err != nil {
		panic(fmt.Sprintf("payroll: %s: %v", RatioName, err))
	}
	return out
}

// Ratio is amount × num ÷ den (RND-PAY-RATIO): a part month's pay (num calendar days of den in the month) or an unpaid-leave deduction (num days of den working days). The product is
// exact, the quotient is worked to 60 digits, and the cent is rounded once. A zero denominator is a programming error and panics.
func Ratio(amount *apd.Decimal, num, den int) *apd.Decimal {
	if den <= 0 {
		panic("payroll: a ratio needs a denominator above zero")
	}
	return roundCents(quo(mul(amount, apd.New(int64(num), 0)), apd.New(int64(den), 0)))
}

// OvertimePay is the pay for hours of overtime (RND-PAY-RATIO): the hourly basic rate is twelve months of basic salary over fifty-two weeks of the normal hours, the pay is that rate
// times the pack's multiple times the hours, and the cent is rounded once (Employment Act, Part IV; the accountant confirms the method).
func OvertimePay(monthly, weeklyHours, multiple, hours *apd.Decimal) *apd.Decimal {
	num := mul(mul(mul(monthly, apd.New(12, 0)), multiple), hours)
	den := mul(apd.New(52, 0), weeklyHours)
	return roundCents(quo(num, den))
}

// RatioDays is amount × days ÷ den for a number of days that may be a half (an unpaid half day), under the same single rounding.
func RatioDays(amount, days *apd.Decimal, den int) *apd.Decimal {
	if den <= 0 {
		panic("payroll: a ratio needs a denominator above zero")
	}
	return roundCents(quo(mul(amount, days), apd.New(int64(den), 0)))
}

// PartMonthMethod is how a part month is counted, as a named parameter (NM-PAY-12): the PRD counts the calendar days a pay record covers over the days of the month. MOM's page counts working days; which
// the company follows is an owner decision that is pending, so the method is not changed here: only named, so that the day the decision is made one place changes and one golden test (the adapter's
// `TestAPartMonthIsCountedByCalendarDays`) says what it was.
const PartMonthMethod = "calendar_days"
