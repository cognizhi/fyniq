//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// bank.rule_upsert (Appendix E, FR-BNK-05, FR-BNK-14; p1-ai-proposals task 3.2): the AI proposes a bank rule, created or changed, with its back-test shown;
// someone who may keep bank rules approves it, and the executor saves it once through the functions a person's create and change run, marked as an AI
// proposal's, never applying by itself: the payload has no auto-apply field, and a rule's change stops auto-apply meanwhile. A bank, receivables or payables
// account, a regular expression that does not compile, no condition, a rule that is none and any other field are refused. The undo switches a created rule off or puts a
// changed one back, as the person, once, while nobody has changed the rule since (AI-31, AI-91).
// req: AI-31
// req: AI-91
// req: AI-11
// req: FR-BNK-05
// req: ai/task-catalogue bank.rule_upsert Create or edit a bank rule
// asvs: V8.2.2, V8.3.1
func TestTheAIProposesABankRuleThatAPersonApprovesAndUndoes(t *testing.T) {
	e := newEnv(t)
	k := newTaskKit(e, "bank.rule_upsert")
	bk, _ := k.roleToken("Bk Br", authz.Bookkeeper)
	_, approver := k.roleToken("Acc Br", authz.Accountant)
	turn := proposeTurn(e, bk.String())
	acc := e.as(authz.Accountant)
	var cash string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1990", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true}})
		cash = c.Code
		return err
	})
	rules := func() []any {
		items, _ := e.call(http.MethodGet, "/v1/bank-rules", acc, nil).Body["items"].([]any)
		return items
	}
	before := len(rules())
	good := map[string]any{"name": "AWS to rent", "position": 1, "when": map[string]any{"description_contains": "AWS", "direction": "out"}, "then": map[string]any{"account": e.books.Rent.Code, "gst_code": "TX"}}
	made := k.propose(turn, "bank.rule_upsert", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.HasPrefix(made.str("summary"), `Adds the bank rule "AWS to rent": it would have matched 0 of`) || len(rules()) != before {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	bad := func(what string, mutate func(p map[string]any)) {
		t.Helper()
		p := map[string]any{}
		for key, v := range good {
			p[key] = v
		}
		mutate(p)
		if r := k.propose(turn, "bank.rule_upsert", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("%s made a proposal: %s", what, r.Raw)
		}
	}
	bad("an auto-apply field", func(p map[string]any) { p["auto_apply"] = true })
	bad("an approval id", func(p map[string]any) { p["auto_apply_approval_id"] = uuid.NewString() })
	bad("a bank account as the coding", func(p map[string]any) { p["then"] = map[string]any{"account": cash} })
	bad("a regular expression that does not compile", func(p map[string]any) { p["when"] = map[string]any{"description_regex": "(unclosed"} })
	bad("no condition", func(p map[string]any) { p["when"] = map[string]any{} })
	bad("an account that is none", func(p map[string]any) { p["then"] = map[string]any{"account": "9999"} })
	bad("a rule that is none", func(p map[string]any) { p["rule_id"] = uuid.NewString() })
	bad("a position of zero", func(p map[string]any) { p["position"] = 0 })

	k.approve(made.str("id"), approver)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "bank_rule:") || len(rules()) != before+1 {
		t.Fatalf("the executed proposal: %s %s %s (%d rules, was %d)", pr.Status, pr.Reason, pr.Execution, len(rules()), before)
	}
	var rule map[string]any
	for _, it := range rules() {
		if m := it.(map[string]any); m["name"] == "AWS to rent" {
			rule = m
		}
	}
	if rule == nil || rule["from_ai_proposal"] != true || rule["auto_apply"] != false || rule["active"] != true {
		t.Fatalf("the saved rule: %v", rule)
	}
	undo := func(tok, id string) response {
		return e.call(http.MethodPost, "/v1/ai/proposal-undos", tok, map[string]any{"proposal_id": id, "reason": "Too broad"}, "Idempotency-Key", uuid.NewString())
	}
	_, outsider := k.roleToken("Aud Br", authz.Auditor)
	if r := undo(outsider, made.str("id")); r.Status != http.StatusForbidden && r.Status != http.StatusNotFound {
		t.Errorf("an auditor undoing it: %d %s", r.Status, r.Raw)
	}
	if r := undo(approver, made.str("id")); r.Status != http.StatusCreated {
		t.Fatalf("the undo: %d %s", r.Status, r.Raw)
	}
	if got := e.call(http.MethodGet, "/v1/bank-rules/"+rule["id"].(string), acc, nil); got.Body["active"] != false {
		t.Errorf("the rule after its undo: %s", got.Raw)
	}

	// A change of the rule is a proposal too: it puts the old rule back on undo, while nobody has touched it since.
	change := k.propose(turn, "bank.rule_upsert", map[string]any{"rule_id": rule["id"], "name": "AWS to rent (broader)", "position": 1,
		"when": map[string]any{"description_contains": "AWS"}, "then": map[string]any{"account": e.books.Rent.Code}})
	if change.Status != http.StatusCreated || !strings.HasPrefix(change.str("summary"), `Changes the bank rule "AWS to rent (broader)"`) {
		t.Fatalf("the change: %d %s", change.Status, change.Raw)
	}
	k.approve(change.str("id"), approver)
	if done := k.execute(change.str("id"), 1); done.Status != storage.ProposalExecuted {
		t.Fatalf("the change executed: %s %s", done.Status, done.Reason)
	}
	if r := undo(approver, change.str("id")); r.Status != http.StatusCreated {
		t.Fatalf("undoing the change: %d %s", r.Status, r.Raw)
	}
	back := e.call(http.MethodGet, "/v1/bank-rules/"+rule["id"].(string), acc, nil)
	if back.str("name") != "AWS to rent" || back.Body["active"] != false || back.Body["auto_apply"] != false {
		t.Errorf("the rule after undoing its change: %s", back.Raw)
	}
	// A rule a person changed since the execution is not undone.
	again := k.propose(turn, "bank.rule_upsert", map[string]any{"rule_id": rule["id"], "name": "AWS again", "position": 1, "when": map[string]any{"description_contains": "AWS"}, "then": map[string]any{"account": e.books.Rent.Code}})
	k.approve(again.str("id"), approver)
	if done := k.execute(again.str("id"), 1); done.Status != storage.ProposalExecuted {
		t.Fatalf("the third change: %s %s", done.Status, done.Reason)
	}
	cur := e.call(http.MethodGet, "/v1/bank-rules/"+rule["id"].(string), acc, nil)
	if r := e.call(http.MethodPut, "/v1/bank-rules/"+rule["id"].(string), acc, map[string]any{"name": "By hand", "position": 1, "active": true,
		"when": map[string]any{"description_contains": "AWS"}, "then": map[string]any{"account_id": e.books.Rent.ID.String()}}, "If-Match", cur.Header.Get("ETag"), "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("a person changes the rule: %d %s", r.Status, r.Raw)
	}
	if moved := undo(approver, again.str("id")); moved.Status != http.StatusConflict || moved.str("code") != CodeUndoMoved {
		t.Errorf("undoing what moved: %d %s", moved.Status, moved.Raw)
	}
}
