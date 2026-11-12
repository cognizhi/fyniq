//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

var riskBody = map[string]any{"title": "A supplier's bank details are changed by a fraudster", "owner": "controller", "likelihood": "medium",
	"impact": "high", "mitigation": "A second person approves every bank change; the supplier is called back on a known number."}

// The risk register (CMP-06): the Compliance officer adds a risk, its first review the end of the next quarter, changes it
// as its next version with If-Match, and reviews it, which sets its next review date; a stale version is refused; each is
// audited; the Controller reads the register but may not write it, and a Bookkeeper may not read it.
// req: CMP-06
// req: AUD-01
func TestTheRiskRegister(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	r := e.call(http.MethodPost, "/v1/compliance/risks", dpo, riskBody, "Idempotency-Key", "risk-1")
	if r.Status != 201 || r.str("ref") != "R1" || r.Body["version"].(float64) != 1 || r.str("state") != "open" || r.Body["review_months"].(float64) != 3 ||
		r.Body["review_overdue"] != false || !strings.HasPrefix(r.str("saved_by"), "user:") || r.Header.Get("ETag") != `"v1"` {
		t.Fatalf("a new risk: %d %s", r.Status, r.Raw)
	}
	id, first := r.str("id"), r.str("next_review_on")
	if again := e.call(http.MethodPost, "/v1/compliance/risks", dpo, riskBody, "Idempotency-Key", "risk-1"); again.str("id") != id {
		t.Errorf("the same Idempotency-Key made another risk: %s", again.Raw)
	}
	changed := map[string]any{}
	for k, v := range riskBody {
		changed[k] = v
	}
	changed["likelihood"], changed["review_months"] = "low", 6
	r = e.call(http.MethodPut, "/v1/compliance/risks/"+id, dpo, changed, "If-Match", `"v1"`, "Idempotency-Key", "risk-1-v2")
	if r.Status != 200 || r.Body["version"].(float64) != 2 || r.str("likelihood") != "low" || r.str("next_review_on") != first || r.Header.Get("ETag") != `"v2"` {
		t.Fatalf("a changed risk: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/compliance/risks/"+id, dpo, changed, "If-Match", `"v1"`, "Idempotency-Key", "risk-1-stale"), "reviseRisk", 412, CodeStale)
	r = e.call(http.MethodPost, "/v1/compliance/risks/"+id+"/reviews", dpo, map[string]any{"note": "Mitigations hold."}, "If-Match", `"v2"`, "Idempotency-Key", "risk-1-review")
	last, _ := r.Body["last_review"].(map[string]any)
	if r.Status != 201 || last == nil || last["version"].(float64) != 2 || last["note"] != "Mitigations hold." || r.str("next_review_on") == first ||
		last["next_review_on"] != r.str("next_review_on") {
		t.Fatalf("a review: %d %s", r.Status, r.Raw)
	}
	list := e.call(http.MethodGet, "/v1/compliance/risks", e.as(authz.Controller), nil)
	if items, _ := list.Body["items"].([]any); list.Status != 200 || len(items) != 1 || items[0].(map[string]any)["version"].(float64) != 2 {
		t.Fatalf("the register: %d %s", list.Status, list.Raw)
	}
	bad := map[string]any{"title": " ", "owner": "controller", "likelihood": "medium", "impact": "high", "mitigation": "x", "next_review_on": "2000-01-01"}
	if r := e.call(http.MethodPost, "/v1/compliance/risks", dpo, bad, "Idempotency-Key", "risk-bad"); r.Status != 422 ||
		!strings.Contains(r.Raw, "body.next_review_on") || !strings.Contains(r.Raw, "body.title") {
		t.Errorf("a risk wrong in two fields: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/risks", e.as(authz.Controller), riskBody, "Idempotency-Key", "risk-ctl"), "createRisk", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/risks", e.as(authz.Bookkeeper), nil), "listRisks", 403, CodeForbidden)
}

// matrixRiskOf is the matrix's risk, made once per environment by the Compliance officer.
var matrixRiskOf = map[*env]string{}

func matrixRisk(e *env) string {
	e.t.Helper()
	if id, ok := matrixRiskOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/compliance/risks", e.as(authz.DPO), riskBody, "Idempotency-Key", "matrix-risk")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's risk: %d %s", r.Status, r.Raw)
	}
	matrixRiskOf[e] = r.str("id")
	return r.str("id")
}

func matrixRisksState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/risks", e.as(authz.DPO), nil).Raw
}

// matrixRisks are the authorisation matrix's rows for the risk register (CMP-06, Appendix D "Compliance Center").
var matrixRisks = map[string]func(e *env, tok string) (response, func() bool){
	"listRisks": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/risks", tok, nil), nil
	},
	"createRisk": func(e *env, tok string) (response, func() bool) {
		matrixRisk(e)
		was := matrixRisksState(e)
		r := e.call(http.MethodPost, "/v1/compliance/risks", tok, riskBody, "Prefer", "return=preview")
		return r, func() bool { return matrixRisksState(e) == was }
	},
	"reviseRisk": func(e *env, tok string) (response, func() bool) {
		id := matrixRisk(e)
		was := matrixRisksState(e)
		r := e.call(http.MethodPut, "/v1/compliance/risks/"+id, tok, riskBody, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixRisksState(e) == was }
	},
	"reviewRisk": func(e *env, tok string) (response, func() bool) {
		id := matrixRisk(e)
		was := matrixRisksState(e)
		r := e.call(http.MethodPost, "/v1/compliance/risks/"+id+"/reviews", tok, map[string]any{}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixRisksState(e) == was }
	},
}
