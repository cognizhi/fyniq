package director

import (
	"testing"

	"github.com/google/uuid"
)

// FM-9 (design D10), worked by hand: Wei Ling and Arjun could sign Mei Lin's settlement. When both are directors with a
// settlement of their own waiting or decided in the last 30 days, every available approver benefits, and the Accountant
// reviews it too; when Arjun has none, or signs in as no director, or nobody but the person could sign, not.
// req: FM-9
// req: access/team-modes Every available approver benefits
func TestEveryApproverBenefits(t *testing.T) {
	weiling, arjun := uuid.New(), uuid.New()
	weilingC, arjunC := uuid.New(), uuid.New()
	contacts := map[uuid.UUID]uuid.UUID{weiling: weilingC, arjun: arjunC}
	contactOf := func(u uuid.UUID) (uuid.UUID, bool) { c, ok := contacts[u]; return c, ok }
	both := map[uuid.UUID]bool{weilingC: true, arjunC: true}
	if !EveryApproverBenefits([]uuid.UUID{weiling, arjun}, contactOf, both) {
		t.Error("both benefit, but the Accountant is not asked")
	}
	if EveryApproverBenefits([]uuid.UUID{weiling, arjun}, contactOf, map[uuid.UUID]bool{weilingC: true}) {
		t.Error("Arjun has no settlement, but the Accountant is asked")
	}
	if EveryApproverBenefits([]uuid.UUID{weiling, uuid.New()}, contactOf, both) {
		t.Error("a signer who is no director benefits")
	}
	if EveryApproverBenefits(nil, contactOf, both) {
		t.Error("with nobody to sign, the Accountant is asked")
	}
}
