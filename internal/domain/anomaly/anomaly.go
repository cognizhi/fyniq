// Package anomaly finds the month a category's spend broke from its trend (PRD-001 FR-KPI-06; p2-reporting-planning design D10). The rule is
// the one the requirement names, spend above its trend by three standard deviations, worked out exactly: with n months of history, this month's
// spend s, their sum S and their sum of squares Q, the sample variance is (n*Q - S^2) / (n*(n-1)) and the spend is an anomaly when it is above
// the mean (n*s > S) and (n*s - S)^2 * (n-1) > 9 * n * (n*Q - S^2). There is no square root and no floating point, so the answer for a given set
// of amounts is the same on every machine. Two guards keep the rule honest: at least six months of history, and an excess over the mean of at
// least a tenth of the mean, so that a history of identical months does not raise an alert for a cent.
package anomaly

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

// MinHistory is the fewest months of history a trend is worked out from.
const MinHistory = 6

// Alert is a month that broke from its trend.
type Alert struct {
	Mean   *apd.Decimal // the history's mean
	Spend  *apd.Decimal // this month's spend
	Excess *apd.Decimal // the spend over the mean
}

var ctx = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(200)
	c.MaxExponent, c.MinExponent = 1000, -1000
	return c
}()

// Detect says whether spend is an anomaly against its history, the months before it, oldest first.
func Detect(history []*apd.Decimal, spend *apd.Decimal) (Alert, bool, error) {
	n := len(history)
	if n < MinHistory {
		return Alert{}, false, nil
	}
	var sum, squares apd.Decimal
	for _, h := range history {
		if _, err := ctx.Add(&sum, &sum, h); err != nil {
			return Alert{}, false, err
		}
		var sq apd.Decimal
		if _, err := ctx.Mul(&sq, h, h); err != nil {
			return Alert{}, false, err
		}
		if _, err := ctx.Add(&squares, &squares, &sq); err != nil {
			return Alert{}, false, err
		}
	}
	N := apd.New(int64(n), 0)
	var ns, diff apd.Decimal // n*s and n*s - S
	if _, err := ctx.Mul(&ns, N, spend); err != nil {
		return Alert{}, false, err
	}
	if _, err := ctx.Sub(&diff, &ns, &sum); err != nil {
		return Alert{}, false, err
	}
	if diff.Sign() <= 0 {
		return Alert{}, false, nil // not above the mean
	}
	// A tenth of the mean at least: 10 * (n*s - S) >= S.
	var tenfold apd.Decimal
	if _, err := ctx.Mul(&tenfold, &diff, apd.New(10, 0)); err != nil {
		return Alert{}, false, err
	}
	if tenfold.Cmp(&sum) < 0 {
		return Alert{}, false, nil
	}
	// (n*s - S)^2 * (n-1) > 9 * n * (n*Q - S^2)
	var lhs, rhs, nq, ss apd.Decimal
	if _, err := ctx.Mul(&lhs, &diff, &diff); err != nil {
		return Alert{}, false, err
	}
	if _, err := ctx.Mul(&lhs, &lhs, apd.New(int64(n-1), 0)); err != nil {
		return Alert{}, false, err
	}
	if _, err := ctx.Mul(&nq, N, &squares); err != nil {
		return Alert{}, false, err
	}
	if _, err := ctx.Mul(&ss, &sum, &sum); err != nil {
		return Alert{}, false, err
	}
	if _, err := ctx.Sub(&rhs, &nq, &ss); err != nil {
		return Alert{}, false, err
	}
	if _, err := ctx.Mul(&rhs, &rhs, apd.New(int64(9*n), 0)); err != nil {
		return Alert{}, false, err
	}
	if lhs.Cmp(&rhs) <= 0 {
		return Alert{}, false, nil
	}
	var mean, excess apd.Decimal
	if _, err := ctx.Quo(&mean, &sum, N); err != nil {
		return Alert{}, false, fmt.Errorf("anomaly: %w", err)
	}
	if _, err := ctx.Sub(&excess, spend, &mean); err != nil {
		return Alert{}, false, err
	}
	var m, x apd.Decimal
	_, _, _ = ctx.Reduce(&m, &mean)
	_, _, _ = ctx.Reduce(&x, &excess)
	return Alert{Mean: &m, Spend: spend, Excess: &x}, true, nil
}
