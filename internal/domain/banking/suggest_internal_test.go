package banking

import "testing"

// A document's number is named exactly, or once normalised (NFKC, without regard to case, non-breaking and other
// spaces collapsed), and only on word boundaries, so INV-1 is not named by INV-10 (FR-BNK-07).
// req: FR-BNK-07
func TestAReferenceIsNamedOnWordBoundaries(t *testing.T) {
	for _, c := range []struct {
		text, number  string
		exact, folded bool
	}{
		{"GIRO INV-0001 ACME", "INV-0001", true, true},
		{"GIRO inv-0001 ACME", "INV-0001", false, true},
		{"GIRO ＩＮＶ-0001", "INV-0001", false, true},
		{"GIRO INV-0001", "INV-0001", true, true},
		{"GIRO INV-10", "INV-1", false, false},
		{"INV-1/INV-2", "INV-1", true, true},
		{"XINV-1", "INV-1", false, false},
		{"", "INV-1", false, false},
	} {
		if got := names(c.text, []string{c.number}, false); got != c.exact {
			t.Errorf("%q names %q exactly: %v", c.text, c.number, got)
		}
		if got := names(c.text, []string{c.number}, true); got != c.folded {
			t.Errorf("%q names %q once normalised: %v", c.text, c.number, got)
		}
	}
	if names("INV-1", []string{""}, true) {
		t.Error("an empty number names nothing")
	}
}
