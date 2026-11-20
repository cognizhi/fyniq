// Package performance holds the rules of goals and KPIs (PRD-002 NM-PRF-01, NM-PRF-02): what a goal and a measure must say, how far along a measure is and whether it is on track. It is pure: no
// storage, no clock, no network, and no float: values are decimal strings worked out exactly.
package performance

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// Codes of a refusal; each is in the API's catalogue.
const (
	CodeInvalid  = "NM_PERFORMANCE_INVALID"
	CodeConflict = "NM_PERFORMANCE_CONFLICT"
	CodeNotFound = "NM_NOT_FOUND"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) error { return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)} }

// Scopes of a goal.
const (
	Company  = "company"
	Team     = "team"
	Personal = "personal"
)

// Measure is a key result of a goal or a role KPI: a number with a target.
type Measure struct {
	Title, Unit            string
	Start, Target, Current string // decimal strings
	Down                   bool   // lower is better: response time, defects
}

var number = regexp.MustCompile(`^-?\d{1,12}(\.\d{1,4})?$`)

// Check refuses a measure that is not whole, naming the field.
func Check(m Measure) error {
	if n := len([]rune(strings.TrimSpace(m.Title))); n < 1 || n > 200 {
		return invalid("The title of a measure is 1 to 200 characters, such as Pilot customers live.")
	}
	if len([]rune(m.Unit)) > 30 {
		return invalid("The unit is at most 30 characters, such as customers or ms.")
	}
	for name, v := range map[string]string{"start value": m.Start, "target": m.Target, "current value": m.Current} {
		if !number.MatchString(v) {
			return invalid("The %s is a decimal number with at most four decimals, such as 3 or 12.5.", name)
		}
	}
	if rat(m.Start).Cmp(rat(m.Target)) == 0 {
		return invalid("The target is the same as the start value: choose a target to move towards.")
	}
	if m.Down != (rat(m.Target).Cmp(rat(m.Start)) < 0) {
		return invalid("The target is on the wrong side of the start value for the direction chosen: lower is better only when the target is below it.")
	}
	return nil
}

// CheckValue refuses a value that is not a decimal number with at most four decimals.
func CheckValue(v string) error {
	if !number.MatchString(v) {
		return invalid("The value is a decimal number with at most four decimals, such as 2 or 12.5.")
	}
	return nil
}

func rat(s string) *big.Rat {
	r, _ := new(big.Rat).SetString(s)
	return r
}

// Progress is how far a measure is from its start value to its target, as a whole percentage between 0 and 100, rounded down so that a goal is never shown more done than it is [owner to confirm].
func Progress(m Measure) int {
	span := new(big.Rat).Sub(rat(m.Target), rat(m.Start))
	done := new(big.Rat).Sub(rat(m.Current), rat(m.Start))
	pct := new(big.Rat).Quo(done, span)
	pct.Mul(pct, big.NewRat(100, 1))
	n := new(big.Int).Div(pct.Num(), pct.Denom()) // floor for the positive and the negative alike
	switch {
	case n.Sign() < 0:
		return 0
	case n.Cmp(big.NewInt(100)) > 0:
		return 100
	}
	return int(n.Int64())
}

// Status says where a measure stands: done at 100%, on track when its progress is at least the share of the period that has passed, behind otherwise. A measure with no period (a KPI) is met or not yet.
// Rollup is the progress of a goal with the goals aligned to it (NM-PRF-01): the whole-percentage average, rounded down, of its own key results' progress (when it has any) and the progress of each aligned
// goal. A goal with neither has none, so ok is false. Only the goals the reader may read are passed in, so a personal goal never shows through a number it was not meant to.
func Rollup(ownProgress int, hasMeasures bool, aligned []int) (progress int, ok bool) {
	sum, n := 0, len(aligned)
	for _, p := range aligned {
		sum += p
	}
	if hasMeasures {
		sum, n = sum+ownProgress, n+1
	}
	if n == 0 {
		return 0, false
	}
	return sum / n, true
}

func Status(progress int, from, due, today time.Time) string {
	if progress >= 100 {
		return "done"
	}
	if due.IsZero() || !due.After(from) {
		return "not_yet"
	}
	total := due.Sub(from).Hours()
	passed := today.Sub(from).Hours()
	if passed < 0 {
		passed = 0
	}
	if passed > total {
		passed = total
	}
	elapsed := int(passed / total * 100) // a share of days, not money
	if progress >= elapsed {
		return "on_track"
	}
	return "behind"
}

// CheckGoal refuses a goal that is not whole.
func CheckGoal(scope, title, team string, due, today time.Time, measures []Measure) error {
	switch scope {
	case Company, Team, Personal:
	default:
		return invalid("The scope is company, team or personal.")
	}
	if n := len([]rune(strings.TrimSpace(title))); n < 1 || n > 200 {
		return invalid("The title of a goal is 1 to 200 characters, such as 3 pilot customers live by 31 December.")
	}
	if (scope == Team) != (strings.TrimSpace(team) != "") {
		return invalid("A team goal names its team, and only a team goal does.")
	}
	if due.IsZero() || due.Before(today) {
		return invalid("The due date is today or later.")
	}
	if len(measures) > 10 {
		return invalid("A goal has at most 10 key results: split it.")
	}
	for i, m := range measures {
		if err := Check(m); err != nil {
			return invalid("Key result %d: %s", i+1, err.(*Refusal).Message)
		}
	}
	return nil
}
