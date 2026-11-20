// Package forecastaccuracy compares a week's kept cash projection with the cash that came (PRD-001 FR-PLN-04; p2-reporting-planning design D12).
//
// A snapshot holds the projection as it stood: the money accounts' balance, its 13 weeks and every expected movement. Four and thirteen weeks later the
// cash at the window's end is known, and the difference between it and the projected balance is split by cause:
//
//   - receipts later than expected: an invoice the projection expected in the window that was not settled by its end (its amount is missing from the cash);
//   - timing: a bill the projection expected in the window that was not paid by its end (the cash is higher than projected), and an invoice or bill expected
//     after the window that was settled inside it (earlier than projected);
//   - unplanned: the rest, so the three always add to the difference exactly: spend and receipts the projection did not hold, and anything else that moved cash.
//
// Only invoices and bills have a settlement day, so recurring occurrences and payment batches fall into the remainder. All amounts are exact decimals, in
// the snapshot's currency, signed as the projection holds them: money in above zero, money out below.
package forecastaccuracy

import (
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// Horizons are the weeks ahead the accuracy is shown at.
var Horizons = []int{4, 13}

// Week is a week of a snapshot.
type Week struct {
	From  string `json:"from"`
	To    string `json:"to"`
	In    string `json:"in"`
	Out   string `json:"out"`
	Close string `json:"close"`
}

// Item is an expected movement of a snapshot.
type Item struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	ExpectedOn string `json:"expected_on"`
	Amount     string `json:"amount"`
}

// Projection is what a snapshot keeps.
type Projection struct {
	Today   string `json:"today"`
	Opening string `json:"opening"`
	Weekly  []Week `json:"weekly"`
	Items   []Item `json:"items"`
}

// Causes split a difference.
type Causes struct {
	ReceiptsLater string `json:"receipts_later"`
	Timing        string `json:"timing"`
	Unplanned     string `json:"unplanned"`
}

// Result is a snapshot's accuracy at a horizon.
type Result struct {
	Weeks     int    `json:"weeks"`
	WindowEnd string `json:"window_end"`
	Projected string `json:"projected"`
	Actual    string `json:"actual"`
	// Difference is actual - projected: below zero, less cash came than the projection said.
	Difference string `json:"difference"`
	Causes     Causes `json:"causes"`
}

var ctx = apd.BaseContext.WithPrecision(34)

func dec(s string) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil || d.Form != apd.Finite {
		return nil, fmt.Errorf("%q is not a decimal", s)
	}
	return d, nil
}

// Compare works out a snapshot's accuracy at a horizon of weeks, given the actual cash at the window's end and the days (by document ID) on which each invoice
// and bill was settled in full on or after the snapshot's day. The window is the horizon's week of the snapshot; a snapshot with fewer weeks has no such window.
func Compare(p Projection, weeks int, actualClose string, settledOn map[string]time.Time) (Result, error) {
	if weeks < 1 || weeks > len(p.Weekly) {
		return Result{}, fmt.Errorf("the snapshot holds %d weeks, not %d", len(p.Weekly), weeks)
	}
	w := p.Weekly[weeks-1]
	end, err := time.Parse(time.DateOnly, w.To)
	if err != nil {
		return Result{}, fmt.Errorf("week %d ends %q", weeks, w.To)
	}
	projected, err := dec(w.Close)
	if err != nil {
		return Result{}, err
	}
	actual, err := dec(actualClose)
	if err != nil {
		return Result{}, err
	}
	diff := new(apd.Decimal)
	if _, err := ctx.Sub(diff, actual, projected); err != nil {
		return Result{}, err
	}
	zero := func() *apd.Decimal { return new(apd.Decimal).SetFinite(0, projected.Exponent) } // so that a zero shows the scale of the figures
	later, timing := zero(), zero()
	for _, it := range p.Items {
		if it.Kind != "invoice" && it.Kind != "bill" {
			continue
		}
		on, err := time.Parse(time.DateOnly, it.ExpectedOn)
		if err != nil {
			return Result{}, fmt.Errorf("item %s is expected on %q", it.Key, it.ExpectedOn)
		}
		amt, err := dec(it.Amount)
		if err != nil {
			return Result{}, err
		}
		settled, ok := settledOn[it.ID]
		settledInside := ok && !settled.After(end)
		switch {
		case !on.After(end) && !settledInside: // expected in the window and not settled by its end: the projected amount is missing from the cash
			missing := new(apd.Decimal).Neg(amt)
			if it.Kind == "invoice" {
				_, err = ctx.Add(later, later, missing)
			} else {
				_, err = ctx.Add(timing, timing, missing)
			}
		case on.After(end) && settledInside: // expected after the window and settled inside it: earlier than projected
			_, err = ctx.Add(timing, timing, amt)
		}
		if err != nil {
			return Result{}, err
		}
	}
	unplanned := new(apd.Decimal)
	if _, err := ctx.Sub(unplanned, diff, later); err != nil {
		return Result{}, err
	}
	if _, err := ctx.Sub(unplanned, unplanned, timing); err != nil {
		return Result{}, err
	}
	return Result{Weeks: weeks, WindowEnd: w.To, Projected: projected.Text('f'), Actual: actual.Text('f'), Difference: diff.Text('f'),
		Causes: Causes{ReceiptsLater: later.Text('f'), Timing: timing.Text('f'), Unplanned: unplanned.Text('f')}}, nil
}
