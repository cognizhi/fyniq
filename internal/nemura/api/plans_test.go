package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Only the HR officer, the HR administrator and the Owner make templates and plans; any signed-in member reads and decides the tasks that are theirs (the service decides which); nobody without a session does
// anything; and everything is refused with the reason while Onboarding is switched off (NM-ONB-01, NM-FEAT-01).
// req: NM-ONB-01, NM-FEAT-01
// asvs: V8.2.1, V8.2.2, V8.3.1
func TestOnboardingPlanOperationsAreForThePeopleTeamAndEachPersonsOwnTasks(t *testing.T) {
	const id = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a01"
	type op struct{ method, path, body string }
	teamOps := []op{{"GET", "/v1/onboarding/templates", ""}, {"POST", "/v1/onboarding/templates", `{"name":"Office joiner"}`}, {"POST", "/v1/onboarding/templates/" + id + "/archive", ``},
		{"POST", "/v1/people/" + id + "/onboarding-plan", `{}`}, {"GET", "/v1/onboarding/plans", ""},
		{"POST", "/v1/onboarding/plans/" + id + "/tasks", `{"title":"Order a pass","assignee":"hr","due_on":"2026-12-03"}`}}
	ownOps := []op{{"GET", "/v1/me/onboarding", ""}, {"POST", "/v1/onboarding/plan-tasks/" + id + "/decision", `{"state":"done"}`}}
	call := func(o op, cookie string) int {
		_, mux := testAPI(t)
		req := httptest.NewRequest(o.method, o.path, strings.NewReader(o.body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	defer func() { testRoles, featuresOff = []authz.Role{authz.HRAdmin}, false }()
	for _, o := range append(teamOps, ownOps...) {
		if code := call(o, ""); code != 401 {
			t.Errorf("%s %s with no session: %d", o.method, o.path, code)
		}
	}
	for name, roles := range map[string][]authz.Role{"an employee": {authz.Employee}, "a line manager": {authz.LineManager}, "a payroll administrator": {authz.PayrollAdmin}, "an administrator": {authz.Administrator}} {
		testRoles = roles
		for _, o := range teamOps {
			if code := call(o, "nemura_session=good-token"); code != 403 {
				t.Errorf("%s: %s %s is not for them: %d", name, o.method, o.path, code)
			}
		}
		for _, o := range ownOps {
			if code := call(o, "nemura_session=good-token"); code != 200 && code != 204 {
				t.Errorf("%s: %s %s: %d", name, o.method, o.path, code)
			}
		}
	}
	for name, roles := range map[string][]authz.Role{"an HR officer": {authz.HROfficer}, "an HR administrator": {authz.HRAdmin}, "the Owner": {authz.Owner}} {
		testRoles = roles
		for _, o := range teamOps {
			if code := call(o, "nemura_session=good-token"); code != 200 && code != 201 && code != 204 {
				t.Errorf("%s: %s %s: %d", name, o.method, o.path, code)
			}
		}
	}
	testRoles, featuresOff = []authz.Role{authz.HRAdmin}, true
	for _, o := range append(teamOps, ownOps...) {
		if code := call(o, "nemura_session=good-token"); code != 409 {
			t.Errorf("Onboarding off: %s %s: %d", o.method, o.path, code)
		}
	}
}
