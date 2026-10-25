package authz_test

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The AI writes nothing in Banking (FR-BNK-14, AI-P4, AI-34; the banking threat model's T17): acting for any roles at
// all, every Banking operation that changes something is refused to it, so its suggestions can only ever be proposals
// a person approves; nothing in Banking is open to it by an Owner's exception either.
// req: FR-BNK-14
// req: AI-34
// asvs: V8.2.1
func TestTheAIWritesNothingInBanking(t *testing.T) {
	var everyone []authz.Role
	for r := range authz.Roles {
		everyone = append(everyone, r)
	}
	banking := []authz.Capability{authz.BankTransactions, authz.BankReconciliation, authz.BankConnections}
	writes := 0
	for op, p := range authz.Policies {
		if !slices.Contains(banking, p.Capability) || p.Need == authz.View {
			continue
		}
		writes++
		if d := authz.Decide(p, everyone, true); d.Allowed {
			t.Errorf("%s (%s on %q) is open to the AI", op, string(p.Need), p.Capability)
		}
		if d := authz.Decide(p, everyone, false); !d.Allowed {
			t.Errorf("%s (%s on %q) is open to nobody: the check proves nothing", op, string(p.Need), p.Capability)
		}
	}
	if writes < 25 {
		t.Fatalf("only %d Banking writes in the policy table: the capabilities moved", writes)
	}
}
