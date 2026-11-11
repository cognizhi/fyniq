package retention

import (
	"time"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
)

// Period is what the year plan needs of a period: its days and whether it is still open.
type Period struct {
	StartsOn, EndsOn time.Time
	Open             bool
}

// Year is the plan's answer: the financial year that may go and its boundary, the first day after it.
type Year struct {
	StartsOn, Boundary time.Time
}

// NextYear is the oldest financial year whose lines may be purged on now (AUD-10, design D7b): the year that holds
// earliest, the date of the books' first line still kept, once that year's records' retention has passed (the end of
// the year plus years, at least 7, evidence.RetainUntil), every period it holds is closed and no record dated in it is
// on legal hold (holds are the held records' dates). An earlier year is always purged first, since earliest is the
// first line kept. ok is false when no year may go yet.
func NextYear(fyStartMonth, years int, earliest time.Time, periods []Period, holds []time.Time, now time.Time, loc *time.Location) (Year, bool) {
	if earliest.IsZero() {
		return Year{}, false
	}
	if fyStartMonth < 1 || fyStartMonth > 12 {
		fyStartMonth = 1
	}
	local := earliest.In(loc)
	start := time.Date(local.Year(), time.Month(fyStartMonth), 1, 0, 0, 0, 0, loc)
	if start.After(local) {
		start = start.AddDate(-1, 0, 0)
	}
	boundary := start.AddDate(1, 0, 0)
	if evidence.RetainUntil(fyStartMonth, max(years, 7), earliest, loc).After(now) {
		return Year{}, false
	}
	for _, p := range periods {
		if p.Open && p.StartsOn.Before(boundary) {
			return Year{}, false
		}
	}
	for _, h := range holds {
		if !h.Before(start) && h.Before(boundary) {
			return Year{}, false
		}
	}
	return Year{StartsOn: start, Boundary: boundary}, true
}

// EntryRef is what the kept set needs of an entry: its key, its posting date and the entry it reverses ("" none).
type EntryRef struct {
	ID       string
	Date     time.Time
	Reverses string
}

// Kept is every entry the purge of the lines before boundary keeps (AUD-10, rev 0.238): those dated on or after the
// boundary, those named (a permanent register's or a reconciliation of the bank's), and, until nothing changes, every
// entry a kept entry reverses or is reversed by, so no reversal names an entry that is gone.
func Kept(entries []EntryRef, named map[string]bool, boundary time.Time) map[string]bool {
	kept := map[string]bool{}
	for _, e := range entries {
		if named[e.ID] || !e.Date.Before(boundary) {
			kept[e.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, e := range entries {
			if e.Reverses == "" || kept[e.ID] == kept[e.Reverses] {
				continue
			}
			kept[e.ID], kept[e.Reverses] = true, true
			changed = true
		}
	}
	return kept
}
