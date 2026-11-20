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

// recur.create_template (Appendix E, FR-GL-02, FR-AUT-01; p1-ai-proposals task 4.1): the AI proposes a recurring journal, and a person other than the
// requester approves it (AI-10); the executor adds it once, through the function a person's Add a recurring journal runs, and it posts nothing: the
// scheduler makes drafts. Lines on a bank, an equity or a heading account, lines that do not balance, a monthly equivalent over the limit, a bad schedule
// and a field the task type does not name are refused; an account changed since the dry run makes it Stale. A person undoes it, once, as the person, by
// pausing the template, while nobody has changed it since (AI-31, AI-91).
// req: AI-31
// req: AI-91
// req: AI-10
// req: AI-11
// req: FR-GL-02
// req: ai/task-catalogue recur.create_template Create a recurring template
// asvs: V8.2.2, V8.3.1
func TestTheAIAddsARecurringJournalThatAPersonApprovesAndUndoes(t *testing.T) {
	e := newEnv(t)
	k := newTaskKit(e, "recur.create_template")
	acct, _ := k.roleToken("Acc Rt", authz.Accountant)
	_, approver := k.roleToken("Acc Rt 2", authz.Accountant) // who may add a recurring journal themselves, and is not who asked
	turn := proposeTurn(e, acct.String())
	var accrued, cash string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2999", Name: "Accrued expenses", Type: storage.Liability})
		if err != nil {
			return err
		}
		accrued = a.Code
		c, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1990", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true}})
		cash = c.Code
		return err
	})
	good := map[string]any{"name": "Monthly rent accrual", "memo": "Rent accrued", "reverses": true, "every_months": 1, "day_of_month": 28, "starts_on": "2099-01-01",
		"lines": []map[string]any{{"account": e.books.Rent.Code, "amount": "1200.00", "description": "Rent"}, {"account": accrued, "amount": "-1200.00", "description": "Accrued rent"}}}
	list := func() []any {
		items, _ := e.call(http.MethodGet, "/v1/recurring-journals", e.as(authz.Accountant), nil).Body["items"].([]any)
		return items
	}
	before := len(list())
	made := k.propose(turn, "recur.create_template", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.HasPrefix(made.str("summary"), `Adds the recurring journal "Monthly rent accrual" every 1 month(s) on day 28 from 2099-01-01, about 1200`) ||
		len(list()) != before {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	bad := func(what string, mutate func(p map[string]any)) {
		t.Helper()
		p := map[string]any{}
		for key, v := range good {
			p[key] = v
		}
		mutate(p)
		if r := k.propose(turn, "recur.create_template", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("%s made a proposal: %s", what, r.Raw)
		}
	}
	line := func(code, amount string) map[string]any {
		return map[string]any{"account": code, "amount": amount, "description": "x"}
	}
	bad("a bank account", func(p map[string]any) {
		p["lines"] = []map[string]any{line(e.books.Rent.Code, "100.00"), line(cash, "-100.00")}
	})
	bad("a heading", func(p map[string]any) {
		p["lines"] = []map[string]any{line(e.books.Expenses.Code, "100.00"), line(accrued, "-100.00")}
	})
	bad("lines that do not balance", func(p map[string]any) {
		p["lines"] = []map[string]any{line(e.books.Rent.Code, "100.00"), line(accrued, "-90.00")}
	})
	bad("a monthly equivalent over the limit", func(p map[string]any) {
		p["lines"] = []map[string]any{line(e.books.Rent.Code, "5000.01"), line(accrued, "-5000.01")}
	})
	bad("a quarterly template over the limit", func(p map[string]any) {
		p["every_months"] = 3
		p["lines"] = []map[string]any{line(e.books.Rent.Code, "15001.00"), line(accrued, "-15001.00")}
	})
	bad("a schedule of every 2 months", func(p map[string]any) { p["every_months"] = 2 })
	bad("an end before the start", func(p map[string]any) { p["ends_on"] = "2098-12-31" })
	bad("an auto-post field", func(p map[string]any) { p["auto_post"] = true })
	bad("a recurring invoice's mode", func(p map[string]any) { p["mode"] = "issue" })

	k.approve(made.str("id"), approver)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "recurring_journal:") || len(list()) != before+1 {
		t.Fatalf("the executed proposal: %s %s %s (%d templates, was %d)", pr.Status, pr.Reason, pr.Execution, len(list()), before)
	}
	var tpl map[string]any
	for _, it := range list() {
		if m := it.(map[string]any); m["name"] == "Monthly rent accrual" {
			tpl = m
		}
	}
	if tpl == nil || tpl["status"] != "active" {
		t.Fatalf("the recurring journal: %v", list())
	}
	undo := func(tok, id string) response {
		return e.call(http.MethodPost, "/v1/ai/proposal-undos", tok, map[string]any{"proposal_id": id, "reason": "Not needed"}, "Idempotency-Key", uuid.NewString())
	}
	_, outsider := k.roleToken("Aud Rt", authz.Auditor)
	if r := undo(outsider, made.str("id")); r.Status != http.StatusForbidden && r.Status != http.StatusNotFound {
		t.Errorf("an auditor undoing it: %d %s", r.Status, r.Raw)
	}
	if r := undo(approver, made.str("id")); r.Status != http.StatusCreated {
		t.Fatalf("the undo: %d %s", r.Status, r.Raw)
	}
	got := e.call(http.MethodGet, "/v1/recurring-journals/"+tpl["id"].(string), e.as(authz.Accountant), nil)
	if got.str("status") != "paused" {
		t.Errorf("the template after its undo: %s", got.Raw)
	}
	if again := undo(approver, made.str("id")); again.Status != http.StatusConflict || again.str("code") != CodeUndoAlready {
		t.Errorf("undoing twice: %d %s", again.Status, again.Raw)
	}

	// An account changed since the dry run makes the proposal Stale, and a template changed since its execution is not undone.
	second := k.propose(turn, "recur.create_template", map[string]any{"name": "Second", "every_months": 1, "day_of_month": 1, "starts_on": "2099-02-01",
		"lines": []map[string]any{line(e.books.Rent.Code, "10.00"), line(accrued, "-10.00")}})
	if second.Status != http.StatusCreated {
		t.Fatalf("a second proposal: %d %s", second.Status, second.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Get(ctx, e.books.Rent.ID)
		if err != nil {
			return err
		}
		_, err = u.Accounts().Update(ctx, a.ID, a.Version, storage.AccountChange{Name: "Office rent (renamed)", ParentID: a.ParentID, Group: a.Group, Active: a.Active, AccountAttributes: a.AccountAttributes})
		return err
	})
	k.approve(second.str("id"), approver)
	if st := k.execute(second.str("id"), 1); st.Status != storage.ProposalStale {
		t.Errorf("the second proposal after the account changed: %s %s", st.Status, st.Reason)
	}
}

