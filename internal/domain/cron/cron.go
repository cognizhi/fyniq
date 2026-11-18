// Package cron is a scheduled agent's schedule (PRD-001 AI-62; p1-scheduled-agents design D2, D4): five-field cron
// expressions in Singapore time, and quiet hours as a time range. "off" turns an entry off.
package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
)

// Off is the value of an entry that never runs.
const Off = "off"

// Spec is a parsed five-field cron expression: minute, hour, day of the month, month and day of the week, each a set
// of the values it matches. With both day fields restricted, a day matches either (as cron has always done).
type Spec struct {
	minute, hour, dom, month, dow [60]bool
	domAny, dowAny                bool
	off                           bool
}

var fields = []struct {
	name   string
	lo, hi int
}{{"minute", 0, 59}, {"hour", 0, 23}, {"day of the month", 1, 31}, {"month", 1, 12}, {"day of the week", 0, 7}}

// Parse reads expr: five fields of numbers, *, ranges (1-5), lists (1,15) and steps (*/15, 0-30/10); or "off".
func Parse(expr string) (Spec, error) {
	var s Spec
	expr = strings.TrimSpace(expr)
	if expr == Off {
		s.off = true
		return s, nil
	}
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return s, fmt.Errorf("%q has %d fields: a schedule has five, minute hour day month weekday, such as 0 7 * * *", expr, len(parts))
	}
	sets := []*[60]bool{&s.minute, &s.hour, &s.dom, &s.month, &s.dow}
	for i, p := range parts {
		if err := set(sets[i], p, fields[i].lo, fields[i].hi); err != nil {
			return s, fmt.Errorf("its %s %q: %w", fields[i].name, p, err)
		}
	}
	if s.dow[7] { // 7 is Sunday too
		s.dow[0] = true
	}
	s.domAny, s.dowAny = parts[2] == "*", parts[4] == "*"
	return s, nil
}

func set(into *[60]bool, field string, lo, hi int) error {
	for _, item := range strings.Split(field, ",") {
		rng, stepText, stepped := strings.Cut(item, "/")
		step := 1
		if stepped {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return errors.New("a step is a whole number from 1")
			}
			step = n
		}
		from, to := lo, hi
		if rng != "*" {
			a, b, ranged := strings.Cut(rng, "-")
			var err error
			if from, err = strconv.Atoi(a); err != nil {
				return errors.New("not a number, *, a range or a list")
			}
			to = from
			if ranged {
				if to, err = strconv.Atoi(b); err != nil {
					return errors.New("a range is two numbers, such as 1-5")
				}
			} else if stepped {
				to = hi
			}
		}
		if from < lo || to > hi || from > to {
			return fmt.Errorf("outside %d to %d", lo, hi)
		}
		for v := from; v <= to; v += step {
			into[v] = true
		}
	}
	return nil
}

// Off reports whether the entry never runs.
func (s Spec) Off() bool { return s.off }

func (s Spec) day(t time.Time) bool {
	if !s.month[int(t.Month())] {
		return false
	}
	dom, dow := s.dom[t.Day()], s.dow[int(t.Weekday())]
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	}
	return dom || dow
}

// Next is the first time after after, to the minute, that s matches in Singapore time; the zero time when it never
// does (off, or no such day within eight years, such as 30 February).
func (s Spec) Next(after time.Time) time.Time {
	if s.off {
		return time.Time{}
	}
	t := after.In(clock.Singapore).Truncate(time.Minute).Add(time.Minute)
	for d := 0; d < 8*366; d++ {
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, clock.Singapore)
		if s.day(day) {
			for h := 0; h < 24; h++ {
				if !s.hour[h] || (d == 0 && h < t.Hour()) {
					continue
				}
				for m := 0; m < 60; m++ {
					if !s.minute[m] || (d == 0 && h == t.Hour() && m < t.Minute()) {
						continue
					}
					return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, clock.Singapore)
				}
			}
		}
		t = day.AddDate(0, 0, 1)
	}
	return time.Time{}
}

// Range is a daily time range in Singapore time, such as quiet hours from 22:00 to 07:00, which wraps past midnight.
type Range struct {
	from, to int // minutes after midnight
	off      bool
}

// ParseRange reads "HH:MM-HH:MM", or "off".
func ParseRange(text string) (Range, error) {
	text = strings.TrimSpace(text)
	if text == Off {
		return Range{off: true}, nil
	}
	a, b, ok := strings.Cut(text, "-")
	from, err1 := minutes(a)
	to, err2 := minutes(b)
	if !ok || err1 != nil || err2 != nil || from == to {
		return Range{}, fmt.Errorf("%q is not a time range: send two times of the day, such as 22:00-07:00, or off", text)
	}
	return Range{from: from, to: to}, nil
}

func minutes(hhmm string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(hhmm), ":")
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if !ok || len(h) != 2 || len(m) != 2 || err1 != nil || err2 != nil || hh > 23 || mm > 59 || hh < 0 || mm < 0 {
		return 0, errors.New("not HH:MM")
	}
	return hh*60 + mm, nil
}

// Contains reports whether t falls in the range, in Singapore time.
func (r Range) Contains(t time.Time) bool {
	if r.off {
		return false
	}
	t = t.In(clock.Singapore)
	m := t.Hour()*60 + t.Minute()
	if r.from < r.to {
		return m >= r.from && m < r.to
	}
	return m >= r.from || m < r.to
}

// End is when the range containing t ends: t itself when it is outside.
func (r Range) End(t time.Time) time.Time {
	if !r.Contains(t) {
		return t
	}
	s := t.In(clock.Singapore)
	end := time.Date(s.Year(), s.Month(), s.Day(), r.to/60, r.to%60, 0, 0, clock.Singapore)
	if !end.After(s) {
		end = end.AddDate(0, 0, 1)
	}
	return end
}
