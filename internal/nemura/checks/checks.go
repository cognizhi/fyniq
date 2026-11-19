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
