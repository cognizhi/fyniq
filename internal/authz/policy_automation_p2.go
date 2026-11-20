package authz

// Workflow rules without code (FR-AUT-03; p2-mobile-approvals-automation): an organisation integration like the webhooks, an Administrator's to make and change, read by those who view
// integrations; never the AI. They are added here so the policy index of policy.go keeps to the operations of earlier changes.
var (
	ListAutomationRules  = Policy{Operation: "listAutomationRules", Capability: Integrations, Need: View}
	CreateAutomationRule = Policy{Operation: "createAutomationRule", Capability: Integrations, Need: Create}
	ChangeAutomationRule = Policy{Operation: "changeAutomationRule", Capability: Integrations, Need: Create}
)

func init() {
	for _, p := range []Policy{ListAutomationRules, CreateAutomationRule, ChangeAutomationRule} {
		if _, dup := Policies[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		Policies[p.Operation] = p
	}
}
