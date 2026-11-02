// Package accessreview holds the rules of the user access review (PRD-001 FR-CTL-06, SEC-06; rev 0.162): an Owner
// attests who can do what once each calendar quarter has ended, and the review is overdue (C.2 #7) once a quarter
// ended more than 14 days ago with neither its attestation nor a later one. Dates are the organisation's, at UTC
// midnight.
package accessreview

import (
	"fmt"
	"time"
)

// CodeAttest is an attestation that cannot be made: no quarter has ended since the organisation began, or the
// latest one is attested already.
const CodeAttest = "CHECK-03"

// Grace is how long after its quarter ends a review may wait before it is overdue (C.2 #7).
const Grace = 14

// Dormant is how long without a sign-in marks a member for review (FR-CTL-06).
const Dormant = 30 * 24 * time.Hour

// QuarterOf returns the first day of the calendar quarter holding day.
func QuarterOf(day time.Time) time.Time {
	m := (int(day.Month())-1)/3*3 + 1
	return time.Date(day.Year(), time.Month(m), 1, 0, 0, 0, 0, time.UTC)
}

// LastDay is a quarter's last day.
func LastDay(quarter time.Time) time.Time { return quarter.AddDate(0, 3, -1) }

// DueBy is the last day a quarter's review is on time: 14 days after the quarter's last day.
func DueBy(quarter time.Time) time.Time { return LastDay(quarter).AddDate(0, 0, Grace) }

// Label names a quarter, such as "Q3 2026".
func Label(quarter time.Time) string {
	return fmt.Sprintf("Q%d %d", (int(quarter.Month())-1)/3+1, quarter.Year())
}

// ToAttest returns the latest quarter that has ended by today, and whether there is one since the organisation's
// first member joined (since, a day): a quarter that ended before the organisation began has nobody to review.
func ToAttest(today, since time.Time) (time.Time, bool) {
	q := QuarterOf(today).AddDate(0, -3, 0)
	return q, !since.After(LastDay(q))
}

// Due returns the latest quarter whose review is past its due date by today, and whether there is one since the
// organisation began.
func Due(today, since time.Time) (time.Time, bool) {
	q := QuarterOf(today).AddDate(0, -3, 0)
	if !today.After(DueBy(q)) {
		q = q.AddDate(0, -3, 0)
	}
	return q, !since.After(LastDay(q))
}

// Covered reports whether attestations, the quarters attested, cover quarter: its own or a later one's. An
// attestation of a later quarter reviews access as it stands, so it answers for the quarters before it.
func Covered(quarter time.Time, attested []time.Time) bool {
	for _, a := range attested {
		if !a.Before(quarter) {
			return true
		}
	}
	return false
}
