//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The restore drill register (DR-06, CTL-RES-02 Q; p1-operations-dr design D2): the DPO records a full drill with its
// scenario, what it restored from, the data lost, the time taken and DR-06's checks, audited; it counts as recovered only
// when every check passed; a day after today, a scenario that is none and a drill that took no time are refused; the
// Auditor reads the register; a Bookkeeper sees none of it and the Owner records none.
// req: DR-06
// req: CTL-RES-02
// req: NFR-DR-03
// req: access/roles-and-segregation Permission matrix: Compliance Center
// asvs: V8.2.1
func TestTheRestoreDrillRegister(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	drill := func(over map[string]any) map[string]any {
		b := map[string]any{"held_on": "2026-10-10", "scenario": "project_loss", "restored_from": "The weekly export of 2026-10-04",
			"data_lost_minutes": 10080, "took_minutes": 185, "ledger": true, "audit_chain": true, "documents": true, "keys": true, "sign_in": true,
			"tick": true, "outbox": true, "findings": "Importing the export took 40 minutes; the runbook now starts it first."}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	r := e.call(http.MethodPost, "/v1/compliance/restore-drills", dpo, drill(nil), "Idempotency-Key", "d-1")
	if r.Status != 201 || r.Body["recovered"] != true || r.str("scenario") != "project_loss" || r.Body["took_minutes"] != float64(185) {
		t.Fatalf("a recovered drill: %d %s", r.Status, r.Raw)
	}
	f := e.call(http.MethodPost, "/v1/compliance/restore-drills", dpo, drill(map[string]any{"held_on": "2026-10-11", "scenario": "corruption", "outbox": false}),
		"Idempotency-Key", "d-2")
	if f.Status != 201 || f.Body["recovered"] != false {
		t.Fatalf("a drill whose outbox failed: %d %s", f.Status, f.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/restore-drills", dpo, drill(map[string]any{"held_on": "2026-10-16"}), "Idempotency-Key", "x-1"),
		"recordRestoreDrill", 422, CodeInvalid) // after today
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/restore-drills", dpo, drill(map[string]any{"scenario": "meteor"}), "Idempotency-Key", "x-2"),
		"recordRestoreDrill", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/restore-drills", dpo, drill(map[string]any{"took_minutes": 0}), "Idempotency-Key", "x-3"),
		"recordRestoreDrill", 422, CodeInvalid)

	list := e.call(http.MethodGet, "/v1/compliance/restore-drills", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 2 || items[0].(map[string]any)["recovered"] != true || items[1].(map[string]any)["outbox"] != false {
		t.Fatalf("the register: %d %s", list.Status, list.Raw)
	}
	var events int
	for _, l := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(l, `"action":"restore_drill.recorded"`) {
			events++
		}
	}
	if events != 2 {
		t.Errorf("the drills' audit events: %d, want 2", events)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/restore-drills", e.as(authz.Bookkeeper), nil), "listRestoreDrills", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/restore-drills", e.as(authz.Owner), drill(nil), "Idempotency-Key", "x-4"),
		"recordRestoreDrill", 403, CodeForbidden)
}

// matrixRestoreDrills are the authorisation matrix's rows for the restore drill register (DR-06, Appendix D "Compliance
// Center").
var matrixRestoreDrills = map[string]func(e *env, tok string) (response, func() bool){
	"listRestoreDrills": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/restore-drills", tok, nil), nil
	},
	"recordRestoreDrill": func(e *env, tok string) (response, func() bool) {
		was := e.call(http.MethodGet, "/v1/compliance/restore-drills", e.as(authz.DPO), nil).Raw
		r := e.call(http.MethodPost, "/v1/compliance/restore-drills", tok, map[string]any{"held_on": "2026-10-10", "scenario": "corruption",
			"restored_from": "x", "data_lost_minutes": 5, "took_minutes": 60, "ledger": true, "audit_chain": true, "documents": true, "keys": true,
			"sign_in": true, "tick": true, "outbox": true, "findings": "y"}, "Prefer", "return=preview")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/compliance/restore-drills", e.as(authz.DPO), nil).Raw == was
		}
	},
}
