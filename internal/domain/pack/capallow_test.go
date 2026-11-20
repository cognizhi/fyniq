package pack_test

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// The pack loads and its fourteen pinned cases, worked by hand from IRAS's page and its own examples, pass.
// req: FR-FA-04
// req: QA-ENG-08
func TestTheCapitalAllowancesPackLoadsAndItsPinnedCasesPass(t *testing.T) {
	v := version(t, "sg-capital-allowances/2026.1")
	rs := pack.RunPinned(v)
	if len(rs) != 15 {
		t.Fatalf("%d pinned cases ran, want 15", len(rs))
	}
	for _, r := range rs {
		if !r.Pass {
			t.Errorf("%s %s: got %q, want %q (%s)", r.Table, r.Case, r.Got, r.Want, r.Why)
		}
	}
}

// The allowances of every method add to the cost exactly, whatever the cost in cents, and a year of assessment the pack does not hold has no rules.
// req: FR-FA-04
func TestEveryMethodWritesOffTheWholeCostExactly(t *testing.T) {
	v := version(t, "sg-capital-allowances/2026.1")
	for _, cost := range []int64{1, 2, 3, 100, 333, 99999, 440000, 1234567, 500000} {
		for _, c := range []struct {
			method string
			years  int
			count  int
		}{{pack.MethodS19, 6, 6}, {pack.MethodS19, 12, 12}, {pack.MethodS19, 16, 16}, {pack.MethodS19A1, 0, 3}, {pack.MethodS19A10A, 0, 1}} {
			s, err := pack.CapitalAllowanceSeries(v, c.method, c.years, cost, 2027)
			if err != nil {
				t.Fatal(err)
			}
			var sum int64
			for _, x := range s {
				if x < 0 {
					t.Errorf("%s over %d of %d cents: a negative year %v", c.method, c.years, cost, s)
				}
				sum += x
			}
			if sum != cost || len(s) != c.count {
				t.Errorf("%s over %d of %d cents: %v adds to %d over %d years", c.method, c.years, cost, s, sum, len(s))
			}
		}
	}
	if _, err := pack.CapitalAllowanceSeries(v, pack.MethodS19A1, 0, 1000, 2022); err == nil {
		t.Errorf("an asset of the basis period of YA 2022 is not classified by this version")
	}
	if _, err := pack.CapitalAllowanceSeries(v, "s19a2", 0, 1000, 2027); err == nil {
		t.Errorf("a method the pack does not hold")
	}
	if _, err := pack.CapitalAllowanceSeries(v, pack.MethodS19, 6, 0, 2027); err == nil {
		t.Errorf("a cost of nothing")
	}
}

// A working life is elected only among those the Sixth Schedule's life allows, and only from YA 2023.
// req: FR-FA-04
func TestAWorkingLifeIsElectedAmongThoseThePrescribedLifeAllows(t *testing.T) {
	v := version(t, "sg-capital-allowances/2026.1")
	if got := pack.WorkingLifeElections(v, 6, 2027); !slices.Equal(got, []int{6, 12}) {
		t.Errorf("6: %v", got)
	}
	if got := pack.WorkingLifeElections(v, 16, 2027); !slices.Equal(got, []int{6, 12, 16}) {
		t.Errorf("16: %v", got)
	}
	for _, p := range []int{0, 13, 15, 20} {
		if got := pack.WorkingLifeElections(v, p, 2027); got != nil {
			t.Errorf("a prescribed life of %d: %v", p, got)
		}
	}
	if got := pack.WorkingLifeElections(v, 6, 2022); got != nil {
		t.Errorf("before YA 2023: %v", got)
	}
}
