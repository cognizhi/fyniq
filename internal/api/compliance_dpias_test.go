//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// The impact assessments (CMP-PD-08, rev 0.244; p1-pdpa-retention D4): Fyniq's two drafts, required once their features
// are switched on; the Compliance officer approves the payroll link's draft stepped up, which adopts it as version 1,
// edits it to version 2, which needs approving again, and writes one for an inventory entry; the reviews list says what
// waits; an approval without a second factor, of a stale version, and anyone but the DPO are refused.
// req: CMP-PD-08
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestTheImpactAssessments(t *testing.T) {
	e := newEnv(t)
	dpo := e.personAs(uuid.New(), authz.DPO) // stepped up now
	list := e.call(http.MethodGet, "/v1/compliance/dpias", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 2 || items[0].(map[string]any)["key"] != "payroll" || items[0].(map[string]any)["required"] != false ||
		items[0].(map[string]any)["version"] != float64(0) || items[0].(map[string]any)["unapproved"] != true {
		t.Fatalf("the assessments: %d %s", list.Status, list.Raw)
	}
	g := e.call(http.MethodGet, "/v1/compliance/dpias/payroll", dpo, nil)
	if !strings.HasPrefix(g.str("body"), "# Data protection impact assessment of the payroll link") || g.Header.Get("ETag") != `"v0"` || len(g.Body["sources"].([]any)) == 0 {
		t.Fatalf("the payroll draft: %s", g.Raw)
	}

	// Approving needs a second factor; approving Fyniq's draft adopts it as version 1.
	stale := e.principal(authn.Principal{Subject: "dpo-stale", UserID: uuid.New(), Client: "web", Class: authn.ClassUser, Organisation: e.books.Org,
		Roles: []authz.Role{authz.DPO}, StepUpAt: e.clock.Now().Add(-time.Hour)})
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/dpias/payroll/approvals", stale, nil, "If-Match", `"v0"`,
		"Idempotency-Key", "ap-0"), "approveDPIA", 403, CodeStepUpRequired)
	a := e.call(http.MethodPost, "/v1/compliance/dpias/payroll/approvals", dpo, nil, "If-Match", `"v0"`, "Idempotency-Key", "ap-1")
	if a.Status != 201 || a.Body["version"] != float64(1) || a.str("from") != "fyniq-dpia-draft@1" || a.Body["unapproved"] != false ||
		a.Body["approved"].(map[string]any)["version"] != float64(1) {
		t.Fatalf("approving the draft: %d %s", a.Status, a.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/dpias/payroll/approvals", dpo, nil, "If-Match", `"v1"`, "Idempotency-Key", "ap-2"),
		"approveDPIA", 422, CodeInvalid)

	// A new version needs approving again; a stale one is refused.
	s := e.call(http.MethodPut, "/v1/compliance/dpias/payroll", dpo, map[string]any{"body": "# Data protection impact assessment of the payroll link\n\nOurs.\n"},
		"If-Match", `"v1"`, "Idempotency-Key", "sv-1")
	if s.Status != 200 || s.Body["version"] != float64(2) || s.Body["unapproved"] != true || s.str("from") != "organisation" {
		t.Fatalf("version 2: %d %s", s.Status, s.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/dpias/payroll/approvals", dpo, nil, "If-Match", `"v1"`, "Idempotency-Key", "ap-3"),
		"approveDPIA", 412, CodeStale)

	// One for an inventory entry that needs it.
	entry := e.call(http.MethodPost, "/v1/compliance/data-inventory/entries", dpo, map[string]any{"what": "A mailing list", "why": "News", "where": "A tool",
		"who": "Marketing", "retention": "Until they unsubscribe", "transfers": "United States", "needs_dpia": true}, "Idempotency-Key", "inv-1")
	c := e.call(http.MethodPost, "/v1/compliance/dpias", dpo, map[string]any{"title": "Data protection impact assessment of the mailing list",
		"body": "# Mailing list\n", "entry_id": entry.str("entry_id")}, "Idempotency-Key", "dp-1")
	if c.Status != 201 || !strings.HasPrefix(c.str("key"), "other-") || c.Body["required"] != true || c.str("entry_id") != entry.str("entry_id") {
		t.Fatalf("an assessment of the entry: %d %s", c.Status, c.Raw)
	}
	reviews := e.call(http.MethodGet, "/v1/compliance/reviews", e.as(authz.Auditor), nil).Body["items"].([]any)
	dpia := reviews[len(reviews)-1].(map[string]any)
	if dpia["kind"] != "dpia" || dpia["status"] != "overdue" || dpia["waiting"] != float64(1) || !strings.Contains(dpia["detail"].(string), "a mailing list") {
		t.Errorf("the reviews' DPIA line: %v", dpia)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "dpia.approved") || !strings.Contains(trail, "dpia.saved") {
		t.Error("the assessments are not in the audit trail")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/dpias", e.as(authz.Controller), nil), "listDPIAs", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPut, "/v1/compliance/dpias/payroll", e.as(authz.Owner), map[string]any{"body": "x"}, "If-Match", `"v2"`),
		"saveDPIA", 403, CodeForbidden)
}

// matrixDPIAs are the authorisation matrix's rows for the impact assessments (CMP-PD-08, Appendix D "Data protection (PDPA)").
var matrixDPIAs = map[string]func(e *env, tok string) (response, func() bool){
	"listDPIAs": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/dpias", tok, nil), nil
	},
	"getDPIA": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/dpias/ai-assistant", tok, nil), nil
	},
	"createDPIA": func(e *env, tok string) (response, func() bool) {
		was := matrixDPIAState(e)
		r := e.call(http.MethodPost, "/v1/compliance/dpias", tok, map[string]any{"title": "Matrix", "body": "# Matrix\n"}, "Prefer", "return=preview")
		return r, func() bool { return matrixDPIAState(e) == was }
	},
	"saveDPIA": func(e *env, tok string) (response, func() bool) {
		was := matrixDPIAState(e)
		r := e.call(http.MethodPut, "/v1/compliance/dpias/ai-assistant", tok, map[string]any{"body": "# Matrix\n"}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixDPIAState(e) == was }
	},
	"approveDPIA": func(e *env, tok string) (response, func() bool) {
		was := matrixDPIAState(e)
		r := e.call(http.MethodPost, "/v1/compliance/dpias/ai-assistant/approvals", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixDPIAState(e) == was }
	},
}

func matrixDPIAState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/dpias", e.as(authz.DPO), nil).Raw
}
