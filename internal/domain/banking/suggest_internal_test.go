package banking

import (
	"slices"
	"testing"
)

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

// The workbook's document numbers and references (its golden sets, p1-launch-readiness task 3.1;
// test/golden/workbook/document-numbers and regressions): a number holding * ? ~ or < names only itself, never what a
// spreadsheet's look-up would read as a pattern or a comparison ("INV-9*" every number starting INV-9, "<B1>" anything
// sorting before B1>), digits typed as a number or as text name their own, a reference that only looks like a pattern
// names nothing, and spaces around a reference, or one of only spaces, change nothing.
// req: quality/calculation-correctness Golden data set, document numbers
// req: quality/calculation-correctness Golden data set, regressions
func TestAWorkbookNumberNamesOnlyItself(t *testing.T) {
	numbers := []string{"INV-9*", "INV-90", "INV-91", "1001", "1002", "~A", "<B1>", "A-7", "INV-1", "INV-2", "B-7", "SUP 42 "}
	for ref, want := range map[string][]string{
		"INV-9*":  {"INV-9*"},
		"INV-90":  {"INV-90"},
		"1001":    {"1001"},
		"1002":    {"1002"},
		"~A":      {"~A"},
		"INV-9?":  nil,
		"<B1>":    {"<B1>"},
		"A-7":     {"A-7"},
		"INV-1 ":  {"INV-1"},
		"  INV-2": {"INV-2"},
		"B-7 ":    {"B-7"},
		"SUP 42 ": {"SUP 42 "},
		"   ":     nil,
	} {
		var got []string
		for _, n := range numbers {
			if names(ref, []string{n}, false) || names(ref, []string{n}, true) {
				got = append(got, n)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("the reference %q names %q; want %q", ref, got, want)
		}
	}
}
