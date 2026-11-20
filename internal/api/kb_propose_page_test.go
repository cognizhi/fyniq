//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// kb.propose_page (AI-59, Appendix E; p2-ai-expansion): the AI proposes a know-how page from a conversation; an Owner or Administrator other than the
// requester approves it (AI-10); the executor writes one general NOTE under "proposed.", and nothing is published or compiled. The red-team case: a
// text that carries the organisation's figures (an amount, a percentage, a decimal, an ID, a long number) never becomes a note, whatever the model wrote;
// a key that is taken or already prefixed, a policy kind and an unnamed field are refused too.
// req: AI-59
// req: AI-10
// req: AI-11
// req: ai/task-catalogue kb.propose_page Propose a know-how page
// asvs: V8.2.2, V8.3.1
func TestTheAIProposesAKnowHowPageWithNoFigureAndAPersonApprovesIt(t *testing.T) {
	e := newEnv(t)
	k := newTaskKit(e, "kb.propose_page")
	asker, _ := k.roleToken("Adm Kb", authz.Administrator)
	_, approver := k.roleToken("Own Kb", authz.Owner)
	turn := proposeTurn(e, asker.String())
	notes := func() []any {
		items, _ := e.call(http.MethodGet, "/v1/knowledge-base/notes", e.as(authz.Administrator), nil).Body["items"].([]any)
		return items
	}
	before := len(notes())
	good := map[string]any{"key": "how-we-file-claims", "title": "How we file expense claims", "body": "Attach the receipt, choose the category and submit within 14 days; your line manager approves it first."}
	made := k.propose(turn, "kb.propose_page", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.Contains(made.str("summary"), "Adds the knowledge-base note") || len(notes()) != before {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	bad := func(what string, mutate func(p map[string]any)) {
		t.Helper()
		p := map[string]any{}
		for key, v := range good {
			p[key] = v
		}
		mutate(p)
		if r := k.propose(turn, "kb.propose_page", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("%s made a proposal: %s", what, r.Raw)
		}
	}
	// The red-team case: the organisation's figures never become a page.
	bad("an amount of money", func(p map[string]any) {
		p["body"] = "Last month we spent S$4,800 on hosting; claims are paid from that."
	})
	bad("a percentage", func(p map[string]any) { p["body"] = "Gross margin is 61% so claims are cheap." })
	bad("a decimal", func(p map[string]any) { p["body"] = "Burn is 2.4 times revenue." })
	bad("a long number", func(p map[string]any) { p["title"] = "We have 48000 users" })
	bad("an ID", func(p map[string]any) { p["body"] = "See invoice 01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90." })
	bad("a key that is already prefixed", func(p map[string]any) { p["key"] = "proposed.how" })
	bad("a key with capitals", func(p map[string]any) { p["key"] = "How We" })
	bad("a kind field", func(p map[string]any) { p["kind"] = "policy" })
	bad("a data class", func(p map[string]any) { p["data_class"] = "payroll" })
	bad("an empty body", func(p map[string]any) { p["body"] = " " })

	k.approve(made.str("id"), approver)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "kb_note:") || len(notes()) != before+1 {
		t.Fatalf("the executed proposal: %s %s %s", pr.Status, pr.Reason, pr.Execution)
	}
	var note map[string]any
	for _, x := range notes() {
		if m := x.(map[string]any); m["key"] == "proposed.how-we-file-claims" {
			note = m
		}
	}
	if note == nil || note["kind"] != "note" || note["data_class"] != "general" {
		t.Fatalf("the note: %v", notes())
	}
	// A key taken since the dry run, or taken at the dry run, is not overwritten.
	if r := k.propose(turn, "kb.propose_page", good); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
		t.Errorf("a second proposal for the same key: %s", r.Raw)
	}
}
