package pack

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The people pack of Singapore (sg-people; PRD-002 NM-PACK-01, 02, 06, 07; p1-nemura-records-packs slice 4): the statutory facts Nemura's leave, records and
// people checks read, as effective-dated, sourced rows with hand-pinned cases. The same library validates it as Fyniq's packs (owner decision O1, 19 Nov
// 2026). Every row carries a status: "researched" (read from the official page by the publisher) or "verified" (checked by a second person, NM-PACK-07).
// A pure library: no clock, no I/O.

// Status of a value (NM-PACK-07).
const (
	Researched = "researched"
	Verified   = "verified"
)

// peopleTables are the tables Nemura reads from sg-people and the columns of their pinned cases.
func init() {
	Schemas["sg-people"] = Schema{
		Tables: map[string][]string{
			"annual_leave": {"service_year", "days", "effective_from", "effective_to", "status", "source"},
			"sick_leave":   {"months_from", "outpatient_days", "hospital_days", "effective_from", "effective_to", "status", "source"},
			"holidays":     {"date", "name", "status", "source"},
			"family_leave": {"scheme", "days_per_year", "min_service_months", "effective_from", "effective_to", "status", "source"},
			"timing":       {"rule", "days", "effective_from", "effective_to", "status", "source"},
			"records":      {"person", "months", "keep_after_leaving_months", "effective_from", "effective_to", "status", "source"},
			"retirement":   {"born_from", "born_to", "retirement_age", "reemployment_age", "effective_from", "effective_to", "status", "source"},
		},
		Pinned: map[string][]string{
			"annual_leave": {"case", "service_year", "date", "want_days", "why", "source"},
			"sick_leave":   {"case", "months", "date", "want", "why", "source"},
			"holidays":     {"case", "date", "want", "why", "source"},
			"family_leave": {"case", "scheme", "date", "want_days", "why", "source"},
			"timing":       {"case", "rule", "date", "want_days", "why", "source"},
			"records":      {"case", "person", "date", "want_months", "why", "source"},
			"retirement":   {"case", "born", "date", "want", "why", "source"},
		},
	}
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

// peopleInForce reports whether a row with effective_from and effective_to (empty: open) is in force on a day.
func peopleInForce(from, to string, on time.Time) bool {
	f, err := ParseDate(from)
	if err != nil || on.Before(f) {
		return false
	}
	if to == "" {
		return true
	}
	t, err := ParseDate(to)
	return err == nil && !on.After(t)
}

// checkPeople validates sg-people's values: whole numbers where they are counts, a known status, ordered ages, holidays that are dates, and no overlap of
// effective dates for one key (PACK-06, PACK-08).
func checkPeople(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	for _, name := range []string{"annual_leave", "sick_leave", "holidays", "family_leave", "timing", "records", "retirement"} {
		t := v.Tables[name]
		st := t.Col("status")
		for i, row := range t.Rows {
			if row[st] != Researched && row[st] != Verified {
				add(CodeValue, fmt.Sprintf("%s.csv row %d", name, i+2), fmt.Sprintf("status %q is not researched or verified", row[st]), "Write researched, or verified once a second person has checked the official page (NM-PACK-07)")
			}
		}
	}
	nums := func(name string, cols ...string) {
		t := v.Tables[name]
		for i, row := range t.Rows {
			for _, c := range cols {
				if n, ok := atoi(row[t.Col(c)]); !ok || n < 0 {
					add(CodeValue, fmt.Sprintf("%s.csv row %d", name, i+2), fmt.Sprintf("%s %q is not a whole number of zero or more", c, row[t.Col(c)]), "Write a whole number")
				}
			}
		}
	}
	nums("annual_leave", "service_year", "days")
	nums("sick_leave", "months_from", "outpatient_days", "hospital_days")
	nums("family_leave", "days_per_year", "min_service_months")
	nums("timing", "days")
	nums("records", "months", "keep_after_leaving_months")
	nums("retirement", "retirement_age", "reemployment_age")
	for i, row := range v.Tables["holidays"].Rows {
		if _, err := ParseDate(row[0]); err != nil {
			add(CodeValue, fmt.Sprintf("holidays.csv row %d", i+2), fmt.Sprintf("date %q is not a date", row[0]), "Write YYYY-MM-DD")
		}
	}
	for i, row := range v.Tables["retirement"].Rows {
		r, _ := atoi(row[2])
		e, _ := atoi(row[3])
		if e < r {
			add(CodeValue, fmt.Sprintf("retirement.csv row %d", i+2), "the re-employment age is below the retirement age", "Check both ages against the official page")
		}
	}
	periods(add, "annual_leave.csv", v.Tables["annual_leave"].Rows, func(r []string) string { return r[0] }, 2, 3)
	periods(add, "sick_leave.csv", v.Tables["sick_leave"].Rows, func(r []string) string { return r[0] }, 3, 4)
	periods(add, "family_leave.csv", v.Tables["family_leave"].Rows, func(r []string) string { return r[0] }, 3, 4)
	periods(add, "timing.csv", v.Tables["timing"].Rows, func(r []string) string { return r[0] }, 2, 3)
	periods(add, "records.csv", v.Tables["records"].Rows, func(r []string) string { return r[0] }, 3, 4)
	periods(add, "retirement.csv", v.Tables["retirement"].Rows, func(r []string) string { return r[0] + "-" + r[1] }, 4, 5)
	if len(v.Tables["holidays"].Rows) == 0 {
		add(CodeTable, "holidays.csv", "has no holiday", "Add the year's public holidays")
	}
	return out
}

// AnnualLeaveDays is the paid annual leave days a year for an employee in their nth year of service (7 in the first, one more a year, a cap in the last row).
func AnnualLeaveDays(v Version, serviceYear int, on time.Time) (int, bool) {
	best, bestYear := 0, 0
	for _, r := range v.Tables["annual_leave"].Rows {
		y, _ := atoi(r[0])
		d, _ := atoi(r[1])
		if y <= serviceYear && y >= bestYear && peopleInForce(r[2], r[3], on) {
			best, bestYear = d, y
		}
	}
	return best, bestYear > 0
}

// SickLeave is the paid outpatient and hospitalisation days a year for an employee with this many completed months of service.
func SickLeave(v Version, months int, on time.Time) (outpatient, hospital int, ok bool) {
	bestFrom := -1
	for _, r := range v.Tables["sick_leave"].Rows {
		m, _ := atoi(r[0])
		if m <= months && m > bestFrom && peopleInForce(r[3], r[4], on) {
			outpatient, _ = atoi(r[1])
			hospital, _ = atoi(r[2])
			bestFrom, ok = m, true
		}
	}
	return
}

// PublicHoliday is the name of the public holiday on a date, or false.
func PublicHoliday(v Version, date time.Time) (string, bool) {
	s := date.Format(time.DateOnly)
	for _, r := range v.Tables["holidays"].Rows {
		if r[0] == s {
			return r[1], true
		}
	}
	return "", false
}

// HolidaysIn are the public holidays of a calendar year in the pack, by date; none means the pack holds no calendar for that year (NM-PACK-05).
func HolidaysIn(v Version, year int) []string {
	var out []string
	prefix := strconv.Itoa(year) + "-"
	for _, r := range v.Tables["holidays"].Rows {
		if strings.HasPrefix(r[0], prefix) {
			out = append(out, r[0])
		}
	}
	return out
}

// FamilyLeave is a scheme's days a year and the months of service it needs.
func FamilyLeave(v Version, scheme string, on time.Time) (days, minMonths int, ok bool) {
	for _, r := range v.Tables["family_leave"].Rows {
		if r[0] == scheme && peopleInForce(r[3], r[4], on) {
			days, _ = atoi(r[1])
			minMonths, _ = atoi(r[2])
			return days, minMonths, true
		}
	}
	return 0, 0, false
}

// TimingDays is a payment or filing rule's days (salary within 7 days, overtime within 14).
func TimingDays(v Version, rule string, on time.Time) (int, bool) {
	for _, r := range v.Tables["timing"].Rows {
		if r[0] == rule && peopleInForce(r[2], r[3], on) {
			n, _ := atoi(r[1])
			return n, true
		}
	}
	return 0, false
}

// RecordKeeping is the months of employment records to keep for "current" or "former" employees, and for a former one the months to keep them after leaving.
func RecordKeeping(v Version, person string, on time.Time) (months, afterLeaving int, ok bool) {
	for _, r := range v.Tables["records"].Rows {
		if r[0] == person && peopleInForce(r[3], r[4], on) {
			months, _ = atoi(r[1])
			afterLeaving, _ = atoi(r[2])
			return months, afterLeaving, true
		}
	}
	return 0, 0, false
}

// RetirementAges are the retirement and re-employment ages for someone born on a date, as they stand on a day (by birth cohort, NM-PPL-08).
func RetirementAges(v Version, born, on time.Time) (retirement, reemployment int, ok bool) {
	for _, r := range v.Tables["retirement"].Rows {
		from, err1 := ParseDate(r[0])
		to, err2 := ParseDate(r[1])
		if err1 != nil || err2 != nil || born.Before(from) || born.After(to) || !peopleInForce(r[4], r[5], on) {
			continue
		}
		retirement, _ = atoi(r[2])
		reemployment, _ = atoi(r[3])
		return retirement, reemployment, true
	}
	return 0, 0, false
}

// Ref names one value of a pack: the table and its row, with how far it is checked.
type Ref struct {
	Table, Key, Status string
}

// Unverified lists the values of a version that are only researched (NM-PACK-07): what a company must decide on before a calculation uses them.
func Unverified(v Version) []Ref {
	var out []Ref
	for _, name := range []string{"annual_leave", "sick_leave", "holidays", "family_leave", "timing", "records", "retirement"} {
		t := v.Tables[name]
		st := t.Col("status")
		for _, row := range t.Rows {
			if row[st] == Researched {
				out = append(out, Ref{Table: name, Key: row[0], Status: row[st]})
			}
		}
	}
	return out
}

// runPeople runs the sg-people pinned cases, hand-worked from the official pages.
func runPeople(v Version) []Result {
	if v.Pack != "sg-people" {
		return nil
	}
	var out []Result
	day := func(r *Result, s string) (time.Time, bool) {
		d, err := ParseDate(s)
		if err != nil {
			r.Got = fmt.Sprintf("the date %q does not parse", s)
		}
		return d, err == nil
	}
	num := func(n int, ok bool) string {
		if !ok {
			return "none"
		}
		return strconv.Itoa(n)
	}
	for _, row := range v.Pinned["annual_leave"].Rows {
		r := Result{Table: "annual_leave", Case: row[0], Why: row[4], Want: row[3]}
		if d, ok := day(&r, row[2]); ok {
			y, _ := atoi(row[1])
			n, found := AnnualLeaveDays(v, y, d)
			r.Got = num(n, found)
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["sick_leave"].Rows {
		r := Result{Table: "sick_leave", Case: row[0], Why: row[4], Want: row[3]}
		if d, ok := day(&r, row[2]); ok {
			m, _ := atoi(row[1])
			o, h, found := SickLeave(v, m, d)
			r.Got = "none"
			if found {
				r.Got = fmt.Sprintf("%d/%d", o, h)
			}
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["holidays"].Rows {
		r := Result{Table: "holidays", Case: row[0], Why: row[3], Want: row[2]}
		if d, ok := day(&r, row[1]); ok {
			name, found := PublicHoliday(v, d)
			r.Got = "none"
			if found {
				r.Got = name
			}
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["family_leave"].Rows {
		r := Result{Table: "family_leave", Case: row[0], Why: row[4], Want: row[3]}
		if d, ok := day(&r, row[2]); ok {
			n, _, found := FamilyLeave(v, row[1], d)
			r.Got = num(n, found)
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["timing"].Rows {
		r := Result{Table: "timing", Case: row[0], Why: row[4], Want: row[3]}
		if d, ok := day(&r, row[2]); ok {
			n, found := TimingDays(v, row[1], d)
			r.Got = num(n, found)
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["records"].Rows {
		r := Result{Table: "records", Case: row[0], Why: row[4], Want: row[3]}
		if d, ok := day(&r, row[2]); ok {
			n, _, found := RecordKeeping(v, row[1], d)
			r.Got = num(n, found)
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["retirement"].Rows {
		r := Result{Table: "retirement", Case: row[0], Why: row[4], Want: row[3]}
		born, ok1 := day(&r, row[1])
		d, ok2 := day(&r, row[2])
		if ok1 && ok2 {
			a, b, found := RetirementAges(v, born, d)
			r.Got = "none"
			if found {
				r.Got = fmt.Sprintf("%d/%d", a, b)
			}
			r.Pass = r.Got == r.Want
		}
		out = append(out, r)
	}
	return out
}
