//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// secretaryRun is a running run of the release's company secretary skill in e's organisation, acting for user.
func secretaryRun(e *env, user string) storage.AgentRun {
	e.t.Helper()
	skill, _ := agents.Default().Get("company-secretary")
	var run storage.AgentRun
	now := e.clock.Now()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "agent", ID: "company-secretary"}})
	if err := workerStore(e.t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		queued, _, err := u.AgentRuns().Queue(ctx, storage.NewAgentRun{Agent: skill.Name, SkillVersion: skill.Version, SkillSHA256: skill.SHA256,
			Trigger: "event:test:" + uuid.NewString(), SlotAt: now, ActsFor: uuid.MustParse(user), QueuedAt: now})
		if err == nil {
			run, err = u.AgentRuns().Start(ctx, queued.ID, queued.Version, now)
		}
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return run
}

// withAIRuns starts the AI service beside e's API, answering turns and runs: the real assistant over HTTP, calling the
// API's internal operations with its own token.
func withAIRuns(e *env) {
	e.t.Helper()
	prompt, err := assistant.DefaultPrompt(assistant.Agent)
	if err != nil {
		e.t.Fatal(err)
	}
	client := &assistant.Client{URL: e.srv.URL + "/internal/v1", ServiceToken: func(context.Context) (string, error) { return e.services.Token(ServiceAI) },
		HTTP: e.srv.Client()}
	orchestrator := &assistant.Assistant{Backend: client, Prompt: prompt}
	mux := http.NewServeMux()
	mux.Handle("POST /turns", assistant.Handler{Assistant: orchestrator, Callers: e.services})
	mux.Handle("POST /runs", assistant.RunHandler{Assistant: orchestrator, Callers: e.services})
	srv := httptest.NewServer(mux)
	e.t.Cleanup(srv.Close)
	e.api.deps.Assistant = &AssistantService{URL: srv.URL, Token: func(context.Context) (string, error) { return e.services.Token(assistant.Class) },
		HTTP: srv.Client()}
}

