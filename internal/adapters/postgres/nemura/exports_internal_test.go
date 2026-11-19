package nemura

import "testing"

// A cell that starts with =, +, - or @ is prefixed so a spreadsheet does not run it as a formula (OWASP CSV injection).
// req: NM-PAY-05
func TestAFileCellThatLooksLikeAFormulaIsDefused(t *testing.T) {
	for in, want := range map[string]string{"=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "-1": "'-1", "@x": "'@x", "\tx": "'\tx", "Ben Lim": "Ben Lim", "": "", "3598.50": "3598.50"} {
		if got := safeCell(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
