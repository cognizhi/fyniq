package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Goals need a session and Performance switched on; any signed-in member reaches the operations (the service decides who may read or change which goal and person); and everything is refused with the reason
// while Performance is off (NM-PRF-01, NM-FEAT-01).
// req: NM-PRF-01, NM-PRF-02, NM-FEAT-01
// asvs: V8.2.1, V8.2.2, V8.3.1
func TestGoalOperationsNeedASessionAndPerformanceOn(t *testing.T) {
	const id = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"
	type op struct{ method, path, body string }
	ops := []op{{"GET", "/v1/me/goals", ""}, {"GET", "/v1/people/" + id + "/goals", ""}, {"POST", "/v1/goals", `{"scope":"personal","title":"Bank sync","due_on":"2099-12-31"}`},
		{"POST", "/v1/goals/" + id + "/closing", `{"state":"done"}`}, {"POST", "/v1/measures/" + id + "/updates", `{"value":"2"}`}, {"POST", "/v1/people/" + id + "/kpis", `{"title":"Tickets","target":"10"}`}}
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
	for _, o := range ops {
		if code := call(o, ""); code != 401 {
			t.Errorf("%s %s with no session: %d", o.method, o.path, code)
		}
	}
	for _, role := range []authz.Role{authz.Employee, authz.LineManager, authz.HRAdmin} {
		testRoles = []authz.Role{role}
		for _, o := range ops {
			if code := call(o, "nemura_session=good-token"); code != 200 && code != 201 && code != 204 {
				t.Errorf("%s: %s %s: %d", role, o.method, o.path, code)
			}
		}
	}
	featuresOff = true
	for _, o := range ops {
		if code := call(o, "nemura_session=good-token"); code != 409 {
			t.Errorf("Performance off: %s %s: %d", o.method, o.path, code)
		}
	}
}
