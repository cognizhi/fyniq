package capture

import (
	"testing"
	"time"
)

// req: FR-AP-08
func TestAReceiptIsSuggestedOnlyForTheSameAmountNearTheDate(t *testing.T) {
	s := func(v string) Text { return Text{Value: &v, Confidence: 0.9} }
	r := func(kind, total, date, cur string) Reading {
		x := Reading{Kind: kind, Total: s(total), Date: s(date)}
		if cur != "" {
			x.Currency = s(cur)
		}
		return x
	}
	booked := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		r    Reading
		want bool
	}{
		{"the same amount the day before", r("receipt", "27.30", "2026-10-19", "SGD"), true},
		{"a bill the same day, no currency printed", r("bill", "27.3", "2026-10-20", ""), true},
		{"five days after", r("receipt", "27.30", "2026-10-25", "SGD"), true},
		{"six days after", r("receipt", "27.30", "2026-10-26", "SGD"), false},
		{"a cent different", r("receipt", "27.31", "2026-10-20", "SGD"), false},
		{"another currency", r("receipt", "27.30", "2026-10-20", "USD"), false},
		{"a contract", r("contract", "27.30", "2026-10-20", "SGD"), false},
		{"no date", Reading{Kind: "receipt", Total: s("27.30")}, false},
	} {
		if got := SuggestsFor(c.r, "-27.30", "SGD", booked); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
