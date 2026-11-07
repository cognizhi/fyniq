package search

import "testing"

func ptr(s string) *string { return &s }

func same(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// A query is an amount when the whole of it is one, in the forms people type: exact with its thousands separators, a
// currency sign or a k or m; above or below a bound. A bare whole number is a reference's digits or an amount; anything
// else is text, kept as typed.
// req: FR-UX-02
// req: experience/navigation Search by amount
func TestAQueryReadsAsAnAmountOrText(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Query
	}{
		{"1,234.56", Query{Min: ptr("1234.56"), Max: ptr("1234.56"), MinInclusive: true, MaxInclusive: true}},
		{"S$1,234.56", Query{Min: ptr("1234.56"), Max: ptr("1234.56"), MinInclusive: true, MaxInclusive: true}},
		{"-1234.56", Query{Min: ptr("1234.56"), Max: ptr("1234.56"), MinInclusive: true, MaxInclusive: true}},
		{"> 5k", Query{Min: ptr("5000")}},
		{">=2.5m", Query{Min: ptr("2500000"), MinInclusive: true}},
		{"< 100", Query{Max: ptr("100")}},
		{"<= 99.90", Query{Max: ptr("99.9"), MaxInclusive: true}},
		{"0.5k", Query{Min: ptr("500"), Max: ptr("500"), MinInclusive: true, MaxInclusive: true}},
		{"138", Query{Text: "138", Either: true, Min: ptr("138"), Max: ptr("138"), MinInclusive: true, MaxInclusive: true}},
		{"INV-0138", Query{Text: "INV-0138"}},
		{"  Acme rent ", Query{Text: "Acme rent"}},
		{"1,23", Query{Text: "1,23"}},
		{"> five", Query{Text: "> five"}},
	} {
		got := Parse(c.in)
		if got.Text != c.want.Text || got.Either != c.want.Either || !same(got.Min, c.want.Min) || !same(got.Max, c.want.Max) ||
			got.MinInclusive != c.want.MinInclusive || got.MaxInclusive != c.want.MaxInclusive {
			t.Errorf("Parse(%q) = %+v; want %+v", c.in, got, c.want)
		}
	}
	if long := Parse(string(make([]rune, 300))); len([]rune(long.Text)) > MaxLength {
		t.Errorf("a long query is read to %d characters", len([]rune(long.Text)))
	}
}
