//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The heartbeat drill register (NFR-OBS-02, CTL-RES-05 Q; p1-operations-dr design D6): the DPO records a drill with how
// long the tick was paused and when the first alert and the escalation came, audited; it passes only when both came in
// time, and an escalation left out means none came; a day after today, an unknown environment and a negative minute
// are refused; the Auditor reads it; a Bookkeeper sees none of it and the Owner records none.
// req: NFR-OBS-02
// req: CTL-RES-05
// req: access/roles-and-segregation Permission matrix: Compliance Center
// asvs: V8.2.1
func TestTheHeartbeatDrillRegister(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	drill := func(over map[string]any) map[string]any {
		b := map[string]any{"held_on": "2026-10-10", "environment": "staging", "paused_minutes": 75, "first_alert_minutes": 28,
			"escalation_minutes": 58, "findings": "Both alerts came on time."}
		for k, v := range over {
			if v == nil {
				delete(b, k)
				continue
			}
			b[k] = v
		}
		return b
	}
	r := e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", dpo, drill(nil), "Idempotency-Key", "h-1")
	if r.Status != 201 || r.Body["passed"] != true || r.Body["escalation_minutes"] != float64(58) {
		t.Fatalf("a drill that passed: %d %s", r.Status, r.Raw)
	}
	f := e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", dpo, drill(map[string]any{"held_on": "2026-10-11", "escalation_minutes": nil}), "Idempotency-Key", "h-2")
	if f.Status != 201 || f.Body["passed"] != false || f.Body["escalation_minutes"] != nil {
		t.Fatalf("a drill with no escalation: %d %s", f.Status, f.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", dpo, drill(map[string]any{"held_on": "2026-10-16"}), "Idempotency-Key", "x-1"),
		"recordHeartbeatDrill", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", dpo, drill(map[string]any{"environment": "laptop"}), "Idempotency-Key", "x-2"),
		"recordHeartbeatDrill", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", dpo, drill(map[string]any{"first_alert_minutes": -1}), "Idempotency-Key", "x-3"),
		"recordHeartbeatDrill", 422, CodeInvalid)

	list := e.call(http.MethodGet, "/v1/compliance/heartbeat-drills", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 2 || items[0].(map[string]any)["passed"] != true {
		t.Fatalf("the register: %d %s", list.Status, list.Raw)
	}
	var events int
	for _, l := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(l, `"action":"tick_drill.recorded"`) {
			events++
		}
	}
	if events != 2 {
		t.Errorf("the drills' audit events: %d, want 2", events)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/heartbeat-drills", e.as(authz.Bookkeeper), nil), "listHeartbeatDrills", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", e.as(authz.Owner), drill(nil), "Idempotency-Key", "x-4"),
		"recordHeartbeatDrill", 403, CodeForbidden)
}

// matrixHeartbeatDrills are the authorisation matrix's rows for the heartbeat drill register (NFR-OBS-02, Appendix D
// "Compliance Center").
var matrixHeartbeatDrills = map[string]func(e *env, tok string) (response, func() bool){
	"listHeartbeatDrills": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/heartbeat-drills", tok, nil), nil
	},
	"recordHeartbeatDrill": func(e *env, tok string) (response, func() bool) {
		was := e.call(http.MethodGet, "/v1/compliance/heartbeat-drills", e.as(authz.DPO), nil).Raw
		r := e.call(http.MethodPost, "/v1/compliance/heartbeat-drills", tok, map[string]any{"held_on": "2026-10-10", "environment": "staging",
			"paused_minutes": 75, "first_alert_minutes": 28, "escalation_minutes": 58, "findings": "y"}, "Prefer", "return=preview")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/compliance/heartbeat-drills", e.as(authz.DPO), nil).Raw == was
		}
	},
}
