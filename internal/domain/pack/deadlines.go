package pack

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The sg-deadlines pack (PRD-001 FR-TAX-07, CMP-02; p1-gst design D1 and D2): each obligation's date from its anchor
// day, Singapore's public holidays and one-off extensions. DueOn is a pure function of a version, an anchor and the
// organisation's facts: no clock, no I/O.

// The anchors a rule counts from.
const (
	AnchorGSTPeriodEnd = "gst_period_end"    // the last day of a GST return period
	AnchorMonthEnd     = "month_end"         // the last day of a month
	AnchorCalendarYear = "calendar_year_end" // 31 December
	AnchorFYEnd        = "fy_end"            // the last day of a financial year
	AnchorFYEndYearEnd = "fy_end_year_end"   // 31 December of the year a financial year ends in
	AnchorForecastOn   = "forecast_on"       // the day a GST forecast was made
)

// How a date rolls, what a rule gives, and the areas of the obligations.
const (
	RollNone           = "none"
	RollNextWorkingDay = "next_working_day" // past Saturdays, Sundays and public holidays
	KindDeadline       = "deadline"
	KindStarts         = "starts" // the day a registration takes effect
	AreaTax            = "tax"
	AreaPayroll        = "payroll"
	AreaCorporate      = "corporate"
)

