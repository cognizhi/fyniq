// Package checks are the people checks of PRD-002 Appendix C N.1 that need only the people record and the packs: #3 a work pass, #8 next year's
// calendar, and #13 a live calculation that needs an unverified value with no recorded decision. The others need leave, letters or payroll and arrive with
// them. Each check is a pure function over facts the caller reads, so a fixture triggers every message.
package checks

import (
	"fmt"
	"sort"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// Status of a finding: ✖ blocks, ⚠ needs a look, ⏰ is due.
const (
	Fail = "fail"
	Warn = "warn"
	Due  = "due"
)

// Finding is one thing a check found.
type Finding struct {
	Check   int    // the row of N.1
	Area    string // People or Packs
	Status  string
	Subject string // what it is about: a person's ID, a pack version
	Message string // in plain words, saying how to fix it
}

// Pass is a work pass with what the check needs to know about its person.
type Pass struct {
	Person       string
	Type         string
	Expires      time.Time
	LeavingDate  *time.Time
	RenewedLater bool // a later pass of the same person and type is recorded
}

// WorkPasses is N.1 #3 (NM-PPL-03): a pass that expires within 30 days with no renewal or leaving date, or has expired, is ✖.
func WorkPasses(passes []Pass, on time.Time) []Finding {
	var out []Finding
	for _, p := range passes {
		if p.RenewedLater || p.LeavingDate != nil {
			continue
		}
		days := int(p.Expires.Sub(on).Hours() / 24)
		switch {
		case p.Expires.Before(on):
			out = append(out, Finding{3, "People", Fail, p.Person, fmt.Sprintf("A %s expired on %s and no renewal or leaving date is recorded: record the renewed pass or the leaving date.", p.Type, p.Expires.Format(time.DateOnly))})
		case days <= 30:
			out = append(out, Finding{3, "People", Fail, p.Person, fmt.Sprintf("A %s expires on %s, in %d days, with no renewal or leaving date: record the renewed pass or the leaving date.", p.Type, p.Expires.Format(time.DateOnly), days)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// Calendar is N.1 #8 (NM-PACK-05): from 1 December, next year's public-holiday calendar must be in the pack in force.
func Calendar(v pack.Version, on time.Time) []Finding {
	if on.Month() < time.December {
		return nil
	}
	next := on.Year() + 1
	if len(pack.HolidaysIn(v, next)) > 0 {
		return nil
	}
	return []Finding{{8, "Packs", Warn, v.ID, fmt.Sprintf("The public holidays of %d are not in %s: MOM publishes them, so load a pack version that holds them before leave or pay is worked out for %d.", next, v.ID, next)}}
}

// Decision is a company's recorded decision to use a value that is only researched.
type Decision struct{ Version, Table, Key string }

// UnverifiedInUse is N.1 #13 (NM-PACK-07): a live calculation that reads a value that is not verified, with no recorded company decision for it, is ✖.
func UnverifiedInUse(version string, used []pack.Ref, decided []Decision) []Finding {
	have := map[Decision]bool{}
	for _, d := range decided {
		have[d] = true
	}
	var out []Finding
	for _, u := range used {
		if u.Status == pack.Verified || have[Decision{version, u.Table, u.Key}] {
			continue
		}
		out = append(out, Finding{13, "Packs", Fail, version, fmt.Sprintf("The company policy needs the %s value %q of %s, which is only researched and has no recorded decision: record the company's decision to use it, or wait for it to be verified.", u.Table, u.Key, version)})
	}
	return out
}

// GovClaim is a block of government-paid leave with whether any claim for it has been filed.
type GovClaim struct {
	Person   string
	Scheme   string
	Deadline time.Time
	Filed    bool
}

// GovClaims is N.1 #10 (NM-LV-07): a reimbursement claim not filed 30 days before its deadline is ⏰, and once the deadline has passed the message says so.
func GovClaims(claims []GovClaim, on time.Time) []Finding {
	var out []Finding
	for _, c := range claims {
		if c.Filed {
			continue
		}
		days := int(c.Deadline.Sub(on).Hours() / 24)
		switch {
		case days < 0:
			out = append(out, Finding{10, "Leave", Due, c.Person, fmt.Sprintf("The reimbursement claim for %s leave had to be filed by %s and none is recorded: file it on the Government-Paid Leave portal if it is still open, or record why it was not.", c.Scheme, c.Deadline.Format(time.DateOnly))})
		case days <= 30:
			out = append(out, Finding{10, "Leave", Due, c.Person, fmt.Sprintf("The reimbursement claim for %s leave must be filed by %s, in %d days: file it on the Government-Paid Leave portal and record it here.", c.Scheme, c.Deadline.Format(time.DateOnly), days)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// Waiting is a leave request that has not been decided.
type Waiting struct {
	Person    string
	Requested time.Time
}

// workingDaysSince counts the working days after a day up to and including another, leaving out weekends and the public holidays of the pack.
func workingDaysSince(v pack.Version, from, to time.Time) int {
	n := 0
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		if _, holiday := pack.PublicHoliday(v, d); holiday {
			continue
		}
		n++
	}
	return n
}

// Negative is a balance below zero in a leave type that does not allow it.
type Negative struct {
	Person, Type string
	Year         int
	Left         string
}

// LeaveWaiting is N.1 #4 (NM-LV-02, NM-LV-04): a balance below zero for a type that does not allow it, or a leave request that has waited more than 3 working days
// for its approver, is ⚠.
func LeaveWaiting(v pack.Version, waiting []Waiting, negative []Negative, on time.Time) []Finding {
	var out []Finding
	for _, n := range negative {
		out = append(out, Finding{4, "Leave", Warn, n.Person, fmt.Sprintf("The %s balance for %d is %s days, below zero, and that leave type does not allow it: correct the ledger with a correcting entry.", n.Type, n.Year, n.Left)})
	}
	for _, w := range waiting {
		if days := workingDaysSince(v, w.Requested, on); days > 3 {
			out = append(out, Finding{4, "Leave", Warn, w.Person, fmt.Sprintf("A leave request has waited %d working days for its approver: ask the person's manager to decide it, or decide it as the people team.", days)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// FlexRequest is a flexible work request that has not been answered in writing.
type FlexRequest struct {
	Person string
	Due    time.Time
}

// FlexReplies is N.1 #5 (NM-LV-10): a flexible work request with no written reply is ⏰ from 14 days before its two months are up, and says so once they are.
func FlexReplies(waiting []FlexRequest, on time.Time) []Finding {
	var out []Finding
	for _, w := range waiting {
		days := int(w.Due.Sub(on).Hours() / 24)
		switch {
		case days < 0:
			out = append(out, Finding{5, "Requests", Due, w.Person, fmt.Sprintf("A flexible work request had to be answered in writing by %s and has no reply: reply now, with the business grounds if you refuse.", w.Due.Format(time.DateOnly))})
		case days <= 14:
			out = append(out, Finding{5, "Requests", Due, w.Person, fmt.Sprintf("A flexible work request must be answered in writing by %s, in %d days: reply, with the business grounds if you refuse.", w.Due.Format(time.DateOnly), days)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// OvertimeMonth is a person's overtime recorded in a month, in hundredths of an hour.
type OvertimeMonth struct {
	Person string
	Month  time.Time
	Hours  int
}

// OvertimeOverLimit is N.1 #16 (NM-TIM-04): a Part IV employee's overtime in a month beyond the pack's monthly limit is ⚠. The hours stay recorded and are paid; HR acts on
// them with an exemption or a change of pattern.
func OvertimeOverLimit(months []OvertimeMonth, limitHours int) []Finding {
	var out []Finding
	for _, m := range months {
		if m.Hours > limitHours*100 {
			out = append(out, Finding{16, "Time", Warn, m.Person, fmt.Sprintf("Overtime in %s is %d.%02d hours, beyond the monthly limit of %d: the hours stay recorded and are paid, so apply for an overtime exemption or change the person's working pattern.",
				m.Month.Format("January 2006"), m.Hours/100, m.Hours%100, limitHours)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}
