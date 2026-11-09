//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The personal-data inventory (CMP-PD-02, CMP-05; rev 0.244; p1-pdpa-retention D3): Fyniq's own classes and what it never
// holds, the organisation's entries saved as versions and retired, the retention schedule from the settings in force,
// and the quarterly review once a quarter has ended; the DPO keeps it, the Owner and the Auditor read it, a Controller
// sees none of it.
// req: CMP-PD-02
// req: CMP-05
// req: CTL-DP-05
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestThePersonalDataInventory(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	r := e.call(http.MethodGet, "/v1/compliance/data-inventory", e.as(authz.Auditor), nil)
	classes, _ := r.Body["classes"].([]any)
	if r.Status != 200 || len(classes) < 8 || classes[0].(map[string]any)["key"] != "members" || len(r.Body["entries"].([]any)) != 0 {
		t.Fatalf("the inventory: %d %s", r.Status, r.Raw)
	}
	held := map[string]bool{}
	for _, n := range r.Body["not_held"].([]any) {
		held[n.(map[string]any)["key"].(string)] = true
	}
	schedule := r.Body["schedule"].([]any)
	if !held["nric"] || !held["card-data"] || len(schedule) != 6 ||
		!strings.HasPrefix(schedule[0].(map[string]any)["retention"].(string), "7 years after the end of the financial year") ||
		!strings.HasPrefix(schedule[1].(map[string]any)["retention"].(string), "400 days") {
		t.Errorf("not held, and the schedule: %v %v", r.Body["not_held"], schedule)
	}

	// An entry for processing outside Fyniq, changed and retired, each a version.
	body := map[string]any{"what": "Job applicants' CVs", "why": "To hire", "where": "A shared drive in Singapore", "who": "The founders",
		"retention": "6 months after the role is filled", "transfers": "None", "needs_dpia": false}
	c := e.call(http.MethodPost, "/v1/compliance/data-inventory/entries", dpo, body, "Idempotency-Key", "inv-1")
	if c.Status != 201 || c.Header.Get("ETag") != `"v1"` || c.str("state") != "open" {
		t.Fatalf("an entry: %d %s", c.Status, c.Raw)
	}
	path := "/v1/compliance/data-inventory/entries/" + c.str("entry_id")
	body["state"] = "retired"
	e.problemIs(e.call(http.MethodPut, path, dpo, body, "If-Match", `"v2"`, "Idempotency-Key", "inv-stale"), "reviseDataInventoryEntry", 412, CodeStale)
	if u := e.call(http.MethodPut, path, dpo, body, "If-Match", `"v1"`, "Idempotency-Key", "inv-2"); u.Status != 200 || u.Body["version"] != float64(2) || u.str("state") != "retired" {
		t.Fatalf("retiring it: %d %s", u.Status, u.Raw)
	}
	body["what"] = ""
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/data-inventory/entries", dpo, body, "Idempotency-Key", "inv-3"), "createDataInventoryEntry", 422, CodeInvalid)

	// The quarterly review: none waits before a quarter ends; a wrong quarter is refused.
	review := e.call(http.MethodGet, "/v1/compliance/data-inventory", dpo, nil).Body["review"].(map[string]any)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/data-inventory/reviews", dpo, map[string]any{"quarter": "2020-01-01"}, "Idempotency-Key", "rev-x"),
		"reviewDataInventory", 422, CodeInvalid)
	if q, _ := review["quarter"].(string); q != "" {
		if r := e.call(http.MethodPost, "/v1/compliance/data-inventory/reviews", dpo, map[string]any{"quarter": q, "note": "Checked"},
			"Idempotency-Key", "rev-1"); r.Status != 201 || r.str("last_quarter") != q || r.Body["quarter"] != nil {
			t.Errorf("the review of %s: %d %s", q, r.Status, r.Raw)
		}
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "data_inventory_entry.saved") {
		t.Error("the entries are not in the audit trail")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/data-inventory", e.as(authz.Controller), nil), "getDataInventory", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/data-inventory/entries", e.as(authz.Owner), body, "Idempotency-Key", "inv-o"),
		"createDataInventoryEntry", 403, CodeForbidden)
}

// matrixInventory are the authorisation matrix's rows for the inventory (CMP-PD-02, Appendix D "Data protection (PDPA)").
var matrixInventory = map[string]func(e *env, tok string) (response, func() bool){
	"getDataInventory": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/data-inventory", tok, nil), nil
	},
	"createDataInventoryEntry": func(e *env, tok string) (response, func() bool) {
		was := matrixInventoryState(e)
		r := e.call(http.MethodPost, "/v1/compliance/data-inventory/entries", tok, map[string]any{"what": "x", "why": "y", "where": "z", "who": "w",
			"retention": "r", "transfers": "None", "needs_dpia": false}, "Prefer", "return=preview")
		return r, func() bool { return matrixInventoryState(e) == was }
	},
	"reviseDataInventoryEntry": func(e *env, tok string) (response, func() bool) {
		id := matrixInventoryEntry(e)
		was := matrixInventoryState(e)
		r := e.call(http.MethodPut, "/v1/compliance/data-inventory/entries/"+id, tok, map[string]any{"what": "x",
			"why": "y", "where": "z", "who": "w", "retention": "r", "transfers": "None", "needs_dpia": false}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixInventoryState(e) == was }
	},
	// A quarter waits for its review once one has ended since the first member joined: the organisation gains a member
	// once, the clock moves to 2100, as for the access review, and the row reviews the quarter the inventory names.
	"reviewDataInventory": func(e *env, tok string) (response, func() bool) {
		if !matrixInventoryMember[e] {
			e.joined("Matrix member", "employee")
			matrixInventoryMember[e] = true
		}
		in2100 := func(f func()) {
			was := e.clock.Now()
			defer e.clock.Set(was)
			e.clock.Set(time.Date(2100, 1, 5, 9, 0, 0, 0, time.UTC))
			f()
		}
		var r response
		var was string
		in2100(func() {
			was = matrixInventoryState(e)
			q, _ := e.call(http.MethodGet, "/v1/compliance/data-inventory", e.as(authz.DPO), nil).Body["review"].(map[string]any)["quarter"].(string)
			r = e.call(http.MethodPost, "/v1/compliance/data-inventory/reviews", tok, map[string]any{"quarter": q}, "Prefer", "return=preview")
		})
		return r, func() (same bool) {
			in2100(func() { same = matrixInventoryState(e) == was })
			return same
		}
	},
}

var (
	matrixInventoryEntryOf = map[*env]string{}
	matrixInventoryMember  = map[*env]bool{}
)

// matrixInventoryEntry is an entry of the company's own, so the matrix revises one that exists.
func matrixInventoryEntry(e *env) string {
	e.t.Helper()
	if id, ok := matrixInventoryEntryOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/compliance/data-inventory/entries", e.as(authz.DPO), map[string]any{"what": "Matrix", "why": "y", "where": "z",
		"who": "w", "retention": "r", "transfers": "None", "needs_dpia": false}, "Idempotency-Key", "matrix-inventory-entry")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's inventory entry: %d %s", r.Status, r.Raw)
	}
	matrixInventoryEntryOf[e] = r.str("entry_id")
	return r.str("entry_id")
}

func matrixInventoryState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/data-inventory", e.as(authz.DPO), nil).Raw
}
