package audit

import (
	"slices"
	"strings"
	"testing"
)

// The log scan finds each kind of value SEC-15 keeps out of the logs, and nothing in a line RedactText leaves alone
// (CTL-DP-03). The fixtures are built so no secret scanner mistakes them for real ones.
// req: SEC-15
// req: CTL-DP-03
// asvs: V16.2.5
func TestLeaksNameWhatRedactionWouldRemove(t *testing.T) {
	jwt := "eyJ" + strings.Repeat("a", 8) + "." + strings.Repeat("b", 8) + "." + strings.Repeat("c", 4)
	key := "sk_" + strings.Repeat("x", 16)
	for _, c := range []struct {
		line string
		want []string
	}{
		{`{"msg":"payslip sent","organisation":"7b0c"}`, nil},
		{"member S1234567D joined", []string{"an NRIC or FIN number"}},
		{"session " + jwt, []string{"a JSON web token"}},
		{"Authorization: Bearer " + strings.Repeat("t", 12), []string{"a bearer token"}},
		{"connector key " + key + " and fin g7654321x", []string{"an NRIC or FIN number", "an API key"}},
	} {
		got := Leaks(c.line)
		if !slices.Equal(got, c.want) {
			t.Errorf("Leaks(%q) = %v, want %v", c.line, got, c.want)
		}
		if (len(got) == 0) != (RedactText(c.line) == c.line) {
			t.Errorf("Leaks and RedactText disagree on %q: %v, %q", c.line, got, RedactText(c.line))
		}
	}
}
