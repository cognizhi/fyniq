//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
)

// A policy's rules choose a request's steps (FR-APR-01; PRD-001 rev 0.178, the owner's decision of 6 Nov 2026). Ann asks
// for the journal policy: Cat approves a journal of S$5,000.00 or more; Ben one on Rent; anyone else the rest. A rule
// with no condition, or naming an account the chart does not have, is refused. Ben approves the change. Then a
// S$6,000.00 rent journal waits for Cat, the first rule that holds; a S$3,000.00 rent journal for Ben; a S$3,000.00 sale
// for anyone, under the default steps; and each request says which rule chose its steps.
// req: FR-APR-01
// req: access/approvals Conditions decide the steps
func TestAPolicysRulesChooseARequestsSteps(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben)
	const path = "/v1/approval-policies/journal"
	rule := func(conds map[string]any, user string) map[string]any {
		return map[string]any{"conditions": conds, "steps": []map[string]any{{"quorum": 1, "users": []string{user}}}}
	}
	change := func(rules ...map[string]any) response {
		return e.call(http.MethodPatch, path, ann, map[string]any{"steps": []map[string]any{{"quorum": 1}}, "rules": rules}, "If-Match", `"0"`)
	}
	empty := change(rule(map[string]any{}, f.cat))
	e.problemIs(empty, "changeApprovalPolicy", 422, CodeInvalid)
	if !strings.Contains(empty.Raw, `"field":"body.rules[0].conditions"`) || !strings.Contains(empty.Raw, approval.CodeRules) {
		t.Errorf("a rule with no condition: %s", empty.Raw)
	}
	unknown := change(rule(map[string]any{"accounts": []string{uuid.NewString()}}, f.cat))
	e.problemIs(unknown, "changeApprovalPolicy", 422, CodeInvalid)
	if !strings.Contains(unknown.Raw, `"field":"body.rules[0].conditions.accounts[0]"`) {
		t.Errorf("an unknown account: %s", unknown.Raw)
	}

	r := change(rule(map[string]any{"amount_at_least": "5000"}, f.cat), rule(map[string]any{"accounts": []string{e.books.Rent.ID.String()}}, f.ben))
	if r.Status != http.StatusAccepted || !strings.Contains(r.Raw, `"field":"rules","before":"none","after":"rule 1 (amount at least 5000.00)`) {
		t.Fatalf("asking for the rules: %d %s", r.Status, r.Raw)
	}
	if d := f.decide(ben, r, "approve"); d.Status != http.StatusOK {
		t.Fatalf("Ben approving: %d %s", d.Status, d.Raw)
	}
	g := e.call(http.MethodGet, path, ben, nil)
	rules, _ := g.Body["rules"].([]any)
	if len(rules) != 2 || g.str("number") != "" && g.Body["number"] != float64(1) ||
		rules[0].(map[string]any)["conditions"].(map[string]any)["amount_at_least"].(map[string]any)["amount"] != "5000.00" {
		t.Fatalf("the policy: %s", g.Raw)
	}

	// The journals: each request takes the steps of the first rule that holds, else the default steps.
	accountant := f.as(f.ann, authz.Accountant, authz.Controller)
	ask := func(draft map[string]any) response {
		t.Helper()
		d := e.call(http.MethodPost, "/v1/journals", accountant, draft)
		a := e.call(http.MethodPost, "/v1/journals/"+d.str("id")+"/post-approval", accountant, nil, "If-Match", d.Header.Get("ETag"))
		if a.Status != http.StatusAccepted {
			t.Fatalf("asking to post: %d %s", a.Status, a.Raw)
		}
		return a
	}
	sale := e.draft("3000.00")
	sale["lines"] = []map[string]any{
		{"account_id": e.books.Bank.ID.String(), "amount": map[string]any{"amount": "3000.00", "currency": "SGD"}},
		{"account_id": e.books.Sales.ID.String(), "amount": map[string]any{"amount": "-3000.00", "currency": "SGD"}},
	}
	for name, c := range map[string]struct {
		draft map[string]any
		rule  any
		user  string
	}{
		"a S$6,000.00 rent journal": {e.draft("6000.00"), float64(1), f.cat},
		"a S$3,000.00 rent journal": {e.draft("3000.00"), float64(2), f.ben},
		"a S$3,000.00 sale":         {sale, nil, ""},
	} {
		a := ask(c.draft)
		steps := a.Body["steps"].([]any)
		users, _ := steps[0].(map[string]any)["users"].([]any)
		got := ""
		if len(users) == 1 {
			got = users[0].(string)
		}
		if a.Body["policy_rule"] != c.rule || got != c.user {
			t.Errorf("%s: rule %v, steps %v; want rule %v, %q", name, a.Body["policy_rule"], steps, c.rule, c.user)
		}
	}
}
