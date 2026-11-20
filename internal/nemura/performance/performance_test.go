package performance_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/nemura/performance"
)

func d(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

// A measure is whole and its progress is exact: from its start value to its target, rounded down, clamped to 0 and 100, in either direction, and never a float (NM-PRF-01, NM-PRF-02).
// req: NM-PRF-01, NM-PRF-02
func TestAMeasureIsWholeAndItsProgressIsExactAndRoundedDown(t *testing.T) {
	m := performance.Measure{Title: "Pilot customers live", Unit: "customers", Start: "0", Target: "3", Current: "2"}
	if err := performance.Check(m); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		m    performance.Measure
		want int
	}{
		"two of three":        {m, 66},
		"not started":         {performance.Measure{Start: "0", Target: "3", Current: "0"}, 0},
		"past the target":     {performance.Measure{Start: "0", Target: "3", Current: "5"}, 100},
		"backwards":           {performance.Measure{Start: "0", Target: "3", Current: "-1"}, 0},
		"lower is better":     {performance.Measure{Start: "480", Target: "300", Current: "340", Down: true}, 77},
		"lower, not improved": {performance.Measure{Start: "480", Target: "300", Current: "500", Down: true}, 0},
		"decimals":            {performance.Measure{Start: "0.1", Target: "0.3", Current: "0.2"}, 50},
	} {
		if got := performance.Progress(tc.m); got != tc.want {
			t.Errorf("%s: %d%%, want %d%%", name, got, tc.want)
		}
	}
	for name, bad := range map[string]performance.Measure{
		"no title":        {Title: " ", Start: "0", Target: "3", Current: "0"},
		"a float":         {Title: "x", Start: "0", Target: "3e2", Current: "0"},
		"five decimals":   {Title: "x", Start: "0", Target: "3.12345", Current: "0"},
		"no distance":     {Title: "x", Start: "3", Target: "3", Current: "3"},
		"wrong direction": {Title: "x", Start: "480", Target: "300", Current: "340"},
		"unit too long":   {Title: "x", Unit: "a unit that is far too long to be a unit", Start: "0", Target: "3", Current: "0"},
	} {
		if err := performance.Check(bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A measure is done at 100%, otherwise on track when its progress keeps up with the share of the period that has passed, and behind when it does not; with no period it is met or not yet; a goal is whole (NM-PRF-01).
// req: NM-PRF-01
func TestAMeasureIsOnTrackWhenItKeepsUpWithThePeriod(t *testing.T) {
	from, due := d("2026-07-01"), d("2026-12-31")
	for name, tc := range map[string]struct {
		progress int
		today    string
		want     string
	}{
		"done":         {100, "2026-08-01", "done"},
		"ahead":        {60, "2026-09-01", "on_track"},
		"behind":       {20, "2026-11-01", "behind"},
		"before start": {0, "2026-06-01", "on_track"},
		"overdue":      {50, "2027-02-01", "behind"},
	} {
		if got := performance.Status(tc.progress, from, due, d(tc.today)); got != tc.want {
			t.Errorf("%s: %s", name, got)
		}
	}
	if performance.Status(40, from, time.Time{}, d("2026-09-01")) != "not_yet" || performance.Status(100, from, time.Time{}, d("2026-09-01")) != "done" {
		t.Error("a measure with no period is met or not yet")
	}
	ok := []performance.Measure{{Title: "Pilot customers", Start: "0", Target: "3", Current: "0"}}
	if err := performance.CheckGoal("team", "Ship the sync", "Engineering", d("2026-12-31"), d("2026-11-21"), ok); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"unknown scope":      performance.CheckGoal("squad", "x", "", d("2026-12-31"), d("2026-11-21"), ok),
		"team with no team":  performance.CheckGoal("team", "x", "", d("2026-12-31"), d("2026-11-21"), ok),
		"personal with team": performance.CheckGoal("personal", "x", "Engineering", d("2026-12-31"), d("2026-11-21"), ok),
		"past due":           performance.CheckGoal("company", "x", "", d("2026-11-01"), d("2026-11-21"), ok),
		"no title":           performance.CheckGoal("company", " ", "", d("2026-12-31"), d("2026-11-21"), ok),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