// A scheduled agent's run asks the model through the API alone (PRD-001 AI-64, AI-63, AI-14, rev 0.281; p1-scheduled-agents
// design D5 step 5, D6): the worker calls one internal operation with its own service token, which no other token opens
// and which opens nothing else. The API asks the AI service with the skill and the findings; the model reads and
// proposes with the run's token, as the Owner, its model calls routed for the agent; its words come back for the report;
// the run's token ends with the step. A run that is not running, or an agent paused, gets no step. The reasons people
// rejected the agent's proposals reach its next run, as data.
// req: AI-64
// req: AI-63
// req: AI-14
// asvs: V8.2.1, V8.2.2
func TestTheWorkerAsksTheModelForARun(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	enableSecretary(e, f)
	e.api.deps.Catalogue = fixtureCatalogue(t, "sec.create_task")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	enableTaskTypes(e, "sec.create_task", 1)
	connection := routable(e, "Secretary", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "company_secretary", Version: 1, Classes: []string{"business-figures", "personal-data"},
			MadeBy: "user:test", MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: connection, Model: "gpt-6"}}})
		return err
	})
	words := func(text string) ports.ModelResponse {
		b, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: text}}})
		return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}, StopReason: ports.StopEnd}
	}
	model := &scripted{answers: []ports.ModelResponse{
		{Parts: []ports.ModelPart{{Kind: ports.PartToolCall, CallID: "p1", Tool: "propose_sec_create_task",
			Input: json.RawMessage(`{"payload":{"note":"Book the AGM room"},"rationale":"The AGM is due.","evidence":[]}`)}}, StopReason: ports.StopToolCall},
		words("The GST return is due soon: file it before the end of January."),
		words("Nothing new needs you this week."),
	}}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	withAIRuns(e)
	worker := agents.APIModel{URL: e.srv.URL + "/internal/v1", Token: func(context.Context) (string, error) { return e.services.Token(agents.ModelClass) },
		HTTP: e.srv.Client()}
	findings := []agents.Finding{{Key: "calendar:gst_f5:2027-01-31", Status: agents.FindingDue, Title: "File the GST return", Rule: "gst.return-due",
		DueOn: "2027-01-31", Window: "14"}}
	ctx := context.Background()

	run := secretaryRun(e, f.ahkow)
	out, err := worker.Step(ctx, e.books.Org, run, findings)
	if err != nil || out.Text != "The GST return is due soon: file it before the end of January." || out.Model == "" || out.Tokens == 0 || out.Proposals != 1 {
		t.Fatalf("the run's model step: %+v %v", out, err)
	}
	first := model.requests[0]
	if !strings.Contains(first.System, "Company secretary assistant") || !strings.Contains(first.System, "## How you report") ||
		!strings.Contains(textOfRequest(first), "calendar:gst_f5:2027-01-31") {
		t.Errorf("the model was not told the skill and the findings: %s", first.System[:min(200, len(first.System))])
	}
	// The run's proposal, for the Owner, from the run; the token ended with the step.
	var pr storage.AIProposal
	var req storage.ApprovalRequest
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		ps, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Limit: 10})
		if err != nil || len(ps) != 1 || ps[0].ApprovalRequest == nil {
			t.Fatalf("the run's proposals: %+v %v", ps, err)
		}
		pr = ps[0]
		req, err = u.Approvals().Get(ctx, *pr.ApprovalRequest)
		if _, err := u.AIGrants().Classes(ctx, run.ID, e.clock.Now()); err == nil {
			t.Error("the run's token outlived its step")
		}
		return err
	})
	if pr.Run == nil || *pr.Run != run.ID || pr.Agent != "company_secretary" || pr.Prompt != "company_secretary@1" || req.RequesterMFAMethod != runMFA {
		t.Fatalf("the run's proposal: %+v, its request %+v", pr, req)
	}

	// Rejected, its reason reaches the agent's next run, quoted as data.
	weiling := f.as(f.weiling)
	shown := e.call(http.MethodGet, "/v1/approvals/"+req.ID.String(), weiling, nil)
	if d := f.decide(weiling, req.ID.String(), "reject", map[string]any{"envelope_hash": shown.str("envelope_hash"), "reason": "The AGM is not until June"}); d.Status != http.StatusOK {
		t.Fatalf("rejecting the run's proposal: %d %s", d.Status, d.Raw)
	}
	finish := func(r storage.AgentRun) {
		t.Helper()
		if err := workerStore(t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AgentRuns().Fail(ctx, r.ID, r.Version, "the test ends it", e.clock.Now())
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	finish(run)
	next := secretaryRun(e, f.ahkow)
	if out, err := worker.Step(ctx, e.books.Org, next, findings); err != nil || out.Text != "Nothing new needs you this week." {
		t.Fatalf("the next run's step: %+v %v", out, err)
	}
	if last := textOfRequest(model.requests[len(model.requests)-1]); !strings.Contains(last, "The AGM is not until June") || !strings.Contains(last, "untrusted_text") {
		t.Errorf("the rejection did not reach the next run as data: %s", last)
	}

	// No step for a run that ended, nor for a paused agent; no other token opens the step, and the worker's opens nothing else.
	if _, err := worker.Step(ctx, e.books.Org, run, findings); err == nil || !strings.Contains(err.Error(), CodeAgentRunStep) {
		t.Errorf("a step for a run that ended: %v", err)
	}
	service, _ := e.services.Token(ServiceAI)
	path := "/internal/v1/agent-runs/" + next.ID.String() + "/model"
	if r := e.call(http.MethodPost, path, "", map[string]any{"organisation_id": e.books.Org.String(), "findings": []any{}}, ServiceTokenHeader, service); r.Status != http.StatusUnauthorized {
		t.Errorf("the AI service asking for a run's step: %d %s", r.Status, r.Raw)
	}
	mine, _ := e.services.Token(agents.ModelClass)
	if r := e.call(http.MethodPost, "/internal/v1/tool-calls", "", map[string]any{"tool": "metrics.query", "input": map[string]any{}}, ServiceTokenHeader, mine); r.Status != http.StatusUnauthorized {
		t.Errorf("the worker's token calling a tool: %d %s", r.Status, r.Raw)
	}
	if _, err := worker.Step(ctx, uuid.New(), next, findings); err == nil || !strings.Contains(err.Error(), CodeNotFound) {
		t.Errorf("a step naming another organisation: %v", err)
	}
	pause := "/v1/organisation/settings/agents.company_secretary.paused"
	ahkow := f.as(f.ahkow)
	if r := e.call(http.MethodPatch, pause, ahkow, map[string]any{"value": "true"}, "If-Match", e.call(http.MethodGet, pause, ahkow, nil).Header.Get("ETag")); r.Status >= 300 {
		t.Fatalf("pausing the agent: %d %s", r.Status, r.Raw)
	}
	if _, err := worker.Step(ctx, e.books.Org, next, findings); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Errorf("a step for a paused agent: %v", err)
	}
}

// textOfRequest is every text a model request's messages carry.
func textOfRequest(r ports.ModelRequest) string {
	var b strings.Builder
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}
