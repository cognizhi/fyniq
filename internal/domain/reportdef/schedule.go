package reportdef

import "time"

// NextRun is the first day after `after` a schedule runs: a weekly one on its ISO weekday (1 Monday to 7 Sunday), a monthly one on its
// day of the month (1 to 28, so every month has it). Days are calendar dates; the time of day is dropped.
func NextRun(cadence string, weekday, dayOfMonth int, after time.Time) time.Time {
	d := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	switch cadence {
	case "weekly":
		for isoWeekday(d) != weekday {
			d = d.AddDate(0, 0, 1)
		}
	default:
		for d.Day() != dayOfMonth {
			d = d.AddDate(0, 0, 1)
		}
	}
	return d
}

func isoWeekday(d time.Time) int {
	if w := int(d.Weekday()); w != 0 {
		return w
	}
	return 7
}
