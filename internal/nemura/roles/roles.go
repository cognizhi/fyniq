// Package roles holds Nemura's rules for who may give whom a role (PRD-002 NM-ROLE-01, NM-ROLE-04, NM-ROLE-05; PRD-001 SOD-6), over
// the permission matrix of package authz. The database enforces the invariants that must hold even when this code is wrong: a
// privileged grant stands only on an approval from an Owner who is neither the one asking, granting nor receiving it, and nobody
// gives themselves a role (migration 000004). These functions give the plain refusals first.
package roles

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Capability is the row of Appendix B that governs roles.
const Capability = "Roles (NM-ROLE-05)"

// Privileged are the roles that carry pay or signing rights: granting one needs the Owner's approval (NM-ROLE-05). The Owner role
// itself is privileged: an HR administrator who configures roles cannot give anyone, themselves included, pay or signing rights.
var Privileged = []authz.Role{authz.PayrollAdmin, authz.Owner}

// IsPrivileged reports whether granting role needs an approval.
func IsPrivileged(role authz.Role) bool { return slices.Contains(Privileged, role) }

// Refusal is a refusal with a stable code and a plain fix (golden rule 13).
type Refusal struct {
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// The codes of the refusals.
const (
	CodeRoleRequired     = "NM_ROLE_REQUIRED"      // the actor holds no right to do this
	CodeApprovalRequired = "NM_APPROVAL_REQUIRED"  // pay and signing rights need an Owner's approval first
	CodeApprovalSelf     = "NM_APPROVAL_SELF"      // nobody approves their own request, change or role
	CodeApprovalNotOwner = "NM_APPROVAL_NOT_OWNER" // only an Owner decides a request for pay or signing rights
)

// Holds reports whether any of the actor's roles holds right on the roles capability.
func holds(actor []authz.Role, right authz.Right) bool {
	for _, r := range actor {
		if authz.Can(r, Capability, right) {
			return true
		}
	}
	return false
}

// CheckGrant says whether an actor with these roles may give subject the role directly, or must ask first. who is the actor and
// subject the person who receives it. A non-privileged role is granted by whoever holds Create on the roles capability (the HR
// administrator, the Administrator) or the Owner; a privileged one is asked for the same way and stands only once an Owner
// approves (needsApproval). Nobody grants themselves a role.
func CheckGrant(actor []authz.Role, who, subject uuid.UUID, role authz.Role) (needsApproval bool, err error) {
	if who == subject {
		return false, &Refusal{CodeApprovalSelf, "You cannot give yourself a role. Ask someone else who holds the right to manage roles."}
	}
	if !holds(actor, authz.Create) && !slices.Contains(actor, authz.Owner) {
		return false, &Refusal{CodeRoleRequired, "Your roles do not let you manage roles. Ask an HR administrator, an Administrator or the Owner."}
	}
	return IsPrivileged(role), nil
}

// CheckDecision says whether an approver may decide the request that who made to give subject a privileged role: only an Owner, who
// is neither the one who asked nor the one who receives it.
func CheckDecision(approver []authz.Role, who, requester, subject uuid.UUID) error {
	switch {
	case who == requester || who == subject:
		return &Refusal{CodeApprovalSelf, "You cannot approve your own request, or a role you would receive. Ask another Owner."}
	case !holds(approver, authz.Approve):
		return &Refusal{CodeApprovalNotOwner, "Only an Owner approves pay and signing rights. Ask an Owner."}
	}
	return nil
}

// Digest is what an approval is for: the grant's member, role and day, so an approval of one grant is no approval of another.
func Digest(member uuid.UUID, role authz.Role, from time.Time) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("role_assignment|%s|%s|%s", member, role, from.Format(time.DateOnly))))
	return sum[:]
}
