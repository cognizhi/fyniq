package money

import (
	"slices"
	"testing"
)

// A quantity is read strictly and kept exactly, with its significant decimals only: never rounded, at most 6 decimals
// and 14 digits before the point (NUMERIC(20,6), §11.3).
// req: ENG-11
// req: ENG-44
func TestParseQuantity(t *testing.T) {
	for in, want := range map[string]string{"3": "3", "3.000000": "3", "2.50": "2.5", "0.125": "0.125", "-1": "-1", "0": "0", "0.000000": "0",
		"99999999999999.999999": "99999999999999.999999"} {
		q, err := ParseQuantity(in, "quantity")
		if err != nil || q.String() != want {
			t.Errorf("ParseQuantity(%q) = %v, %v; want %s", in, q, err, want)
		}
	}
	for in, code := range map[string]string{"1.1234567": CodePrecision, "abc": CodeNotDecimal, "1e3": CodeNotDecimal, "+1": CodeNotDecimal,
		"100000000000000": CodeRange} {
		if _, err := ParseQuantity(in, "quantity"); err == nil || err.(*Error).Code != code {
			t.Errorf("ParseQuantity(%q) = %v; want %s", in, err, code)
		}
	}
	a, _ := ParseQuantity("2.5", "q")
	b, _ := ParseQuantity("0.75", "q")
	if a.Add(b).String() != "3.25" || a.Sub(b).String() != "1.75" || b.Sub(a).String() != "-1.75" || a.Cmp(b) != 1 || b.Cmp(a) != -1 ||
		a.Sub(a).String() != "0" || !a.Sub(a).IsZero() || a.Sign() != 1 || b.Sub(a).Sign() != -1 || (Quantity{}).String() != "0" {
		t.Error("quantity arithmetic")
	}
	if got := []string{a.Decimal().Text('f')}; !slices.Equal(got, []string{"2.5"}) {
		t.Errorf("Decimal: %v", got)
	}
}
