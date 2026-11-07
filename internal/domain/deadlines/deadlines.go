// Package deadlines is the statutory calendar (PRD-001 FR-TAX-07, CMP-02; p1-gst design D5 and D6): every filing and
// payment deadline of the organisation, generated each time from the sg-deadlines pack's rules and the organisation's
// facts (its financial years, its GST return periods, whether it pays staff, its AGM route), never stored, so a changed
// year or registration moves the dates at once. What is stored is what a person recorded of an item: filed, with its
// reference and the acknowledgement as evidence, or not required with the reason; a record is withdrawn, never edited.
package deadlines

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/gstmonitor"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Status is where an item stands on today.
type Status string

// The statuses of an item (design D5).
const (
	Open        Status = "open"         // due later than in 14 days
	DueSoon     Status = "due_soon"     // due within 14 days
	Overdue     Status = "overdue"      // its day has passed, and nothing is recorded
	Filed       Status = "filed"        // recorded as filed
	NotRequired Status = "not_required" // recorded as not required, with the reason
	NotAssessed Status = "not_assessed" // an input is missing, which Reason names
)

// SoonDays is how near an item is due soon (C.2 #15).
const SoonDays = 14

// The settings the calendar reads (design D11).
const (
	SettingPaysStaff      = "calendar.pays_staff"
	SettingAGMRoute       = "corporate.agm_route"
	SettingBranchRegister = "corporate.overseas_branch_register"
)

// Item is one deadline.
type Item struct {
	Key        string // the obligation and its anchor, such as gst_f5:2027-03-31
	Obligation string
	Area       string // tax, payroll or corporate
	Title      string
	Anchor     time.Time
	Due        time.Time // zero when not assessed
	Why        string    // the rule's words, from the pack
	Source     string    // the pack's source ids
	Extended   bool      // a one-off extension moved it
	Rolled     bool      // it moved to the next working day
	Status     Status
	Reason     string  // why it is not assessed
	Filing     *Filing // what was recorded, when it was
	Link       string  // where in the web app it is done, for a register's item
	Register   bool    // from a register's own record (design D7): done there, never recorded here
	Pack       string  // the pack version it was read with
}

// Calendar is the calendar from one day to another.
type Calendar struct {
	From, To, Today time.Time
	Pack            string    // the sg-deadlines version read
	Starts          time.Time // the day its rules start: nothing due before it is in the calendar
	Items           []Item    // by due day, the earliest first; not assessed ones first
}

// MaxMonths is the longest window a calendar is read for.
const MaxMonths = 18

// ErrWindow is a window longer than MaxMonths or ending before it starts.
var ErrWindow = errors.New("the calendar's window is longer than 18 months, or ends before it starts")

