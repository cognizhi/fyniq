//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The incident register (SEC-32; p1-security-hardening design D10): the DPO opens an incident with its severity; it is
// contained, assessed with the breach register's breach, notified, recovered and reviewed, which closes it, each step
// audited; notifying before assessing, recovering before containing, a step twice and a step after the review are
// refused, as are a breach on another step and a breach the register does not hold; the Auditor reads it; a
// Bookkeeper sees none of it.
// req: SEC-32
// req: access/roles-and-segregation Permission matrix: Compliance Center
// asvs: V8.2.1
func TestTheIncidentRegister(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	r := e.call(http.MethodPost, "/v1/compliance/incidents", dpo, map[string]any{"title": "An admin signed in from a new device",
		"summary": "The security alert new_device fired for an Owner.", "severity": "high", "detected_at": "2026-10-15T08:30:00Z"}, "Idempotency-Key", "i-1")
	if r.Status != 201 || r.str("status") != "open" || r.str("severity") != "high" || r.str("detected_at") != "2026-10-15T08:30:00Z" ||
		r.Header.Get("ETag") != `"s0"` {
		t.Fatalf("an incident: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/incidents", dpo, map[string]any{"title": "x", "summary": "y", "severity": "high",
		"detected_at": "2026-10-16T00:00:00Z"}, "Idempotency-Key", "i-2"), "openIncident", 422, CodeInvalid) // later than now
	path := "/v1/compliance/incidents/" + r.str("id") + "/steps"
	step := func(tag, key string, body map[string]any) response {
		return e.call(http.MethodPost, path, dpo, body, "If-Match", tag, "Idempotency-Key", key)
	}
	e.problemIs(step(`"s0"`, "x-1", map[string]any{"step": "notified", "note": "Told the founders."}), "stepIncident", 409, "STORAGE-INCIDENT-01")
	e.problemIs(step(`"s0"`, "x-2", map[string]any{"step": "recovered", "note": "Restored."}), "stepIncident", 409, "STORAGE-INCIDENT-01")
	if s := step(`"s0"`, "s-1", map[string]any{"step": "contained", "note": "Signed out the device; the Owner confirmed by phone."}); s.Status != 201 ||
		s.str("status") != "contained" {
		t.Fatalf("containing: %d %s", s.Status, s.Raw)
	}
	e.problemIs(step(`"s1"`, "x-3", map[string]any{"step": "contained", "note": "Again."}), "stepIncident", 409, "STORAGE-INCIDENT-01")
	e.problemIs(step(`"s0"`, "x-4", map[string]any{"step": "assessed", "note": "Stale."}), "stepIncident", 412, CodeStale)

	// The assessment names the breach recorded for it; only the assessment may, and only a breach of the register.
	b := e.call(http.MethodPost, "/v1/compliance/breaches", dpo, map[string]any{"title": "Payslips read", "description": "Seen in the session.",
		"individuals": 3, "classes": []string{"names", "pay"}}, "Idempotency-Key", "b-1")
	if b.Status != 201 {
		t.Fatalf("the breach: %d %s", b.Status, b.Raw)
	}
	e.problemIs(step(`"s1"`, "x-5", map[string]any{"step": "assessed", "note": "y", "breach_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"}), "stepIncident", 422, CodeInvalid)
	a := step(`"s1"`, "s-2", map[string]any{"step": "assessed", "note": "Three payslips were read: personal data.", "breach_id": b.str("id")})
	if a.Status != 201 || a.str("breach_id") != b.str("id") {
		t.Fatalf("assessing: %d %s", a.Status, a.Raw)
	}
	e.problemIs(step(`"s2"`, "x-6", map[string]any{"step": "notified", "note": "y", "breach_id": b.str("id")}), "stepIncident", 422, CodeInvalid)
	e.problemIs(step(`"s2"`, "x-7", map[string]any{"step": "reviewed", "note": "Too soon."}), "stepIncident", 409, "STORAGE-INCIDENT-01")
	for i, s := range []map[string]any{
		{"step": "notified", "note": "The PDPC and the three people were told."},
		{"step": "recovered", "note": "New passkeys for every Owner."},
		{"step": "reviewed", "note": "A new-device alert now asks for a step-up."},
	} {
		if o := step(`"s`+string(rune('2'+i))+`"`, "s-"+string(rune('3'+i)), s); o.Status != 201 || o.str("status") != s["step"] {
			t.Fatalf("%s: %d %s", s["step"], o.Status, o.Raw)
		}
	}
	e.problemIs(step(`"s5"`, "x-8", map[string]any{"step": "notified", "note": "After the review."}), "stepIncident", 409, "STORAGE-INCIDENT-01")

	list := e.call(http.MethodGet, "/v1/compliance/incidents", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 1 || items[0].(map[string]any)["status"] != "reviewed" || len(items[0].(map[string]any)["steps"].([]any)) != 5 {
		t.Fatalf("the register: %d %s", list.Status, list.Raw)
	}
	var events int
	for _, l := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(l, `"action":"incident.`) {
			events++
		}
	}
	if events != 6 {
		t.Errorf("the incident's audit events: %d, want 6", events)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/incidents", e.as(authz.Bookkeeper), nil), "listIncidents", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/incidents", e.as(authz.Owner), map[string]any{"title": "x", "summary": "y", "severity": "low"},
		"Idempotency-Key", "i-3"), "openIncident", 403, CodeForbidden)
}

// matrixIncidents are the authorisation matrix's rows for the incident register (SEC-32, Appendix D "Compliance Center").
var matrixIncidents = map[string]func(e *env, tok string) (response, func() bool){
	"listIncidents": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/incidents", tok, nil), nil
	},
	"openIncident": func(e *env, tok string) (response, func() bool) {
		was := matrixIncidentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/incidents", tok, map[string]any{"title": "x", "summary": "y", "severity": "low"}, "Prefer", "return=preview")
		return r, func() bool { return matrixIncidentState(e) == was }
	},
	"stepIncident": func(e *env, tok string) (response, func() bool) {
		id := matrixIncident(e)
		was := matrixIncidentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/incidents/"+id+"/steps", tok, map[string]any{"step": "contained", "note": "y"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixIncidentState(e) == was }
	},
}

var matrixIncidentOf = map[*env]string{}

// matrixIncident is an incident opened once, so the matrix steps one that exists.
func matrixIncident(e *env) string {
	e.t.Helper()
	if id, ok := matrixIncidentOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/compliance/incidents", e.as(authz.DPO), map[string]any{"title": "Matrix", "summary": "y", "severity": "low"},
		"Idempotency-Key", "matrix-incident")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's incident: %d %s", r.Status, r.Raw)
	}
	matrixIncidentOf[e] = r.str("id")
	return r.str("id")
}

func matrixIncidentState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/incidents", e.as(authz.DPO), nil).Raw
}
