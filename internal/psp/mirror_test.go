package psp

import (
	"testing"
	"time"
)

// The months a provider line's service period touches decide its spread (design D6): a monthly plan is one month at any hour, an annual plan twelve, a
// proration from the middle of a month spans the months it touches. req: FR-PSP-07
func TestAServicePeriodTouchesItsCalendarMonths(t *testing.T) {
	at := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, time.UTC) }
	for name, c := range map[string]struct {
		start, end time.Time
		want       int
	}{
		"a month from the 1st":       {at(11, 1, 9), at(12, 1, 9), 1},
		"a year":                     {at(1, 1, 0), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 12},
		"from the middle of a month": {at(11, 15, 9), at(12, 15, 9), 2},
		"three months":               {at(10, 1, 9), at(1, 1, 9).AddDate(1, 0, 0), 3},
		"a proration inside a month": {at(5, 15, 9), at(6, 1, 9), 1},
		"no period":                  {time.Time{}, time.Time{}, 0},
		"backwards":                  {at(12, 1, 0), at(11, 1, 0), 0},
	} {
		if got := monthsOf(c.start, c.end); got != c.want {
			t.Errorf("%s: %d months, want %d", name, got, c.want)
		}
	}
}