// Read generates the calendar of the items due from from to to, with every earlier one still open, as on today
// (design D5).
func Read(ctx context.Context, u storage.UnitOfWork, from, to, today time.Time) (Calendar, error) {
	from, to, today = dayOf(from), dayOf(to), dayOf(today)
	out := Calendar{From: from, To: to, Today: today}
	if to.Before(from) || to.After(from.AddDate(0, MaxMonths, 0)) {
		return out, ErrWindow
	}
	v, ok, err := gst.PackOn(ctx, u, "sg-deadlines", today)
	if err != nil {
		return out, err
	}
	if !ok {
		out.Items = []Item{{Key: "sg-deadlines:" + today.Format(time.DateOnly), Obligation: "sg-deadlines", Title: "The statutory calendar's rules",
			Status: NotAssessed, Reason: "No sg-deadlines version is in force on " + today.Format("2 Jan 2006") + ": activate the pack in Settings › Statutory packs"}}
		return out, nil
	}
	out.Pack, out.Starts = v.ID, v.EffectiveFrom
	filings, err := Filings(ctx, u)
	if err != nil {
		return out, err
	}
	facts, err := factsOn(ctx, u, today)
	if err != nil {
		return out, err
	}
	var items []Item
	add := func(obligation string, anchor time.Time, title string) {
		key := Key(obligation, anchor)
		it := Item{Key: key, Obligation: obligation, Title: title, Anchor: anchor, Pack: v.ID}
		d, err := pack.DueOn(v, obligation, anchor, facts)
		switch {
		case errors.Is(err, pack.ErrNoRule):
			return // no rule in force gives it a day: before the calendar starts, or a condition no rule has (an AGM dispensed with)
		case errors.Is(err, pack.ErrNoHolidays):
			it.Area, it.Why, it.Source = d.Rule.Area, d.Rule.Description, d.Rule.Source
			it.Status, it.Reason = NotAssessed, fmt.Sprintf("%s holds no public holidays for the year its day falls in: a new version adds them", v.ID)
		case err != nil:
			it.Status, it.Reason = NotAssessed, err.Error()
		default:
			it.Area, it.Due, it.Why, it.Source, it.Extended, it.Rolled = d.Rule.Area, d.On, d.Rule.Description, d.Rule.Source, d.Extended, d.Rolled
		}
		items = append(items, it)
	}
	// What a window reaches back for: Form C-S falls up to 23 months after its year's end.
	earliest := v.EffectiveFrom.AddDate(-2, 0, 0)

	// The financial years: ECI, Form C-S/C, the annual return, and the AGM or the statements to members.
	o, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	if cal, err := period.CalendarOf(o.Profile); err != nil {
		items = append(items, Item{Key: "financial_year:-", Obligation: "financial_year", Area: pack.AreaCorporate, Title: "The financial years",
			Status: NotAssessed, Reason: "The books start and the first financial year's end are not set: set them in Settings › Organisation"})
	} else {
		for n := 1; ; n++ {
			y := cal.Year(n)
			if y.EndsOn.After(to) {
				break
			}
			if y.EndsOn.Before(earliest) {
				continue
			}
			ended := y.EndsOn.Format("2 Jan 2006")
			add("eci", y.EndsOn, "Estimated chargeable income for the year ended "+ended)
			add("form_cs", y.EndsOn, fmt.Sprintf("Form C-S or C for YA %d (the year ended %s)", y.EndsOn.Year()+1, ended))
			add("annual_return", y.EndsOn, "Annual return for the year ended "+ended)
			add("agm", y.EndsOn, "Annual general meeting for the year ended "+ended)
			add("statements_to_members", y.EndsOn, "Financial statements to members for the year ended "+ended)
		}
	}

	// The GST return periods of each registration.
	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range regs {
		for d := r.RegisteredFrom; !d.After(to); {
			start, end, ok := gst.ReturnPeriod(r, d)
			if !ok {
				break
			}
			if !end.Before(earliest) {
				add("gst_f5", end, fmt.Sprintf("GST F5 return for %s to %s", start.Format("2 Jan 2006"), end.Format("2 Jan 2006")))
			}
			d = end.AddDate(0, 0, 1)
		}
	}

	// The months the organisation pays staff: CPF each month, the year's employment income each year.
	staff, _ := settings.Lookup(SettingPaysStaff)
	changes, err := u.Settings().Changes(ctx, staff.Key)
	if err != nil {
		return out, err
	}
	years := map[int]bool{}
	for m := monthEnd(earliest); !m.After(to); m = monthEnd(m.AddDate(0, 0, 1)) {
		if settings.On(staff, changes, m).Value != "true" {
			continue
		}
		add("cpf", m, "CPF contributions for "+m.Format("January 2006"))
		years[m.Year()] = true
	}
	for _, y := range slices.Sorted(maps.Keys(years)) {
		end := time.Date(y, time.December, 31, 0, 0, 0, 0, time.UTC)
		if !end.After(to) {
			add("ir8a_ais", end, fmt.Sprintf("Employment income for %d (IR8A, AIS)", y))
		}
	}

	// Applying to register for GST, while the monitor finds a test met (FR-TAX-05).
	m, err := gstmonitor.Monitor(ctx, u, today)
	if err != nil {
		return out, err
	}
	if m.Status == gstmonitor.StatusLiable {
		for _, t := range []*gstmonitor.YearTest{m.LastYear, m.ThisYear} {
			if t.Exceeded {
				add("gst_register_retrospective", time.Date(t.Year, time.December, 31, 0, 0, 0, 0, time.UTC),
					fmt.Sprintf("Apply to register for GST: taxable supplies of %d above the threshold", t.Year))
			}
		}
		if f := m.Forecast; f != nil && f.Exceeded {
			add("gst_register_prospective", f.ForecastOn, "Apply to register for GST: the forecast of "+f.ForecastOn.Format("2 Jan 2006")+" above the threshold")
		}
	}

	// The registers' own deadlines (design D7), with their status already.
	registers, err := registerItems(ctx, u, today)
	if err != nil {
		return out, err
	}
	for _, it := range items {
		if it.Status != NotAssessed {
			if f, ok := filings[it.Key]; ok {
				it.Filing = &f
				it.Status = Status(f.Outcome)
			} else {
				it.Status = statusOn(it.Due, today)
			}
		}
		earlier := it.Status != NotAssessed && it.Due.Before(from)
		if it.Status != NotAssessed && (it.Due.After(to) || (earlier && it.Status != Overdue)) {
			continue
		}
		out.Items = append(out.Items, it)
	}
	for _, it := range registers {
		if it.Due.After(to) || (it.Due.Before(from) && it.Status != Overdue) {
			continue
		}
		out.Items = append(out.Items, it)
	}
	slices.SortStableFunc(out.Items, func(a, b Item) int {
		switch {
		case (a.Status == NotAssessed) != (b.Status == NotAssessed):
			if a.Status == NotAssessed {
				return -1
			}
			return 1
		case !a.Due.Equal(b.Due):
			return a.Due.Compare(b.Due)
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out, nil
}

// statusOn is where an item due on due, with nothing recorded, stands on today.
func statusOn(due, today time.Time) Status {
	switch {
	case due.Before(today):
		return Overdue
	case !due.After(today.AddDate(0, 0, SoonDays)):
		return DueSoon
	}
	return Open
}

// factsOn are the organisation's settings the rules' conditions name, on day.
func factsOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (map[string]string, error) {
	out := map[string]string{}
	for fact, key := range map[string]string{"agm_route": SettingAGMRoute, "overseas_branch_register": SettingBranchRegister} {
		def, ok := settings.Lookup(key)
		if !ok {
			return nil, fmt.Errorf("the setting %s is not in the catalogue", key)
		}
		changes, err := u.Settings().Changes(ctx, key)
		if err != nil {
			return nil, err
		}
		out[fact] = settings.On(def, changes, day).Value
	}
	return out, nil
}

// Key names an item: its obligation and its anchor day.
func Key(obligation string, anchor time.Time) string {
	return obligation + ":" + anchor.Format(time.DateOnly)
}

// ParseKey reads a key back: its obligation and its anchor day.
func ParseKey(key string) (string, time.Time, bool) {
	obligation, day, ok := strings.Cut(key, ":")
	if !ok || obligation == "" {
		return "", time.Time{}, false
	}
	d, err := time.Parse(time.DateOnly, day)
	return obligation, d, err == nil
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func monthEnd(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
}
