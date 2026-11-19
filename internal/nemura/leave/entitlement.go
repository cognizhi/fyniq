package leave

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// Service is the facts about a person's service that an entitlement is worked out from.
type Service struct {
	Start     time.Time
	Leaving   *time.Time // the last day of employment, the notice period included (MOM)
	Employee  bool       // a contract of service: a contractor has no statutory leave
	EACovered bool       // the Employment Act covers the person (NM-PPL-08)
}

// Entitlement is a year's entitlement and how it was worked out, so a person can be shown where the figure comes from (golden rule 3).
type Entitlement struct {
	Days    Days
	Months  int      // completed months of service the year counts, over all the service years in it
	Basis   []string // one plain line for each step
	Sources []string // the pack version and policy the days came from
}

// completedMonths is the whole months from a day up to, not including, another: 14 March to 1 August is four.
func completedMonths(from, until time.Time) int {
	m := (until.Year()-from.Year())*12 + int(until.Month()) - int(from.Month())
	if until.Day() < from.Day() {
		m--
	}
	return max(m, 0)
}

func dayOf(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// anniversary is the first day of the nth year of service plus one: the start date n years on.
func anniversary(start time.Time, n int) time.Time {
	return dayOf(start.Year()+n, start.Month(), start.Day())
}

func at(days []int, i int) int {
	if len(days) == 0 {
		return 0
	}
	return days[min(i, len(days)-1)]
}

// AnnualGrant is the paid annual leave for a calendar leave year (NM-LV-02): the days of each year of service the leave year touches, by the completed months
// of service in it over twelve, rounded once to a whole day (under one half down, a half or more up: MOM). The statute's days come from the pack; for a
// person the Employment Act covers the company's policy may give more, never less (NM-PACK-08); for a person it does not cover, the policy alone gives the
// days. A person has no entitlement before three completed months of service, and months worked before that count when it begins. A contractor has none.
func AnnualGrant(v pack.Version, s Service, policyDays []int, year int) (Entitlement, error) {
	var g Entitlement
	if !s.Employee {
		g.Basis = append(g.Basis, "The person works under a contract for service, so the Employment Act's annual leave does not apply.")
		return g, nil
	}
	yearStart, yearEnd := dayOf(year, time.January, 1), dayOf(year+1, time.January, 1)
	if !s.Start.Before(yearEnd) || (s.Leaving != nil && s.Leaving.Before(yearStart)) {
		g.Basis = append(g.Basis, "The person was not employed in this leave year.")
		return g, nil
	}
	end := yearEnd
	if s.Leaving != nil && s.Leaving.AddDate(0, 0, 1).Before(end) {
		end = s.Leaving.AddDate(0, 0, 1)
	}
	joined := s.Start.Year() == year
	if joined && completedMonths(s.Start, end) < 3 {
		g.Basis = append(g.Basis, "The person has not yet completed three months of service, so annual leave starts when they do.")
		return g, nil
	}
	catchUp := 0 // the months of the joining year that granted nothing, because they were fewer than three
	if s.Start.Year() == year-1 {
		if m := completedMonths(s.Start, yearStart); m < 3 {
			catchUp = m
		}
	}
	total := 0 // the sum of days times months, over twelve
	for n := 1; ; n++ {
		from, until := anniversary(s.Start, n-1), anniversary(s.Start, n)
		if !until.After(yearStart) {
			continue
		}
		if !from.Before(end) {
			break
		}
		from, until = maxDay(from, yearStart), minDay(until, end)
		months := completedMonths(from, until)
		if n == 1 {
			months += catchUp
		}
		if months == 0 {
			continue
		}
		statute, ok := pack.AnnualLeaveDays(v, n, from)
		if !ok {
			return g, fmt.Errorf("the pack %s has no annual leave days for year of service %d", v.ID, n)
		}
		days := statute
		if !s.EACovered {
			days = 0
		}
		if p := at(policyDays, n-1); p > days || !s.EACovered {
			days = p
		}
		total += days * months
		g.Months += months
		g.Basis = append(g.Basis, fmt.Sprintf("Year of service %d: %d days a year, %d completed months of 12.", n, days, months))
	}
	g.Days = Whole((total + 6) / 12)
	g.Basis = append(g.Basis, fmt.Sprintf("Added up and rounded to a whole day (under one half down, a half or more up): %s days.", g.Days))
	g.Sources = []string{v.ID + " annual_leave"}
	if len(policyDays) > 0 {
		g.Sources = append(g.Sources, "the company policy")
	}
	return g, nil
}

func maxDay(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minDay(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// DefaultCarryMonths is how long unused annual leave is carried into the next year before it expires, until the company's policy says otherwise (owner decision
// O1 of p1-nemura-leave-time, default A: it matches the statutory right of a Part IV employee).
const DefaultCarryMonths = 12

// CarryForward is what a leave year leaves for the next: the unused balance, and the last day it may be taken, the end of the month this many months
// after the year ends. A balance of nothing or below carries nothing; zero months carries nothing.
func CarryForward(left Days, year, months int) (Days, time.Time) {
	if left <= 0 || months <= 0 {
		return 0, time.Time{}
	}
	end := dayOf(year+1, time.January, 1).AddDate(0, months, 0).AddDate(0, 0, -1)
	return left, end
}

// ExpiredCarry is how much of a carried-forward grant lapses when its last day has passed: days taken or booked in the new year use the carried days first, so
// what lapses is the carried amount less all that was taken and booked, and never more than is left.
func ExpiredCarry(carried Days, b Balance) Days {
	lapse := carried - b.Taken - b.Booked
	return max(min(lapse, b.Left), 0)
}
