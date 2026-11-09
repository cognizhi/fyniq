//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The breach register (CMP-PD-05, CTL-DP-06; rev 0.244; p1-pdpa-retention D6): a breach of names with pay is prescribed,
// its assessment due 30 days on; it is assessed, decided notifiable, which starts the PDPC's 3 calendar days and a
// critical notice, reported with its reference and closed; deciding it not notifiable, reporting before the decision and
// closing before the report are refused; a tabletop exercise is recorded; the reviews list says what waits; a Controller
// sees none of it.
// req: CMP-PD-05
// req: CTL-DP-06
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestTheBreachRegister(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	r := e.call(http.MethodPost, "/v1/compliance/breaches", dpo, map[string]any{"title": "A payroll file emailed to the wrong person",
		"description": "The October payroll file went to a supplier's address.", "individuals": 12, "classes": []string{"pay", "names"}}, "Idempotency-Key", "b-1")
	if r.Status != 201 || r.Body["prescribed"] != true || r.str("status") != "recorded" || r.Header.Get("ETag") != `"s0"` {
		t.Fatalf("a breach: %d %s", r.Status, r.Raw)
	}
	path := "/v1/compliance/breaches/" + r.str("id") + "/steps"
	step := func(tag, key string, body map[string]any) response {
		return e.call(http.MethodPost, path, dpo, body, "If-Match", tag, "Idempotency-Key", key)
	}
	e.problemIs(step(`"s0"`, "x-1", map[string]any{"step": "decided", "notifiable": true}), "stepBreach", 409, "STORAGE-BREACH-01")
	if s := step(`"s0"`, "s-1", map[string]any{"step": "assessed", "note": "Pay and names: significant harm."}); s.Status != 201 {
		t.Fatalf("assessing: %d %s", s.Status, s.Raw)
	}
	e.problemIs(step(`"s1"`, "x-2", map[string]any{"step": "decided", "notifiable": false}), "stepBreach", 409, "STORAGE-BREACH-01")
	e.problemIs(step(`"s1"`, "x-3", map[string]any{"step": "pdpc_notified", "reference": "DBN-1"}), "stepBreach", 409, "STORAGE-BREACH-01")
	d := step(`"s1"`, "s-2", map[string]any{"step": "decided", "notifiable": true})
	if d.Status != 201 || d.Body["notifiable"] != true || d.str("pdpc_due_on") == "" {
		t.Fatalf("deciding: %d %s", d.Status, d.Raw)
	}
	e.problemIs(step(`"s2"`, "x-4", map[string]any{"step": "closed"}), "stepBreach", 409, "STORAGE-BREACH-01")
	e.problemIs(step(`"s2"`, "x-5", map[string]any{"step": "pdpc_notified"}), "stepBreach", 422, CodeInvalid)
	if s := step(`"s2"`, "s-3", map[string]any{"step": "pdpc_notified", "reference": "DBN-2026-0123"}); s.Status != 201 || s.Body["pdpc_overdue"] != false {
		t.Fatalf("reporting: %d %s", s.Status, s.Raw)
	}
	if s := step(`"s3"`, "s-4", map[string]any{"step": "closed", "note": "Recalled the file; training given."}); s.Status != 201 || s.str("status") != "closed" {
		t.Fatalf("closing: %d %s", s.Status, s.Raw)
	}
	var notices int
	for _, l := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(l, "breach.") {
			notices++
		}
	}
	if notices < 5 {
		t.Errorf("the breach's audit events: %d", notices)
	}

	// A breach of contact details alone is not prescribed, and may be decided not notifiable.
	c := e.call(http.MethodPost, "/v1/compliance/breaches", dpo, map[string]any{"title": "A mailing list exposed", "description": "Twenty addresses visible.",
		"individuals": 20, "classes": []string{"contact_details"}}, "Idempotency-Key", "b-2")
	if c.Status != 201 || c.Body["prescribed"] != false {
		t.Fatalf("a breach of contact details: %d %s", c.Status, c.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/breaches", dpo, map[string]any{"title": "x", "description": "y", "individuals": 1,
		"classes": []string{"secrets"}}, "Idempotency-Key", "b-3"), "recordBreach", 422, CodeInvalid)

	// A tabletop exercise.
	if x := e.call(http.MethodPost, "/v1/compliance/breach-exercises", dpo, map[string]any{"held_on": "2026-10-01", "scenario": "A stolen laptop.",
		"findings": "Name who calls the PDPC."}, "Idempotency-Key", "ex-1"); x.Status != 201 {
		t.Fatalf("an exercise: %d %s", x.Status, x.Raw)
	}
	reviews := e.call(http.MethodGet, "/v1/compliance/reviews", e.as(authz.Auditor), nil).Body["items"].([]any)
	lines := map[string]map[string]any{}
	for _, x := range reviews {
		lines[x.(map[string]any)["kind"].(string)] = x.(map[string]any)
	}
	if lines["breach"]["waiting"] != float64(1) || lines["breach-tabletop"]["status"] != "signed" {
		t.Errorf("the reviews' breach lines: %v %v", lines["breach"], lines["breach-tabletop"])
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/breaches", e.as(authz.Controller), nil), "listBreaches", 403, CodeForbidden)
}

// matrixBreaches are the authorisation matrix's rows for the breach register (CMP-PD-05, Appendix D "Data protection (PDPA)").
var matrixBreaches = map[string]func(e *env, tok string) (response, func() bool){
	"listBreaches": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/breaches", tok, nil), nil
	},
	"listBreachExercises": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/breach-exercises", tok, nil), nil
	},
	"recordBreach": func(e *env, tok string) (response, func() bool) {
		was := matrixBreachState(e)
		r := e.call(http.MethodPost, "/v1/compliance/breaches", tok, map[string]any{"title": "x", "description": "y", "individuals": 1, "classes": []string{"other"}},
			"Prefer", "return=preview")
		return r, func() bool { return matrixBreachState(e) == was }
	},
	"stepBreach": func(e *env, tok string) (response, func() bool) {
		id := matrixBreach(e)
		was := matrixBreachState(e)
		r := e.call(http.MethodPost, "/v1/compliance/breaches/"+id+"/steps", tok, map[string]any{"step": "assessed"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixBreachState(e) == was }
	},
	"recordBreachExercise": func(e *env, tok string) (response, func() bool) {
		was := matrixBreachState(e)
		r := e.call(http.MethodPost, "/v1/compliance/breach-exercises", tok, map[string]any{"held_on": "2026-10-01", "scenario": "x", "findings": "y"},
			"Prefer", "return=preview")
		return r, func() bool { return matrixBreachState(e) == was }
	},
}

var matrixBreachOf = map[*env]string{}

// matrixBreach is a breach recorded once, so the matrix steps one that exists.
func matrixBreach(e *env) string {
	e.t.Helper()
	if id, ok := matrixBreachOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/compliance/breaches", e.as(authz.DPO), map[string]any{"title": "Matrix", "description": "y", "individuals": 1,
		"classes": []string{"other"}}, "Idempotency-Key", "matrix-breach")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's breach: %d %s", r.Status, r.Raw)
	}
	matrixBreachOf[e] = r.str("id")
	return r.str("id")
}

func matrixBreachState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/breaches", e.as(authz.DPO), nil).Raw +
		e.call(http.MethodGet, "/v1/compliance/breach-exercises", e.as(authz.DPO), nil).Raw
}
