//go:build local

package api

import (
	"net/http"
	"sync"

	"github.com/cognizhi/fyniq/internal/authz"
)

var (
	matrixGrantMu sync.Mutex
	matrixGrantOf = map[*env][2]string{} // the matrix's grant and its ETag
)

func matrixGrantBody(e *env) map[string]any {
	return map[string]any{"recipient": "Matrix", "fair_value": "1.00", "service_start": "2026-01-01", "expense_account": e.books.Rent.ID.String(), "reserve_account": matrixReserve(e),
		"tranches": []map[string]any{{"shares": 100, "vest_on": "2026-12-31"}}}
}

// matrixReserve is an equity account that is not share capital, added when there is none.
func matrixReserve(e *env) string {
	l := e.call(http.MethodGet, "/v1/accounts?page_size=500", e.as(authz.Accountant), nil)
	for _, x := range l.Body["items"].([]any) {
		if a := x.(map[string]any); a["code"] == "3250" {
			return a["id"].(string)
		}
	}
	return e.call(http.MethodPost, "/v1/accounts", e.as(authz.Accountant), map[string]any{"code": "3250", "name": "Share-based payment reserve", "type": "equity"}).str("id")
}

func matrixGrant(e *env) (id, etag string) {
	matrixGrantMu.Lock()
	known, ok := matrixGrantOf[e]
	matrixGrantMu.Unlock()
	if ok {
		return known[0], known[1]
	}
	r := e.call(http.MethodPost, "/v1/share-grants", e.as(authz.Accountant), matrixGrantBody(e))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's grant = %d: %s", r.Status, r.Raw)
	}
	matrixGrantMu.Lock()
	matrixGrantOf[e] = [2]string{r.str("id"), r.Header.Get("ETag")}
	matrixGrantMu.Unlock()
	return r.str("id"), r.Header.Get("ETag")
}

// matrixShareGrants are the authorisation matrix's rows for the grants of shares for services (FR-EQ-13): the cap table's row; a write is sent as a preview.
var matrixShareGrants = map[string]func(e *env, tok string) (response, func() bool){
	"listShareGrants": func(e *env, tok string) (response, func() bool) {
		matrixGrant(e)
		return e.call(http.MethodGet, "/v1/share-grants", tok, nil), nil
	},
	"getShareGrant": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixGrant(e)
		return e.call(http.MethodGet, "/v1/share-grants/"+id, tok, nil), nil
	},
	"getShareGrantSchedule": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixGrant(e)
		return e.call(http.MethodGet, "/v1/share-grants/"+id+"/schedule", tok, nil), nil
	},
	"listShareGrantMonths": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixGrant(e)
		return e.call(http.MethodGet, "/v1/share-grant-months?grant_id="+id, tok, nil), nil
	},
	"createShareGrant": func(e *env, tok string) (response, func() bool) {
		owner := e.as(authz.Accountant)
		n := len(e.call(http.MethodGet, "/v1/share-grants", owner, nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/share-grants", tok, matrixGrantBody(e), "Prefer", "return=preview")
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/share-grants", owner, nil).Body["items"].([]any)) == n
		}
	},
	"replaceShareGrant": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixGrant(e)
		r := e.call(http.MethodPut, "/v1/share-grants/"+id, tok, matrixGrantBody(e), "If-Match", etag, "Prefer", "return=preview")
		return r, func() bool { _, now := matrixGrant(e); return now == etag }
	},
	"proposeShareGrantMonth": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixGrant(e)
		r := e.call(http.MethodPost, "/v1/share-grant-months", tok, map[string]any{"grant_id": id, "month": "2026-10"}, "Prefer", "return=preview")
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/share-grant-months?grant_id="+id, e.as(authz.Accountant), nil).Body["items"].([]any)) == 0
		}
	},
}
