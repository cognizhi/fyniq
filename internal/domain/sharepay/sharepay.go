// Package sharepay is the expense of shares issued for services (PRD-001 FR-EQ-13; p2-reporting-planning design D15): equity-settled share-based payment under SFRS(I) 2 /
// IFRS 2 and section 26 of the SFRS for Small Entities `[accountant to confirm]`. A grant has a fair value per share, measured at the grant date and never remeasured, and tranches
// of whole shares, each with the day it vests. A tranche's service period runs from the grant's service start (the first day of a month) to the month its vest day falls in, in whole
// calendar months, both counted. Its total expense is its shares times the fair value, rounded once (RND-LINE), spread over its months by cumulative rounding (ALLOC-EVEN), so the
// months add to the total exactly; a month's expense is the tranches' parts for it. The month's expense is debited to share-based payment expense and credited to the share-based payment
// reserve, in equity. No tax deduction is claimed for newly issued shares. A forfeiture before vesting, a non-market condition's true-up and a cash alternative are not worked here.
package sharepay

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Stable codes (ENG-23).
const (
	CodeGrant   = "SBP-01" // the grant's fair value, service start or tranches are not usable
	CodeTranche = "SBP-02" // a tranche has no shares, or vests before its service starts
)

// Error is a refusal in plain words.
type Error struct{ Code, Field, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Message + " (" + e.Fix + ")" }

// Tranche is some of a grant's shares and the day they vest.
type Tranche struct {
	Shares int64
	VestOn time.Time
}

// TrancheExpense is a tranche's service months, its total expense and the part of each month.
type TrancheExpense struct {
	Months int
	Total  money.Money
	Parts  []money.Money
}

// Month is one calendar month's expense: the first day of the month and the sum of the tranches' parts.
type Month struct {
	Month   time.Time
	Expense money.Money
}

// Expense is a grant's schedule.
type Expense struct {
	Tranches []TrancheExpense
	Months   []Month
	Total    money.Money
}

// MonthsBetween is the whole calendar months from the month of start to the month of vestOn, both counted.
func MonthsBetween(start, vestOn time.Time) int {
	return (vestOn.Year()-start.Year())*12 + int(vestOn.Month()) - int(start.Month()) + 1
}

// Work is the schedule of a grant with a fair value per share, a service start and tranches. The fair value is above nothing, the service starts on the first day of a month, there is
// a tranche, and each has at least one share and vests in or after the month the service starts.
func Work(fairValue money.UnitPrice, serviceStart time.Time, tranches []Tranche) (Expense, error) {
	cur := fairValue.Currency()
	if fairValue.Decimal().Sign() <= 0 {
		return Expense{}, &Error{CodeGrant, "fair_value", "the fair value per share is not above nothing", "Enter the fair value at the grant date, from the valuation"}
	}
	if serviceStart.Day() != 1 {
		return Expense{}, &Error{CodeGrant, "service_start", "the service starts on " + serviceStart.Format("2 Jan 2006") + ", which is not the first day of a month", "Start the service on the first day of a month"}
	}
	if len(tranches) == 0 {
		return Expense{}, &Error{CodeGrant, "tranches", "the grant has no tranche", "Add the shares and the day they vest"}
	}
	out := Expense{Total: money.Zero(cur)}
	byMonth := map[int]money.Money{}
	last := 0
	for i, t := range tranches {
		if t.Shares < 1 {
			return Expense{}, &Error{CodeTranche, fmt.Sprintf("tranche %d", i+1), fmt.Sprintf("tranche %d has no shares", i+1), "Give it a whole number of shares, at least one"}
		}
		n := MonthsBetween(serviceStart, t.VestOn)
		if n < 1 {
			return Expense{}, &Error{CodeTranche, fmt.Sprintf("tranche %d", i+1), fmt.Sprintf("tranche %d vests on %s, before its service starts in %s", i+1, t.VestOn.Format("2 Jan 2006"), serviceStart.Format("January 2006")),
				"Move the vest day to the month the service starts or later"}
		}
		q, err := money.ParseQuantity(fmt.Sprint(t.Shares), "shares")
		if err != nil {
			return Expense{}, err
		}
		total, err := rounding.Line(q, fairValue)
		if err != nil {
			return Expense{}, err
		}
		parts, err := rounding.AllocEven(total, n)
		if err != nil {
			return Expense{}, err
		}
		for k, p := range parts {
			cur, ok := byMonth[k]
			if !ok {
				cur = money.Zero(total.Currency())
			}
			if byMonth[k], err = cur.Add(p); err != nil {
				return Expense{}, err
			}
		}
		last = max(last, n)
		if out.Total, err = out.Total.Add(total); err != nil {
			return Expense{}, err
		}
		out.Tranches = append(out.Tranches, TrancheExpense{Months: n, Total: total, Parts: parts})
	}
	for k := 0; k < last; k++ {
		out.Months = append(out.Months, Month{Month: serviceStart.AddDate(0, k, 0), Expense: byMonth[k]})
	}
	return out, nil
}
