//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// A proposal waits for a person (AI-89, AI-P4, AI-P5, AI-06, AI-10; design D4): Pending, it is at once an ai_proposal
// request, made by the person who asked and signing the payload's hash. The task type's approver roles who could do it
// themselves see and sign it; the person who asked may sign their own tier-1 proposal; nobody else sees it, and the AI
// never does. Approved, the proposal is Approved with exactly the payload approved and queued for the executor;
// rejected, it is Rejected with the reason. No policy removes or narrows its step, and an envelope not shown is refused.
// req: AI-89
// req: AI-P4
// req: AI-P5
// req: AI-06
// req: AI-10
// req: CTL-AI-03
// asvs: V8.2.1, V8.2.2, V8.3.1
func TestAProposalWaitsForAPersonsApproval(t *testing.T) {
	e := newEnv(t)
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Staff", "roles": []string{"employee"}}).str("code")
	f := foundersIn(e)
	staffID := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "staff-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Staff"}).str("user_id")
	aiSwitchedOn(e)
	e.api.deps.Catalogue = fixtureCatalogue(t, "test.annotate_note")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	enableTaskTypes(e, "test.annotate_note", 1)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	staff := e.principal(authn.Principal{Subject: "staff", UserID: uuid.MustParse(staffID), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Employee}, StepUpAt: e.clock.Now()})
	turn := proposeTurn(e, f.ahkow)
	proposeNote := func(note string) (storage.AIProposal, storage.ApprovalRequest) {
		t.Helper()
		r := e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "test.annotate_note", "payload": map[string]any{"note": note},
			"rationale": "It is due.", "model": "claude-opus-5-5", "prompt": "chat@3"}, ServiceTokenHeader, service)
		if r.Status != http.StatusCreated || r.str("status") != storage.ProposalPending {
			t.Fatalf("proposing: %d %s", r.Status, r.Raw)
		}
		var pr storage.AIProposal
		var req storage.ApprovalRequest
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if pr, err = u.AIProposals().Get(ctx, uuid.MustParse(r.str("id"))); err != nil || pr.ApprovalRequest == nil {
				return err
			}
			req, err = u.Approvals().Get(ctx, *pr.ApprovalRequest)
			return err
		})
		if pr.ApprovalRequest == nil || req.ObjectType != approval.ObjectAIProposal || req.Operation != approval.OpAIProposalExecute ||
			req.RequestedBy.String() != f.ahkow || req.RequesterMFAMethod != "ai_turn" || req.ObjectKey != pr.ID.String() || !req.ExpiresAt.Equal(pr.ExpiresAt) {
			t.Fatalf("the request: %+v of %+v", req, pr)
		}
		return pr, req
	}

	// The request, as the people who may sign it see it: its envelope holds the payload's hash.
	pr, req := proposeNote("Book the room")
	path := "/v1/approvals/" + req.ID.String()
	mine := e.call(http.MethodGet, path, ahkow, nil)
	if mine.Status != http.StatusOK || mine.Body["you_may_approve"] != true || mine.str("object_type") != "ai_proposal" {
		t.Fatalf("the requester's view: %d %s", mine.Status, mine.Raw)
	}
	if v := e.call(http.MethodGet, path, weiling, nil); v.Status != http.StatusOK || v.Body["you_may_approve"] != true {
		t.Fatalf("another Owner's view: %d %s", v.Status, v.Raw)
	}
	e.problemIs(e.call(http.MethodGet, path, staff, nil), "getApproval", 403, CodeForbidden) // no right on the approvals at all
	if r := e.call(http.MethodGet, "/v1/approvals", turn, nil); r.Status != http.StatusForbidden {
		t.Fatalf("the AI listing the approvals: %d %s", r.Status, r.Raw)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(req.Envelope), &envelope); err != nil {
		t.Fatal(err)
	}
	change, _ := envelope["change"].(map[string]any)
	if change["payload_sha256"] != pr.PayloadSHA256 || change["task_type"] != "test.annotate_note" || change["proposal"] != pr.ID.String() {
		t.Fatalf("the envelope: %v", envelope)
	}

	// No policy removes, narrows or automates its step.
	e.problemIs(e.call(http.MethodPatch, "/v1/approval-policies/ai_proposal", steppedUp(e, e.as(authz.Administrator)), map[string]any{"steps": []map[string]any{{"quorum": 1, "roles": []string{"controller"}}}},
		"If-Match", `"0"`), "changeApprovalPolicy", 409, approval.CodeFixed)

	// An envelope not shown is refused; the person who asked signs their own tier-1 proposal.
	e.problemIs(f.decide(ahkow, req.ID.String(), "approve", map[string]any{"envelope_hash": strings.Repeat("0", 64)}), "approveApproval", 409, approval.CodeChanged)
	if d := f.decide(ahkow, req.ID.String(), "approve", hashOf(mine)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("the requester approving their tier-1 proposal: %d %s", d.Status, d.Raw)
	}
	var done storage.AIProposal
	var queued *storage.OutboxRow
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if done, err = u.AIProposals().Get(ctx, pr.ID); err != nil {
			return err
		}
		queued, err = u.Outbox().ByKey(ctx, proposal.ExecutorKind, pr.ID.String())
		return err
	})
	if done.Status != storage.ProposalApproved || done.ApprovedPayload != pr.Payload || done.ApprovedSHA256 != pr.PayloadSHA256 ||
		done.DecidedBy == nil || done.DecidedBy.String() != f.ahkow {
		t.Fatalf("approved: %+v", done)
	}
	if queued == nil || queued.Class != storage.ClassExecutor {
		t.Fatalf("queued for the executor: %+v", queued)
	}
	if items, _ := e.call(http.MethodGet, "/v1/contacts", e.as(authz.Owner), nil).Body["items"].([]any); len(items) != 0 {
		t.Fatal("the approval executed it in the API")
	}

	// Rejected, with the reason, by another who may sign it.
	pr, req = proposeNote("Book another room")
	shown := e.call(http.MethodGet, "/v1/approvals/"+req.ID.String(), weiling, nil)
	if d := f.decide(weiling, req.ID.String(), "reject", map[string]any{"envelope_hash": shown.str("envelope_hash"), "reason": "Not this year"}); d.Status != http.StatusOK {
		t.Fatalf("rejecting: %d %s", d.Status, d.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		done, err = u.AIProposals().Get(ctx, pr.ID)
		return err
	})
	if done.Status != storage.ProposalRejected || done.Reason != "Not this year" {
		t.Fatalf("rejected: %+v", done)
	}
}
