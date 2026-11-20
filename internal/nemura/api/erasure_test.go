package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Asking for an erasure and approving one are the Owner's and the HR administrator's alone, each after a passkey confirmed in the last five minutes; no other role reads or changes one.
// part of NM-SEC-08
// asvs: V8.2.1, V8.3.1, V4.2.1
func TestOnlyAnOwnerOrAnHRAdministratorAsksForOrApprovesAnErasureAfterAFreshPasskey(t *testing.T) {
	do := func(roles []authz.Role, stepUp bool, method, path, payload string) int {
		testRoles, testStepUp = roles, stepUp
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	ask := `{"person_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61","reason":"the periods have run"}`
	decide := `{"approve":true}`
	order := "/v1/erasures/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71/decision"
	for _, roles := range [][]authz.Role{{authz.Employee}, {authz.LineManager}, {authz.HROfficer}, {authz.PayrollAdmin}, {authz.Administrator}, {authz.Recruiter}} {
		for _, c := range []struct{ m, p, b string }{{"GET", "/v1/erasures", ""}, {"POST", "/v1/erasures", ask}, {"POST", order, decide}} {
			if got := do(roles, true, c.m, c.p, c.b); got != 403 {
				t.Errorf("%v %s %s: %d, want 403", roles, c.m, c.p, got)
			}
		}
	}
	for _, roles := range [][]authz.Role{{authz.HRAdmin}, {authz.Owner}} {
		if got := do(roles, true, "GET", "/v1/erasures", ""); got != 200 {
			t.Errorf("%v listing: %d", roles, got)
		}
		if got := do(roles, true, "POST", "/v1/erasures", ask); got != 201 {
			t.Errorf("%v asking: %d", roles, got)
		}
		if got := do(roles, true, "POST", order, decide); got != 200 {
			t.Errorf("%v approving: %d", roles, got)
		}
		for _, c := range []struct{ p, b string }{{"/v1/erasures", ask}, {order, decide}} {
			if got := do(roles, false, "POST", c.p, c.b); got != 403 {
				t.Errorf("%v %s without a fresh passkey: %d", roles, c.p, got)
			}
		}
	}
}
