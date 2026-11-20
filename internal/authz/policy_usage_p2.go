package authz

// The tenant usage and cost view (FR-ADM-05; p2-mobile-approvals-automation): what the organisation used of AI, storage and exports, read-only, by those who view the
// organisation's data export row (the Owner, the Administrator and the DPO). Never the AI: its column of that row is empty.
var GetUsage = Policy{Operation: "getUsage", Capability: TenantExport, Need: View}

func init() {
	if _, dup := Policies[GetUsage.Operation]; dup {
		panic("authz: two policies for " + GetUsage.Operation)
	}
	Policies[GetUsage.Operation] = GetUsage
}
