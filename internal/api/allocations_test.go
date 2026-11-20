//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/allocation"
)

// An allocation rule moves a shared cost between dimension values by journal (FR-DIM-03, ALLOC-PRORATA): Oct's 1,000.00 of rent with no department is split 40% to Engineering and 35%
// to Sales, the 25% the rule cannot place staying on the account with none (250.00). The rule is saved, previewed and run into a draft journal whose lines carry the departments; the rule
// runs once for a month; a rule with an account that is no expense account, percentages above 100 or a target with no department is refused, every problem at once; the headcount key with
// nobody in the people projection has nothing to split by.
// req: FR-DIM-03
// req: ALLOC-PRORATA
func TestAnAllocationRuleSplitsASharedCostIntoADraftJournal(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"})
	sales := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "SALES", "name": "Sales"})
	e.postedJournal("1000.00") // October's rent, shared: no department
	rent := e.books.Rent.ID.String()
	rule := func(key string, targets []map[string]any) map[string]any {
		return map[string]any{"name": "Rent by department", "accounts": []string{rent}, "key": key, "targets": targets}
	}
	fixed := rule("fixed", []map[string]any{
		{"label": "Engineering", "values": []string{eng.str("id")}, "percent": "40"},
		{"label": "Sales", "values": []string{sales.str("id")}, "percent": "35"}})

	// Refused, every problem at once.
	bad := rule("fixed", []map[string]any{{"label": "Engineering", "values": []string{eng.str("id")}, "percent": "70"}, {"label": "Sales", "values": []string{sales.str("id")}, "percent": "40"}})
	bad["accounts"] = []string{e.books.Bank.ID.String()}
	r := e.call(http.MethodPost, "/v1/allocation-rules", accountant, bad)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "not an active expense account") || !strings.Contains(r.Raw, "more than 100") {
		t.Errorf("a bad rule: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/allocation-rules", accountant, rule("headcount", []map[string]any{{"label": "Nowhere", "values": []string{}}})); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a target with no values: %d %s", r.Status, r.Raw)
	}
	made := e.call(http.MethodPost, "/v1/allocation-rules", accountant, fixed)
	if made.Status != http.StatusCreated || made.Header.Get("ETag") == "" {
		t.Fatalf("the rule: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")

	// Previewed: the cost, the weights, the split and the journal's lines; nothing is saved.
	period := map[string]any{"rule_id": id, "from": "2026-10-01", "to": "2026-10-31"}
	pre := e.call(http.MethodPost, "/v1/allocation-previews", accountant, period)
	if pre.Status != http.StatusOK || !strings.Contains(pre.Raw, `"source":"1000.00"`) || !strings.Contains(pre.Raw, `"unallocated":"250.00"`) || !strings.Contains(pre.Raw, `"amount":"400.00"`) ||
		!strings.Contains(pre.Raw, `"amount":"350.00"`) || !strings.Contains(pre.Raw, `"amount":"-750.00"`) || pre.Body["proposed"] != false {
		t.Fatalf("the preview: %d %s", pre.Status, pre.Raw)
	}
	if runs := e.call(http.MethodGet, "/v1/allocation-runs?rule_id="+id, accountant, nil); runs.Status != http.StatusOK || strings.Contains(runs.Raw, `"journal_id"`) {
		t.Errorf("a preview saved a run: %s", runs.Raw)
	}
	// Months that are not whole months, or that the books lack, are refused.
	e.problemIs(e.call(http.MethodPost, "/v1/allocation-previews", accountant, map[string]any{"rule_id": id, "from": "2026-10-02", "to": "2026-10-31"}), "previewAllocation", 422, allocation.CodePeriod)
	e.problemIs(e.call(http.MethodPost, "/v1/allocation-previews", accountant, map[string]any{"rule_id": id, "from": "2025-01-01", "to": "2025-01-31"}), "previewAllocation", 422, allocation.CodePeriod)

	// Run into a draft journal whose lines carry the departments.
	run := e.call(http.MethodPost, "/v1/allocation-runs", accountant, period)
	if run.Status != http.StatusCreated || run.str("journal_id") == "" || run.Body["proposed"] != true {
		t.Fatalf("the run: %d %s", run.Status, run.Raw)
	}
	j := e.call(http.MethodGet, "/v1/journals/"+run.str("journal_id"), accountant, nil)
	if j.Status != http.StatusOK || j.str("status") != "draft" || !strings.Contains(j.Raw, eng.str("id")) || !strings.Contains(j.Raw, sales.str("id")) || !strings.Contains(j.Raw, "Allocation of Rent by department, October 2026") {
		t.Errorf("the draft journal: %d %s", j.Status, j.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/journals/"+run.str("journal_id")+"/post", accountant, nil, "If-Match", j.Header.Get("ETag")); p.Status != http.StatusOK {
		t.Errorf("posting the allocation like any journal: %d %s", p.Status, p.Raw)
	}
	// A rule allocates a month once; the run is recorded.
	e.problemIs(e.call(http.MethodPost, "/v1/allocation-runs", accountant, period), "proposeAllocation", 409, allocation.CodeRun)
	if runs := e.call(http.MethodGet, "/v1/allocation-runs?rule_id="+id, accountant, nil); !strings.Contains(runs.Raw, run.str("journal_id")) || !strings.Contains(runs.Raw, `"key":"fixed"`) {
		t.Errorf("the recorded run: %s", runs.Raw)
	}
	// After it posts, what is left with no department is the 250.00 the rule could not place: another run of the month is refused, and November has nothing to split.
	e.problemIs(e.call(http.MethodPost, "/v1/allocation-runs", accountant, map[string]any{"rule_id": id, "from": "2026-11-01", "to": "2026-11-30"}), "proposeAllocation", 422, allocation.CodeNothing)

	// The next version of the rule needs its ETag; archiving puts it out of use.
	body := fixed
	body["archived"] = true
	e.problemIs(e.call(http.MethodPut, "/v1/allocation-rules/"+id, accountant, body), "replaceAllocationRule", 428, CodeIfMatchRequired)
	if r := e.call(http.MethodPut, "/v1/allocation-rules/"+id, accountant, body, "If-Match", made.Header.Get("ETag")); r.Status != http.StatusOK || r.Body["archived"] != true {
		t.Errorf("archiving: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/allocation-previews", accountant, period), "previewAllocation", 422, allocation.CodeRule)

	// The headcount key counts people of the projection, and with nobody it has nothing to split by.
	hc := e.call(http.MethodPost, "/v1/allocation-rules", accountant, rule("headcount", []map[string]any{{"label": "Engineering", "values": []string{eng.str("id")}}}))
	if hc.Status != http.StatusCreated {
		t.Fatalf("the headcount rule: %d %s", hc.Status, hc.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/allocation-previews", accountant, map[string]any{"rule_id": hc.str("id"), "from": "2026-10-01", "to": "2026-10-31"}), "previewAllocation", 422, allocation.CodeWeights)
}

// A usage file is refused unless its rows add up to its stated total and the supplier's bill it explains has that total (FR-DIM-03); the rows naming no target are what the key cannot place.
// A bill of 1,000.00 split by usage of 600, 300 and 100 units, the last of a code that is no target, is 600.00 and 300.00 with 100.00 left unallocated.
// req: FR-DIM-03
func TestAUsageFileMustEqualTheBillItExplains(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	customers := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Customer"})
	acme := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": customers.str("id"), "code": "ACME", "name": "Acme"})
	beta := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": customers.str("id"), "code": "BETA", "name": "Beta"})
	bill, _ := e.postedBill("1000.00", "0.00")
	made := e.call(http.MethodPost, "/v1/allocation-rules", accountant, map[string]any{"name": "Hosting by customer", "accounts": []string{e.books.Rent.ID.String()}, "key": "usage",
		"targets": []map[string]any{{"label": "Acme", "values": []string{acme.str("id")}, "code": "acme"}, {"label": "Beta", "values": []string{beta.str("id")}, "code": "beta"}}})
	if made.Status != http.StatusCreated {
		t.Fatalf("the rule: %d %s", made.Status, made.Raw)
	}
	path := "/v1/allocation-previews"
	usage := func(total string, rows ...map[string]any) map[string]any {
		return map[string]any{"rule_id": made.str("id"), "from": "2026-10-01", "to": "2026-10-31", "usage": map[string]any{"bill_id": bill, "billed_total": total, "rows": rows}}
	}
	row := func(code, qty, billed string) map[string]any {
		return map[string]any{"code": code, "quantity": qty, "billed": billed}
	}
	// No file, rows that do not add up, a total that is not the bill's, and a bill that is not there are each refused.
	e.problemIs(e.call(http.MethodPost, path, accountant, map[string]any{"rule_id": made.str("id"), "from": "2026-10-01", "to": "2026-10-31"}), "previewAllocation", 422, allocation.CodeUsage)
	e.problemIs(e.call(http.MethodPost, path, accountant, usage("1000.00", row("acme", "600", "600.00"), row("beta", "300", "300.00"))), "previewAllocation", 422, allocation.CodeUsage)
	e.problemIs(e.call(http.MethodPost, path, accountant, usage("900.00", row("acme", "600", "500.00"), row("beta", "300", "300.00"), row("other", "100", "100.00"))), "previewAllocation", 422, allocation.CodeUsage)
	gone := usage("1000.00", row("acme", "600", "600.00"), row("beta", "300", "300.00"), row("other", "100", "100.00"))
	gone["usage"].(map[string]any)["bill_id"] = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c0000"
	e.problemIs(e.call(http.MethodPost, path, accountant, gone), "previewAllocation", 422, allocation.CodeUsage)
	// The right file splits the bill's cost by usage; the code that is no target stays unallocated.
	ok := e.call(http.MethodPost, path, accountant, usage("1000.00", row("acme", "600", "600.00"), row("beta", "300", "300.00"), row("other", "100", "100.00")))
	if ok.Status != http.StatusOK || !strings.Contains(ok.Raw, `"amount":"600.00"`) || !strings.Contains(ok.Raw, `"amount":"300.00"`) || !strings.Contains(ok.Raw, `"unallocated":"100.00"`) {
		t.Errorf("the split by usage: %d %s", ok.Status, ok.Raw)
	}
}
