//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// oneYearStudy is a one-year idea: 100.00 invested and depreciated over the year, one unit sold for 300.00, no other
// costs, at 10%, with a payback of a year acceptable.
func oneYearStudy() map[string]any {
	return map[string]any{"name": "One-year idea", "years": 1, "discount_rate": "0.1", "max_payback": "1", "investment": "100.00", "life": 1,
		"salvage": "0.00", "working_capital": "0", "units": "1", "unit_growth": "0", "price": "300.00", "price_growth": "0",
		"variable_cost": "0.00", "variable_cost_growth": "0", "fixed": "0.00", "fixed_growth": "0"}
}

// A feasibility study through the API (FR-PLN-06, FR-PLN-08; p1-planning design D8), worked by hand at sg-cit's 17%:
// the year's profit of 200.00 bears 34.00 of tax, so its cash flow is 166.00 + 100.00 of depreciation = 266.00, worth
// 241.82 at 10%: an NPV of 141.82. The IRR is 166%: −100 × 2.66 + 266 = 0. The payback is 100 ÷ 266 = 0.4 years, the
// discounted one 100 ÷ 241.82 = 0.4, the profitability index 2.42. Feasible; marginal once only 0.2 years are acceptable.
// req: FR-PLN-06
// req: FR-PLN-08
func TestFeasibilityThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	revision := func() int64 {
		var n int64
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) { n, err = u.Ledger().Revision(ctx); return err })
		return n
	}
	before := revision()

	bad := oneYearStudy()
	bad["salvage"], bad["unit_growth"] = "200.00", "-1"
	r := e.call(http.MethodPost, "/v1/feasibility-studies", accountant, bad)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"field":"body.salvage"`) || !strings.Contains(r.Raw, `"field":"body.unit_growth"`) {
		t.Fatalf("a study out of its limits: %d %s", r.Status, r.Raw)
	}

	c := e.call(http.MethodPost, "/v1/feasibility-studies", accountant, oneYearStudy())
	if c.Status != http.StatusCreated || c.Header.Get("ETag") == "" || c.str("date") != "2026-10-15" || c.str("study", "name") != "One-year idea" {
		t.Fatalf("drafting the study: %d %s", c.Status, c.Raw)
	}
	id := c.str("id")
	res := e.call(http.MethodGet, "/v1/feasibility-studies/"+id+"/results", e.as(authz.Auditor), nil)
	if res.Status != http.StatusOK || res.str("tax_rate") != "0.17" || res.str("tax_pack") != "sg-cit@2026.1" || res.str("npv") != "141.82" ||
		res.str("irr") != "1.6600" || res.str("payback_years") != "0.4" || res.str("discounted_payback_years") != "0.4" ||
		res.str("profitability_index") != "2.42" || res.str("break_even_units") != "0.0" || res.str("margin_of_safety_percent") != "100.0" ||
		res.str("verdict") != "feasible" || res.str("verdict_words") != "Feasible: a positive NPV, an IRR of 166.00%, and a payback of 0.4 years" {
		t.Fatalf("the results: %d %s", res.Status, res.Raw)
	}
	y := res.Body["years"].([]any)[1].(map[string]any)
	if y["tax"] != "34.00" || y["cash_flow"] != "266.00" || y["present_value"] != "241.82" || len(res.Body["sensitivity"].([]any)) != 5 ||
		len(res.Body["npv_at_rates"].([]any)) != 8 {
		t.Errorf("the year and the tables: %v", y)
	}

	change := oneYearStudy()
	change["max_payback"] = "0.2"
	e.problemIs(e.call(http.MethodPut, "/v1/feasibility-studies/"+id, accountant, change, "If-Match", `"99-draft"`), "replaceFeasibilityStudy", 412, CodeStale)
	p := e.call(http.MethodPut, "/v1/feasibility-studies/"+id, accountant, change, "If-Match", c.Header.Get("ETag"))
	if p.Status != http.StatusOK || p.Body["version"] != float64(2) || p.str("date") != "2026-10-15" {
		t.Fatalf("changing the study: %d %s", p.Status, p.Raw)
	}
	if m := e.call(http.MethodGet, "/v1/feasibility-studies/"+id+"/results", accountant, nil); m.str("verdict") != "marginal" ||
		m.str("verdict_words") != "Marginal: a positive NPV, but the payback of 0.4 years is longer than 0.2" {
		t.Errorf("a payback longer than acceptable: %s", m.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/feasibility-studies", accountant, nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the list: %d %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/feasibility-studies/"+id, accountant, nil); g.Status != http.StatusOK || g.Header.Get("ETag") != p.Header.Get("ETag") {
		t.Errorf("reading the study: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/feasibility-studies/"+e.books.Rent.ID.String(), accountant, nil), "getFeasibilityStudy", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, "/v1/feasibility-studies", e.as(authz.Bookkeeper), oneYearStudy()), "createFeasibilityStudy", 403, CodeForbidden)
	if after := revision(); after != before {
		t.Errorf("a study moved the ledger from revision %d to %d", before, after)
	}
}

// matrixFeasibility are the authorisation matrix's rows for feasibility studies.
var matrixFeasibility = map[string]func(e *env, tok string) (response, func() bool){
	"listFeasibilityStudies": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/feasibility-studies", tok, nil), nil
	},
	"createFeasibilityStudy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/feasibility-studies", tok, oneYearStudy()), nil
	},
	"getFeasibilityStudy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/feasibility-studies/"+matrixStudy(e), tok, nil), nil
	},
	"replaceFeasibilityStudy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/feasibility-studies/"+matrixStudy(e), tok, oneYearStudy(), "If-Match", "*"), nil
	},
	"getFeasibilityResults": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/feasibility-studies/"+matrixStudy(e)+"/results", tok, nil), nil
	},
}

// matrixStudy is a study for the matrix's rows, drafted by an Accountant.
func matrixStudy(e *env) string {
	return e.call(http.MethodPost, "/v1/feasibility-studies", e.as(authz.Accountant), oneYearStudy()).str("id")
}
