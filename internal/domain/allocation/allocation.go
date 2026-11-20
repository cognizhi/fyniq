// Package allocation is the calculation behind an allocation rule (PRD-001 FR-DIM-03; p2-reporting-planning design D15): a shared cost split over target
// dimension values by a key, under ALLOC-PRORATA (the largest-remainder method, ties to the earlier target), so the parts add to the whole exactly. What the key
// cannot place is its own last part, **unallocated**, and stays on the source account without a dimension value: it is never given to a target by rounding.
//
// The keys are three: fixed percentages (hundredths of a percent as weights), headcount (the people of each target department, those with no department being what
// the key cannot place) and usage (an imported quantity per target, the rows naming no target being what the key cannot place). Each reduces to whole-number weights,
// which this package checks; the split itself is the rounding package's.
package allocation

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Stable codes (ENG-23).
const (
	CodePercent  = "ALLOC-01" // a percentage that is not a plain decimal of at most two places above nothing, or percentages above 100
	CodeQuantity = "ALLOC-02" // a quantity that is not a plain decimal of at most six places, or is negative
	CodeWeights  = "ALLOC-03" // no weight to split by, or too many for the amount
)

// Error is a refusal in plain words.
type Error struct{ Code, Field, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Message + " (" + e.Fix + ")" }

// QuantityScale is how many decimals a usage quantity has at most; its weight is the quantity in millionths.
const QuantityScale = 6

// PercentTotal is 100.00% in the hundredths of a percent a fixed percentage is weighed in.
const PercentTotal = 10000

// scaled reads a plain non-negative decimal with at most scale decimals as a whole number of 10^-scale, exactly.
func scaled(s string, scale int) (int64, bool) {
	s = strings.TrimSpace(s)
	whole, frac, dot := strings.Cut(s, ".")
	if whole == "" || len(frac) > scale || (dot && frac == "") || strings.ContainsAny(s, "+-eE ") || strings.Count(s, ".") > 1 {
		return 0, false
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	digits := whole + frac + strings.Repeat("0", scale-len(frac))
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok || !n.IsInt64() {
		return 0, false
	}
	return n.Int64(), true
}

// PercentUnits turns fixed percentages, each above nothing with at most two decimals, into weights in hundredths of a percent, and says how much of the 100.00% they
// leave: that is what the rule cannot place, so it stays unallocated. The percentages may add to less than 100 and never to more.
func PercentUnits(percents []string) (units []int64, unplaced int64, err error) {
	var total int64
	for i, p := range percents {
		n, ok := scaled(p, 2)
		if !ok || n == 0 {
			return nil, 0, &Error{CodePercent, fmt.Sprintf("percent %d", i+1), fmt.Sprintf("target %d's percentage %q is not above nothing, with at most two decimals", i+1, p),
				"Enter a percentage such as 40 or 12.5"}
		}
		units = append(units, n)
		total += n
		if total > PercentTotal {
			return nil, 0, &Error{CodePercent, fmt.Sprintf("percent %d", i+1), "the percentages add up to more than 100", "Lower a percentage so they add up to 100 or less"}
		}
	}
	return units, PercentTotal - total, nil
}

// QuantityUnits turns usage quantities, each zero or more with at most six decimals, into weights in millionths.
func QuantityUnits(quantities []string) ([]int64, error) {
	out := make([]int64, len(quantities))
	var total int64
	for i, q := range quantities {
		n, ok := scaled(q, QuantityScale)
		if !ok {
			return nil, &Error{CodeQuantity, fmt.Sprintf("quantity %d", i+1), fmt.Sprintf("quantity %d, %q, is not zero or more with at most %d decimals", i+1, q, QuantityScale),
				"Enter the usage as a plain number such as 1250 or 3.5"}
		}
		if total+n < total || total+n > math.MaxInt64/2 {
			return nil, &Error{CodeQuantity, fmt.Sprintf("quantity %d", i+1), fmt.Sprintf("quantity %d makes the quantities too large to add up", i+1), "Use a smaller unit"}
		}
		out[i] = n
		total += n
	}
	return out, nil
}

// Result is a shared cost split: the part of each target, in the order of the weights, and what the key could not place.
type Result struct {
	Parts       []money.Money
	Unallocated money.Money
}

// Split splits amount over the targets' weights and what cannot be placed, by ALLOC-PRORATA: each target's share is rounded down to the minor unit and the units left go one
// each to the largest remainders, ties to the earlier (the unallocated part last of all). The parts and the unallocated amount add to amount exactly. The weights are whole
// numbers, none negative, and some above nothing.
func Split(amount money.Money, units []int64, unplaced int64) (Result, error) {
	weights := append(append([]int64{}, units...), unplaced)
	var anyWeight int64
	for _, w := range weights {
		anyWeight |= w
	}
	if anyWeight == 0 {
		return Result{}, &Error{CodeWeights, "weights", "no target has a weight to split by", "Give a target a share, or choose a key that finds one"}
	}
	parts, err := rounding.AllocProRata(amount, weights)
	if err != nil {
		return Result{}, &Error{CodeWeights, "weights", err.Error(), "Check the weights: whole numbers, none negative, and not too large"}
	}
	return Result{Parts: parts[:len(units)], Unallocated: parts[len(units)]}, nil
}
