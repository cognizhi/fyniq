package director

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// FR-EQ-04 (design D8), worked by hand: Mei Lin's choices are no salary from January 2026 (recorded 2 Jan), salary
// deferred from April (recorded 28 Mar), and, recorded later on 15 Apr, salary paid from April instead. In December
// 2025 nothing applies, so the default, no salary; in March, no salary; in April and after, salary paid, the later
// recording for the same month superseding the earlier.
// req: FR-EQ-04
// req: equity/founders-money Choice recorded with the signed decision
// req: equity/founders-money Recorded before payroll (rev 0.167)
func TestThePayChoiceInForceByMonth(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 9, 0, 0, 0, time.UTC) }
	recorded := func(from time.Month, choice string, at time.Time) RecordedChoice {
		return RecordedChoice{ID: uuid.New(), Choice: PayChoice{From: time.Date(2026, from, 1, 0, 0, 0, 0, time.UTC), Choice: choice}, RecordedAt: at}
	}
	choices := []RecordedChoice{
		recorded(time.January, ChoiceNoSalary, day(time.January, 2)),
		recorded(time.April, ChoiceSalaryDeferred, day(time.March, 28)),
		recorded(time.April, ChoiceSalaryPaid, day(time.April, 15)),
	}
	for _, c := range []struct {
		month time.Time
		want  string
	}{
		{time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC), ""},
		{day(time.March, 31), ChoiceNoSalary},
		{day(time.April, 1), ChoiceSalaryPaid},
		{day(time.November, 3), ChoiceSalaryPaid},
	} {
		got := InForce(choices, c.month)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: %s, want none (the default, no salary)", c.month.Format(time.DateOnly), got.Choice.Choice)
		case c.want != "" && (got == nil || got.Choice.Choice != c.want):
			t.Errorf("%s: %v, want %s", c.month.Format(time.DateOnly), got, c.want)
		}
	}
	if InForce(nil, day(time.May, 1)) != nil {
		t.Error("a person with no choice recorded has one in force")
	}
}
