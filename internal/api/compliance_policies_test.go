//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// The policy library (CMP-07; Appendix D "Policy library"): it starts with Fyniq's eight drafts, none in force; the
// Compliance officer publishes a draft as version 1; a member's acknowledgement falls due 30 days after it, and they give
// it once, signed in for themselves; an edited draft publishes as version 2 while version 1 stays as it was; a stale
// draft, a member's draft, a token's acknowledgement and the AI are refused.
// req: CMP-07
// req: AUD-01
// req: access/roles-and-segregation Permission matrix: Policy library
func TestThePolicyLibrary(t *testing.T) {
	e := newEnv(t)
	dpo, member := e.as(authz.DPO), e.as(authz.Bookkeeper)
	list := e.call(http.MethodGet, "/v1/compliance/policies", member, nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 8 {
		t.Fatalf("the library: %d %s", list.Status, list.Raw)
	}
	first := items[0].(map[string]any)
	draft := first["draft"].(map[string]any)
	if first["key"] != "information-security" || first["current"] != nil || draft["source"] != "fyniq" || draft["revision"].(float64) != 0 ||
		draft["unpublished"] != true || draft["body"] != nil {
		t.Errorf("a policy before its first version: %v", first)
	}
	// A member reads the draft's text nowhere; the Compliance officer does.
	if r := e.call(http.MethodGet, "/v1/compliance/policies/data-protection", member, nil); r.Body["draft"].(map[string]any)["body"] != nil {
		t.Errorf("a member read the draft: %s", r.Raw)
	}
	r := e.call(http.MethodGet, "/v1/compliance/policies/data-protection", dpo, nil)
	if !strings.HasPrefix(r.Body["draft"].(map[string]any)["body"].(string), "# Data protection policy") || len(r.Body["sources"].([]any)) == 0 ||
		r.Header.Get("ETag") != `"r0"` {
		t.Fatalf("the Compliance officer's view: %s", r.Raw)
	}

	// Version 1, from Fyniq's draft.
	r = e.call(http.MethodPost, "/v1/compliance/policies/data-protection/versions", dpo, nil, "If-Match", `"r0"`, "Idempotency-Key", "pol-1")
	current, _ := r.Body["current"].(map[string]any)
	if r.Status != 201 || current == nil || current["version"].(float64) != 1 || current["from"] != "fyniq-policy-draft@1" ||
		r.Body["draft"].(map[string]any)["unpublished"] != false {
		t.Fatalf("version 1: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/policies/data-protection/versions", dpo, nil, "If-Match", `"r0"`, "Idempotency-Key", "pol-1-again"),
		"publishPolicyVersion", 422, CodeInvalid)

	// The member acknowledges it, due 30 days after it was published (they joined before), once.
	r = e.call(http.MethodGet, "/v1/compliance/policies/data-protection", member, nil)
	mine, _ := r.Body["mine"].(map[string]any)
	if due := e.clock.Now().Add(AcknowledgeWithin).In(time.FixedZone("SGT", 8*3600)).Format(time.DateOnly); mine == nil || mine["due_on"] != due ||
		mine["acknowledged_at"] != nil || mine["overdue"] != false || !strings.HasPrefix(r.str("body"), "# Data protection policy") {
		t.Fatalf("the member's view of version 1: %s", r.Raw)
	}
	ack := "/v1/compliance/policies/data-protection/versions/1/acknowledgements"
	r = e.call(http.MethodPost, ack, member, nil, "If-Match", `"v1"`, "Idempotency-Key", "ack-1")
	if r.Status != 201 || r.Body["mine"].(map[string]any)["acknowledged_at"] == nil || r.Body["current"].(map[string]any)["acknowledged"].(float64) != 1 {
		t.Fatalf("an acknowledgement: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, ack, member, nil, "If-Match", `"v1"`, "Idempotency-Key", "ack-2"), "acknowledgePolicyVersion", 422, CodeInvalid)
	token := e.principal(authn.Principal{Via: authn.ViaToken, Client: "erp", Class: authn.ClassIntegration, Organisation: e.books.Org,
		Roles: []authz.Role{authz.Accountant}})
	e.problemIs(e.call(http.MethodPost, ack, token, nil, "If-Match", `"v1"`, "Idempotency-Key", "ack-token"), "acknowledgePolicyVersion", 403, CodeSessionRequired)
	ai := e.principal(authn.Principal{Client: "agent", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}})
	if r := e.call(http.MethodPost, ack, ai, nil, "If-Match", `"v1"`, "Idempotency-Key", "ack-ai"); r.Status != 403 {
		t.Errorf("the AI acknowledging a policy: %d %s", r.Status, r.Raw)
	}

	// An edited draft publishes as version 2; version 1 stays as it was published.
	edited := map[string]any{"body": "# Data protection policy\n\nOur own words.\n"}
	e.problemIs(e.call(http.MethodPut, "/v1/compliance/policies/data-protection/draft", member, edited, "If-Match", `"r0"`, "Idempotency-Key", "d-m"),
		"savePolicyDraft", 403, CodeForbidden)
	r = e.call(http.MethodPut, "/v1/compliance/policies/data-protection/draft", dpo, edited, "If-Match", `"r0"`, "Idempotency-Key", "d-1")
	if r.Status != 200 || r.Header.Get("ETag") != `"r1"` || r.Body["draft"].(map[string]any)["unpublished"] != true {
		t.Fatalf("a draft revision: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/compliance/policies/data-protection/draft", dpo, edited, "If-Match", `"r0"`, "Idempotency-Key", "d-stale"),
		"savePolicyDraft", 412, CodeStale)
	r = e.call(http.MethodPost, "/v1/compliance/policies/data-protection/versions", dpo, nil, "If-Match", `"r1"`, "Idempotency-Key", "pol-2")
	if r.Status != 201 || r.Body["current"].(map[string]any)["version"].(float64) != 2 || r.Body["current"].(map[string]any)["from"] != "revision 1" ||
		r.str("body") != "# Data protection policy\n\nOur own words.\n" {
		t.Fatalf("version 2: %d %s", r.Status, r.Raw)
	}
	versions := r.Body["versions"].([]any)
	if len(versions) != 2 || versions[0].(map[string]any)["from"] != "fyniq-policy-draft@1" || versions[0].(map[string]any)["acknowledged"].(float64) != 1 {
		t.Errorf("the versions: %v", versions)
	}
	// Version 2 waits for the member's acknowledgement again.
	if r := e.call(http.MethodGet, "/v1/compliance/policies/data-protection", member, nil); r.Body["mine"].(map[string]any)["acknowledged_at"] != nil {
		t.Errorf("version 2 acknowledged without the member: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/policies/payroll", dpo, nil), "getPolicy", 422, CodeInvalid)
}

// matrixPolicies are the authorisation matrix's rows for the policy library (CMP-07, Appendix D "Policy library").
var matrixPolicies = map[string]func(e *env, tok string) (response, func() bool){
	"listPolicies": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/policies", tok, nil), nil
	},
	"getPolicy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/policies/backup", tok, nil), nil
	},
	"savePolicyDraft": func(e *env, tok string) (response, func() bool) {
		was := matrixPoliciesState(e)
		r := e.call(http.MethodPut, "/v1/compliance/policies/backup/draft", tok, map[string]any{"body": "# Backup\n"}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPoliciesState(e) == was }
	},
	"publishPolicyVersion": func(e *env, tok string) (response, func() bool) {
		was := matrixPoliciesState(e)
		r := e.call(http.MethodPost, "/v1/compliance/policies/backup/versions", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPoliciesState(e) == was }
	},
	"acknowledgePolicyVersion": func(e *env, tok string) (response, func() bool) {
		matrixLibraryPolicy(e)
		was := matrixPoliciesState(e)
		r := e.call(http.MethodPost, "/v1/compliance/policies/ai-use/versions/1/acknowledgements", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPoliciesState(e) == was }
	},
}

// matrixLibraryPolicyOf marks the environments whose ai-use policy is published, once, for the acknowledgement's row.
var matrixLibraryPolicyOf = map[*env]bool{}

func matrixLibraryPolicy(e *env) {
	e.t.Helper()
	if matrixLibraryPolicyOf[e] {
		return
	}
	r := e.call(http.MethodPost, "/v1/compliance/policies/ai-use/versions", e.as(authz.DPO), nil, "If-Match", `"r0"`, "Idempotency-Key", "matrix-policy")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's policy: %d %s", r.Status, r.Raw)
	}
	matrixLibraryPolicyOf[e] = true
}

func matrixPoliciesState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/policies/backup", e.as(authz.DPO), nil).Raw +
		e.call(http.MethodGet, "/v1/compliance/policies/ai-use", e.as(authz.DPO), nil).Raw
}
