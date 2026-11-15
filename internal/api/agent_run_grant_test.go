//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// runningRun is a running run of the company secretary's in e's organisation, acting for user, as the worker starts it.
func runningRun(e *env, user string) storage.AgentRun {
	e.t.Helper()
	var run storage.AgentRun
	now := e.clock.Now()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "agent", ID: "company-secretary"}})
	if err := workerStore(e.t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		queued, _, err := u.AgentRuns().Queue(ctx, storage.NewAgentRun{Agent: "company-secretary", SkillVersion: 1, SkillSHA256: strings.Repeat("ab", 32),
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

// A scheduled agent's run reads and proposes as the Owner it acts for, within its skill alone (PRD-001 AI-63, AI-89 rev
// 0.281; p1-scheduled-agents design D6): its token calls only the skill's tools and proposes only the skill's outputs
// that are on; its proposal comes from the run, is asked for that Owner authenticated by the run (ai_run), and the
// Owner may sign it at tier 1; the audit names the agent, on behalf of the Owner. A run that is not running gets no
// token, and once its grant ends, the token reads nothing.
// req: AI-63
// req: AI-89
// asvs: V8.2.1, V8.2.2
func TestARunReadsAndProposesWithinItsSkill(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	e.api.deps.Catalogue = fixtureCatalogue(t, "test.annotate_note", "test.tag_note")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}, "TagNote": noteCommand{}}
	enableTaskTypes(e, "test.annotate_note,test.tag_note", 1)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	skill := agents.Skill{Name: "company-secretary", Version: 1, Tools: []string{"metrics.query", "packs.get", "kb.nothing"},
		Outputs: []string{"run_report", "test.annotate_note"}}
	run := runningRun(e, f.ahkow)
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueRunGrant(ctx, u, e.books.Org, run, skill)
		return err
	})
	var grant storage.AIGrantHolder
	if org, hash, ok := authn.ParseAIToken(token); ok {
		grant, err = e.store.AIGrant(context.Background(), org, hash, e.clock.Now())
	}
	if err != nil || grant.Run != run.ID || grant.UserID.String() != f.ahkow || !grant.Propose || strings.Join(grant.Tools, ",") != "metrics.query,packs.get" ||
		strings.Join(grant.TaskTypes, ",") != "test.annotate_note" {
		t.Fatalf("the run's grant: %+v %v", grant, err)
	}
	tool := func(name string, input map[string]any) response {
		return e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": name, "input": input}, ServiceTokenHeader, service)
	}
	propose := func(taskType, note string) response {
		return e.call(http.MethodPost, "/internal/v1/proposals", token, map[string]any{"task_type": taskType, "payload": map[string]any{"note": note},
			"rationale": "The AGM is due.", "evidence": []map[string]any{}, "model": "claude-opus-5-5", "prompt": "company_secretary@1"},
			ServiceTokenHeader, service)
	}

	// Its skill's tools, as the Owner; no other.
	if r := tool("metrics.query", map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"}); r.Status != http.StatusOK {
		t.Fatalf("the run reading with its skill's tool: %d %s", r.Status, r.Raw)
	}
	if r := tool("records.search", map[string]any{"kind": "invoice", "query": "x"}); r.Status != http.StatusForbidden || r.problemCode() != CodeToolGrant {
		t.Errorf("the run calling a tool its skill does not name: %d %s", r.Status, r.Raw)
	}

	// Its skill's outputs that are on; no other, even one on for the organisation.
	if r := propose("test.tag_note", "Tag it"); r.Status != http.StatusForbidden || r.problemCode() != CodeProposeNotAllowed {
		t.Errorf("the run proposing a task type its skill does not output: %d %s", r.Status, r.Raw)
	}
	made := propose("test.annotate_note", "Book the room")
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending {
		t.Fatalf("the run's proposal: %d %s", made.Status, made.Raw)
	}
	var pr storage.AIProposal
	var req storage.ApprovalRequest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if pr, err = u.AIProposals().Get(ctx, uuid.MustParse(made.str("id"))); err == nil && pr.ApprovalRequest != nil {
			req, err = u.Approvals().Get(ctx, *pr.ApprovalRequest)
		}
		return err
	})
	if pr.Run == nil || *pr.Run != run.ID || pr.Turn != nil || pr.Agent != "company_secretary" || pr.RequestedBy.String() != f.ahkow ||
		req.RequesterMFAMethod != runMFA || req.RequestedBy.String() != f.ahkow {
		t.Fatalf("the run's proposal and its request: %+v %+v", pr, req)
	}
	// The Owner it acts for signs it, at tier 1.
	ahkow := steppedUp(e, f.as(f.ahkow))
	view := e.call(http.MethodGet, "/v1/approvals/"+req.ID.String(), ahkow, nil)
	if d := f.decide(ahkow, req.ID.String(), "approve", hashOf(view)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Errorf("the Owner approving their agent's tier-1 proposal: %d %s", d.Status, d.Raw)
	}

	// The audit names the agent, on behalf of the Owner.
	var named bool
	if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 100000)
		for _, ev := range events {
			var b struct {
				Action, Object string
				Actor          struct{ Type, ID string }
				OnBehalfOf     *struct{ Type, ID string } `json:"on_behalf_of"`
			}
			_ = json.Unmarshal([]byte(ev.Body), &b)
			if b.Action == "ai_proposal.proposed" || b.Action == "ai_proposal.pending" {
				named = b.Actor.Type == "ai" && b.Actor.ID == "company_secretary" && b.OnBehalfOf != nil && b.OnBehalfOf.ID == f.ahkow
				if !named {
					t.Errorf("%s audited as %+v on behalf of %+v", b.Action, b.Actor, b.OnBehalfOf)
				}
			}
		}
		return err
	}); err != nil || !named {
		t.Errorf("the run's proposal's audit: named %v, %v", named, err)
	}

	// Once the run's grant ends, its token reads nothing; a run that is not running gets none.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error { return e.api.endAIGrant(ctx, u, run.ID) })
	if r := tool("metrics.query", map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"}); r.Status != http.StatusUnauthorized {
		t.Errorf("the run's ended token: %d %s", r.Status, r.Raw)
	}
	queued := run
	queued.Status = storage.AgentRunQueued
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := e.api.issueRunGrant(ctx, u, e.books.Org, queued, skill); err == nil {
			t.Error("a grant for a run that is not running")
		}
		return nil
	})
}
