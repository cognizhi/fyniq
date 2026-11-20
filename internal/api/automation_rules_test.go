//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/automation"
)

// A workflow rule without code (FR-AUT-03): "when a bill of at least 1,000.00 is posted, tell Cat". It is refused while the switch is off, which an Owner turns on with a second
// approver; it fires for a posting that meets its conditions, tells only the members it names who may see the document, never the person who posted it, and not for a smaller bill; paused it
// does nothing; archived it is final; and it posts, approves, pays and files nothing: its only action is a notification.
// req: FR-AUT-03
// asvs: V8.2.2, V4.2.1
func TestAWorkflowRuleTellsMembersWhenABillOverItsAmountIsPosted(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben, authz.Accountant, authz.Controller)
	body := func(over map[string]any) map[string]any {
		m := map[string]any{"name": "Large bills", "document_type": "bill", "amount_at_least": "1000.00", "recipients": []string{f.cat}}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	make1 := func(over map[string]any) response {
		return e.call(http.MethodPost, "/v1/automation-rules", ann, body(over), "Idempotency-Key", uuid.NewString())
	}

	// Off until an Owner turns it on with a second approver (golden rule 6).
	e.problemIs(make1(nil), "createAutomationRule", 409, automation.CodeOff)
	on := e.call(http.MethodPatch, "/v1/organisation/settings/features.automation_rules", ann, map[string]any{"value": "true"}, "If-Match", `"0"`)
	if on.Status != 202 {
		t.Fatalf("asking to turn rules on: %d %s", on.Status, on.Raw)
	}
	if a := f.decide(f.as(f.ben), on, "approve"); a.Status != 200 {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}

	// What a rule refuses, field by field.
	e.problemIs(make1(map[string]any{"document_type": "posting"}), "createAutomationRule", 422, automation.CodeRule)
	e.problemIs(make1(map[string]any{"recipients": []string{uuid.NewString()}}), "createAutomationRule", 422, automation.CodeRule)
	e.problemIs(make1(map[string]any{"name": "  "}), "createAutomationRule", 422, automation.CodeRule)
	e.problemIs(e.call(http.MethodPost, "/v1/automation-rules", e.as(authz.Employee), body(nil), "Idempotency-Key", uuid.NewString()), "createAutomationRule", 403, CodeForbidden)
	ai := e.principal(authn.Principal{Client: "agent", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}})
	e.problemIs(e.call(http.MethodPost, "/v1/automation-rules", ai, body(nil), "Idempotency-Key", uuid.NewString()), "createAutomationRule", 403, CodeForbidden)

	r := make1(nil)
	if r.Status != 201 || r.str("trigger") != "document.posted" || r.str("action") != "notify" || r.str("state") != "active" || r.Header.Get("ETag") != `"1"` {
		t.Fatalf("making the rule: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/automation-rules", ann, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("the rules: %d %s", l.Status, l.Raw)
	}
	drainWork(t, e)

	postBill := func(net string) {
		t.Helper()
		d := e.call(http.MethodPost, "/v1/bills", ben, e.billDraft(net, "0.00"))
		if d.Status != http.StatusCreated {
			t.Fatalf("createBill %s = %d: %s", net, d.Status, d.Raw)
		}
		if p := e.call(http.MethodPost, "/v1/bills/"+d.str("id")+"/post", ben, nil, "If-Match", d.Header.Get("ETag")); p.Status != http.StatusOK {
			t.Fatalf("postBill %s = %d: %s", net, p.Status, p.Raw)
		}
		fanOutAll(t, e)
	}
	count := func(who string) int {
		n := 0
		for _, it := range e.call(http.MethodGet, "/v1/notifications?page_size=100", who, nil).Body["items"].([]any) {
			if it.(map[string]any)["type"] == "automation.rule" {
				n++
			}
		}
		return n
	}
	cat := f.as(f.cat)
	postBill("1500.00")
	if count(cat) != 1 || count(ben) != 0 || count(ann) != 0 {
		t.Fatalf("a bill over the amount: Cat %d, the poster Ben %d, Ann %d", count(cat), count(ben), count(ann))
	}
	var title string
	for _, it := range e.call(http.MethodGet, "/v1/notifications", cat, nil).Body["items"].([]any) {
		if n := it.(map[string]any); n["type"] == "automation.rule" {
			title, _ = n["title"].(string)
		}
	}
	if !strings.HasPrefix(title, "Rule “Large bills”: bill") || strings.Contains(title, "1500") {
		t.Fatalf("the title says which rule fired and carries no amount: %q", title)
	}
	postBill("500.00")
	if count(cat) != 1 {
		t.Fatalf("a bill under the amount: %d", count(cat))
	}

	// Paused it does nothing; resumed it fires; archived it is final.
	id, tag := r.str("id"), r.Header.Get("ETag")
	p := e.call(http.MethodPatch, "/v1/automation-rules/"+id, ann, map[string]any{"state": "paused"}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
	if p.Status != 200 || p.str("state") != "paused" {
		t.Fatalf("pausing: %d %s", p.Status, p.Raw)
	}
	postBill("1600.00")
	if count(cat) != 1 {
		t.Fatalf("a paused rule fired: %d", count(cat))
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/automation-rules/"+id, ann, map[string]any{"state": "active"}, "If-Match", tag, "Idempotency-Key", uuid.NewString()), "changeAutomationRule", 412, CodeStale)
	rs := e.call(http.MethodPatch, "/v1/automation-rules/"+id, ann, map[string]any{"state": "active", "amount_at_least": "1550.00"}, "If-Match", p.Header.Get("ETag"), "Idempotency-Key", uuid.NewString())
	if rs.Status != 200 || rs.str("amount_at_least") != "1550.0000" {
		t.Fatalf("resuming with a new amount: %d %s", rs.Status, rs.Raw)
	}
	postBill("1700.00")
	if count(cat) != 2 {
		t.Fatalf("the resumed rule: %d", count(cat))
	}
	ar := e.call(http.MethodPatch, "/v1/automation-rules/"+id, ann, map[string]any{"state": "archived"}, "If-Match", rs.Header.Get("ETag"), "Idempotency-Key", uuid.NewString())
	if ar.Status != 200 || ar.str("state") != "archived" {
		t.Fatalf("archiving: %d %s", ar.Status, ar.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/automation-rules/"+id, ann, map[string]any{"state": "active"}, "If-Match", ar.Header.Get("ETag"), "Idempotency-Key", uuid.NewString()), "changeAutomationRule", 422, automation.CodeRule)
	e.problemIs(e.call(http.MethodPatch, "/v1/automation-rules/"+uuid.NewString(), ann, map[string]any{"state": "paused"}, "If-Match", `"1"`, "Idempotency-Key", uuid.NewString()), "changeAutomationRule", 404, automation.CodeNotFound)

	// The rule posted, approved, paid and filed nothing: only the bills the person posted are in the ledger, and no approval request was made for it.
	l := e.call(http.MethodGet, "/v1/approvals?status=pending", ann, nil)
	if pending, _ := l.Body["items"].([]any); l.Status != 200 || len(pending) != 0 {
		t.Fatalf("a rule asked for an approval: %d %s", l.Status, l.Raw)
	}
}

// matrixAutomationRules are the authorisation matrix's rows of the workflow rule operations.
var matrixAutomationRules = map[string]func(e *env, tok string) (response, func() bool){
	"listAutomationRules": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/automation-rules", tok, nil), nil
	},
	"createAutomationRule": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/automation-rules", steppedUp(e, tok), map[string]any{"name": "Matrix", "recipients": []string{uuid.NewString()}}, "Idempotency-Key", uuid.NewString()), nil
	},
	"changeAutomationRule": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPatch, "/v1/automation-rules/"+uuid.NewString(), steppedUp(e, tok), map[string]any{"state": "paused"}, "If-Match", `"1"`, "Idempotency-Key", uuid.NewString()), nil
	},
}

func init() {
	// The matrix's organisation never switches workflow rules on and has no rule, so a role the policy allows is answered by its state, past the policy.
	pastPolicy["createAutomationRule"] = automation.CodeOff
	pastPolicy["changeAutomationRule"] = automation.CodeNotFound
}
