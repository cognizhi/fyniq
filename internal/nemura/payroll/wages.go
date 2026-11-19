// Package payroll is Nemura's payroll calculation (PRD-002 ADR-N2, NM-PAY-01; PRD-001 §8.7 and Appendix B.3): the employee-month, exact, from wage items to net pay and the
// amount to disburse, with wage classes by CPF Board's facts, CPF by residency, age band and permanent-resident year, the ordinary and additional wage ceilings and the year to
// date, SDL, the self-help group funds and the deductions MOM allows. It is pure: no clock, storage or network. The statutory values come from the packs the caller passes, the
// rounding is the named policies of internal/domain/rounding, and every figure carries the working it came from.
package payroll

import (
	"time"

	"github.com/cockroachdb/apd/v3"
)

// Classes of a pay item for CPF (FR-PAY-17).
const (
	Ordinary   = "ordinary"
	Additional = "additional"
	NotWages   = "none" // not CPF wages
)

// Item is one pay item of a wage month: what it is, how much, the employment period it is for, the day it is payable and whether it is CPF wages. A deduction from pay for
// unpaid leave is an item with a negative amount.
type Item struct {
	Key       string       // links the item to its source
	Name      string       // as the payslip shows it
	Nature    string       // basic, allowance, overtime, bonus, commission, unpaid_leave, other: only a suggestion of the class
	Amount    *apd.Decimal // negative for unpaid leave
	ForMonth  time.Time    // the first day of the month of employment the item is for; the zero value means the wage month
	PayableOn time.Time    // the day the item is payable; the zero value means the pay day of the month
	CPFWages  bool         // whether the item is CPF wages at all
	Suggested string       // the class the item's name suggests, if the caller has one
}

// Classed is an item with its class by the facts and whether the facts disagree with the name.
type Classed struct {
	Item
	Class    string
	Mismatch bool
	Why      string
}

// firstOf is the first day of the month d is in.
func firstOf(d time.Time) time.Time { return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC) }

// Classify is FR-PAY-17: an item is ordinary wages when it is for the month's employment and payable by the 14th of the following month, and additional wages otherwise; a name
// only suggests a default, and a name that disagrees with the facts is reported. An item that is not CPF wages is neither. payDay is the pay day of the month, for an item that
// names no date of its own.
func Classify(it Item, month, payDay time.Time) Classed {
	c := Classed{Item: it}
	if !it.CPFWages {
		c.Class = NotWages
		c.Why = "It is not CPF wages."
		return c
	}
	forMonth, payable := it.ForMonth, it.PayableOn
	if forMonth.IsZero() {
		forMonth = month
	}
	if payable.IsZero() {
		payable = payDay
	}
	deadline := time.Date(month.Year(), month.Month()+1, 14, 23, 59, 59, 0, time.UTC)
	switch {
	case firstOf(forMonth).Equal(firstOf(month)) && !payable.After(deadline):
		c.Class, c.Why = Ordinary, "It is for the month's employment and payable by the 14th of the following month."
	case !firstOf(forMonth).Equal(firstOf(month)):
		c.Class, c.Why = Additional, "It is not for the month's employment."
	default:
		c.Class, c.Why = Additional, "It is payable after the 14th of the following month."
	}
	if it.Suggested != "" && it.Suggested != c.Class {
		c.Mismatch = true
		c.Why += " Its name suggests " + it.Suggested + " wages: the facts decide."
	}
	return c
}

// ByFacts classifies every item of the month.
func ByFacts(items []Item, month, payDay time.Time) []Classed {
	out := make([]Classed, len(items))
	for i, it := range items {
		out[i] = Classify(it, month, payDay)
	}
	return out
}
