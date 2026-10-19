// Package member holds the rules for inviting people and changing their roles (FR-ADM-01, FM-1): every rule
// that fails is reported at once, with its field, a stable code and how to fix it (ENG-23). Until the
// organisation has a second active member its Owner administers members and roles alone (the founder
// bootstrap, PRD-001 rev 0.35); from then on a role change needs a second approver and waits for the approval
// engine. The database (migrations 000010 and 000029) keeps roles known and the last Owner in place.
package member

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// The codes of the members' rules.
const (
	CodeRole           = "MEM-01" // not a role a member can hold
	CodeName           = "MEM-02" // the name is blank or too long
	CodeRoles          = "MEM-03" // no roles, or one named twice
	CodeExpiry         = "MEM-04" // the roles' expiry is not in the future, or is set on the Owner role
	CodeSecondApprover = "MEM-05" // a role change needs a second approver, and the approval engine does not exist yet
	CodeLastOwner      = "MEM-06" // the organisation's last Owner keeps the role
	CodeHeld           = "MEM-07" // the member holds that role already, or does not hold it
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

// Assignable are the roles a member can hold (§6.2), as the database knows them. The Super admin is the
// installation's, the AI and integrations are principals, and a line manager is a relationship.
var Assignable = []string{"accountant", "administrator", "ap_specialist", "approver", "ar_specialist", "auditor", "board_viewer",
	"bookkeeper", "company_secretary", "controller", "dpo", "employee", "owner", "payment_releaser", "payroll_administrator", "tax_agent"}

// CheckRoles returns every rule roles and their expiry (nil: none) break, at now.
func CheckRoles(roles []string, expires *time.Time, now time.Time) []Problem {
	var out []Problem
	if len(roles) == 0 || len(roles) > 16 {
		out = append(out, Problem{CodeRoles, "roles", "names no role, or more than 16", "Give each person 1 to 16 roles"})
	}
	seen := map[string]bool{}
	for i, r := range roles {
		field := fmt.Sprintf("roles[%d]", i)
		switch {
		case !slices.Contains(Assignable, r):
			out = append(out, Problem{CodeRole, field, fmt.Sprintf("%q is not a role a member can hold", r),
				"Use a role of §6.2, such as owner, administrator, controller, accountant or bookkeeper"})
		case seen[r]:
			out = append(out, Problem{CodeRoles, field, fmt.Sprintf("%s is named twice", r), "Name each role once"})
		}
		seen[r] = true
	}
	if expires != nil {
		switch {
		case !expires.After(now):
			out = append(out, Problem{CodeExpiry, "roles_expire_at", fmt.Sprintf("%s is not in the future", expires.UTC().Format(time.RFC3339)),
				"Give a later date, or leave it out for a member who stays"})
		case seen["owner"]:
			out = append(out, Problem{CodeExpiry, "roles_expire_at", "the Owner's role never expires",
				"Leave the expiry out, or give the Owner role separately"})
		}
	}
	return out
}

// CheckName returns the problem with an invitation's name, or nil.
func CheckName(name string) []Problem {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 200 {
		return []Problem{{CodeName, "display_name", "is blank or longer than 200 characters", "Enter the name of the person you invite"}}
	}
	return nil
}

// Bootstrap returns the problem with changing roles in an organisation of members active members, or nil:
// until a second member joins, its Owner acts alone; after that the change needs a second approver (FM-1),
// which arrives with the approval engine.
func Bootstrap(members int) *Problem {
	if members > 1 {
		return &Problem{CodeSecondApprover, "roles", fmt.Sprintf("the organisation has %d members, so a role change needs a second approver (FM-1)", members),
			"Role changes wait for the approval engine, which brings the second approver; until then nothing is changed"}
	}
	return nil
}
