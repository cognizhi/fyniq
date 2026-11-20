package authz

// The parent's consolidation (FR-ENT-05, FR-ENT-06, FR-RPT-12; p3-multi-entity): the Entity groups row's View reads the set-up and the runs, its Create maps, enters rates, sets the
// pairs and runs. Never the AI: a group's figures are not on its read tools.
var (
	ListGroupMappings       = Policy{Operation: "listGroupMappings", Capability: EntityGroups, Need: View}
	MapGroupAccount         = Policy{Operation: "mapGroupAccount", Capability: EntityGroups, Need: Create}
	ListGroupRates          = Policy{Operation: "listGroupRates", Capability: EntityGroups, Need: View}
	AddGroupRate            = Policy{Operation: "addGroupRate", Capability: EntityGroups, Need: Create}
	GetGroupPairs           = Policy{Operation: "getGroupPairs", Capability: EntityGroups, Need: View}
	SetGroupPairs           = Policy{Operation: "setGroupPairs", Capability: EntityGroups, Need: Create}
	ListGroupEntityAccounts = Policy{Operation: "listGroupEntityAccounts", Capability: EntityGroups, Need: Create}
	RunConsolidation        = Policy{Operation: "runConsolidation", Capability: EntityGroups, Need: Create}
	ListConsolidations      = Policy{Operation: "listConsolidations", Capability: EntityGroups, Need: View}
	GetConsolidation        = Policy{Operation: "getConsolidation", Capability: EntityGroups, Need: View}
)

func init() {
	for _, p := range []Policy{ListGroupMappings, MapGroupAccount, ListGroupRates, AddGroupRate, GetGroupPairs, SetGroupPairs, ListGroupEntityAccounts, RunConsolidation, ListConsolidations, GetConsolidation} {
		if _, dup := Policies[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		Policies[p.Operation] = p
	}
}
