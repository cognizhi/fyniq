//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The Inbox's items for each role of §7.1 (QA-UI-08; FR-UX-03, FR-UX-40): a setting change under dual control that a founder
// asked for waits for approval. Every role Appendix D gives the Approvals row a view lists it, and only the Owner, who holds
// the approval right on settings, has it as their turn (you_may_approve); an Employee, a line manager, the HR officer
// (Nemura's) and a Super admin (outside every organisation) are refused the list, so the Inbox shows them no request.
// The AI reads the list and never decides it.
// req: QA-UI-08
// req: FR-UX-03
// req: FM-1
// asvs: V8.2.1, V4.2.1
func TestEachRoleSeesTheInboxItemsItsRightsGive(t *testing.T) {
	f := newFounders(t)
	e := f.e
	asked := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", f.as(f.ahkow), map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking for the change: %d %s", asked.Status, asked.Raw)
	}
	refused := map[authz.Role]bool{authz.Employee: true, authz.LineManager: true, authz.HROfficer: true, authz.SuperAdmin: true}
	for _, role := range authz.Principals() {
		l := e.call(http.MethodGet, "/v1/approvals?status=pending", f.as(f.weiling, role), nil)
		if refused[role] {
			if l.Status != http.StatusForbidden {
				t.Errorf("%s: the Inbox's requests answered %d; want 403", role, l.Status)
			}
			continue
		}
		items, _ := l.Body["items"].([]any)
		if l.Status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != asked.str("id") {
			t.Errorf("%s: the Inbox's requests: %d %s", role, l.Status, l.Raw)
			continue
		}
		if may := items[0].(map[string]any)["you_may_approve"] == true; may != (role == authz.Owner) {
			t.Errorf("%s: its turn to approve is %v; only the Owner holds the approval on settings", role, may)
		}
	}
}
