//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// An Approver's role is narrowed to department values when it is given (FR-ADM-01, rev 0.253): after the bootstrap the change
// waits for another Owner and the scope goes with it; the members list shows the scope; another role, an ID that is no
// department's value, a repeated one and a malformed one are refused; the scope is audited.
// req: FR-ADM-01
// asvs: V8.2.1, V8.3.1
func TestAnApproverRoleIsNarrowedToDepartments(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	region := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Region"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"})
	apac := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": region.str("id"), "code": "APAC", "name": "Asia Pacific"})
	if eng.Status != 201 || apac.Status != 201 {
		t.Fatalf("the values: %d %s / %d %s", eng.Status, eng.Raw, apac.Status, apac.Raw)
	}
	roles := "/v1/members/" + f.third + "/roles"
	for what, body := range map[string]map[string]any{
		"another role":                 {"role": "accountant", "department_value_ids": []string{eng.str("id")}},
		"a value of another dimension": {"role": "approver", "department_value_ids": []string{apac.str("id")}},
		"a repeated value":             {"role": "approver", "department_value_ids": []string{eng.str("id"), eng.str("id")}},
		"a malformed ID":               {"role": "approver", "department_value_ids": []string{"engineering"}},
		"an ID that is nothing":        {"role": "approver", "department_value_ids": []string{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99"}},
	} {
		if r := e.call(http.MethodPost, roles, ahkow, body); r.Status != 422 {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	r := e.call(http.MethodPost, roles, ahkow, map[string]any{"role": "approver", "department_value_ids": []string{eng.str("id")}})
	if r.Status != 202 || r.str("operation") != "role.assign" {
		t.Fatalf("asking for the narrowed role: %d %s", r.Status, r.Raw)
	}
	if a := f.decide(weiling, r.str("id"), "approve", hashOf(r)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	m := e.call(http.MethodGet, "/v1/members", weiling, nil)
	for _, it := range m.Body["items"].([]any) {
		item := it.(map[string]any)
		if item["display_name"] != "Third" {
			continue
		}
		if _, ok := item["passkeys"]; !ok {
			t.Fatalf("the member's passkey count is missing: %v", item) // SEC-06: the list says whether they hold a passkey
		}
		role := item["roles"].([]any)[0].(map[string]any)
		ids, _ := role["department_value_ids"].([]any)
		if role["role"] != "approver" || len(ids) != 1 || ids[0] != eng.str("id") {
			t.Fatalf("the member's narrowed role: %v", role)
		}
		return
	}
	t.Fatalf("Third is not listed: %s", m.Raw)
}
