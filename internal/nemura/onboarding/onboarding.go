// Package onboarding holds the rules of a joiner's tasks and a leaver's dates (PRD-002 NM-ONB-02, NM-ONB-03): the kinds of leaving, the dates that start statutory clocks and
// are kept apart, and when a leaver's Nemura access becomes narrow. It is pure.
package onboarding

import (
	"fmt"
	"time"
)

// Codes of a refusal; each is in the API's catalogue.
const (
	CodeInvalid  = "NM_ONBOARDING_INVALID"
	CodeConflict = "NM_ONBOARDING_CONFLICT"
	CodeNotFound = "NM_NOT_FOUND"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) error { return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)} }

// Kinds of leaving.
const (
	Resignation   = "resignation"
	Termination   = "termination"
	Retrenchment  = "retrenchment"
	EndOfContract = "end_of_contract"
	Retirement    = "retirement"
	Other         = "other"
)

// Kinds are the kinds of leaving, in the order the page lists them.
var Kinds = []string{Resignation, Termination, Retrenchment, EndOfContract, Retirement, Other}

// Leaving is a person's offboarding: the dates that start statutory clocks, kept apart (NM-ONB-03). NoticeGiven is when notice was given or the company learned of the
// cessation (it starts the tax-clearance hold), LastDay is the last day of employment (final pay), and Retrenchment is, for a retrenchment only, when employees were told.
// The checklist dates are optional.
type Leaving struct {
	Kind                                                 string
	NoticeGiven, Retrenchment                            *time.Time
	LastDay                                              time.Time
	EquipmentReturned, ExitInterview, FyniqAccessRemoved *time.Time
}

// Check refuses a leaving that is not whole, naming the field.
func Check(l Leaving, start time.Time) error {
	known := false
	for _, k := range Kinds {
		known = known || k == l.Kind
	}
	if !known {
		return invalid("The kind of leaving is not resignation, termination, retrenchment, end_of_contract, retirement or other.")
	}
	if l.LastDay.IsZero() {
		return invalid("The last day is required: enter the last day of employment, the notice period included.")
	}
	if l.LastDay.Before(start) {
		return invalid("The last day is before the person started: check both dates.")
	}
	if l.NoticeGiven != nil && l.NoticeGiven.After(l.LastDay) {
		return invalid("Notice cannot be given after the last day: check the date notice was given.")
	}
	if l.Retrenchment != nil && l.Kind != Retrenchment {
		return invalid("The day employees were told is for a retrenchment only: leave it empty, or choose retrenchment.")
	}
	if l.Retrenchment != nil && l.Retrenchment.After(l.LastDay) {
		return invalid("Employees cannot be told of a retrenchment after the last day: check the date.")
	}
	return nil
}

// AccessNarrowsOn is the day a leaver's Nemura account becomes the narrow one for their own documents: the day after the last day (NM-ESS-06).
func AccessNarrowsOn(last time.Time) time.Time { return last.AddDate(0, 0, 1) }
