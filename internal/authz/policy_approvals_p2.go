package authz

// Delegation of approval authority (FR-APR-06; p2-mobile-approvals-automation). Like the approvals themselves they are not an Appendix D row: a person who holds a right on one of the rows
// the approval engine covers delegates their own authority (A), lists what concerns them or ends it (V; the operation itself limits it to the delegator, the delegate and an Owner). The AI never
// sees or makes one (§9.6). They are added here so the policy index of policy.go keeps to the operations of earlier changes.
var (
	ListApprovalDelegations  = Policy{Operation: "listApprovalDelegations", Capability: Approvals, Need: View, Note: approvalNote + " Lists the delegations the caller made or holds; an Owner may list them all."}
	CreateApprovalDelegation = Policy{Operation: "createApprovalDelegation", Capability: Approvals, Need: Approve, Note: approvalNote + " A person delegates only their own authority, with step-up."}
	EndApprovalDelegation    = Policy{Operation: "endApprovalDelegation", Capability: Approvals, Need: View, Note: approvalNote + " The delegator, the delegate or an Owner ends a delegation, with step-up."}
)

func init() {
	for _, p := range []Policy{ListApprovalDelegations, CreateApprovalDelegation, EndApprovalDelegation} {
		if _, dup := Policies[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		Policies[p.Operation] = p
	}
}
