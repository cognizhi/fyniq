package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Any signed-in member files and reads their own grievances; only an HR administrator or the Owner reads, records, decides, passes on or takes one, and nobody without a session does anything; a person who has left files
// nothing (NM-SEC-06).
// req: NM-SEC-06
// asvs: V8.2.1, V8.2.2, V8.3.1
func TestGrievanceOperationsAreForMembersAndHandlersOnly(t *testing.T) {
	const g = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"
	type op struct{ method, path, body string }
	filing := []op{{"POST", "/v1/me/grievances", `{"summary":"On the twelfth, in the meeting room, a remark was made that I found demeaning."}`}, {"GET", "/v1/me/grievances", ""}}
	handling := []op{{"GET", "/v1/grievances", ""}, {"GET", "/v1/grievances/" + g, ""}, {"POST", "/v1/grievances/" + g + "/entries", `{"kind":"note","note":"x"}`},
		{"POST", "/v1/grievances/" + g + "/outcome", `{"finding":"substantiated","outcome":"The inquiry found that the remark was made and it was demeaning."}`},
		{"POST", "/v1/grievances/" + g + "/handover", `{"reason":"x"}`}, {"POST", "/v1/grievances/" + g + "/taking", ``}}
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
	defer func() { testRoles, testLeaver = []authz.Role{authz.HRAdmin}, false }()
	for _, o := range append(filing, handling...) {
		if code := call(o, ""); code != 401 {
			t.Errorf("%s %s with no session: %d", o.method, o.path, code)
		}
	}
	for name, roles := range map[string][]authz.Role{"an employee": {authz.Employee}, "a line manager": {authz.LineManager}, "an HR officer": {authz.HROfficer}, "a payroll administrator": {authz.PayrollAdmin}, "an administrator": {authz.Administrator}} {
		testRoles = roles
		for _, o := range filing {
			if code := call(o, "nemura_session=good-token"); code != 200 && code != 201 {
				t.Errorf("%s: %s %s: %d", name, o.method, o.path, code)
			}
		}
		for _, o := range handling {
			if code := call(o, "nemura_session=good-token"); code != 403 {
				t.Errorf("%s: %s %s is not for them: %d", name, o.method, o.path, code)
			}
		}
	}
	for name, roles := range map[string][]authz.Role{"an HR administrator": {authz.HRAdmin}, "the Owner": {authz.Owner}} {
		testRoles = roles
		for _, o := range handling {
			if code := call(o, "nemura_session=good-token"); code != 200 && code != 201 {
				t.Errorf("%s: %s %s: %d", name, o.method, o.path, code)
			}
		}
	}
	testRoles, testLeaver = []authz.Role{authz.Employee}, true
	for _, o := range filing {
		if code := call(o, "nemura_session=good-token"); code != 403 {
			t.Errorf("a person who has left: %s %s: %d", o.method, o.path, code)
		}
	}
}
