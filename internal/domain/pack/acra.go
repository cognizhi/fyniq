package pack

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The sg-acra pack (PRD-001 FR-CS-02; p1-company-secretary design D1): the corporate rules the company secretary's
// checks and assistant read. A period counts days, business days or months from its event; a threshold is an amount or
// a count; a rule has neither. Each carries its verdict: the Act states it, or only ACRA's guidance does. AcraDue is a
// pure function of a version, an event day and the working days: no clock, no I/O.

// The units a period counts in.
const (
	UnitDays         = "days"
	UnitBusinessDays = "business_days" // weekdays that are not public holidays, from the day after the event
	UnitMonths       = "months"        // the same day of the month, or the month's last day when it has no such day
)

// The verdicts: where a rule stands (FR-CS-02, "Guidance kept apart from the Act").
const (
	VerdictAct      = "act"      // a cited source is the Act (a law or regulation) and states the rule
	VerdictGuidance = "guidance" // only ACRA's guidance states it, including where the Act sets no period
)

// The units a threshold is in.
const (
	ThresholdSGD   = "SGD"
	ThresholdCount = "count"
)

var (
	acraName      = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	acraThreshold = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,4})?$`)
	// the most a period may count in each unit, so a typing slip (140 days, 60 months) is refused
	acraMost = map[string]int{UnitDays: 366, UnitBusinessDays: 30, UnitMonths: 36}
)

// Period is one rule of the periods table: Count units after Event, in force from From to To.
type Period struct {
	Rule, Event, After string
	Count              int
	Unit               string
	Section, Verdict   string
	From               time.Time
	To                 *time.Time
	Description        string
	Sources            []string
}

// CorporateRule is one rule of the rules table: a rule with no period or amount.
type CorporateRule struct {
	Rule, Section, Verdict string
	From                   time.Time
	To                     *time.Time
	Description            string
	Sources                []string
}

// Workday says whether a day is a working day: a weekday that is not a public holiday. Its error is ErrNoHolidays for a
// day in a year whose holidays it does not know.
type Workday func(time.Time) (bool, error)

// Weekdays counts every weekday as a working day. The pinned cases use it on days with no public holiday between the
// event and the due date; the checks use WorkdaysOf.
func Weekdays(d time.Time) (bool, error) {
	wd := d.Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

// WorkdaysOf counts the weekdays that are not public holidays of cal, an sg-deadlines version.
func WorkdaysOf(cal Version) Workday {
	years := map[int]bool{}
	off := map[time.Time]bool{}
	for _, h := range Holidays(cal) {
		years[h.On.Year()] = true
		off[h.On] = true
	}
	return func(d time.Time) (bool, error) {
		d = dayOf(d)
		if !years[d.Year()] {
			return false, ErrNoHolidays
		}
		weekday, _ := Weekdays(d)
		return weekday && !off[d], nil
	}
}

func effective(fromCol, toCol string) (time.Time, *time.Time, error) {
	from, err := ParseDate(fromCol)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("effective_from %q is not a date", fromCol)
	}
	if toCol == "" {
		return from, nil, nil
	}
	to, err := ParseDate(toCol)
	if err != nil || to.Before(from) {
		return time.Time{}, nil, fmt.Errorf("effective_to %q is not a date on or after effective_from", toCol)
	}
	return from, &to, nil
}

func inForce(from time.Time, to *time.Time, on time.Time) bool {
	on = dayOf(on)
	return !on.Before(from) && (to == nil || !on.After(*to))
}

func parsePeriod(row []string) (Period, error) {
	p := Period{Rule: row[0], Event: row[1], After: row[2], Unit: row[4], Section: row[5], Verdict: row[6],
		Description: row[9], Sources: strings.Fields(row[10])}
	var err error
	if p.Count, err = strconv.Atoi(row[3]); err != nil || p.Count < 1 {
		return Period{}, fmt.Errorf("count %q is not a whole number above zero", row[3])
	}
	if p.From, p.To, err = effective(row[7], row[8]); err != nil {
		return Period{}, err
	}
	return p, nil
}

// PeriodOn returns rule's period in force on day; ErrNoRule when v holds none then.
func PeriodOn(v Version, rule string, on time.Time) (Period, error) {
	for _, row := range v.Tables["periods"].Rows {
		if row[0] != rule {
			continue
		}
		if p, err := parsePeriod(row); err == nil && inForce(p.From, p.To, on) {
			return p, nil
		}
	}
	return Period{}, ErrNoRule
}

// AcraDue returns rule's period in force on the event's day and the day it falls due: Count days, business days or
// months after event. Business days start the day after the event and skip the days workday says are not working
// days. ErrNoRule when no period of the rule is in force on the event's day; workday's error when it fails.
func AcraDue(v Version, rule string, event time.Time, workday Workday) (Period, time.Time, error) {
	return acraDue(v, rule, event, event, workday)
}

// AcraDueSince is AcraDue for an event that may come before the version's first day, such as a company's incorporation:
// the period in force on the later of the event's day and the version's first day, counted from the event. The checks
// read it for a standing rule, such as a secretary within six months of incorporation, that the version states and
// that held before it [accountant to confirm for an event long before it].
func AcraDueSince(v Version, rule string, event time.Time, workday Workday) (Period, time.Time, error) {
	on := event
	if on.Before(v.EffectiveFrom) {
		on = v.EffectiveFrom
	}
	return acraDue(v, rule, event, on, workday)
}

func acraDue(v Version, rule string, event, ruleOn time.Time, workday Workday) (Period, time.Time, error) {
	p, err := PeriodOn(v, rule, ruleOn)
	if err != nil {
		return Period{}, time.Time{}, err
	}
	d := dayOf(event)
	switch p.Unit {
	case UnitDays:
		return p, d.AddDate(0, 0, p.Count), nil
	case UnitMonths:
		return p, monthsOn(d, p.Count), nil
	case UnitBusinessDays:
		for n := p.Count; n > 0; {
			d = d.AddDate(0, 0, 1)
			ok, err := workday(d)
			if err != nil {
				return Period{}, time.Time{}, err
			}
			if ok {
				n--
			}
		}
		return p, d, nil
	}
	return Period{}, time.Time{}, ErrNoRule
}

// monthsOn moves d n months forward to the same day of the month, or the target month's last day when it has no such
// day: 30 Nov and six months is 30 May, 31 Aug and six months is 28 Feb. Unlike AddMonths, the last day of a month
// does not move to the last day of the target month.
func monthsOn(d time.Time, n int) time.Time {
	target := time.Date(d.Year(), d.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	return time.Date(target.Year(), target.Month(), min(d.Day(), lastDay(target.Year(), target.Month())), 0, 0, 0, 0, time.UTC)
}

// AcraThreshold returns the threshold's value and unit in force on day, and false when v holds none then.
func AcraThreshold(v Version, name string, on time.Time) (value, unit string, ok bool) {
	for _, row := range v.Tables["thresholds"].Rows {
		if row[0] != name {
			continue
		}
		if from, to, err := effective(row[3], row[4]); err == nil && inForce(from, to, on) {
			return row[1], row[2], true
		}
	}
	return "", "", false
}

// CorporateRuleOn returns the rule in force on day, and false when v holds none then.
func CorporateRuleOn(v Version, rule string, on time.Time) (CorporateRule, bool) {
	for _, row := range v.Tables["rules"].Rows {
		if row[0] != rule {
			continue
		}
		if from, to, err := effective(row[3], row[4]); err == nil && inForce(from, to, on) {
			return CorporateRule{Rule: row[0], Section: row[1], Verdict: row[2], From: from, To: to, Description: row[5],
				Sources: strings.Fields(row[6])}, true
		}
	}
	return CorporateRule{}, false
}

// checkAcra checks the sg-acra tables: names, units, counts and amounts that hold what their columns say, each rule's
// periods in order without an overlap, and each verdict borne out by the kind of the sources its row cites: act cites
// a law or regulation, guidance cites official guidance.
func checkAcra(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	kinds := map[string]string{}
	for _, s := range v.Sources {
		kinds[s.ID] = s.Kind
	}
	verdict := func(at, verdict, section string, sources []string) {
		cites := func(want ...string) bool {
			return slices.ContainsFunc(sources, func(id string) bool { return slices.Contains(want, kinds[id]) })
		}
		switch {
		case verdict != VerdictAct && verdict != VerdictGuidance:
			add(CodeValue, at, fmt.Sprintf("verdict %q is not act or guidance", verdict),
				"Enter act when the Act states the rule, or guidance when only ACRA's guidance does")
		case verdict == VerdictAct && !cites("law", "regulation"):
			add(CodeSource, at, "the verdict is act, but no cited source is a law or regulation",
				"Cite the Act's section as a source of kind law, or record the rule as guidance")
		case verdict == VerdictAct && strings.TrimSpace(section) == "":
			add(CodeValue, at, "the verdict is act, but the section is blank", "Name the Act's section, such as s173A")
		case verdict == VerdictGuidance && !cites("official guidance"):
			add(CodeSource, at, "the verdict is guidance, but no cited source is official guidance", "Cite ACRA's page that states it")
		}
	}
	type span struct {
		from time.Time
		to   *time.Time
	}
	seen := map[string][]span{}
	overlaps := func(at, name string, from time.Time, to *time.Time) {
		for _, o := range seen[name] {
			if (o.to == nil || !from.After(*o.to)) && (to == nil || !o.from.After(*to)) {
				add(CodeDates, at, fmt.Sprintf("overlaps another row of %s", name), "A rule's rows follow one another without an overlap")
			}
		}
		seen[name] = append(seen[name], span{from, to})
		if from.Before(v.EffectiveFrom) {
			add(CodeDates, at, "starts before the version is in force", "Start the row on or after effective_from")
		}
	}
	rules := map[string]bool{}
	for _, row := range v.Tables["periods"].Rows {
		rules[row[0]] = true
	}
	for i, row := range v.Tables["periods"].Rows {
		at := fmt.Sprintf("periods.csv row %d", i+2)
		p, err := parsePeriod(row)
		if err != nil {
			code := CodeValue
			if strings.HasPrefix(err.Error(), "effective") {
				code = CodeDates
			}
			add(code, at, err.Error(), "Correct the column as its description in pack.toml says")
			continue
		}
		switch most, known := acraMost[p.Unit]; {
		case !acraName.MatchString(p.Rule):
			add(CodeValue, at, fmt.Sprintf("rule %q is not lower-case words joined by _", p.Rule), "Name the rule such as officer_change")
		case !acraName.MatchString(p.Event):
			add(CodeValue, at, fmt.Sprintf("event %q is not lower-case words joined by _", p.Event), "Name the event such as office_change")
		case p.After != "" && (p.After == p.Rule || !rules[p.After]):
			add(CodeValue, at, fmt.Sprintf("after %q is not another rule of the table", p.After), "Name the rule this one follows, or leave it blank")
		case !known:
			add(CodeValue, at, fmt.Sprintf("unit %q is not days, business_days or months", p.Unit), "Enter days, business_days or months")
		case p.Count > most:
			add(CodeValue, at, fmt.Sprintf("count %d is more than %d %s", p.Count, most, p.Unit), "Check the count against the source")
		case strings.TrimSpace(p.Description) == "":
			add(CodeValue, at, "description is blank", "Say what is due and when, in the source's words")
		}
		verdict(at, p.Verdict, p.Section, p.Sources)
		overlaps(at, "periods "+p.Rule, p.From, p.To)
	}
	if len(v.Tables["periods"].Rows) == 0 {
		add(CodeTable, "periods.csv", "has no periods", "Add the corporate deadlines")
	}
	for i, row := range v.Tables["thresholds"].Rows {
		at := fmt.Sprintf("thresholds.csv row %d", i+2)
		from, to, err := effective(row[3], row[4])
		if err != nil {
			add(CodeDates, at, err.Error(), "Correct the column as its description in pack.toml says")
			continue
		}
		switch {
		case !acraName.MatchString(row[0]):
			add(CodeValue, at, fmt.Sprintf("threshold %q is not lower-case words joined by _", row[0]), "Name the threshold such as audit_exempt_revenue")
		case row[2] != ThresholdSGD && row[2] != ThresholdCount:
			add(CodeValue, at, fmt.Sprintf("unit %q is not SGD or count", row[2]), "Enter SGD for an amount, or count")
		case !acraThreshold.MatchString(row[1]) || (row[2] == ThresholdCount && strings.Contains(row[1], ".")):
			add(CodeValue, at, fmt.Sprintf("value %q is not an exact amount, or a whole count", row[1]), "Enter the value as the source gives it, such as 500000")
		case strings.TrimSpace(row[5]) == "":
			add(CodeValue, at, "description is blank", "Say what the threshold is, in the source's words")
		}
		overlaps(at, "thresholds "+row[0], from, to)
	}
	for i, row := range v.Tables["rules"].Rows {
		at := fmt.Sprintf("rules.csv row %d", i+2)
		from, to, err := effective(row[3], row[4])
		if err != nil {
			add(CodeDates, at, err.Error(), "Correct the column as its description in pack.toml says")
			continue
		}
		switch {
		case !acraName.MatchString(row[0]):
			add(CodeValue, at, fmt.Sprintf("rule %q is not lower-case words joined by _", row[0]), "Name the rule such as secretary_not_sole_director")
		case strings.TrimSpace(row[5]) == "":
			add(CodeValue, at, "description is blank", "Say what the rule asks, in the source's words")
		}
		verdict(at, row[2], row[1], strings.Fields(row[6]))
		overlaps(at, "rules "+row[0], from, to)
	}
	return out
}

// runAcra runs the sg-acra pinned cases: each rule's due day for an event, counting business days as weekdays (the
// cases pick days with no public holiday between), and each threshold on a day; "none" where the version must not
// know one.
func runAcra(v Version) []Result {
	if v.Pack != "sg-acra" {
		return nil
	}
	var out []Result
	for _, row := range v.Pinned["periods"].Rows {
		r := Result{Table: "periods", Case: row[0] + " " + row[1], Why: row[4], Want: row[3]}
		event, err := ParseDate(row[2])
		if err != nil {
			r.Got = fmt.Sprintf("the date %q does not parse", row[2])
			out = append(out, r)
			continue
		}
		if _, due, err := AcraDue(v, row[1], event, Weekdays); err != nil {
			r.Got, r.Pass = "none", r.Want == "none"
		} else {
			r.Got = due.Format(time.DateOnly)
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["thresholds"].Rows {
		r := Result{Table: "thresholds", Case: row[0] + " " + row[1], Why: row[4], Want: row[3]}
		on, err := ParseDate(row[2])
		if err != nil {
			r.Got = fmt.Sprintf("the date %q does not parse", row[2])
			out = append(out, r)
			continue
		}
		if value, _, ok := AcraThreshold(v, row[1], on); !ok {
			r.Got, r.Pass = "none", r.Want == "none"
		} else {
			r.Got, r.Pass = value, sameDecimal(value, r.Want)
		}
		out = append(out, r)
	}
	return out
}
