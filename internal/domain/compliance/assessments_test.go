package compliance

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func ymd(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// A finding is due by its severity's days from the day it was found: Critical 7, High 30 (CTL-RES-04's SLA), Medium 90, Low 180; an
// informational one has no day.
// req: SEC-25
// asvs: V15.1.1
func TestAFindingIsDueBySeverity(t *testing.T) {
	found := ymd(2026, 11, 2)
	for sev, want := range map[string]time.Time{"critical": ymd(2026, 11, 9), "high": ymd(2026, 12, 2), "medium": ymd(2027, 1, 31), "low": ymd(2027, 5, 1)} {
		if got := FindingDue(sev, found); got == nil || !got.Equal(want) {
			t.Errorf("%s: due %v, want %v", sev, got, want)
		}
	}
	if FindingDue("info", found) != nil {
		t.Error("an informational finding has no day")
	}
}

// The register's review (SEC-25, CTL-CHG-12): another organisation than the installation's has nothing to record; with nothing recorded
// it waits quietly; a recent independent assessment is signed; one over 12 months old is overdue; a Critical finding past its due day is
// overdue, and its fix without a retest waits 30 days; an accepted or retested one waits for nothing; Medium findings never make it overdue.
// req: SEC-25
// req: CTL-CHG-12
// asvs: V15.1.1
func TestTheSecurityAssessmentsReview(t *testing.T) {
	today := ymd(2026, 11, 20)
	by, at := "user:u-1", ymd(2026, 11, 1)
	assess := func(ended time.Time, independent bool, fs ...storage.Finding) storage.Assessment {
		return storage.Assessment{Kind: "penetration_test", Independent: independent, EndedOn: ended, RecordedBy: by, RecordedAt: at, Findings: fs}
	}
	finding := func(sev string, found time.Time, steps ...storage.FindingStep) storage.Finding {
		return storage.Finding{Ref: "F-1", Title: "Reflected script", Severity: sev, FoundOn: found, DueOn: FindingDue(sev, found), Steps: steps}
	}
	step := func(s string, on time.Time) storage.FindingStep { return storage.FindingStep{Step: s, OnDate: on} }
	for _, c := range []struct {
		what         string
		installation bool
		list         []storage.Assessment
		status       string
		overdue      int
	}{
		{"not the installation's", false, nil, ReviewNotApplicable, 0},
		{"nothing recorded", true, nil, ReviewDue, 0},
		{"a recent independent test", true, []storage.Assessment{assess(ymd(2026, 6, 1), true)}, ReviewSigned, 0},
		{"an internal test only", true, []storage.Assessment{assess(ymd(2026, 6, 1), false)}, ReviewDue, 0},
		{"a test over a year old", true, []storage.Assessment{assess(ymd(2025, 6, 1), true)}, ReviewOverdue, 1},
		{"a test falling due within 30 days", true, []storage.Assessment{assess(ymd(2025, 12, 1), true)}, ReviewDue, 0},
		{"a critical finding open past 7 days", true, []storage.Assessment{assess(ymd(2026, 10, 20), true, finding("critical", ymd(2026, 10, 25)))}, ReviewOverdue, 1},
		{"a critical finding open within 7 days", true, []storage.Assessment{assess(ymd(2026, 11, 18), true, finding("critical", ymd(2026, 11, 18)))}, ReviewDue, 0},
		{"a critical fixed and not retested for under 30 days", true, []storage.Assessment{assess(ymd(2026, 10, 20), true,
			finding("critical", ymd(2026, 10, 25), step("fixed", ymd(2026, 11, 5))))}, ReviewDue, 0},
		{"a critical fixed and not retested for over 30 days", true, []storage.Assessment{assess(ymd(2026, 9, 1), true,
			finding("critical", ymd(2026, 9, 5), step("fixed", ymd(2026, 9, 10))))}, ReviewOverdue, 1},
		{"a critical retested", true, []storage.Assessment{assess(ymd(2026, 9, 1), true, finding("critical", ymd(2026, 9, 5), step("fixed", ymd(2026, 9, 10)),
			step("retested", ymd(2026, 9, 20))))}, ReviewSigned, 0},
		{"a high accepted", true, []storage.Assessment{assess(ymd(2026, 5, 1), true, finding("high", ymd(2026, 5, 5), step("accepted", ymd(2026, 5, 20))))}, ReviewSigned, 0},
		{"a medium open past 90 days", true, []storage.Assessment{assess(ymd(2026, 5, 1), true, finding("medium", ymd(2026, 5, 5)))}, ReviewSigned, 0},
	} {
		r := assessmentsReview(c.list, today, c.installation)
		if r.Status != c.status || r.Overdue != c.overdue {
			t.Errorf("%s: %s with %d overdue (%s), want %s with %d", c.what, r.Status, r.Overdue, r.Detail, c.status, c.overdue)
		}
	}
}
