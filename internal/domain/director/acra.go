package director

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The ACRA steps of a conversion (FR-EQ-09; design D12), and what they raise.
const (
	CodeStep            = "FOUNDER-37" // a step that does not fit: its kind, a day after today, or a reference too long
	CodeApprovalDue     = "FOUNDER-38" // ⏰ the shareholders' approval is not lodged; due 14 days after the resolution
	CodeNotRegistered   = "FOUNDER-39" // ⚠ approved and not registered after the set number of days
	CodeApprovalLapsing = "FOUNDER-40" // ⚠ the approval lapses within 30 days and the conversion is not effective
)

// RegistrationDaysSetting is how many days after its approval a conversion not registered by ACRA is flagged (FR-EQ-09:
// default 14, matching ACRA's guidance; the Act sets no deadline for private companies).
const RegistrationDaysSetting = "founders.registration_days"

// LodgeWithin is how many days after the resolution the shareholders' approval is lodged with ACRA (Companies Act
// s161(5), s186). LapsingWithin is how long before the approval lapses it is flagged.
const (
	LodgeWithin   = 14
	LapsingWithin = 30
)

// StepKinds are the steps a conversion records, in their order.
var StepKinds = []string{storage.StepResolved, storage.StepApprovalLodged, storage.StepAGMDue, storage.StepAllotmentLodged}

// MaxStepReference is the longest reference a step keeps.
const MaxStepReference = 200

// Steps are a conversion's latest step of each kind.
type Steps map[string]storage.ConversionStep

// StepsOf are the latest step of each kind among a conversion's steps, recorded earliest first.
func StepsOf(all []storage.ConversionStep) Steps {
	out := Steps{}
	for _, s := range all {
		out[s.Kind] = s
	}
	return out
}

// CheckStep is what is wrong with a step as entered.
func CheckStep(kind string, on time.Time, reference string, today time.Time) error {
	switch {
	case !slices.Contains(StepKinds, kind):
		return &Error{CodeStep, fmt.Sprintf("%q is not a step of a conversion", kind), "Choose resolved, approval_lodged, agm_due or allotment_lodged"}
	case kind != storage.StepAGMDue && on.After(today):
		return &Error{CodeStep, fmt.Sprintf("the step is dated %s, after today", on.Format(time.DateOnly)), "Record a step once it has happened"}
	case len(reference) > MaxStepReference:
		return &Error{CodeStep, fmt.Sprintf("the reference has %d characters; the most is %d", len(reference), MaxStepReference), "Enter ACRA's transaction number only"}
	}
	return nil
}

// LapsesOnFor is the day a conversion's approval lapses: the AGM date recorded as a step, else the one LapsesOn works out
// from the day it was approved [verify].
func LapsesOnFor(steps Steps, p storage.OrganisationProfile, approvedOn time.Time) (time.Time, bool) {
	if s, ok := steps[storage.StepAGMDue]; ok {
		return s.On, true
	}
	return LapsesOn(p, approvedOn)
}

// Flag is what a conversion's steps raise: ⏰ due, or ⚠ warning.
type Flag struct {
	Code, Kind, Message, Fix string
	DueOn                    *time.Time
}

// Flags are what an approved conversion that is not effective raises on today (design D12): ⏰ until its approval is
// lodged, due 14 days after the resolution (or its approval, when the resolution is not recorded); ⚠ once it is not
// registered the set number of days after its approval; ⚠ within 30 days of the day its approval lapses.
func Flags(steps Steps, approvedOn, lapsesOn, today time.Time, registrationDays int) []Flag {
	var out []Flag
	day := func(t time.Time) string { return t.Format("2 Jan 2006") }
	if _, ok := steps[storage.StepApprovalLodged]; !ok {
		resolved := approvedOn
		if s, ok := steps[storage.StepResolved]; ok {
			resolved = s.On
		}
		due := resolved.AddDate(0, 0, LodgeWithin)
		out = append(out, Flag{Code: CodeApprovalDue, Kind: "due", DueOn: &due,
			Message: "the shareholders' approval to issue the shares is due to be lodged with ACRA by " + day(due),
			Fix:     "Lodge it, attach ACRA's acknowledgement and record the day it was lodged"})
	}
	if registrationDays > 0 {
		if since := int(today.Sub(approvedOn) / (24 * time.Hour)); since > registrationDays {
			out = append(out, Flag{Code: CodeNotRegistered, Kind: "warning",
				Message: fmt.Sprintf("approved %s and not registered by ACRA after %d days", day(approvedOn), registrationDays),
				Fix:     "Lodge the return of allotment with the conversion agreement, then record the day ACRA's register shows the shares"})
		}
	}
	if !lapsesOn.IsZero() && !today.Before(lapsesOn.AddDate(0, 0, -LapsingWithin)) {
		lapses := lapsesOn
		out = append(out, Flag{Code: CodeApprovalLapsing, Kind: "warning", DueOn: &lapses,
			Message: "the shareholders' approval lapses on " + day(lapsesOn) + ", the date by which the next AGM must be held",
			Fix:     "Have ACRA register the shares before then, or draft the conversion again for a new approval"})
	}
	return out
}

// RegistrationDays is the setting in force on a day: 14 by default.
func RegistrationDays(ctx context.Context, u storage.UnitOfWork, on time.Time) (int, error) {
	changes, err := u.Settings().Changes(ctx, RegistrationDaysSetting)
	if err != nil {
		return 0, err
	}
	def, _ := settings.Lookup(RegistrationDaysSetting)
	n, err := strconv.Atoi(settings.On(def, changes, on).Value)
	if err != nil {
		return 0, err
	}
	return n, nil
}
