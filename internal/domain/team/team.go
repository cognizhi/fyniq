// Package team holds the team modes (PRD-001 §6.4): how Fyniq fits segregation of duties to the size of the team.
// Founder mode allows the conflicts of §6.3 with the compensating controls FM-1 to FM-9; Standard and Enterprise
// mode block them, and an override needs a reason and a second approver and is reported. The organisation starts
// in Founder mode; a switch is an approval request (dual control), and a switch to a mode that blocks conflicts
// lists every member whose roles conflict and waits until each is resolved or overridden (rev 0.42, provisional).
package team

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// The team modes.
const (
	Founder    = "founder"
	Standard   = "standard"
	Enterprise = "enterprise"
)

// Modes are the team modes, smallest team first.
var Modes = []string{Founder, Standard, Enterprise}

// Default is the mode an organisation starts in, until a switch is approved (§6.4: Founder mode at go-live).
const Default = Founder

// Blocks reports whether mode blocks segregation-of-duties conflicts: Standard and Enterprise mode do; Founder mode
// allows them with its compensating controls.
func Blocks(mode string) bool { return mode == Standard || mode == Enterprise }

// The codes of the team-mode rules.
const (
	CodeConflicts   = "TEAM-01" // a switch to a mode that blocks conflicts waits until every conflict is resolved or overridden
	CodeConflict    = "TEAM-02" // a role change would make a conflict the team mode blocks
	CodeUnavailable = "TEAM-03" // Enterprise mode needs features Fyniq does not have yet
	CodeAction      = "TEAM-04" // the action is one side of a conflict the person holds, which the team mode blocks
	CodeSame        = "TEAM-05" // the organisation is in that mode already
	CodeOverride    = "TEAM-06" // an override is not one that can be recorded
)

// MaxReason is the longest reason an override may give.
const MaxReason = 500

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

// CheckMode returns the problems with switching from the mode in force to mode.
func CheckMode(current, mode string) []Problem {
	switch {
	case !slices.Contains(Modes, mode):
		return []Problem{{CodeUnavailable, "mode", fmt.Sprintf("%q is not a team mode", mode), "Use founder, standard or enterprise"}}
	case mode == current:
		return []Problem{{CodeSame, "mode", "the organisation is in " + mode + " mode already", "Nothing to do"}}
	case mode == Enterprise:
		// rev 0.42, provisional: Enterprise mode's approval matrices per entity and department, SSO, SCIM and quarterly
		// access reviews are not built, so it cannot be chosen yet.
		return []Problem{{CodeUnavailable, "mode", "Enterprise mode needs approval matrices per entity and department, sign-in through SSO, " +
			"provisioning through SCIM and quarterly access reviews, which Fyniq does not have yet", "Use Standard mode, which blocks the same conflicts"}}
	}
	return nil
}

// Conflict is a segregation-of-duties conflict that a member's roles, or a pending invitation's, hold together.
type Conflict struct {
	SoD          string    // such as "SOD-6"
	UserID       uuid.UUID // the member's user; uuid.Nil for an invitation
	InvitationID uuid.UUID // the pending invitation; uuid.Nil for a member
	Name         string    // the member's or the invitee's name
	Roles        []string  // the roles that make it
}

// Key names a conflict in an envelope: what it is, whose and which roles make it.
func (c Conflict) Key() string {
	who := "member:" + c.UserID.String()
	if c.InvitationID != uuid.Nil {
		who = "invitation:" + c.InvitationID.String()
	}
	return who + ":" + c.SoD + ":" + strings.Join(c.Roles, "+")
}

// Override is a conflict a member may keep in a mode that blocks it, with the reason (§6.4).
type Override struct {
	UserID uuid.UUID
	SoD    string
	Reason string
}

// CheckOverrides returns the problems with the overrides a switch to a mode that blocks conflicts asks for: each names
// a member's conflict of conflicts once, with a reason; every member's conflict is overridden; and no invitation's
// conflict is left, since an invitation cannot be overridden (withdraw it and invite again).
func CheckOverrides(conflicts []Conflict, overrides []Override) []Problem {
	var out []Problem
	seen := map[string]bool{}
	for i, o := range overrides {
		field := fmt.Sprintf("overrides[%d]", i)
		k := o.UserID.String() + ":" + o.SoD
		switch {
		case !slices.ContainsFunc(conflicts, func(c Conflict) bool { return c.UserID == o.UserID && c.SoD == o.SoD && c.InvitationID == uuid.Nil }):
			out = append(out, Problem{CodeOverride, field, "no member holds that conflict", "Override only the conflicts the team mode lists"})
		case seen[k]:
			out = append(out, Problem{CodeOverride, field, "overrides that conflict twice", "Override each conflict once"})
		}
		seen[k] = true
		out = append(out, CheckReason(o.Reason, field+".reason")...)
	}
	for _, c := range conflicts {
		if c.InvitationID != uuid.Nil {
			out = append(out, Problem{CodeConflicts, "mode", fmt.Sprintf("the pending invitation for %s gives roles that conflict (%s: %s)", c.Name, c.SoD,
				strings.Join(c.Roles, ", ")), "Withdraw the invitation and invite them again with roles that do not conflict"})
			continue
		}
		if !seen[c.UserID.String()+":"+c.SoD] {
			out = append(out, Problem{CodeConflicts, "mode", fmt.Sprintf("%s holds roles that conflict (%s: %s)", c.Name, c.SoD, strings.Join(c.Roles, ", ")),
				"End one of those roles, or override the conflict with a reason"})
		}
	}
	return out
}

// CheckReason returns the problem with an override's reason, or nil.
func CheckReason(reason, field string) []Problem {
	if r := strings.TrimSpace(reason); r == "" || len([]rune(r)) > MaxReason {
		return []Problem{{CodeOverride, field, fmt.Sprintf("an override needs its reason, of at most %d characters", MaxReason),
			"Say why this person keeps both sides of the conflict"}}
	}
	return nil
}
