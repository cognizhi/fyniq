package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Leave requests are read and decided by scope: everyone has their own, a line manager their reports' and decides on them, the people team and the Owner decide on
// anyone's, Payroll reads anyone's and decides none, and an employee decides nothing; deciding one's own request is refused (NM-LV-04, NM-LV-05).
// req: NM-LV-04, NM-LV-05
// asvs: V8.2.1, V8.3.1
func TestLeaveRequestsAreReadAndDecidedByScope(t *testing.T) {
	var got LeaveScope
	requestsScope = &got
	defer func() { requestsScope = nil }()
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80"
	for _, tc := range []struct {
		roles   []authz.Role
		scope   LeaveScope
		decide  int
		settle  int
		message string
	}{
		{[]authz.Role{authz.Employee}, LeaveScope{}, 403, 403, "an employee reads their own and decides nothing"},
		{[]authz.Role{authz.LineManager}, LeaveScope{Reports: true, Decide: true}, 204, 403, "a manager decides on their reports"},
		{[]authz.Role{authz.HROfficer}, LeaveScope{All: true, Decide: true}, 204, 200, "the HR officer decides on anyone's"},
		{[]authz.Role{authz.HRAdmin}, LeaveScope{All: true, Decide: true}, 204, 200, "the HR administrator likewise"},
		{[]authz.Role{authz.PayrollAdmin}, LeaveScope{All: true}, 403, 403, "Payroll reads and decides none"},
		{[]authz.Role{authz.Owner}, LeaveScope{All: true, Decide: true}, 204, 403, "the Owner decides but settling is the people team's"},
		{[]authz.Role{authz.Administrator}, LeaveScope{}, 403, 403, "the Administrator has no leave rights"},
	} {
		if got = (LeaveScope{}); do(tc.roles, "GET", "/v1/leave-requests", "") != 200 || got != tc.scope {
			t.Errorf("%s: scope %+v, want %+v", tc.message, got, tc.scope)
		}
		if code := do(tc.roles, "POST", "/v1/leave-requests/"+id+"/decision", `{"decision":"approve"}`); code != tc.decide {
			t.Errorf("%s: deciding %d, want %d", tc.message, code, tc.decide)
		}
		if code := do(tc.roles, "POST", "/v1/leave/settlement", `{}`); code != tc.settle {
			t.Errorf("%s: settling %d, want %d", tc.message, code, tc.settle)
		}
	}
	// Every signed-in person asks for leave for themselves and cancels their own.
	for _, role := range []authz.Role{authz.Employee, authz.PayrollAdmin, authz.Owner} {
		if do([]authz.Role{role}, "POST", "/v1/me/leave-requests", `{"type":"annual","from":"2026-04-06","to":"2026-04-10"}`) != 201 {
			t.Errorf("%s could not ask for leave", role)
		}
		if do([]authz.Role{role}, "POST", "/v1/leave-requests/"+id+"/cancellation", "") != 204 {
			t.Errorf("%s could not cancel", role)
		}
	}
}
