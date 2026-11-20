package compliance

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FindingDays is how many days a finding of each severity may stay open, from the day it was found (SEC-25, CTL-CHG-12; PRD-001
// rev 0.617 [owner to confirm]): Critical and High as CTL-RES-04's SLA (rev 0.247), Medium and Low beside them. An informational
// finding has no day.
var FindingDays = map[string]int{"critical": 7, "high": 30, "medium": 90, "low": 180}

// FindingDue is the day a finding found on found is due; nil for an informational one.
func FindingDue(severity string, found time.Time) *time.Time {
	n, ok := FindingDays[severity]
	if !ok {
		return nil
	}
	d := Day(found).AddDate(0, 0, n)
	return &d
}

// RetestWithin is how many days after a fix its retest may wait before the finding is overdue again.
const RetestWithin = 30

// FindingState is where a finding stands: open until it is fixed, fixed until it is retested, then retested; or accepted.
func FindingState(f storage.Finding) string {
	if n := len(f.Steps); n > 0 {
		switch f.Steps[n-1].Step {
		case "retested":
			return "retested"
		case "accepted":
			return "accepted"
		}
		return "fixed"
	}
	return "open"
}

// FindingOverdue is the day a finding of Critical or High severity that has not been closed falls overdue, and whether it has:
// an open finding after its due day, a fixed one 30 days after its fix when it was fixed late or is not retested by then.
func FindingOverdue(f storage.Finding, today time.Time) (due time.Time, late, waiting bool) {
	if f.DueOn == nil || (f.Severity != "critical" && f.Severity != "high") {
		return time.Time{}, false, false
	}
	switch FindingState(f) {
	case "retested", "accepted":
		return time.Time{}, false, false
	case "fixed":
		due = Day(f.Steps[len(f.Steps)-1].OnDate).AddDate(0, 0, RetestWithin)
	default:
		due = Day(*f.DueOn)
	}
	return due, today.After(due), true
}

// assessmentsReview (SEC-25, CTL-CHG-12): the register of security assessments of the platform, kept in the installation's own
// organisation. An independent assessment is due 12 months after the last one ended; nothing recorded yet waits quietly, since
// the first is needed before the first external customer is onboarded; each Critical or High finding waits for its fix and its
// retest and is overdue past its due day.
func assessmentsReview(assessments []storage.Assessment, today time.Time, installation bool) Review {
	r := Review{Kind: "security-assessments", Owner: "owner", Where: "/compliance/security-assessments",
		Evidence: "Each assessment with its tester, scope, dates and the report's SHA-256, and each finding with its severity, due day, fix and retest"}
	if !installation {
		r.Status = ReviewNotApplicable
		r.Detail = "The platform's security assessments are recorded in the installation's own organisation."
		return r
	}
	var last *time.Time
	for _, a := range assessments {
		r.signed(a.RecordedBy, a.RecordedAt)
		if a.Independent && (last == nil || a.EndedOn.After(*last)) {
			ended := a.EndedOn
			last = &ended
		}
		for _, f := range a.Findings {
			if due, late, waiting := FindingOverdue(f, today); waiting {
				r.wait(due, late)
				if r.Detail == "" || late {
					r.Detail = fmt.Sprintf("A %s finding, %s (%s), waits for its %s by %s.", f.Severity, f.Title, f.Ref, map[bool]string{true: "retest", false: "fix"}[FindingState(f) == "fixed"], dayOf(due))
				}
			}
		}
	}
	switch {
	case last == nil:
		r.Detail = cmpOr(r.Detail, "No independent penetration test is recorded yet: SEC-25 asks for one before the first external customer is onboarded.")
		if r.Waiting == 0 {
			r.Waiting = 1 // it waits, with no day: nothing is overdue before the first customer
		}
	default:
		due := Day(*last).AddDate(1, 0, 0)
		if !today.Before(due.AddDate(0, 0, -30)) {
			r.wait(due, today.After(due))
			if r.Detail == "" || today.After(due) {
				r.Detail = fmt.Sprintf("The next independent penetration test is due by %s.", dayOf(due))
			}
		} else if r.DueOn == nil {
			r.DueOn = &due
		}
	}
	r.settle(fmt.Sprintf("The last independent assessment ended on %s; the next is due within 12 months.", func() string {
		if last == nil {
			return "an unknown day"
		}
		return dayOf(*last)
	}()))
	return r
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
