package planning

import (
	"errors"
	"fmt"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The forecast's arithmetic (p1-planning design D2): sums and products are exact, at a precision wide enough for 60
// months of compounding, and an operation that would lose a digit fails rather than rounds. Division happens only as a
// fraction, divided once when it is rounded under RND-PLAN.

// CodeDigits is the error of a forecast whose exact figures outgrow the engine's precision.
const CodeDigits = "PLAN-09"

// ErrDigits is returned when an exact figure would need more digits than the engine keeps.
var ErrDigits = errors.New(CodeDigits + ": the forecast's exact figures need more digits than the engine keeps; use fewer decimals in the rates")

var exact = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(4000)
	c.Traps |= apd.Inexact
	return c
}()

// calc works one model: it keeps the first error, so the formulas read as formulas.
type calc struct {
	cur money.Currency
	err error
}

func (c *calc) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// check keeps an operation's error: a lost digit is ErrDigits.
func (c *calc) check(cond apd.Condition, err error) {
	if err == nil {
		return
	}
	if cond&apd.Inexact != 0 {
		err = ErrDigits
	}
	c.fail(err)
}

func d(i int64) *apd.Decimal { return apd.New(i, 0) }

func (c *calc) add(xs ...*apd.Decimal) *apd.Decimal {
	out := apd.New(0, 0)
	for _, x := range xs {
		c.check(exact.Add(out, out, x))
	}
	return out
}

func (c *calc) sub(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	c.check(exact.Sub(out, a, b))
	return out
}

func (c *calc) mul(xs ...*apd.Decimal) *apd.Decimal {
	out := apd.New(1, 0)
	for _, x := range xs {
		c.check(exact.Mul(out, out, x))
	}
	return out
}

func (c *calc) neg(x *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal).Neg(x)
	out.Negative = out.Negative && !out.IsZero()
	return out
}

// pow is x to the power n, n ≥ 0, by repeated multiplication.
func (c *calc) pow(x *apd.Decimal, n int) *apd.Decimal {
	out := apd.New(1, 0)
	for range max(0, n) {
		c.check(exact.Mul(out, out, x))
	}
	return out
}

// grow is 1 + rate.
func (c *calc) grow(rate *apd.Decimal) *apd.Decimal { return c.add(d(1), rate) }

func max0(x *apd.Decimal) *apd.Decimal {
	if x.Negative && !x.IsZero() {
		return apd.New(0, 0)
	}
	return x
}

func minOf(a, b *apd.Decimal) *apd.Decimal {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}

// round is RND-PLAN's amount: round(x, the base currency's minor units), once.
func (c *calc) round(x *apd.Decimal) *apd.Decimal { return rounding.Plan(x, c.cur) }

// Fraction is an exact fraction, num ÷ den, for the model's divisions: a ratio is kept as one until it is shown.
type Fraction struct{ num, den *apd.Decimal }

func whole(x *apd.Decimal) Fraction { return Fraction{x, d(1)} }

func (c *calc) over(num, den *apd.Decimal) Fraction { return Fraction{num, den} }

func (c *calc) addFrac(a, b Fraction) Fraction {
	return Fraction{c.add(c.mul(a.num, b.den), c.mul(b.num, a.den)), c.mul(a.den, b.den)}
}

// roundFrac is RND-PLAN's amount of a fraction, divided once.
func (c *calc) roundFrac(f Fraction) *apd.Decimal {
	q, err := rounding.PlanQuotient(f.num, f.den, c.cur.MinorUnits)
	if err != nil {
		c.fail(err)
		return apd.New(0, 0)
	}
	return q
}

// value is a fraction to places decimals, for a ratio or a rate shown.
func (c *calc) value(f Fraction, places int) *apd.Decimal {
	q, err := rounding.PlanQuotient(f.num, f.den, places)
	if err != nil {
		c.fail(err)
		return apd.New(0, 0)
	}
	return q
}

// sign is the sign of a fraction.
func (f Fraction) sign() int { return f.num.Sign() * f.den.Sign() }

// cmp compares two fractions exactly.
func (c *calc) cmp(a, b Fraction) int {
	l, r := c.mul(a.num, b.den), c.mul(b.num, a.den)
	if a.den.Sign()*b.den.Sign() < 0 {
		return r.Cmp(l)
	}
	return l.Cmp(r)
}

func (f Fraction) String() string { return fmt.Sprintf("%s/%s", f.num.Text('f'), f.den.Text('f')) }

// Round is the fraction to places decimals under RND-PLAN: divided once, half away from zero.
func (f Fraction) Round(places int) (*apd.Decimal, error) {
	return rounding.PlanQuotient(f.num, f.den, places)
}

// Percent is the fraction as a percentage under RND-PERCENT: 1 decimal, with the numerator's sign over the size of the
// denominator.
func (f Fraction) Percent() (*apd.Decimal, error) {
	den := new(apd.Decimal).Abs(f.den)
	num := new(apd.Decimal)
	if _, err := exact.Mul(num, f.num, apd.New(100, 0)); err != nil {
		return nil, err
	}
	return rounding.PlanQuotient(num, den, 1)
}
