package leave

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// Counting bases of a leave type (NM-LV-11): working days by the person's pattern with public holidays and rest days left out, or calendar days.
const (
	WorkingDays  = "working_days"
	CalendarDays = "calendar_days"
)

// MaxSpan is the longest request in calendar days: a longer ask is split by the person, who knows where it divides.
const MaxSpan = 366

// Observed are the public holidays a person has off between two days, by date: a holiday on a day the person works is that day; one on a rest day makes
// the next working day that is not itself a holiday the holiday (Appendix A, MOM). Holidays that fall before the range are read too, as one may land in it.
// A year the pack holds no calendar for is refused, so a day is never counted as working when the holidays are not yet published (NM-PACK-05).
func Observed(v pack.Version, p Pattern, from, to time.Time) (map[string]string, error) {
	for y := from.Year(); y <= to.Year(); y++ {
		if len(pack.HolidaysIn(v, y)) == 0 {
			return nil, invalid("The public holidays for %d are not in the country pack yet: ask again once they are published.", y)
		}
	}
	out := map[string]string{}
	for d := from.AddDate(0, 0, -7); !d.After(to); d = d.AddDate(0, 0, 1) {
		name, ok := pack.PublicHoliday(v, d)
		if !ok {
			continue
		}
		obs := d
		for i := 0; i < 7 && !p.Works(obs.Weekday()); i++ {
			obs = obs.AddDate(0, 0, 1)
		}
		for i := 0; i < 7; i++ { // the next working day that is not another holiday
			if _, other := pack.PublicHoliday(v, obs); other && !obs.Equal(d) {
				obs = obs.AddDate(0, 0, 1)
				for j := 0; j < 7 && !p.Works(obs.Weekday()); j++ {
					obs = obs.AddDate(0, 0, 1)
				}
				continue
			}
			break
		}
		if obs.Equal(d) {
			out[obs.Format(time.DateOnly)] = name
		} else {
			out[obs.Format(time.DateOnly)] = name + " (observed)"
		}
	}
	return out, nil
}

// Charge is what a request costs and how the days were counted.
type Charge struct {
	Days    Days
	Counted []Counted // each day that costs something
	Skipped []Skipped // each day left out, with why
}

// Counted is one day that costs leave.
type Counted struct {
	Date string
	Cost Days // 1.00 or 0.50
}

// Skipped is one day that costs nothing.
type Skipped struct{ Date, Why string }

// Count is the chargeable days of a request from one day to another, both included: every calendar day for a type counted so, otherwise the days the person works
// that are not public holidays. A half day on the first or the last day costs half a day, and only where that day costs anything. A request that costs nothing,
// runs backwards or is longer than MaxSpan is refused with the field named.
func Count(v pack.Version, p Pattern, basis string, from, to time.Time, startHalf, endHalf bool) (Charge, error) {
	var c Charge
	if to.Before(from) {
		return c, invalid("The last day is before the first day: choose a last day on or after %s.", from.Format(time.DateOnly))
	}
	if int(to.Sub(from).Hours()/24)+1 > MaxSpan {
		return c, invalid("The request is longer than %d days: split it at the point where it divides.", MaxSpan)
	}
	if err := p.Check(); err != nil {
		return c, err
	}
	var holidays map[string]string
	if basis == WorkingDays {
		var err error
		if holidays, err = Observed(v, p, from, to); err != nil {
			return c, err
		}
	} else if basis != CalendarDays {
		return c, fmt.Errorf("leave: unknown counting basis %q", basis)
	}
	for d := from; !d.Before(from) && !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format(time.DateOnly)
		if basis == WorkingDays {
			if !p.Works(d.Weekday()) {
				c.Skipped = append(c.Skipped, Skipped{key, "a rest day"})
				continue
			}
			if name, ok := holidays[key]; ok {
				c.Skipped = append(c.Skipped, Skipped{key, name})
				continue
			}
		}
		cost := Whole(1)
		if (d.Equal(from) && startHalf) || (d.Equal(to) && endHalf) {
			cost = 50
		}
		c.Counted = append(c.Counted, Counted{key, cost})
		c.Days += cost
	}
	if c.Days == 0 {
		return c, invalid("Those dates have no working days: they are rest days or public holidays, so there is nothing to ask for.")
	}
	return c, nil
}
