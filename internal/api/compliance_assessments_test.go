//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The register of security assessments (SEC-25, CTL-CHG-12; rev 0.617; p2-security-certification D4): the Owner records a penetration test
// with its findings, each due by its severity from the day found; a finding is fixed, then retested, or accepted with a reason, in that order
// only; a finding added later is due from its own day; a kind, a day after today, a hash that is not a SHA-256, a repeated reference, a step out of
// order and an acceptance with no reason are refused; each is in the audit trail; the Auditor, Controller, DPO and Administrator read it and
// record nothing; the Bookkeeper reads nothing.
// req: SEC-25
// req: CTL-CHG-12
// req: access/roles-and-segregation Permission matrix: Security assessments
// asvs: V8.2.1
func TestTheRegisterOfSecurityAssessments(t *testing.T) {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	register := func() response {
		return e.call(http.MethodGet, "/v1/compliance/security-assessments", e.as(authz.Auditor), nil)
	}
	if r := register(); r.Status != 200 || len(r.Body["items"].([]any)) != 0 || r.Body["overdue_findings"] != float64(0) {
		t.Fatalf("the register at first: %d %s", r.Status, r.Raw)
	}
	body := map[string]any{"kind": "penetration_test", "tester": "Example Security Pte Ltd", "independent": true, "scope": "The web app, the API and the MCP server on staging",
		"started_on": "2026-09-21", "ended_on": "2026-10-05", "summary": "No critical finding; one high",
		"report_sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "report_location": "The security folder of the shared drive",
		"findings": []any{map[string]any{"ref": "PT-1", "title": "Reflected script in the search page", "severity": "high"},
			map[string]any{"ref": "PT-2", "title": "Verbose error page", "severity": "low", "found_on": "2026-09-28"},
			map[string]any{"ref": "PT-3", "title": "Missing header", "severity": "info"}}}
	c := e.call(http.MethodPost, "/v1/compliance/security-assessments", owner, body, "Idempotency-Key", "a-1")
	if c.Status != 201 || len(c.Body["findings"].([]any)) != 3 {
		t.Fatalf("recording: %d %s", c.Status, c.Raw)
	}
	findings := c.Body["findings"].([]any)
	high, low, info := findings[0].(map[string]any), findings[1].(map[string]any), findings[2].(map[string]any)
	if high["due_on"] != "2026-11-04" || low["due_on"] != "2027-03-27" || info["due_on"] != nil || high["state"] != "open" {
		t.Errorf("the findings' days: %v %v %v", high, low, info)
	}
	if r := register(); r.str("last_independent_on") != "2026-10-05" || r.str("next_due_on") != "2027-10-05" {
		t.Errorf("the register's days: %s", r.Raw)
	}
	with := func(k string, v any) map[string]any {
		b := map[string]any{}
		for key, x := range body {
			b[key] = x
		}
		b[k] = v
		return b
	}
	for _, c := range []struct {
		what string
		body map[string]any
	}{
		{"an unknown kind", with("kind", "audit")},
		{"a day after today", with("ended_on", "2099-01-01")},
		{"a last day before the first", with("started_on", "2026-11-05")},
		{"a hash that is not a SHA-256", with("report_sha256", "abc")},
		{"a repeated reference", with("findings", []any{map[string]any{"ref": "X", "title": "a", "severity": "low"}, map[string]any{"ref": "X", "title": "b", "severity": "low"}})},
		{"an unknown severity", with("findings", []any{map[string]any{"ref": "X", "title": "a", "severity": "grave"}})},
	} {
		if r := e.call(http.MethodPost, "/v1/compliance/security-assessments", owner, c.body, "Idempotency-Key", "a-x-"+strings.ReplaceAll(c.what, " ", "-")); r.Status != 422 {
			t.Errorf("%s: %d %s", c.what, r.Status, r.Raw)
		}
	}
	step := func(id, name, on, reason string, key string) response {
		b := map[string]any{"step": name, "on": on}
		if reason != "" {
			b["reason"] = reason
		}
		return e.call(http.MethodPost, "/v1/compliance/security-findings/"+id+"/steps", owner, b, "Idempotency-Key", key)
	}
	hid := high["finding_id"].(string)
	for _, c := range []struct {
		what, name, on, reason string
	}{
		{"a retest before the fix", "retested", "2026-10-10", ""},
		{"a fix before the finding was found", "fixed", "2026-09-01", ""},
		{"an acceptance with no reason", "accepted", "2026-10-10", ""},
		{"a fix with a reason", "fixed", "2026-10-10", "why"},
		{"a day after today", "fixed", "2099-01-01", ""},
	} {
		if r := step(hid, c.name, c.on, c.reason, "s-x-"+strings.ReplaceAll(c.what, " ", "-")); r.Status != 422 && r.Status != 409 {
			t.Errorf("%s: %d %s", c.what, r.Status, r.Raw)
		}
	}
	if r := step(hid, "fixed", "2026-10-08", "", "s-1"); r.Status != 201 || r.str("step") != "fixed" {
		t.Fatalf("the fix: %d %s", r.Status, r.Raw)
	}
	if r := step(hid, "fixed", "2026-10-09", "", "s-2"); r.Status != 409 || r.str("code") != "STORAGE-ASSESSMENT-01" {
		t.Errorf("a second fix: %d %s", r.Status, r.Raw)
	}
	if r := step(hid, "retested", "2026-10-10", "", "s-3"); r.Status != 201 {
		t.Fatalf("the retest: %d %s", r.Status, r.Raw)
	}
	if r := step(hid, "accepted", "2026-10-11", "The page is retired", "s-4"); r.Status != 409 {
		t.Errorf("an acceptance after the retest: %d %s", r.Status, r.Raw)
	}
	lid := low["finding_id"].(string)
	if r := step(lid, "accepted", "2026-10-10", "The page is retired next month", "s-5"); r.Status != 201 || r.str("reason") == "" {
		t.Fatalf("an acceptance: %d %s", r.Status, r.Raw)
	}
	if r := step("01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e99", "fixed", "2026-10-10", "", "s-6"); r.Status != 404 {
		t.Errorf("a finding that does not exist: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/compliance/security-assessments/"+c.str("assessment_id")+"/findings", owner,
		map[string]any{"ref": "RT-1", "title": "Found at the retest", "severity": "critical", "found_on": "2026-10-01"}, "Idempotency-Key", "f-1"); r.Status != 201 || r.str("due_on") != "2026-10-08" || r.Body["overdue"] != true {
		t.Errorf("a finding added later, a critical one found on 1 Oct and due on the 8th, overdue by the env's date: %d %s", r.Status, r.Raw)
	}
	if r := register(); r.Body["overdue_findings"] != float64(1) {
		t.Errorf("one Critical finding is overdue: %s", r.Raw)
	}
	trail := auditTrail(t, e)
	for _, want := range []string{"security_assessment.recorded", "security_finding.recorded", "security_finding.stepped"} {
		if !strings.Contains(trail, want) {
			t.Errorf("%s is not in the audit trail", want)
		}
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/security-assessments", e.as(authz.Bookkeeper), nil), "listSecurityAssessments", 403, CodeForbidden)
	for _, role := range []authz.Role{authz.Administrator, authz.Controller, authz.Auditor, authz.DPO} {
		if r := e.call(http.MethodGet, "/v1/compliance/security-assessments", e.as(role), nil); r.Status != 200 {
			t.Errorf("%s reads the register: %d", role, r.Status)
		}
		e.problemIs(e.call(http.MethodPost, "/v1/compliance/security-assessments", e.as(role), body, "Idempotency-Key", "a-"+string(role)), "createSecurityAssessment", 403, CodeForbidden)
	}
}

// matrixSecurityAssessments are the authorisation matrix's rows for the register (SEC-25, Appendix D "Security assessments").
var matrixSecurityAssessments = map[string]func(e *env, tok string) (response, func() bool){
	"listSecurityAssessments": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/security-assessments", tok, nil), nil
	},
	"createSecurityAssessment": func(e *env, tok string) (response, func() bool) {
		was := matrixAssessmentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/security-assessments", tok, matrixAssessmentBody, "Prefer", "return=preview")
		return r, func() bool { return matrixAssessmentState(e) == was }
	},
	"addSecurityFinding": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixAssessment(e)
		was := matrixAssessmentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/security-assessments/"+id+"/findings", tok, map[string]any{"ref": "M-2", "title": "m", "severity": "low"}, "Prefer", "return=preview")
		return r, func() bool { return matrixAssessmentState(e) == was }
	},
	"stepSecurityFinding": func(e *env, tok string) (response, func() bool) {
		_, finding := matrixAssessment(e)
		was := matrixAssessmentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/security-findings/"+finding+"/steps", tok, map[string]any{"step": "fixed", "on": "2026-10-10"}, "Prefer", "return=preview")
		return r, func() bool { return matrixAssessmentState(e) == was }
	},
}

var (
	matrixAssessmentBody = map[string]any{"kind": "penetration_test", "tester": "t", "independent": true, "scope": "s", "started_on": "2026-09-21", "ended_on": "2026-10-05",
		"summary": "m", "findings": []any{map[string]any{"ref": "M-1", "title": "m", "severity": "low"}}}
	matrixAssessmentOf = map[*env][2]string{}
)

// matrixAssessment is an assessment with a finding, recorded once, so the matrix adds to and steps ones that exist.
func matrixAssessment(e *env) (assessment, finding string) {
	e.t.Helper()
	if v, ok := matrixAssessmentOf[e]; ok {
		return v[0], v[1]
	}
	r := e.call(http.MethodPost, "/v1/compliance/security-assessments", e.as(authz.Owner), matrixAssessmentBody, "Idempotency-Key", "matrix-assessment")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's assessment: %d %s", r.Status, r.Raw)
	}
	v := [2]string{r.str("assessment_id"), r.Body["findings"].([]any)[0].(map[string]any)["finding_id"].(string)}
	matrixAssessmentOf[e] = v
	return v[0], v[1]
}

func matrixAssessmentState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/security-assessments", e.as(authz.Owner), nil).Raw
}
