// Package authz is Nemura's permission matrix (PRD-002 Appendix B, NM-ROLE-01, NM-SEC-02): the rights each role holds per
// capability, from one table that the API, the row-level policies and the web app derive from. Rights are per field and
// action, and the matrix is closed by default: a role or capability it does not name holds nothing. Nemura's roles are its own and
// never grant a right in Fyniq, and the reverse; nothing in this package reads Fyniq's.
package authz

import "strings"

// Role is one of Nemura's roles, assigned per organisation (NM-ROLE-01). The AI is not a role a person holds: it is a principal
// that acts as the asking person and never exceeds their rights.
type Role string

// The roles, in the order of Appendix B's columns.
const (
	Employee      Role = "employee"
	LineManager   Role = "line_manager"
	HROfficer     Role = "hr_officer"
	HRAdmin       Role = "hr_admin"
	PayrollAdmin  Role = "payroll_admin"
	Recruiter     Role = "recruiter"
	Interviewer   Role = "interviewer"
	Administrator Role = "administrator"
	Owner         Role = "owner"
	AI            Role = "ai"
)

// Roles is every column of Appendix B, in order.
var Roles = []Role{Employee, LineManager, HROfficer, HRAdmin, PayrollAdmin, Recruiter, Interviewer, Administrator, Owner, AI}

// RoleCount is the number of columns.
const RoleCount = 10

// Headers are Appendix B's column headings, in the order of Roles.
var Headers = [RoleCount]string{"Employee", "Line manager", "HR officer", "HR admin", "Payroll admin", "Recruiter", "Interviewer", "Administrator", "Owner", "AI"}

// Row is one capability of Appendix B and what each role holds, as the PRD writes it.
type Row struct {
	Capability string
	Cells      [RoleCount]string
}

// Right is one letter of the legend: C create or prepare, A approve, S sign, V view, M masked, O own record only.
type Right byte

// The rights.
const (
	Create  Right = 'C'
	Approve Right = 'A'
	Sign    Right = 'S'
	View    Right = 'V'
	Masked  Right = 'M'
	Own     Right = 'O'
)

// Set is the rights a cell grants.
type Set map[Right]bool

// Parse reads a cell: its letters are rights, and everything in brackets or after a plain word is a condition that the
// code that applies the right reads (for instance "not own", "reports" or "assigned candidates only"). A dash is nothing.
func Parse(cell string) Set {
	set := Set{}
	depth := 0
	var word strings.Builder
	flush := func() {
		if w := word.String(); len(w) == 1 && strings.ContainsAny(w, "CASVMO") {
			set[Right(w[0])] = true
		}
		word.Reset()
	}
	for _, r := range cell {
		switch {
		case r == '(':
			flush()
			depth++
		case r == ')':
			depth--
		case depth > 0:
		case r == '/' || r == ';' || r == ' ' || r == '+':
			flush()
		default:
			word.WriteRune(r)
		}
	}
	flush()
	return set
}

// Rights is what role holds on capability: nothing when either is not in the matrix (closed by default).
func Rights(role Role, capability string) Set {
	col := -1
	for i, r := range Roles {
		if r == role {
			col = i
		}
	}
	if col < 0 {
		return Set{}
	}
	for _, row := range Rows {
		if row.Capability == capability {
			return Parse(row.Cells[col])
		}
	}
	return Set{}
}

// Can reports whether role holds right on capability. A masked view is not a view: it holds Masked, not View.
func Can(role Role, capability string, right Right) bool { return Rights(role, capability)[right] }
