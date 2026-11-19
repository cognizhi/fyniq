//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// exp.draft_claim (Appendix E, FR-DOC-12; p1-ai-proposals task 1.2): the AI proposes a draft claim of the requester's own receipts; the dry run
// shows the lines, the total and how the input tax is decided and makes nothing; the requester approves it and the executor makes the claim
// as them, once, submitting nothing; another person who may claim cannot approve it into a claim of theirs; a category that is not one, mileage,
// a GST code that is not a purchase code, a claimant in the payload and a person who may not claim are refused. An unknown document fact
// claims no input tax (G4-029).
// req: ai/task-catalogue exp.draft_claim Draft an expense claim
// req: AI-10
// asvs: V8.2.2
func TestTheAIDraftsAnExpenseClaimThatTheRequesterApproves(t *testing.T) {
	e := newEnv(t)
	e.claimSetup()
	k := newTaskKit(e, "exp.draft_claim")
	priya, priyaTok := k.roleToken("Priya Claims", authz.Employee, authz.Approver)
	_, managerTok := k.roleToken("Lee Claims", authz.Employee, authz.Approver)
	turn := proposeTurn(e, priya.String())
	good := map[string]any{"title": "Client visit", "currency": "SGD", "lines": []map[string]any{
		{"date": "2026-10-14", "merchant": "ComfortDelGro", "category": "Local travel", "description": "Taxi to Acme", "amount": "27.30", "gst_code": "NR", "gst": "0.00"}}}
	claims := func() int {
		items, _ := e.call(http.MethodGet, "/v1/expense-claims", priyaTok, nil).Body["items"].([]any)
		return len(items)
	}
	before := claims()

	made := k.propose(turn, "exp.draft_claim", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.HasPrefix(made.str("summary"), "Drafts your expense claim ") ||
		!strings.Contains(made.str("summary"), "SGD 27.30") || claims() != before {
		t.Fatalf("the proposal: %d %s (%d claims, was %d)", made.Status, made.Raw, claims(), before)
	}
	bad := func(mutate func(p map[string]any)) {
		t.Helper()
		p := map[string]any{}
		for key, v := range good {
			p[key] = v
		}
		mutate(p)
		if r := k.propose(turn, "exp.draft_claim", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("a payload that should be refused made a proposal: %s", r.Raw)
		}
	}
	line := func(f func(l map[string]any)) []map[string]any {
		l := map[string]any{"date": "2026-10-14", "merchant": "X", "category": "Local travel", "amount": "1.00", "gst_code": "NR", "gst": "0.00"}
		f(l)
		return []map[string]any{l}
	}
	bad(func(p map[string]any) { p["lines"] = line(func(l map[string]any) { l["category"] = "Mileage" }) }) // mileage is the person's own
	bad(func(p map[string]any) { p["lines"] = line(func(l map[string]any) { l["category"] = "Yachts" }) })
	bad(func(p map[string]any) { p["lines"] = line(func(l map[string]any) { l["gst_code"] = "SR" }) }) // a sales code
	bad(func(p map[string]any) { p["lines"] = line(func(l map[string]any) { l["date"] = "last Tuesday" }) })
	bad(func(p map[string]any) { p["claimant_id"] = priya.String() }) // the payload has no claimant
	if x := k.propose(proposeTurn(e, e.joinedIn(e.books.Org, "Cleo Bookkeeper", "bookkeeper").String()), "exp.draft_claim", good); x.Status < 400 && x.str("status") != storage.ProposalInvalid {
		t.Errorf("a claim for someone who may not claim: %s", x.Raw)
	}

	// The requester approves it, and the executor makes their claim once, as a draft.
	k.approve(made.str("id"), priyaTok)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "expense_claim:") || claims() != before+1 {
		t.Fatalf("the executed proposal: %s %s (%d claims, was %d)", pr.Status, pr.Execution, claims(), before)
	}
	items := e.call(http.MethodGet, "/v1/expense-claims", priyaTok, nil).Body["items"].([]any)
	if c := items[0].(map[string]any); c["status"] != "draft" || c["claimant_id"] != priya.String() && c["claimant"] == nil {
		t.Errorf("the drafted claim: %v", c)
	}

	// Another person who may claim cannot approve it into a claim of theirs: it fails and makes nothing.
	second := k.propose(turn, "exp.draft_claim", good)
	if second.Status != http.StatusCreated {
		t.Fatalf("a second proposal: %d %s", second.Status, second.Raw)
	}
	k.approve(second.str("id"), managerTok)
	if pr2 := k.execute(second.str("id"), 1); pr2.Status != storage.ProposalFailed || !strings.Contains(pr2.Reason, "claimant's own") || claims() != before+1 {
		t.Errorf("the second proposal approved by another: %s %s", pr2.Status, pr2.Reason)
	}
}
