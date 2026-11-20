package people

import (
	"regexp"
	"strings"
	"time"
)

// The rules for the restricted fields (PRD-002 NM-PPL-02, 03, 04, 07): what a value looks like, and what a screen may show of it.

var (
	nricShape = regexp.MustCompile(`^[STFGM][0-9]{7}[A-Z]$`)
	bankShape = regexp.MustCompile(`^[0-9]{5,20}$`)
	// BankCodeShape is a Singapore bank code: four digits, or four and the three of a branch.
	BankCodeShape = regexp.MustCompile(`^[0-9]{4}([0-9]{3})?$`)
	passShape = regexp.MustCompile(`^[A-Z0-9]{6,20}$`)
)

// NormaliseNRIC upper-cases and trims an NRIC or FIN, and refuses anything that is not shaped like one. The check digit is not computed here: the pack
// that holds the rule for it arrives with the Singapore pack, and a wrong check digit is caught at the first statutory filing.
func NormaliseNRIC(v string) (string, error) {
	v = strings.ToUpper(strings.TrimSpace(v))
	if !nricShape.MatchString(v) {
		return "", invalid("The NRIC or FIN is not shaped like one: a letter, seven digits and a letter, such as S1234567D.")
	}
	return v, nil
}

// MaskNRIC shows the first letter and the last four characters: S****567D.
func MaskNRIC(v string) string { return v[:1] + "****" + v[len(v)-4:] }

// NormaliseBank keeps the digits of a bank account number and refuses anything that is not 5 to 20 of them.
func NormaliseBank(v string) (string, error) {
	v = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(v))
	if !bankShape.MatchString(v) {
		return "", invalid("The bank account number is not 5 to 20 digits: write the digits only, with no spaces or dashes.")
	}
	return v, nil
}

// MaskBank shows the last four digits: ****1234.
func MaskBank(v string) string { return "****" + v[len(v)-4:] }

// PassTypes are the kinds of work pass of NM-PPL-03.
var PassTypes = []string{"employment_pass", "s_pass", "work_permit", "dp_work_permit"}

// NormalisePass checks a work pass and its number.
func NormalisePass(kind, number string, issued, expires time.Time) (string, error) {
	known := false
	for _, k := range PassTypes {
		known = known || k == kind
	}
	if !known {
		return "", invalid("The pass type %q is not employment_pass, s_pass, work_permit or dp_work_permit.", kind)
	}
	number = strings.ToUpper(strings.TrimSpace(number))
	if !passShape.MatchString(number) {
		return "", invalid("The pass number is not 6 to 20 letters and digits.")
	}
	if !expires.After(issued) {
		return "", invalid("The pass expires on or before the day it was issued: check both dates.")
	}
	return number, nil
}

// MaskPass shows the last three characters: *****A12.
func MaskPass(v string) string { return strings.Repeat("*", len(v)-3) + v[len(v)-3:] }

// ReminderDays are the days before a pass expires that the employer is reminded (NM-PPL-03).
var ReminderDays = []int{90, 60, 30}

// Dependant is a child for family leave (NM-PPL-07).
type Dependant struct {
	Relationship   string // "child", "adopted_child" or "stepchild"
	BirthDate      *time.Time
	AdoptionIntent bool
	Citizenship    string // "citizen", "permanent_resident" or "other"
	DeclaredOn     time.Time
}

// Check validates a dependant: a date of birth, or the formal intent to adopt.
func (d Dependant) Check() error {
	switch d.Relationship {
	case "child", "adopted_child", "stepchild":
	default:
		return invalid("The relationship %q is not child, adopted_child or stepchild.", d.Relationship)
	}
	switch d.Citizenship {
	case "citizen", "permanent_resident", "other":
	default:
		return invalid("The citizenship %q is not citizen, permanent_resident or other.", d.Citizenship)
	}
	if d.BirthDate == nil && !d.AdoptionIntent {
		return invalid("A dependant needs a date of birth, or the formal intent to adopt.")
	}
	if d.DeclaredOn.IsZero() {
		return invalid("The day the employee made the declaration is required.")
	}
	return nil
}

// DocumentTypes are the kinds of document kept for a person (NM-PPL-05).
var DocumentTypes = []string{"contract", "kets", "letter", "pass_copy", "certificate", "other"}
