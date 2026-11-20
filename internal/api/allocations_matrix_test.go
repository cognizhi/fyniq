//go:build local

package api

import (
	"net/http"
	"sync"

	"github.com/cognizhi/fyniq/internal/authz"
)

var (
	matrixAllocationMu     sync.Mutex
	matrixAllocationRuleOf = map[*env][2]string{} // the matrix's rule and its ETag
)

// matrixAllocationBody is a rule of the matrix's rent account split to its one department value at 100%.
func matrixAllocationBody(e *env) map[string]any {
	return map[string]any{"name": "Matrix rule", "accounts": []string{e.books.Rent.ID.String()}, "key": "fixed",
		"targets": []map[string]any{{"label": "Matrix", "values": []string{matrixDimensionValue(e)}, "percent": "100"}}}
}

// matrixAllocation is a rule of the matrix's organisation, made when there is none, and its ETag.
func matrixAllocation(e *env) (id, etag string) {
	matrixAllocationMu.Lock()
	known, ok := matrixAllocationRuleOf[e]
	matrixAllocationMu.Unlock()
	if ok {
		return known[0], known[1]
	}
	r := e.call(http.MethodPost, "/v1/allocation-rules", e.as(authz.Accountant), matrixAllocationBody(e))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's allocation rule = %d: %s", r.Status, r.Raw)
	}
	matrixAllocationMu.Lock()
	matrixAllocationRuleOf[e] = [2]string{r.str("id"), r.Header.Get("ETag")}
	matrixAllocationMu.Unlock()
	return r.str("id"), r.Header.Get("ETag")
}

// matrixAllocations are the authorisation matrix's rows for the allocation rules (FR-DIM-03): reads are the Journals row's view, saving and running one its create; a write is sent as a preview.
var matrixAllocations = map[string]func(e *env, tok string) (response, func() bool){
	"listAllocationRules": func(e *env, tok string) (response, func() bool) {
		matrixAllocation(e)
		return e.call(http.MethodGet, "/v1/allocation-rules", tok, nil), nil
	},
	"getAllocationRule": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixAllocation(e)
		return e.call(http.MethodGet, "/v1/allocation-rules/"+id, tok, nil), nil
	},
	"listAllocationRuns": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixAllocation(e)
		return e.call(http.MethodGet, "/v1/allocation-runs?rule_id="+id, tok, nil), nil
	},
	"previewAllocation": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixAllocation(e)
		return e.call(http.MethodPost, "/v1/allocation-previews", tok, map[string]any{"rule_id": id, "from": "2026-10-01", "to": "2026-10-31"}), nil
	},
	"createAllocationRule": func(e *env, tok string) (response, func() bool) {
		owner := e.as(authz.Accountant)
		n := len(e.call(http.MethodGet, "/v1/allocation-rules", owner, nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/allocation-rules", tok, matrixAllocationBody(e), "Prefer", "return=preview")
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/allocation-rules", owner, nil).Body["items"].([]any)) == n
		}
	},
	"replaceAllocationRule": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixAllocation(e)
		r := e.call(http.MethodPut, "/v1/allocation-rules/"+id, tok, matrixAllocationBody(e), "If-Match", etag, "Prefer", "return=preview")
		return r, func() bool { _, now := matrixAllocation(e); return now == etag }
	},
	"proposeAllocation": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixAllocation(e)
		r := e.call(http.MethodPost, "/v1/allocation-runs", tok, map[string]any{"rule_id": id, "from": "2026-10-01", "to": "2026-10-31"}, "Prefer", "return=preview")
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/allocation-runs?rule_id="+id, e.as(authz.Accountant), nil).Body["items"].([]any)) == 0
		}
	},
}
