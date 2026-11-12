//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
)

// The recurring reviews (CMP-03; p1-compliance-center D6): the seven, in the Compliance Center's order, each with its
// owner role, its evidence, the page where it is signed off and the controls whose attestation check reads it. A policy
// version published waits for each member's acknowledgement, due 30 days on at the earliest; an acknowledgement is its
// latest sign-off, with who gave it. A Bookkeeper may not read them.
// req: CMP-03
// req: FR-CTL-06
// req: compliance/compliance-center Attestation recorded
func TestTheRecurringReviews(t *testing.T) {
	e := newEnv(t)
	grace := e.joined("Grace", "dpo")
	e.joined("Hal", "bookkeeper")
	dpo := e.personAs(grace, authz.DPO)
	reviews := func() map[string]map[string]any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/compliance/reviews", e.as(authz.Auditor), nil)
		items, _ := r.Body["items"].([]any)
		if r.Status != 200 || len(items) != len(controls.ReviewKinds) {
			t.Fatalf("the reviews: %d %s", r.Status, r.Raw)
		}
		out := map[string]map[string]any{}
		for i, x := range items {
			v := x.(map[string]any)
			if v["kind"] != controls.ReviewKinds[i] {
				t.Errorf("review %d is %v, want %s", i, v["kind"], controls.ReviewKinds[i])
			}
			out[v["kind"].(string)] = v
		}
		return out
	}
	all := reviews()
	for kind, want := range map[string]struct {
		owner, where string
		controls     []string
	}{
		"review-log":             {"owner", "/review-log", []string{"CTL-FIN-07"}},
		"access-review":          {"owner", "/settings/access-review", []string{"CTL-IAM-02"}},
		"accountant-review":      {"accountant", "/accounting/reviews", []string{"CTL-FIN-10"}},
		"close-signoff":          {"controller", "/accounting/close", []string{"CTL-FIN-09"}},
		"break-glass-review":     {"owner", "/break-glass", []string{"CTL-FIN-07"}},
		"policy-acknowledgement": {"member", "/compliance/attestations", nil},
		"risk-review":            {"dpo", "/compliance/risks", nil},
		"data-inventory":         {"dpo", "/compliance/data-protection", []string{"CTL-DP-05"}},
	} {
		v := all[kind]
		got := []string{}
		for _, c := range v["controls"].([]any) {
			got = append(got, c.(string))
		}
		if v["owner"] != want.owner || v["where"] != want.where || strings.Join(got, ",") != strings.Join(want.controls, ",") ||
			v["title"] == "" || v["evidence"] == "" || v["detail"] == "" {
			t.Errorf("%s: %v", kind, v)
		}
	}
	if p := all["policy-acknowledgement"]; p["status"] != "nothing_due" || p["detail"] != "No policy is published yet." || p["last_sign_off"] != nil {
		t.Errorf("the acknowledgements before a policy is published: %v", p)
	}
	if b := all["break-glass-review"]; b["status"] != "nothing_due" || b["detail"] != "No break-glass was used." {
		t.Errorf("the break-glass review before any: %v", b)
	}

	// A version published waits for each member's acknowledgement, due 30 days on.
	r := e.call(http.MethodPost, "/v1/compliance/policies/backup/versions", dpo, nil, "If-Match", `"r0"`, "Idempotency-Key", "rev-pol-1")
	if r.Status != 201 {
		t.Fatalf("publishing: %d %s", r.Status, r.Raw)
	}
	members := int(r.Body["members"].(float64))
	if members < 2 {
		t.Fatalf("the members: %s", r.Raw)
	}
	p := reviews()["policy-acknowledgement"]
	// The members joined when the database stamped them, after the version was published, so they have 30 days from then.
	due := controls.Day(e.clock.Now().Add(controls.AcknowledgeWithin)).Format(time.DateOnly)
	if on, _ := p["due_on"].(string); p["status"] != "due" || int(p["waiting"].(float64)) != members || p["overdue"].(float64) != 0 || on < due {
		t.Errorf("the acknowledgements of a new version (%d members, due %s): %v", members, due, p)
	}

	// The Compliance officer's own acknowledgement is the latest sign-off.
	r = e.call(http.MethodPost, "/v1/compliance/policies/backup/versions/1/acknowledgements", dpo, nil, "If-Match", `"v1"`, "Idempotency-Key", "rev-ack-1")
	if r.Status != 201 {
		t.Fatalf("acknowledging: %d %s", r.Status, r.Raw)
	}
	p = reviews()["policy-acknowledgement"]
	last, _ := p["last_sign_off"].(map[string]any)
	if int(p["waiting"].(float64)) != members-1 || last == nil || last["by"] != "user:"+grace.String() || last["by_name"] != "Grace" {
		t.Errorf("the acknowledgements after one: %v", p)
	}

	e.problemIs(e.call(http.MethodGet, "/v1/compliance/reviews", e.as(authz.Bookkeeper), nil), "listComplianceReviews", 403, CodeForbidden)
}

// matrixReviews are the authorisation matrix's row for the recurring reviews (CMP-03, Appendix D "Compliance Center").
var matrixReviews = map[string]func(e *env, tok string) (response, func() bool){
	"listComplianceReviews": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/reviews", tok, nil), nil
	},
}
