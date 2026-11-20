package allocation

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

func sgd(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, money.MustLookup("SGD"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func field(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Field
	}
	return ""
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Percentages are weights in hundredths of a percent, and what they leave of 100 is what the rule cannot place (FR-DIM-03).
// req: FR-DIM-03
func TestPercentagesLeaveTheRestUnplaced(t *testing.T) {
	for _, c := range []struct {
		in       []string
		units    []int64
		unplaced int64
	}{
		{[]string{"40", "35"}, []int64{4000, 3500}, 2500},
		{[]string{"12.5", "87.5"}, []int64{1250, 8750}, 0},
		{[]string{"0.01"}, []int64{1}, 9999},
		{[]string{"100"}, []int64{10000}, 0},
		{[]string{"33.33", "33.33", "33.33"}, []int64{3333, 3333, 3333}, 1},
		{nil, nil, 10000},
	} {
		units, unplaced, err := PercentUnits(c.in)
		if err != nil || !slices.Equal(units, c.units) || unplaced != c.unplaced {
			t.Errorf("%v: %v, %d, %v; want %v, %d", c.in, units, unplaced, err, c.units, c.unplaced)
		}
	}
	for _, bad := range [][]string{{"0"}, {"0.00"}, {"100.01"}, {"60", "40.01"}, {"12.345"}, {"-1"}, {"1e2"}, {"abc"}, {""}, {"+5"}, {"1.2.3"}, {"5 0"}, {"."}, {".5"}, {"99999999999999999999"}} {
		if _, _, err := PercentUnits(bad); code(err) != CodePercent {
			t.Errorf("%v: %v, want %s", bad, err, CodePercent)
		}
	}
	if _, _, err := PercentUnits([]string{"60", "x"}); err == nil || !strings.Contains(err.Error(), "target 2") {
		t.Errorf("the second percentage is wrong: %v; want it to name target 2", err)
	}
	if _, _, err := PercentUnits([]string{"60", "30", "10.01"}); err == nil || !strings.Contains(err.Error(), "more than 100") || field(err) != "percent 3" {
		t.Errorf("percentages above 100: %v (%s)", err, field(err))
	}
	if _, _, err := PercentUnits([]string{"60", "x"}); field(err) != "percent 2" {
		t.Errorf("the field of the second percentage: %q", field(err))
	}
}

// Usage quantities are weights in millionths.
// req: FR-DIM-03
func TestUsageQuantitiesWeighInMillionths(t *testing.T) {
	got, err := QuantityUnits([]string{"1250", "3.5", "0.000001", "0", " 7 "})
	if err != nil || !slices.Equal(got, []int64{1250000000, 3500000, 1, 0, 7000000}) {
		t.Errorf("%v, %v", got, err)
	}
	for _, bad := range []string{"-1", "1.1234567", "1e3", "x", "", ".5", "5.", "1,5", "9223372036854"} {
		if _, err := QuantityUnits([]string{"1", bad}); code(err) != CodeQuantity {
			t.Errorf("%q: %v, want %s", bad, err, CodeQuantity)
		}
	}
	if _, err := QuantityUnits([]string{"1", "x"}); err == nil || !strings.Contains(err.Error(), "quantity 2") || field(err) != "quantity 2" {
		t.Errorf("the second quantity is wrong: %v; want it to name quantity 2", err)
	}
	if _, err := QuantityUnits([]string{"4000000000000", "4000000000000"}); code(err) != CodeQuantity {
		t.Errorf("quantities too large to add: %v", err)
	}
	// The sum may reach half the largest whole number a weight can be, and no more, so the weights can still be added to the unplaced weight.
	if got, err := QuantityUnits([]string{"4611686018427.387903"}); err != nil || got[0] != 4611686018427387903 {
		t.Errorf("the largest sum: %v, %v", got, err)
	}
	if got, err := QuantityUnits([]string{"4611686018427", "0.387903"}); err != nil || got[0]+got[1] != 4611686018427387903 {
		t.Errorf("the largest sum of two: %v, %v", got, err)
	}
	if _, err := QuantityUnits([]string{"1", "4611686018427.387903"}); err == nil || !strings.Contains(err.Error(), "quantity 2") || field(err) != "quantity 2" {
		t.Errorf("one past the largest sum: %v; want it to name quantity 2", err)
	}
	if _, err := QuantityUnits([]string{"4611686018427.387904"}); code(err) != CodeQuantity {
		t.Errorf("one past the largest sum alone: %v", err)
	}
}

// A split gives every unit away: the targets' parts and the unallocated part add to the amount, the unallocated part is the last of the weights, and with no weight at all there is
// nothing to split by.
// req: FR-DIM-03
// req: ALLOC-PRORATA
func TestASplitGivesEveryUnitAwayAndKeepsTheUnplacedAside(t *testing.T) {
	r, err := Split(sgd(t, "1000.00"), []int64{4000, 3500}, 2500)
	if err != nil || r.Parts[0].String() != "400.00" || r.Parts[1].String() != "350.00" || r.Unallocated.String() != "250.00" {
		t.Fatalf("%v, %v", r, err)
	}
	// the unit left over goes to the unallocated part when its remainder is the largest
	r, err = Split(sgd(t, "0.01"), []int64{1, 1}, 2)
	if err != nil || !r.Parts[0].IsZero() || !r.Parts[1].IsZero() || r.Unallocated.String() != "0.01" {
		t.Fatalf("a cent: %v, %v", r, err)
	}
	// no unplaced weight: the unallocated part is nothing
	r, err = Split(sgd(t, "100.00"), []int64{1, 1, 1}, 0)
	if err != nil || r.Unallocated.String() != "0.00" || r.Parts[0].String() != "33.34" {
		t.Fatalf("thirds: %v, %v", r, err)
	}
	for _, w := range [][]int64{nil, {0}, {0, 0}} {
		if _, err := Split(sgd(t, "1.00"), w, 0); code(err) != CodeWeights {
			t.Errorf("weights %v: %v", w, err)
		}
	}
	if _, err := Split(sgd(t, "1.00"), []int64{-1, 2}, 0); code(err) != CodeWeights {
		t.Errorf("a negative weight: %v", err)
	}
	if _, err := Split(sgd(t, "1.00"), []int64{0, 0}, 5); err != nil {
		t.Errorf("only the unplaced has a weight: %v", err) // the whole amount is unallocated
	}
}
