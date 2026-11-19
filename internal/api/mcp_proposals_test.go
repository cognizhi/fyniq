//go:build local

package api

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An AI app proposes over MCP, and a person approves in Fyniq (AI-16, AI-42, AI-34, AI-89 rev 0.298; the owner's decision of 19 Nov
// 2026, option B). The app's token holds the scope of one task type and the person's opt-in lets it in; the proposal is the person's
// own right to do it, checked and dry-run as for a turn's, Pending, with its request asked by the app's delegation, not by a live turn.
// The delegated token never signs: it reaches no approval at all. Another member approves it; the person it is for signs it only on a
// step-up taken after the app proposed it, and not at all for a proposal of tier 2. Zero proposals out of the catalogue, however the
// call is made.
// req: AI-16
// req: AI-42
// req: AI-34
// req: AI-89
// req: AI-10
// req: CTL-MCP-01
// req: QA-MCP-01
// asvs: V8.2.2, V8.3.1, V8.1.1, V10.3.2
func TestAnAIAppProposesAndAPersonApproves(t *testing.T) {
	f := newFounders(t)
	e := f.e
	aiSwitchedOn(e)
	enableTaskTypes(e, "sec.create_task", 1)
	service, err := e.services.Token(ServiceMCP)
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.MustParse(f.ahkow)
	delegated := func(scopes ...string) string {
		return e.principal(authn.Principal{Via: authn.ViaToken, Subject: user.String(), UserID: user, Client: "mcp-delegation", Class: authn.ClassAgent,
			Organisation: e.books.Org, Roles: []authz.Role{authz.Owner, authz.Controller}, Scopes: scopes})
	}
	call := func(token, app, tool string, input map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", token, map[string]any{"app": app, "tool": tool, "input": input}, ServiceTokenHeader, service)
	}
	code := func(r response, status int, want string) {
		t.Helper()
		if r.Status != status || r.str("code") != want {
			t.Errorf("want %d %s: %d %s", status, want, r.Status, r.Raw)
		}
	}
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	versions := map[string]int{}
	set := func(key, was, value string) {
		t.Helper()
		versions[key]++
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Settings().Record(ctx, key, was, value, from, versions[key])
			return err
		})
	}
	set("features.mcp", "false", "true")
	set("mcp.clients", "", "claude-code")
	got := e.call(http.MethodGet, "/v1/me/mcp", f.as(f.ahkow), nil)
	if r := e.call(http.MethodPut, "/v1/me/mcp", f.as(f.ahkow), map[string]any{"opted_in": true}, "If-Match", got.str("etag")); r.Status != 200 {
		t.Fatalf("opting in: %d %s", r.Status, r.Raw)
	}
	const tool = "propose_sec_create_task"
	input := func(key string) map[string]any {
		return map[string]any{"payload": map[string]any{"finding_key": key, "title": "Book the room for the AGM", "owner_role": "company_secretary", "due_on": "2027-06-30"},
			"rationale": "The AGM must be held by 30 June 2027."}
	}
	proposes := delegated("fyniq.read.proposals", "fyniq.propose.sec.create_task")

	// The scope of the task type, and nothing else, lets an app propose it; the read scope does not.
	r := call(delegated("fyniq.read.reports", "fyniq.read.proposals"), "claude-code", tool, input("agent:agm-room-2027"))
	code(r, 403, CodeMCPScope)
	if r.Header.Get(headerNeedScopes) != "fyniq.propose.sec.create_task" {
		t.Errorf("the scope the call needs: %q", r.Header.Get(headerNeedScopes))
	}
	code(call(delegated("fyniq.propose.sec.draft_reminder"), "claude-code", tool, input("agent:agm-room-2027")), 403, CodeMCPScope)
	// A tool that is not one: another type, a verb that decides, and the task type smuggled in as an input.
	for _, name := range []string{"propose_sec_made_up", "propose_ledger_post", "proposals.approve", "proposals.execute", "approve_proposal", "proposals.withdraw"} {
		if r := call(proposes, "claude-code", name, input("agent:x")); r.Status != 422 || r.str("code") != CodeInvalid {
			t.Errorf("%s is a tool the contract names: %d %s", name, r.Status, r.Raw) // the contract's enum is the manifest's: no value, no call
		}
	}
	smuggled := input("agent:agm-room-2027")
	smuggled["task_type"] = "sec.draft_document"
	code(call(proposes, "claude-code", tool, smuggled), 422, CodeToolInput)
	// A task type that is off for the organisation is refused (the Owner turns types on, ai.task_types).
	code(call(delegated("fyniq.propose.sec.draft_reminder"), "claude-code", "propose_sec_draft_reminder",
		map[string]any{"payload": map[string]any{"to_role": "owner", "subject": "AGM notice", "message": "Send the notice.", "obligation": "calendar:agm:2027"}, "rationale": "x"}),
		403, CodeProposeNotAllowed)

	// Pending: the person's own right, checked as for a turn's, the platform's words for what it would do.
	made := call(proposes, "claude-code", tool, input("agent:agm-room-2027"))
	if made.Status != 200 {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	content, _ := made.Body["content"].(map[string]any)
	id, _ := content["id"].(string)
	if content["status"] != storage.ProposalPending || id == "" || content["summary"] != `Creates the task "Book the room for the AGM" for the company secretary, due 30 June 2027` {
		t.Fatalf("the proposal: %s", made.Raw)
	}
	var pr storage.AIProposal
	var req storage.ApprovalRequest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if pr, err = u.AIProposals().Get(ctx, uuid.MustParse(id)); err != nil {
			return err
		}
		req, err = u.Approvals().Get(ctx, *pr.ApprovalRequest)
		return err
	})
	// The AI-89 requester rule: asked by the person, authenticated by the app's delegation, never by a live turn or run.
	if pr.MCPApp != "claude-code" || pr.Agent != "mcp" || pr.Turn != nil || pr.Run != nil || pr.RequestedBy != user ||
		req.RequesterMFAMethod != "mcp_token" || req.RequestedBy != user || !strings.HasPrefix(pr.Model, "claude-code") || pr.Prompt != "mcp@1" {
		t.Errorf("the proposal and its request: %+v %+v", pr, req)
	}

	// An agent token reaches no approval: not the person's own, not by the delegated token, with or without the service token.
	for _, how := range []string{"approve", "reject"} {
		body := map[string]any{"envelope_hash": hex.EncodeToString(req.EnvelopeHash), "reason": "no"}
		for name, r := range map[string]response{
			"the delegated token":               e.call(http.MethodPost, "/v1/approvals/"+req.ID.String()+"/"+how, proposes, body, "If-Match", `"1"`),
			"the delegated token and a service": e.call(http.MethodPost, "/v1/approvals/"+req.ID.String()+"/"+how, proposes, body, "If-Match", `"1"`, ServiceTokenHeader, service),
		} {
			if r.Status != http.StatusForbidden && r.Status != http.StatusUnauthorized {
				t.Errorf("%s %s: %d %s", name, how, r.Status, r.Raw)
			}
		}
	}

	// The person it is for: a step-up from before it was proposed does not sign; one taken after does (tier 1).
	e.clock.Advance(time.Minute)
	before := e.principal(authn.Principal{Subject: "founder-" + f.ahkow, UserID: user, Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Owner, authz.Controller}, StepUpAt: pr.ProposedAt.Add(-time.Second)})
	shown := e.call(http.MethodGet, "/v1/approvals/"+req.ID.String(), before, nil)
	if shown.Status != 200 {
		t.Fatalf("the person's view: %d %s", shown.Status, shown.Raw)
	}
	code(f.decide(before, req.ID.String(), "approve", hashOf(shown)), 403, CodeStepUpRequired)
	fresh := f.as(f.ahkow) // a step-up now, after the app proposed it
	if r := f.decide(fresh, req.ID.String(), "approve", hashOf(shown)); r.Status != 200 || r.str("status") != "approved" {
		t.Errorf("the person on a fresh step-up: %d %s", r.Status, r.Raw)
	}

	// Another member approves without that: a second proposal, approved by Wei Ling on any step-up of the window.
	second := call(proposes, "claude-code", tool, input("agent:agm-notice-2027"))
	secondID, _ := second.Body["content"].(map[string]any)["id"].(string)
	var secondReq storage.ApprovalRequest
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		p2, err := u.AIProposals().Get(ctx, uuid.MustParse(secondID))
		if err != nil {
			return err
		}
		secondReq, err = u.Approvals().Get(ctx, *p2.ApprovalRequest)
		return err
	})
	other := f.as(f.weiling)
	seen := e.call(http.MethodGet, "/v1/approvals/"+secondReq.ID.String(), other, nil)
	if r := f.decide(other, secondReq.ID.String(), "approve", hashOf(seen)); r.Status != 200 || r.str("status") != "approved" {
		t.Errorf("another member approving: %d %s", r.Status, r.Raw)
	}

	// The reads: the person's own proposals this app made, and no one else's.
	list := call(proposes, "claude-code", "proposals.list", map[string]any{})
	if rows, _ := list.Body["content"].(map[string]any)["proposals"].([]any); list.Status != 200 || len(rows) != 2 {
		t.Errorf("the list: %d %s", list.Status, list.Raw)
	}
	if one := call(proposes, "claude-code", "proposals.get", map[string]any{"id": id}); one.Status != 200 || one.Body["content"].(map[string]any)["status"] != "approved" {
		t.Errorf("the proposal's status: %d %s", one.Status, one.Raw)
	}
	code(call(proposes, "claude-code", "proposals.get", map[string]any{"id": uuid.NewString()}), 404, CodeNotFound)
	code(call(delegated("fyniq.read.reports"), "claude-code", "proposals.list", map[string]any{}), 403, CodeMCPScope)

	// The organisation's limit of proposals an hour: set to the two that are made, the third waits.
	set("mcp.proposals_per_hour", "", "2")
	limited := call(proposes, "claude-code", tool, input("agent:agm-minutes-2027"))
	code(limited, 429, CodeMCPLimit)
	if limited.Header.Get("Retry-After") == "" {
		t.Error("the limit does not say when to ask again")
	}
	// A switch off stops the next proposal at once, as it does a read.
	set("features.mcp", "true", "false")
	code(call(proposes, "claude-code", tool, input("agent:agm-agenda-2027")), 403, "MCP-GATE-01")
}
