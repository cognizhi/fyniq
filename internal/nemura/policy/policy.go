// Package policy is a company's leave policy on top of the statutory pack (PRD-002 NM-PACK-04, NM-PACK-08): effective-dated, checked rule by rule against
// each statutory rule for the people it covers (employees covered by the Employment Act). A policy that gives less than the statute for any rule is refused
// with the rule it breaks (NM_POLICY_BELOW_STATUTE); one that gives more says so. The rules are code over pack values, so a new pack version changes values,
// not code.
package policy

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// CodeBelowStatute is the stable code of the refusal.
const CodeBelowStatute = "NM_POLICY_BELOW_STATUTE"

// Rules are the company's entitlements.
type Rules struct {
	AnnualLeaveDays         []int `json:"annual_leave_days"`          // paid days a year, by year of service from the first; the last repeats for later years
	SickOutpatientDays      []int `json:"sick_outpatient_days"`       // at 3, 4, 5 and 6 or more completed months of service
	SickHospitalDays        []int `json:"sick_hospital_days"`         // at 3, 4, 5 and 6 or more completed months of service
	ChildcareCitizenDays    int   `json:"childcare_citizen_days"`     // a year, for a Singapore Citizen child under 7
	ExtendedChildcareDays   int   `json:"extended_childcare_days"`    // a year, for a Singapore Citizen child aged 7 to 12
	ChildcareNonCitizenDays int   `json:"childcare_non_citizen_days"` // a year, for a child who is not a Singapore Citizen
}

// Breach is a statutory rule a policy gives less than.
type Breach struct {
	Rule, Field     string
	Policy, Statute int
	Source          string // the pack source the statutory value comes from
	Message         string
}

// Note is a rule where the policy gives more than the statute.
type Note struct{ Rule, Message string }

// Result is the verdict on a policy.
type Result struct {
	Breaches []Breach
	Notes    []Note
	// Used are the pack values the check read, so a company can be asked to decide on those that are only researched (NM-PACK-07).
	Used []pack.Ref
}

func at(days []int, i int) int {
	if len(days) == 0 {
		return 0
	}
	return days[min(i, len(days)-1)]
}

func source(v pack.Version, table, key string) string {
	t := v.Tables[table]
	s := t.Col("source")
	for _, r := range t.Rows {
		if r[0] == key {
			return r[s]
		}
	}
	return ""
}

// Check compares a policy with the statute in v on a day, rule by rule (NM-PACK-08). A list that is not the right length, or a negative number, is a breach
// of the shape, not of the statute, and is reported the same way so the form can name the field.
func Check(v pack.Version, r Rules, on time.Time) Result {
	var res Result
	breach := func(rule, field string, got, want int, table, key, note string) {
		res.Used = append(res.Used, usedRef(v, table, key))
		switch {
		case got < want:
			res.Breaches = append(res.Breaches, Breach{Rule: rule, Field: field, Policy: got, Statute: want, Source: source(v, table, key),
				Message: fmt.Sprintf("%s gives %d, below the statutory %d%s: raise it to at least %d.", rule, got, want, note, want)})
		case got > want:
			res.Notes = append(res.Notes, Note{Rule: rule, Message: fmt.Sprintf("%s gives %d, more than the statutory %d.", rule, got, want)})
		}
	}
	if len(r.AnnualLeaveDays) == 0 {
		res.Breaches = append(res.Breaches, Breach{Rule: "Annual leave", Field: "annual_leave_days", Message: "Annual leave needs the days for the first year of service, and later years if they differ."})
	}
	for y := 1; y <= 8 && len(r.AnnualLeaveDays) > 0; y++ {
		if want, ok := pack.AnnualLeaveDays(v, y, on); ok {
			breach(fmt.Sprintf("Annual leave in year %d of service", y), "annual_leave_days", at(r.AnnualLeaveDays, y-1), want, "annual_leave", fmt.Sprint(y), "")
		}
	}
	if len(r.SickOutpatientDays) != 4 || len(r.SickHospitalDays) != 4 {
		res.Breaches = append(res.Breaches, Breach{Rule: "Sick leave", Field: "sick_outpatient_days", Message: "Sick leave needs four values for outpatient and four for hospitalisation: at 3, 4, 5 and 6 or more months of service."})
	} else {
		for i, m := range []int{3, 4, 5, 6} {
			if o, h, ok := pack.SickLeave(v, m, on); ok {
				breach(fmt.Sprintf("Outpatient sick leave at %d months", m), "sick_outpatient_days", r.SickOutpatientDays[i], o, "sick_leave", fmt.Sprint(m), "")
				breach(fmt.Sprintf("Hospitalisation leave at %d months", m), "sick_hospital_days", r.SickHospitalDays[i], h, "sick_leave", fmt.Sprint(m), "")
			}
		}
	}
	for _, f := range []struct {
		scheme, rule, field string
		got                 int
	}{
		{"childcare_citizen_under_7", "Childcare leave for a citizen child under 7", "childcare_citizen_days", r.ChildcareCitizenDays},
		{"extended_childcare_citizen_7_to_12", "Extended childcare leave for a citizen child aged 7 to 12", "extended_childcare_days", r.ExtendedChildcareDays},
		{"childcare_non_citizen_under_7", "Childcare leave for a non-citizen child under 7", "childcare_non_citizen_days", r.ChildcareNonCitizenDays},
	} {
		if want, _, ok := pack.FamilyLeave(v, f.scheme, on); ok {
			breach(f.rule, f.field, f.got, want, "family_leave", f.scheme, "")
		}
	}
	return res
}

// usedRef names the pack value a rule read, with its status in v.
func usedRef(v pack.Version, table, key string) pack.Ref {
	t := v.Tables[table]
	st := t.Col("status")
	for _, row := range t.Rows {
		if row[0] == key {
			return pack.Ref{Table: table, Key: key, Status: row[st]}
		}
	}
	return pack.Ref{Table: table, Key: key}
}
