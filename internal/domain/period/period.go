// Package period works out an organisation's financial years and their monthly periods (FR-ORG-02;
// Appendix B: a posting's period is the month of its posting date). The first financial year runs from
// the books start to the first year end; each later year is the twelve months after the one before.
// Periods are calendar months, so a year that starts mid-month has a short first period. A year end is
// always the last day of a month (organisation.Check), so no other period is cut.
//
// Provisional (PRD-001 rev 0.47): a person opens a whole year at once, the first and then each next one,
// at most one year past the year that holds today. Nothing opens a period by posting into it, so the
// database's FY030 stays the backstop.
package period

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable error codes (ENG-23).
const (
	CodeNoYear    = "PERIOD-01" // the books start or the first year end is not set
	CodeTooFar    = "PERIOD-02" // the year starts more than a year after the one that holds today
	CodeNotNext   = "PERIOD-03" // the year asked for is not the next one to open
	CodeYearsOpen = "PERIOD-04" // how the years run cannot change once a period exists
)

// Error is a refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Span is a run of days, both inclusive.
type Span struct {
	StartsOn time.Time
	EndsOn   time.Time
}

// Year is a financial year and its months.
type Year struct {
	Number int // 1 for the first
	Span
	Months []Span
}

// Calendar is how an organisation's financial years run.
type Calendar struct {
	BooksStart   time.Time
	FirstYearEnd time.Time
}

// CalendarOf reads the calendar from the profile, or refuses when the books start or the first year end
// is not set.
func CalendarOf(p storage.OrganisationProfile) (Calendar, error) {
	if p.BooksStart == nil || p.FirstYearEnd == nil {
		return Calendar{}, &Error{CodeNoYear, "the books start or the first financial year's end is not set",
			"Enter when the books start and when the first financial year ends, in Settings › Organisation"}
	}
	return Calendar{BooksStart: day(*p.BooksStart), FirstYearEnd: day(*p.FirstYearEnd)}, nil
}

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

// Year returns financial year n, 1 for the first.
func (c Calendar) Year(n int) Year {
	start, end := c.BooksStart, c.FirstYearEnd
	for i := 1; i < n; i++ {
		start = end.AddDate(0, 0, 1)
		end = start.AddDate(1, 0, -1)
	}
	return Year{Number: n, Span: Span{start, end}, Months: months(start, end)}
}

func months(start, end time.Time) []Span {
	var out []Span
	for s := start; !s.After(end); {
		next := time.Date(s.Year(), s.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		e := next.AddDate(0, 0, -1)
		if e.After(end) {
			e = end
		}
		out = append(out, Span{s, e})
		s = next
	}
	return out
}

// Number returns the financial year that holds the day: 0 before the books start.
func (c Calendar) Number(t time.Time) int {
	t = day(t)
	if t.Before(c.BooksStart) {
		return 0
	}
	n := 1
	for c.Year(n).EndsOn.Before(t) {
		n++
	}
	return n
}

// Next returns the financial year to open after the periods that exist, earliest first: the first year
// when there are none, and the rest of a year whose periods stop before its end (periods made before the
// years were set, such as the sample company's).
func (c Calendar) Next(existing []storage.Period) Year {
	if len(existing) == 0 {
		return c.Year(1)
	}
	last := day(existing[len(existing)-1].EndsOn)
	n := c.Number(last)
	if n == 0 {
		return c.Year(1)
	}
	if y := c.Year(n); last.Before(y.EndsOn) {
		rest := last.AddDate(0, 0, 1)
		return Year{Number: n, Span: Span{rest, y.EndsOn}, Months: months(rest, y.EndsOn)}
	}
	return c.Year(n + 1)
}

// MayOpen refuses a year that starts more than one year past the year that holds today, so a typo can
// never open years far ahead. Before the books start, the first two years may open.
func (c Calendar) MayOpen(y Year, today time.Time) error {
	limit := max(c.Number(today), 1) + 1
	if y.Number > limit {
		return &Error{CodeTooFar, fmt.Sprintf("the financial year %s to %s starts more than a year after the current one",
			y.StartsOn.Format(time.DateOnly), y.EndsOn.Format(time.DateOnly)), "Open it once the year before it has started"}
	}
	return nil
}

// Fixed refuses a change to how the years run once they are set and any period exists: entries and
// balances are keyed on the periods, whose dates never change (FY031). Setting them for the first time
// stays open, for periods made before the years were set (the sample company's); the database refuses a
// year whose periods would overlap them.
func Fixed(before, after storage.OrganisationProfile, periods int) error {
	if periods == 0 || before.BooksStart == nil || before.FirstYearEnd == nil {
		return nil
	}
	same := func(a, b *time.Time) bool { return (a == nil) == (b == nil) && (a == nil || day(*a).Equal(day(*b))) }
	if !same(before.BooksStart, after.BooksStart) || !same(before.FirstYearEnd, after.FirstYearEnd) || before.FYStartMonth != after.FYStartMonth {
		return &Error{CodeYearsOpen, fmt.Sprintf("%d periods are open, so when the books start and how the financial years run can no longer change", periods),
			"Keep the books start, the start month and the first year's end as they are"}
	}
	return nil
}
