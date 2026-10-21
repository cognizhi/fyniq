package journal

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func dateText(t *time.Time) string {
	if t == nil {
		return "none"
	}
	return t.Format(time.DateOnly)
}

// FR-GL-02, FR-AR-07 (rev 0.60): a recurring journal's occurrences fall every 1, 3, 6 or 12 months from its start's month,
// on its day, the 29th to the 31st being the last day of a shorter month, and none after its end.
// req: FR-GL-02
// req: FR-AR-07
func TestRecurringOccurrences(t *testing.T) {
	end := date("2027-03-31")
	cases := []struct {
		name     string
		schedule storage.Schedule
		from     string
		want     string
	}{
		{"monthly on the 1st, from its start", storage.Schedule{EveryMonths: 1, DayOfMonth: 1, StartsOn: date("2026-10-01")}, "2026-10-01", "2026-10-01"},
		{"monthly on the 1st, started mid-month", storage.Schedule{EveryMonths: 1, DayOfMonth: 1, StartsOn: date("2026-10-15")}, "2026-10-15", "2026-11-01"},
		{"the 31st in November is the 30th", storage.Schedule{EveryMonths: 1, DayOfMonth: 31, StartsOn: date("2026-10-01")}, "2026-11-01", "2026-11-30"},
		{"the 30th in February is the 28th", storage.Schedule{EveryMonths: 1, DayOfMonth: 30, StartsOn: date("2027-01-01")}, "2027-02-01", "2027-02-28"},
		{"the 29th in a leap February", storage.Schedule{EveryMonths: 1, DayOfMonth: 31, StartsOn: date("2028-01-01")}, "2028-02-01", "2028-02-29"},
		{"quarterly from October", storage.Schedule{EveryMonths: 3, DayOfMonth: 15, StartsOn: date("2026-10-01")}, "2026-10-16", "2027-01-15"},
		{"quarterly, a year on", storage.Schedule{EveryMonths: 3, DayOfMonth: 1, StartsOn: date("2026-10-01")}, "2027-09-02", "2027-10-01"},
		{"yearly", storage.Schedule{EveryMonths: 12, DayOfMonth: 31, StartsOn: date("2026-12-01")}, "2027-01-01", "2027-12-31"},
		{"from before the start", storage.Schedule{EveryMonths: 6, DayOfMonth: 1, StartsOn: date("2026-11-01")}, "2026-01-01", "2026-11-01"},
		{"on its end", storage.Schedule{EveryMonths: 1, DayOfMonth: 31, StartsOn: date("2026-10-01"), EndsOn: &end}, "2027-03-02", "2027-03-31"},
		{"after its end", storage.Schedule{EveryMonths: 1, DayOfMonth: 1, StartsOn: date("2026-10-01"), EndsOn: &end}, "2027-03-02", "none"},
	}
	for _, c := range cases {
		if got := dateText(FirstOn(c.schedule, date(c.from))); got != c.want {
			t.Errorf("%s: the first occurrence on or after %s is %s, not %s", c.name, c.from, got, c.want)
		}
	}
	monthly := storage.Schedule{EveryMonths: 1, DayOfMonth: 31, StartsOn: date("2026-10-01")}
	on, want := date("2026-10-31"), []string{"2026-11-30", "2026-12-31", "2027-01-31", "2027-02-28", "2027-03-31"}
	for _, w := range want {
		next := NextAfter(monthly, on)
		if dateText(next) != w {
			t.Fatalf("after %s: %s, not %s", on.Format(time.DateOnly), dateText(next), w)
		}
		on = *next
	}
}
