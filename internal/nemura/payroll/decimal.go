package payroll

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

// work is the exact context every sum and product is computed in; nothing here rounds: the named policies of internal/domain/rounding do, once.
var work = apd.BaseContext.WithPrecision(60)

// D reads a decimal string, naming what it was for. It is for tests and for values the engine's callers hold as strings.
func D(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(fmt.Sprintf("payroll: %q is not a decimal: %v", s, err))
	}
	return d
}

func zero() *apd.Decimal { return apd.New(0, 0) }

func add(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Add(out, a, b); err != nil {
		panic("payroll: add: " + err.Error())
	}
	return out
}

func sub(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Sub(out, a, b); err != nil {
		panic("payroll: subtract: " + err.Error())
	}
	return out
}

func mul(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Mul(out, a, b); err != nil {
		panic("payroll: multiply: " + err.Error())
	}
	return out
}

func quo(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Quo(out, a, b); err != nil {
		panic("payroll: divide: " + err.Error())
	}
	return out
}

func minD(a, b *apd.Decimal) *apd.Decimal {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}

func maxD(a, b *apd.Decimal) *apd.Decimal {
	if a.Cmp(b) >= 0 {
		return a
	}
	return b
}

// nonNeg is d, or zero when d is below it.
func nonNeg(d *apd.Decimal) *apd.Decimal { return maxD(d, zero()) }

// cents shows an amount with two decimals, the way a payslip and a posting carry money.
func cents(d *apd.Decimal) string {
	out := new(apd.Decimal)
	c := work
	c.Rounding = apd.RoundHalfUp
	if _, err := c.Quantize(out, d, -2); err != nil {
		panic("payroll: quantize: " + err.Error())
	}
	return out.Text('f')
}
