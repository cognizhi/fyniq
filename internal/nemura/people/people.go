// Package people is Nemura's people record (PRD-002 NM-PPL-01, NM-PPL-06, NM-PPL-08): a person, their effective-dated job and their
// legal classification, and the checks that need no database. Every refusal is plain and has a stable code (ENG-23).
package people

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The stable codes of the refusals.
const (
	CodeInvalid  = "NM_PERSON_INVALID"   // a field is missing or not valid; the message names it
	CodeNotFound = "NM_PERSON_NOT_FOUND" // no such person in this organisation
	CodeHistory  = "NM_PERSON_HISTORY"   // a change that would rewrite or overlap history
	CodeExists   = "NM_PERSON_EXISTS"    // a person with that ID exists already
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) *Refusal {
	return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)}
}

// EmploymentTypes are the kinds of employment of NM-PPL-01. A person's legal classification (NM-PPL-08) is separate: a contractor is
// "contract" here and a contract for service there.
var EmploymentTypes = []string{"full_time", "part_time", "contract", "intern"}

// Person is what is set when a person is created or their contact changes.
type Person struct {
	LegalName       string
	PreferredName   string
	WorkEmail       string
	Phone           string
	EmploymentStart time.Time
	ProbationEnd    *time.Time
	LeavingDate     *time.Time
	LeavingReason   string
	MemberID        *uuid.UUID // the login, when there is one
}

// Job is one row of a person's effective-dated job history.
type Job struct {
	Title, Department, Worksite, EmploymentType string
	Manager                                     *uuid.UUID
}

// Classification is a person's legal classification (NM-PPL-08), effective-dated.
type Classification struct {
	ContractType string // "service" (an employee) or "for_service" (a contractor, who gets no statutory employee entitlement)
	EACovered    bool   // the Employment Act covers the person
	PartIVClass  string // "none", "workman" or "non_workman" (Part IV of the Act)
	Residency    string // "citizen", "permanent_resident" or "foreigner"
	PRStart      *time.Time
	BirthDate    *time.Time // for the retirement and re-employment cohort; restricted
}

func text(name, v string, max int, required bool) error {
	v = strings.TrimSpace(v)
	switch {
	case v == "" && required:
		return invalid("%s is required: enter it.", name)
	case utf8.RuneCountInString(v) > max:
		return invalid("%s is longer than %d characters: shorten it.", name, max)
	}
	return nil
}

// Check validates a person's own fields.
func (p Person) Check() error {
	if err := text("The legal name", p.LegalName, 200, true); err != nil {
		return err
	}
	if err := text("The preferred name", p.PreferredName, 200, false); err != nil {
		return err
	}
	if err := text("The phone number", p.Phone, 40, false); err != nil {
		return err
	}
	if p.WorkEmail != "" {
		a, err := mail.ParseAddress(p.WorkEmail)
		if err != nil || a.Address != p.WorkEmail || len(p.WorkEmail) > 254 {
			return invalid("The work email is not an email address: write it as name@company.example.")
		}
	}
	if p.EmploymentStart.IsZero() {
		return invalid("The start date is required: enter the first day of employment.")
	}
	if p.ProbationEnd != nil && p.ProbationEnd.Before(p.EmploymentStart) {
		return invalid("The probation end is before the start date: enter a date on or after the start.")
	}
	if p.LeavingDate != nil && p.LeavingDate.Before(p.EmploymentStart) {
		return invalid("The leaving date is before the start date: enter a date on or after the start.")
	}
	return text("The leaving reason", p.LeavingReason, 500, false)
}

// Check validates a job row.
func (j Job) Check() error {
	if err := text("The job title", j.Title, 200, true); err != nil {
		return err
	}
	if err := text("The department", j.Department, 200, false); err != nil {
		return err
	}
	known := false
	for _, t := range EmploymentTypes {
		known = known || t == j.EmploymentType
	}
	if !known {
		return invalid("The employment type %q is not one of full_time, part_time, contract or intern.", j.EmploymentType)
	}
	if len(j.Worksite) < 2 || len(j.Worksite) > 11 || strings.ToUpper(j.Worksite) != j.Worksite {
		return invalid("The worksite %q is not a country code such as SG, optionally with a region (MY-JHR).", j.Worksite)
	}
	return nil
}

// Check validates a classification: a contractor is not covered by the Employment Act and so has no Part IV class, a permanent
// resident has the date their residence began, and nobody else has one (NM-PPL-08).
func (c Classification) Check() error {
	switch c.ContractType {
	case "service", "for_service":
	default:
		return invalid("The contract type %q is not service or for_service.", c.ContractType)
	}
	switch c.PartIVClass {
	case "none", "workman", "non_workman":
	default:
		return invalid("The Part IV class %q is not none, workman or non_workman.", c.PartIVClass)
	}
	switch c.Residency {
	case "citizen", "permanent_resident", "foreigner":
	default:
		return invalid("The residency %q is not citizen, permanent_resident or foreigner.", c.Residency)
	}
	if c.ContractType == "for_service" && c.EACovered {
		return invalid("A contractor (a contract for service) is not covered by the Employment Act and gets no statutory employee entitlement: set the Employment Act cover to no.")
	}
	if c.PartIVClass != "none" && !c.EACovered {
		return invalid("A Part IV class needs the Employment Act to cover the person: set the cover to yes, or the class to none.")
	}
	if (c.Residency == "permanent_resident") != (c.PRStart != nil) {
		return invalid("The date permanent residence began is needed for a permanent resident, and only for one.")
	}
	return nil
}

// Day is a date with no time, as the people record uses (a worksite's local calendar day).
func Day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
