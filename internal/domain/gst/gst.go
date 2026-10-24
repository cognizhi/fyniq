// Package gst holds the GST rules that are the organisation's own: its registrations, the status they give
// on a date, and the return periods it files for (FR-ORG-03). The sg-gst pack's rates and codes arrive with
// p1-statutory-packs, and the returns themselves with p1-gst.
package gst

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// The codes of a registration's rules.
const (
	CodeDates     = "GST-01" // deregistered on or before it starts
	CodeNumber    = "GST-02" // the GST registration number is blank or too long
	CodeFrequency = "GST-03" // not quarterly or monthly
	CodeQuarter   = "GST-04" // the quarters' start month is not 1, 2 or 3
	CodeBasis     = "GST-05" // an accounting basis Fyniq does not offer
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

// Problems are every rule that failed.
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		parts[i] = fmt.Sprintf("%s %s: %s (%s)", x.Code, x.Field, x.Message, x.Fix)
	}
	return strings.Join(parts, "; ")
}

// Check returns every rule r breaks, or nil. Overlap with another registration is the database's to
// refuse (gst_registration_no_overlap), under its lock.
func Check(r storage.NewGSTRegistration) Problems {
	var out Problems
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if r.DeregisteredFrom != nil && !r.DeregisteredFrom.After(r.RegisteredFrom) {
		add(CodeDates, "deregistered_from", fmt.Sprintf("%s is not after the registration's start, %s",
			r.DeregisteredFrom.Format(time.DateOnly), r.RegisteredFrom.Format(time.DateOnly)),
			"Enter the first day the organisation is no longer registered, after it was registered")
	}
	if n := strings.TrimSpace(r.GSTNumber); n == "" || len([]rune(n)) > 20 {
		add(CodeNumber, "gst_number", "is blank or longer than 20 characters", "Enter the GST registration number on IRAS's notice of registration")
	}
	if r.ReturnFrequency != storage.ReturnQuarterly && r.ReturnFrequency != storage.ReturnMonthly {
		add(CodeFrequency, "return_frequency", fmt.Sprintf("%q is not a return frequency", r.ReturnFrequency), "Enter quarterly or monthly")
	}
	if r.QuarterStartMonth < 1 || r.QuarterStartMonth > 3 {
		add(CodeQuarter, "quarter_start_month", fmt.Sprintf("%d is not 1, 2 or 3", r.QuarterStartMonth),
			"Enter the month of the year the first quarter starts in: 1 (quarters from January, April, July, October), 2 or 3")
	}
	if r.AccountingBasis != storage.BasisInvoice {
		add(CodeBasis, "accounting_basis", fmt.Sprintf("%q is not offered", r.AccountingBasis),
			"Use invoice basis: cash basis under IRAS's Cash Accounting Scheme is offered once its rules are pinned (ENG-45)")
	}
	return out
}

// valid reports whether d falls within r's registration: on or after RegisteredFrom and before
// DeregisteredFrom.
func valid(r storage.GSTRegistration, d time.Time) bool {
	return !d.Before(r.RegisteredFrom) && (r.DeregisteredFrom == nil || d.Before(*r.DeregisteredFrom))
}

// StatusOn returns the registration in force on d, or false when the organisation was not registered then.
func StatusOn(regs []storage.GSTRegistration, d time.Time) (storage.GSTRegistration, bool) {
	for _, r := range regs {
		if valid(r, d) {
			return r, true
		}
	}
	return storage.GSTRegistration{}, false
}

// ReturnPeriod returns the first and last day of the GST return period of r that contains d: a calendar
// month, or a quarter that starts in r's quarter start month or every third after it. A period never
// starts before the registration or runs past it, so the first and last may be short.
// [verify] IRAS may set a different first period on its notice of registration; this follows the
// frequency and alignment recorded here.
func ReturnPeriod(r storage.GSTRegistration, d time.Time) (start, end time.Time, ok bool) {
	if !valid(r, d) {
		return time.Time{}, time.Time{}, false
	}
	first := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	months := 1
	if r.ReturnFrequency == storage.ReturnQuarterly {
		months = 3
		first = first.AddDate(0, -((int(d.Month())-r.QuarterStartMonth)%3+3)%3, 0)
	}
	start, end = first, first.AddDate(0, months, -1)
	if start.Before(r.RegisteredFrom) {
		start = r.RegisteredFrom
	}
	if r.DeregisteredFrom != nil && !end.Before(*r.DeregisteredFrom) {
		end = r.DeregisteredFrom.AddDate(0, 0, -1)
	}
	return start, end, true
}

// releasePacks are the pack versions this release carries.
var releasePacks = func() ([]pack.Version, error) { return pack.LoadAll(packs.FS) }

// Version is the sg-gst version in force on the document's date among the organisation's active versions, else among
// the versions this release carries: the GST codes and rates the document is read with (ENG-31).
func Version(ctx context.Context, u storage.UnitOfWork, on time.Time) (pack.Version, error) {
	all, err := releasePacks()
	if err != nil {
		return pack.Version{}, err
	}
	if on.IsZero() { // a document without a date has no tax point yet: its codes are read from the newest version
		for _, v := range all {
			if v.Pack == "sg-gst" && v.EffectiveFrom.After(on) {
				on = v.EffectiveFrom
			}
		}
	}
	acts, err := u.Packs().Activations(ctx)
	if err != nil {
		return pack.Version{}, err
	}
	var active []pack.Version
	for _, v := range all {
		if slices.ContainsFunc(acts, func(a storage.PackActivation) bool { return a.VersionID == v.ID }) {
			active = append(active, v)
		}
	}
	if v, ok := pack.At(active, "sg-gst", on); ok {
		return v, nil
	}
	if v, ok := pack.At(all, "sg-gst", on); ok {
		return v, nil
	}
	return pack.Version{}, fmt.Errorf("this release carries no sg-gst version in force on %s", on.Format(time.DateOnly))
}