var (
	anchors          = []string{AnchorGSTPeriodEnd, AnchorMonthEnd, AnchorCalendarYear, AnchorFYEnd, AnchorFYEndYearEnd, AnchorForecastOn}
	obligationFormat = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	conditionFormat  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}=[a-z0-9_]{1,40}$`)
)

// ErrNoRule is DueOn's answer when no rule of the obligation gives a date in force for the facts given: a date before
// the rules were read, or a condition no row meets (an AGM dispensed with).
var ErrNoRule = errors.New("no rule of the obligation is in force on its anchor day")

// ErrNoHolidays is DueOn's and NextWorkingDay's answer for a day in a year whose public holidays the version does not
// hold: whether it is a working day is not known.
var ErrNoHolidays = errors.New("the pack holds no public holidays for the year")

// Rule is one row of the rules table.
type Rule struct {
	Obligation, Area, Kind, Anchor string
	Months, Day, Days              int // Day 0: the anchor's own day
	Roll, Condition                string
	From                           time.Time
	To                             *time.Time
	Description, Source            string
}

// Due is an obligation's date and how it was reached.
type Due struct {
	On       time.Time
	Rule     Rule
	Extended bool // a one-off extension moved it
	Rolled   bool // it moved to the next working day
}

// DeadlineRules returns the version's rules, in the table's order.
func DeadlineRules(v Version) []Rule {
	var out []Rule
	for _, row := range v.Tables["rules"].Rows {
		r, err := parseRule(row)
		if err == nil {
			out = append(out, r)
		}
	}
	return out
}

func parseRule(row []string) (Rule, error) {
	r := Rule{Obligation: row[0], Area: row[1], Kind: row[2], Anchor: row[3], Roll: row[7], Condition: row[8], Description: row[11], Source: row[12]}
	var err error
	if r.Months, err = strconv.Atoi(row[4]); err != nil || r.Months < 0 || r.Months > 36 {
		return r, fmt.Errorf("months %q is not a whole number from 0 to 36", row[4])
	}
	if row[5] != "" {
		if r.Day, err = strconv.Atoi(row[5]); err != nil || r.Day < 1 || r.Day > 31 {
			return r, fmt.Errorf("day %q is not a day of the month from 1 to 31", row[5])
		}
	}
	if r.Days, err = strconv.Atoi(row[6]); err != nil || r.Days < 0 || r.Days > 366 {
		return r, fmt.Errorf("days %q is not a whole number from 0 to 366", row[6])
	}
	if r.From, err = ParseDate(row[9]); err != nil {
		return r, fmt.Errorf("effective_from %q is not a date", row[9])
	}
	if row[10] != "" {
		to, err := ParseDate(row[10])
		if err != nil || to.Before(r.From) {
			return r, fmt.Errorf("effective_to %q is not a date on or after %s", row[10], row[9])
		}
		r.To = &to
	}
	return r, nil
}

// meets reports whether the facts meet the rule's condition: none, or the fact named holding the value.
func (r Rule) meets(facts map[string]string) bool {
	if r.Condition == "" {
		return true
	}
	k, val, _ := strings.Cut(r.Condition, "=")
	return facts[k] == val
}

func (r Rule) inForce(d time.Time) bool {
	return !d.Before(r.From) && (r.To == nil || !d.After(*r.To))
}

// DueOn returns the date of obligation for the anchor day under the rule of v whose condition the facts meet and whose
// effective dates hold the date it gives (design D2): the anchor (for fy_end_year_end, 31 December of its year) moved
// Months forward, a month's last day giving the target month's last day and a day the target month lacks its last; then
// the rule's Day of that month; then Days on. A one-off extension whose window holds that date moves it, and where the
// rule rolls, it moves on to the next working day. ErrNoRule when no rule gives a date in force: a date before the
// rules were read, or a condition no rule meets.
func DueOn(v Version, obligation string, anchor time.Time, facts map[string]string) (Due, error) {
	anchor = dayOf(anchor)
	for _, r := range DeadlineRules(v) {
		if r.Obligation != obligation || !r.meets(facts) {
			continue
		}
		from := anchor
		if r.Anchor == AnchorFYEndYearEnd {
			from = time.Date(anchor.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
		}
		d := AddMonths(from, r.Months)
		if r.Day != 0 {
			last := lastDay(d.Year(), d.Month())
			d = time.Date(d.Year(), d.Month(), min(r.Day, last), 0, 0, 0, 0, time.UTC)
		}
		d = d.AddDate(0, 0, r.Days)
		if !r.inForce(d) {
			continue
		}
		out := Due{On: d, Rule: r}
		if e, ok := Extended(v, obligation, d); ok {
			out.On, out.Extended = e, true
		}
		if r.Roll == RollNextWorkingDay {
			w, err := NextWorkingDay(v, out.On)
			if err != nil {
				return Due{Rule: r}, err
			}
			out.Rolled = !w.Equal(out.On)
			out.On = w
		}
		return out, nil
	}
	return Due{}, ErrNoRule
}

// AddMonths moves d n months forward: the last day of a month gives the last day of the target month (IRAS: the Apr to
// Jun quarter is due on 31 Jul), and a day the target month lacks gives its last day.
func AddMonths(d time.Time, n int) time.Time {
	y, m := d.Year(), d.Month()+time.Month(n)
	target := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	last := lastDay(target.Year(), target.Month())
	if d.Day() == lastDay(d.Year(), d.Month()) {
		return time.Date(target.Year(), target.Month(), last, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(target.Year(), target.Month(), min(d.Day(), last), 0, 0, 0, 0, time.UTC)
}

func lastDay(y int, m time.Month) int { return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day() }

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Holiday is one public holiday of the holidays table.
type Holiday struct {
	On       time.Time
	Name     string
	Observed bool // the Monday after a Sunday holiday
}

// Holidays returns the version's public holidays, in the table's order.
func Holidays(v Version) []Holiday {
	var out []Holiday
	for _, row := range v.Tables["holidays"].Rows {
		d, err := ParseDate(row[0])
		if err == nil {
			out = append(out, Holiday{On: d, Name: row[1], Observed: row[2] == "true"})
		}
	}
	return out
}

// NextWorkingDay returns d when it is a working day, else the first working day after it: a weekday that is not a
// public holiday of v. ErrNoHolidays when a day it passes lies in a year v holds no public holidays for.
func NextWorkingDay(v Version, d time.Time) (time.Time, error) {
	hs := Holidays(v)
	years := map[int]bool{}
	off := map[time.Time]bool{}
	for _, h := range hs {
		years[h.On.Year()] = true
		off[h.On] = true
	}
	d = dayOf(d)
	for range 31 {
		if !years[d.Year()] {
			return time.Time{}, ErrNoHolidays
		}
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday && !off[d] {
			return d, nil
		}
		d = d.AddDate(0, 0, 1)
	}
	return time.Time{}, ErrNoHolidays
}

// Extended returns the day an obligation due on due is due instead under a one-off extension of v whose window holds
// due, and true; else due and false.
func Extended(v Version, obligation string, due time.Time) (time.Time, bool) {
	for _, row := range v.Tables["extensions"].Rows {
		if row[0] != obligation {
			continue
		}
		from, err1 := ParseDate(row[1])
		to, err2 := ParseDate(row[2])
		on, err3 := ParseDate(row[3])
		if err1 != nil || err2 != nil || err3 != nil || due.Before(from) || due.After(to) {
			continue
		}
		return on, true
	}
	return due, false
}

// Facts parses a condition column of a pinned case, "key=value" or empty, into the facts DueOn reads.
func Facts(condition string) map[string]string {
	if condition == "" {
		return nil
	}
	k, val, _ := strings.Cut(condition, "=")
	return map[string]string{k: val}
}

// checkDeadlines checks the sg-deadlines tables: rules whose columns hold what they name, each rule's periods for its
// obligation and condition without an overlap; holidays once each, named; extensions of an obligation the rules know,
// whose window ends before the day they give.
func checkDeadlines(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	known := map[string]bool{}
	var seen []Rule
	for i, row := range v.Tables["rules"].Rows {
		at := fmt.Sprintf("rules.csv row %d", i+2)
		r, err := parseRule(row)
		if err != nil {
			code := CodeValue
			if strings.HasPrefix(err.Error(), "effective") {
				code = CodeDates
			}
			add(code, at, err.Error(), "Correct the column as its description in pack.toml says")
			continue
		}
		switch {
		case !obligationFormat.MatchString(r.Obligation):
			add(CodeValue, at, fmt.Sprintf("obligation %q is not lower-case words joined by _", r.Obligation), "Name the obligation such as gst_f5")
		case !slices.Contains([]string{AreaTax, AreaPayroll, AreaCorporate}, r.Area):
			add(CodeValue, at, fmt.Sprintf("area %q is not tax, payroll or corporate", r.Area), "Enter tax, payroll or corporate")
		case r.Kind != KindDeadline && r.Kind != KindStarts:
			add(CodeValue, at, fmt.Sprintf("kind %q is not deadline or starts", r.Kind), "Enter deadline, or starts for the day a registration takes effect")
		case !slices.Contains(anchors, r.Anchor):
			add(CodeValue, at, fmt.Sprintf("anchor %q is not one Fyniq computes", r.Anchor), "Use one of "+strings.Join(anchors, ", "))
		case r.Roll != RollNone && r.Roll != RollNextWorkingDay:
			add(CodeValue, at, fmt.Sprintf("roll %q is not none or next_working_day", r.Roll), "Enter none or next_working_day")
		case r.Condition != "" && !conditionFormat.MatchString(r.Condition):
			add(CodeValue, at, fmt.Sprintf("condition %q is not setting=value", r.Condition), "Name the fact and its value, such as agm_route=hold, or leave it blank")
		case strings.TrimSpace(r.Description) == "":
			add(CodeValue, at, "description is blank", "Say what is due and when, in the source's words")
		case r.From.Before(v.EffectiveFrom):
			add(CodeDates, at, "starts before the version is in force", "Start the rule on or after effective_from")
		}
		for _, o := range seen {
			if o.Obligation == r.Obligation && o.Condition == r.Condition && (o.To == nil || !r.From.After(*o.To)) && (r.To == nil || !o.From.After(*r.To)) {
				add(CodeDates, at, fmt.Sprintf("overlaps another rule of %s %s", r.Obligation, r.Condition),
					"A rule's periods for one obligation and condition follow one another without an overlap")
			}
		}
		seen = append(seen, r)
		known[r.Obligation] = true
	}
	if len(seen) == 0 && len(v.Tables["rules"].Rows) == 0 {
		add(CodeTable, "rules.csv", "has no rules", "Add the deadlines' rules")
	}
	days := map[string]bool{}
	for i, row := range v.Tables["holidays"].Rows {
		at := fmt.Sprintf("holidays.csv row %d", i+2)
		if _, err := ParseDate(row[0]); err != nil || days[row[0]] {
			add(CodeValue, at, fmt.Sprintf("date %q is not a date, or is repeated", row[0]), "List each public holiday once (YYYY-MM-DD)")
		}
		days[row[0]] = true
		if strings.TrimSpace(row[1]) == "" {
			add(CodeValue, at, "name is blank", "Name the holiday as MOM's page does")
		}
		if row[2] != "true" && row[2] != "false" {
			add(CodeValue, at, fmt.Sprintf("observed %q is not true or false", row[2]), "Enter true for the Monday after a Sunday holiday, else false")
		}
	}
	for i, row := range v.Tables["extensions"].Rows {
		at := fmt.Sprintf("extensions.csv row %d", i+2)
		from, err1 := ParseDate(row[1])
		to, err2 := ParseDate(row[2])
		on, err3 := ParseDate(row[3])
		switch {
		case !known[row[0]]:
			add(CodeValue, at, fmt.Sprintf("obligation %q has no rule", row[0]), "Name an obligation of rules.csv")
		case err1 != nil || err2 != nil || err3 != nil || to.Before(from) || !on.After(to):
			add(CodeDates, at, "is not a window of dates ending before the day it gives", "Enter due_from, due_to and due_on as dates, in that order")
		}
	}
	return out
}
