package cron_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/cron"
)

func sgt(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, clock.Singapore)
}

// A schedule's entries are five-field cron expressions in Singapore time (AI-62): the recommended daily monitor at
// 07:00 and the weekly cross-check on Monday at 08:00 fall when they say, whatever the server's zone, and the next slot
// is always after the last one.
// req: AI-62
// req: ai/scheduled-agents Recommended default schedule
func TestASchedulesNextSlotIsInSingaporeTime(t *testing.T) {
	for _, c := range []struct {
		expr  string
		after time.Time
		want  time.Time
	}{
		{"0 7 * * *", sgt(2026, 11, 15, 6, 59), sgt(2026, 11, 15, 7, 0)},
		{"0 7 * * *", sgt(2026, 11, 15, 7, 0), sgt(2026, 11, 16, 7, 0)},
		{"0 7 * * *", time.Date(2026, 11, 14, 22, 30, 0, 0, time.UTC), sgt(2026, 11, 15, 7, 0)}, // 22:30 UTC is 06:30 SGT
		{"0 8 * * 1", sgt(2026, 11, 15, 12, 0), sgt(2026, 11, 16, 8, 0)},                        // Sunday → Monday
		{"0 8 * * 1", sgt(2026, 11, 16, 8, 0), sgt(2026, 11, 23, 8, 0)},
		{"*/15 9-10 * * *", sgt(2026, 11, 16, 9, 7), sgt(2026, 11, 16, 9, 15)},
		{"0 0 1 * *", sgt(2026, 11, 16, 9, 7), sgt(2026, 12, 1, 0, 0)},
		{"0 9 15 * 0", sgt(2026, 11, 2, 0, 0), sgt(2026, 11, 8, 9, 0)}, // either day field: Sunday the 8th before the 15th
		{"0 9 * * 7", sgt(2026, 11, 2, 0, 0), sgt(2026, 11, 8, 9, 0)},  // 7 is Sunday too
	} {
		s, err := cron.Parse(c.expr)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := s.Next(c.after); !got.Equal(c.want) {
			t.Errorf("%s after %s: %s, want %s", c.expr, c.after, got, c.want)
		}
	}
	if s, _ := cron.Parse("0 0 30 2 *"); !s.Next(sgt(2026, 1, 1, 0, 0)).IsZero() {
		t.Error("30 February has a slot")
	}
	if s, err := cron.Parse("off"); err != nil || !s.Off() || !s.Next(sgt(2026, 1, 1, 0, 0)).IsZero() {
		t.Errorf("off: %v", err)
	}
	for _, bad := range []string{"", "0 7 * *", "60 7 * * *", "0 24 * * *", "0 7 0 * *", "0 7 * 13 *", "0 7 * * 8", "a 7 * * *", "*/0 7 * * *", "5-1 7 * * *"} {
		if _, err := cron.Parse(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// Quiet hours are a daily range in Singapore time, wrapping past midnight (AI-62): a run inside them still runs, and
// what it emails waits until they end.
// req: AI-62
func TestQuietHoursWrapPastMidnight(t *testing.T) {
	r, err := cron.ParseRange("22:00-07:00")
	if err != nil {
		t.Fatal(err)
	}
	for at, in := range map[time.Time]bool{sgt(2026, 11, 15, 21, 59): false, sgt(2026, 11, 15, 22, 0): true, sgt(2026, 11, 16, 3, 0): true,
		sgt(2026, 11, 16, 7, 0): false, sgt(2026, 11, 16, 12, 0): false} {
		if r.Contains(at) != in {
			t.Errorf("%s: in quiet hours %v", at, !in)
		}
	}
	if end := r.End(sgt(2026, 11, 15, 23, 0)); !end.Equal(sgt(2026, 11, 16, 7, 0)) {
		t.Errorf("the end of quiet hours begun at 23:00: %s", end)
	}
	if day, _ := cron.ParseRange("12:00-13:00"); !day.Contains(sgt(2026, 11, 16, 12, 30)) || day.Contains(sgt(2026, 11, 16, 13, 0)) {
		t.Error("a daytime range")
	}
	if off, _ := cron.ParseRange("off"); off.Contains(sgt(2026, 11, 16, 3, 0)) {
		t.Error("quiet hours off still holds")
	}
	for _, bad := range []string{"", "22-07", "22:00", "25:00-07:00", "22:00-22:00", "2200-0700"} {
		if _, err := cron.ParseRange(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}
