package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// Tier 3 is reserved: no release's catalogue holds a task type of it, but the approval engine is ready for one (AI-09, AI-92): its request has two steps
// of the task type's approver roles, the person who asked never signs it, the same person cannot sign twice, and a second, different person signs the
// second step. Tiers 1 and 2 keep their one step.
// req: AI-09
// req: AI-92
// asvs: V8.2.2
func TestATierThreeRequestNeedsTwoDifferentPeople(t *testing.T) {
	roles := []string{"owner", "accountant"}
	if got := proposalSteps(proposal.Tier1, roles); len(got) != 1 {
		t.Fatalf("tier 1 steps: %+v", got)
	}
	if got := proposalSteps(proposal.Tier2, roles); len(got) != 1 {
		t.Fatalf("tier 2 steps: %+v", got)
	}
	steps := proposalSteps(proposal.Tier3, roles)
	if len(steps) != 2 || steps[0].Quorum != 1 || steps[1].Quorum != 1 {
		t.Fatalf("tier 3 steps: %+v", steps)
	}
	requester, first, second := uuid.New(), uuid.New(), uuid.New()
	change, _ := json.Marshal(stored{Execute: map[string]any{"task_type": "test.tier_three", "tier": float64(proposal.Tier3), "command": "createContact"}})
	r := storage.ApprovalRequest{ObjectType: "ai_proposal", RequestedBy: requester, Status: "pending", Change: change, PolicySteps: storageSteps(steps)}
	person := func(id uuid.UUID) authn.Principal {
		return authn.Principal{UserID: id, Class: authn.ClassUser, Roles: []authz.Role{authz.Owner, authz.Controller}}
	}
	if mayDecide(person(requester), r, nil) {
		t.Error("the person who asked signed a tier-3 request: only tier 1 allows that")
	}
	if !mayDecide(person(first), r, nil) {
		t.Fatal("a first signer was refused")
	}
	signed := []storage.ApprovalSignature{{Step: 1, Signer: first, Decision: "approve"}}
	if mayDecide(person(first), r, signed) {
		t.Error("the same person signed the second step")
	}
	if !mayDecide(person(second), r, signed) {
		t.Error("a different person was refused the second step")
	}
	both := append(signed, storage.ApprovalSignature{Step: 2, Signer: second, Decision: "approve"})
	if mayDecide(person(uuid.New()), r, both) {
		t.Error("a third signature after both steps")
	}
	// A tier-1 request keeps its one step and lets the person who asked sign (AI-10).
	one, _ := json.Marshal(stored{Execute: map[string]any{"task_type": "test.tier_one", "tier": float64(proposal.Tier1), "command": "createContact"}})
	r1 := storage.ApprovalRequest{ObjectType: "ai_proposal", RequestedBy: requester, Status: "pending", Change: one, PolicySteps: storageSteps(proposalSteps(proposal.Tier1, roles))}
	if !mayDecide(person(requester), r1, nil) {
		t.Error("the person who asked could not sign their own tier-1 proposal")
	}
}
