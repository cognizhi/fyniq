//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authz"
)

// Readiness by framework (CMP-08, rev 0.617; p2-security-certification D5): each framework of the registry with its stage and how many mapped
// controls pass, fail, are overdue, are not assessed or do not apply, which add up to the controls mapped to it; the controls that are not passing
// are named, worst first; ISO/IEC 27001 Annex A's 93 numbers are covered or not, never both; it says it is not a certificate; whoever reads the
// Compliance Center reads it, and the Bookkeeper does not.
// req: CMP-08
// req: access/roles-and-segregation Permission matrix: Compliance Center
// asvs: V8.2.1
func TestReadinessByFramework(t *testing.T) {
	e := newEnv(t)
	r := e.call(http.MethodGet, "/v1/compliance/readiness", e.as(authz.Auditor), nil)
	if r.Status != 200 {
		t.Fatalf("readiness: %d %s", r.Status, r.Raw)
	}
	fs, _ := r.Body["frameworks"].([]any)
	if len(fs) != len(compliance.Frameworks) {
		t.Fatalf("%d frameworks, want %d", len(fs), len(compliance.Frameworks))
	}
	regs, err := compliance.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i, x := range fs {
		f := x.(map[string]any)
		want := 0
		for _, c := range regs {
			for _, k := range c.Frameworks {
				if k == compliance.Frameworks[i].Key {
					want++
				}
			}
		}
		sum := f["pass"].(float64) + f["fail"].(float64) + f["overdue"].(float64) + f["not_assessed"].(float64) + f["not_applicable"].(float64)
		if f["key"] != compliance.Frameworks[i].Key || f["controls"] != float64(want) || sum != float64(want) || f["stage"] == "" {
			t.Errorf("framework %v: %d mapped, counts add to %v", f, want, sum)
		}
		if gaps := f["gaps"].([]any); float64(len(gaps)) != f["fail"].(float64)+f["overdue"].(float64)+f["not_assessed"].(float64) {
			t.Errorf("%v: %d gaps for %v failing, overdue and not assessed", f["key"], len(gaps), f)
		}
	}
	annex := r.Body["annex_a"].(map[string]any)
	covered, uncovered := annex["covered"].([]any), annex["uncovered"].([]any)
	if annex["total"] != float64(93) || len(covered)+len(uncovered) != 93 || len(covered) == 0 || len(uncovered) == 0 {
		t.Errorf("Annex A: %v covered, %v uncovered of %v", len(covered), len(uncovered), annex["total"])
	}
	seen := map[any]bool{}
	for _, n := range covered {
		seen[n] = true
	}
	for _, n := range uncovered {
		if seen[n] {
			t.Errorf("Annex A %v is both covered and not", n)
		}
	}
	if r.str("notice") == "" {
		t.Error("readiness says nothing of not being a certificate")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/readiness", e.as(authz.Bookkeeper), nil), "getReadiness", 403, CodeForbidden)
	if r := e.call(http.MethodGet, "/v1/compliance/readiness", e.as(authz.DPO), nil); r.Status != 200 {
		t.Errorf("the DPO reads readiness: %d", r.Status)
	}
}

// matrixReadiness is the authorisation matrix's row for readiness (CMP-08, Appendix D "Compliance Center").
var matrixReadiness = map[string]func(e *env, tok string) (response, func() bool){
	"getReadiness": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/readiness", tok, nil), nil
	},
}
