package deadlines

import (
	"slices"
	"testing"
)

// Every obligation a record may name has its area in the sg-deadlines pack, and the corporate ones, which the Company
// secretary records (Appendix D's note, rev 0.285), are the annual return, the AGM and the financial statements to
// members; a key no rule names has none, so a qualified right records nothing with it.
// req: FR-CS-08
// req: FR-TAX-07
func TestEveryObligationHasItsArea(t *testing.T) {
	var corporate []string
	for _, o := range Obligations {
		area, err := AreaOf(o + ":2026-12-31")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains([]string{"tax", "payroll", "corporate"}, area) {
			t.Errorf("%s has the area %q, not tax, payroll or corporate", o, area)
		}
		if area == "corporate" {
			corporate = append(corporate, o)
		}
	}
	if want := []string{"annual_return", "agm", "statements_to_members"}; !slices.Equal(corporate, want) {
		t.Errorf("the corporate obligations are %v, want %v", corporate, want)
	}
	for _, key := range []string{"register_of_members:2026-12-31", "nonsense", ""} {
		if area, err := AreaOf(key); err != nil || area != "" {
			t.Errorf("AreaOf(%q) = %q, %v; want no area", key, area, err)
		}
	}
}