// plan.propose_budget (Appendix E, FR-PLN-01; p1-ai-proposals task 4.1): the AI proposes a budget version, which is DRAFTED, not approved or recorded; a
// person other than the requester approves the proposal (AI-10), the executor drafts it once through the function a person's Draft a budget runs, and
// nothing posts. A line on a balance-sheet account, a year that is not a financial year's first day, a line with too few months and a field the task
// type does not name are refused. A draft is not undoable (design D4), so the review offers no undo.
// req: AI-10
// req: AI-11
// req: FR-PLN-01
// req: ai/task-catalogue plan.propose_budget Create a budget version
// asvs: V8.2.2, V8.3.1
func TestTheAIDraftsABudgetThatAPersonApproves(t *testing.T) {
	e := newEnv(t)
	k := newTaskKit(e, "plan.propose_budget")
	e.booksFrom()
	chart := e.budgetChart()
	acct, _ := k.roleToken("Acc Bg", authz.Accountant)
	_, approver := k.roleToken("Acc Bg 2", authz.Accountant) // the budget's right to draft is the Accountant's (Appendix D): another one approves
	turn := proposeTurn(e, acct.String())
	good := map[string]any{"name": "FY2027 budget", "kind": "original", "financial_year": "2027-01-01",
		"lines": []map[string]any{{"account": "4010", "amounts": months12("10000.00")}, {"account": "6200", "amounts": months12("3000.00")}}}
	budgets := func() int {
		items, _ := e.call(http.MethodGet, "/v1/budgets", e.as(authz.Controller), nil).Body["items"].([]any)
		return len(items)
	}
	before := budgets()
	made := k.propose(turn, "plan.propose_budget", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.HasPrefix(made.str("summary"), `Drafts the original budget "FY2027 budget" for the year from 2027-01-01 with 2 line(s)`) || budgets() != before {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	bad := func(what string, mutate func(p map[string]any)) {
		t.Helper()
		p := map[string]any{}
		for key, v := range good {
			p[key] = v
		}
		mutate(p)
		if r := k.propose(turn, "plan.propose_budget", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("%s made a proposal: %s", what, r.Raw)
		}
	}
	bad("a cash account", func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": "1010", "amounts": months12("1.00")}}
	})
	bad("a share capital account", func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": "3010", "amounts": months12("1.00")}}
	})
	bad("a year that starts mid-year", func(p map[string]any) { p["financial_year"] = "2027-02-01" })
	bad("eleven months", func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": "4010", "amounts": months12("1.00")[:11]}}
	})
	bad("a revised version revising nothing", func(p map[string]any) { p["kind"] = "revised" })
	bad("an account that is none", func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": "9999", "amounts": months12("1.00")}}
	})
	bad("an approval", func(p map[string]any) { p["approve"] = true })
	bad("a dimension value", func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": "4010", "amounts": months12("1.00"), "dimension_value_ids": []string{chart.sales}}}
	})

	k.approve(made.str("id"), approver)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "budget:") || budgets() != before+1 {
		t.Fatalf("the executed proposal: %s %s %s (%d budgets, was %d)", pr.Status, pr.Reason, pr.Execution, budgets(), before)
	}
	items := e.call(http.MethodGet, "/v1/budgets", e.as(authz.Controller), nil).Body["items"].([]any)
	if b := items[len(items)-1].(map[string]any); b["status"] != "draft" {
		t.Errorf("the drafted budget is not a draft: %v", b)
	}
	if r := e.call(http.MethodGet, "/v1/ai/proposals/"+made.str("id"), approver, nil); r.Body["can_undo"] == true {
		t.Errorf("a drafted budget offers an undo: %s", r.Raw)
	}
	_, auditor := k.roleToken("Aud Bg", authz.Auditor)
	_ = auditor
	if r := e.call(http.MethodPost, "/v1/ai/proposal-undos", approver, map[string]any{"proposal_id": made.str("id"), "reason": "x"}, "Idempotency-Key", uuid.NewString()); r.Status != http.StatusConflict || r.str("code") != CodeUndoNotUndoable {
		t.Errorf("undoing a draft: %d %s", r.Status, r.Raw)
	}
}
