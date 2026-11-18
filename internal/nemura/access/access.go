// Package access is Nemura's need to know (PRD-002 NM-SEC-02, NM-PPL-02): what a person may see of a restricted field. Pay, NRIC or
// FIN, bank account, medical, disciplinary, dependants, review and candidate data each have a right in Appendix B (package authz);
// a restricted field is masked by default, shown in full only with the right, and a person always sees their own. Every release of a
// restricted field is audited by the caller (nemura.restricted.read). A field that is not shown says so in plain words.
package access

import (
	"strings"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Level is how much of a restricted field a person sees.
type Level int

// The levels, from least to most.
const (
	Hidden Level = iota // none: the field is not there for this person
	Masked              // the shape only, such as the last four characters
	Full                // the value
)

func (l Level) String() string { return [...]string{"hidden", "masked", "full"}[l] }

// Restricted are the capabilities of Appendix B whose every read is audited (NM-PPL-02): reading one writes nemura.restricted.read.
var Restricted = []string{
	"NRIC or FIN", "Salary bank account", "Medical certificates and notes", "Disciplinary records", "Dependants (NM-PPL-07)", "Pay, pay history, pay runs",
}

// IsRestricted reports whether reading the capability is audited.
func IsRestricted(capability string) bool {
	for _, c := range Restricted {
		if c == capability {
			return true
		}
	}
	return false
}

// Of is what someone holding these roles sees of capability: the highest level any role gives. Their own record is always full (a
// person sees their own: NM-SEC-02), and a role the matrix does not name sees nothing. Whether a manager's reports or an
// interviewer's assigned candidates are the ones concerned is the scope the database policy adds; this is the field level.
func Of(held []authz.Role, capability string, ownRecord bool) Level {
	if ownRecord {
		return Full
	}
	level := Hidden
	for _, r := range held {
		switch rights := authz.Rights(r, capability); {
		case rights[authz.View]:
			level = max(level, Full)
		case rights[authz.Masked]:
			level = max(level, Masked)
		}
	}
	return level
}

// Mask keeps the last four characters of an identifier: S1234567D becomes *****567D (the shape audit masking uses, SEC-15).
func Mask(value string) string {
	r := []rune(value)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return strings.Repeat("*", len(r)-4) + string(r[len(r)-4:])
}

// Release returns value as the level allows: the value, its mask, or nothing (ok false) when the field is hidden.
func Release(value string, level Level) (shown string, ok bool) {
	switch level {
	case Full:
		return value, true
	case Masked:
		return Mask(value), true
	}
	return "", false
}
