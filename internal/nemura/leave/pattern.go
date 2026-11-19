package leave

import (
	"fmt"
	"slices"
	"time"
)

// Codes of the refusals of this package.
const (
	CodeInvalid  = "NM_LEAVE_INVALID"
	CodeBalance  = "NM_LEAVE_BALANCE"
	CodeConflict = "NM_LEAVE_CONFLICT"
	CodeNotFound = "NM_NOT_FOUND"
	CodeRole     = "NM_ROLE_REQUIRED"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) *Refusal {
	return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)}
}

// Pattern is the days a person works and the hours: leave counts days by it (NM-TIM-01, design D3). Weekdays are ISO: 1 Monday to 7 Sunday.
type Pattern struct {
	Weekdays     []int
	HoursPerWeek int // in tenths of an hour: 44.0 hours is 440
}

// Check validates a pattern the way the database does, with the field named.
func (p Pattern) Check() error {
	if len(p.Weekdays) == 0 || len(p.Weekdays) > 7 {
		return invalid("The working days are required: choose the days of the week the person works.")
	}
	seen := map[int]bool{}
	for _, d := range p.Weekdays {
		if d < 1 || d > 7 || seen[d] {
			return invalid("The working days name each day of the week once, from Monday (1) to Sunday (7): remove the repeated or unknown day.")
		}
		seen[d] = true
	}
	if p.HoursPerWeek <= 0 || p.HoursPerWeek > 1680 {
		return invalid("The hours a week must be more than 0 and at most 168: correct the hours.")
	}
	return nil
}

// Works says whether the person works on a day of the week.
func (p Pattern) Works(d time.Weekday) bool {
	iso := int(d)
	if d == time.Sunday {
		iso = 7
	}
	return slices.Contains(p.Weekdays, iso)
}

// DaysPerWeek is how many days a week the person works.
func (p Pattern) DaysPerWeek() int { return len(p.Weekdays) }
