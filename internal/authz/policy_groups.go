package authz

// Groups of entities (FR-ENT-04, rev 0.625; p3-multi-entity): an entity is an organisation and a group is organisations linked by the consent of both Owners. Issuing,
// accepting, withdrawing and ending a link are the Owner's alone (Approve on the Entity groups row, with step-up); reading the group is the row's View. Never the AI.
var (
	GetEntityGroup          = Policy{Operation: "getEntityGroup", Capability: EntityGroups, Need: View}
	InviteGroupEntity       = Policy{Operation: "inviteGroupEntity", Capability: EntityGroups, Need: Approve}
	WithdrawGroupInvitation = Policy{Operation: "withdrawGroupInvitation", Capability: EntityGroups, Need: Approve}
	AcceptGroupInvitation   = Policy{Operation: "acceptGroupInvitation", Capability: EntityGroups, Need: Approve}
	EndGroupLink            = Policy{Operation: "endGroupLink", Capability: EntityGroups, Need: Approve}
)

func init() {
	for _, p := range []Policy{GetEntityGroup, InviteGroupEntity, WithdrawGroupInvitation, AcceptGroupInvitation, EndGroupLink} {
		if _, dup := Policies[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		Policies[p.Operation] = p
	}
}
