//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// A revision supersedes the proposal it replaces (§9.5; p1-ai-proposals-core task 8.2, design D9). The AI may revise
// only a proposal made by a turn for the same person, of the same task type, still waiting. Once the revision is Pending,
// the earlier proposal is Superseded by it and its approval request is void in the same transaction, audited, so no one
// can sign it. A revision that is Invalid leaves the earlier proposal waiting.
// req: ai/proposals Proposal lifecycle
// req: AI-89
// asvs: V8.2.2
func TestARevisionSupersedesTheProposalItReplaces(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	e.api.deps.Catalogue = fixtureCatalogue(t, "test.annotate_note", "test.tag_note")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	enableTaskTypes(e, "test.annotate_note,test.tag_note", 1)
	propose := func(user, taskType string, payload map[string]any, revises string) response {
		t.Helper()
		service, err := e.services.Token(ServiceAI)
		if err != nil {
			t.Fatal(err)
		}
		body := map[string]any{"task_type": taskType, "payload": payload, "rationale": "x", "model": "m", "prompt": "chat@3"}
		if revises != "" {
			body["revises"] = revises
		}
		return e.call(http.MethodPost, "/internal/v1/proposals", proposeTurn(e, user), body, ServiceTokenHeader, service)
	}
	get := func(id string) (p storage.AIProposal) {
		t.Helper()
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			p, err = u.AIProposals().Get(ctx, uuid.MustParse(id))
			return err
		})
		return p
	}
	weiling := f.as(f.weiling)

	first := propose(f.ahkow, "test.annotate_note", map[string]any{"note": "Book the room"}, "")
	if first.Status != http.StatusCreated || first.str("status") != storage.ProposalPending {
		t.Fatalf("the first proposal: %d %s", first.Status, first.Raw)
	}
	earlier := get(first.str("id"))
	shown := e.call(http.MethodGet, "/v1/approvals/"+earlier.ApprovalRequest.String(), weiling, nil)

	// A revision the checks find Invalid leaves the earlier proposal waiting.
	if r := propose(f.ahkow, "test.annotate_note", map[string]any{"note": strings.Repeat("x", 201)}, first.str("id")); r.str("status") != storage.ProposalInvalid {
		t.Fatalf("an invalid revision: %d %s", r.Status, r.Raw)
	}
	if p := get(first.str("id")); p.Status != storage.ProposalPending {
		t.Fatalf("an invalid revision moved the earlier proposal: %+v", p)
	}

	// Pending, the revision supersedes it, and its request is void: it can no longer be signed.
	second := propose(f.ahkow, "test.annotate_note", map[string]any{"note": "Book the hall"}, first.str("id"))
	if second.Status != http.StatusCreated || second.str("status") != storage.ProposalPending {
		t.Fatalf("the revision: %d %s", second.Status, second.Raw)
	}
	superseded := get(first.str("id"))
	if superseded.Status != storage.ProposalSuperseded || superseded.SupersededBy == nil || superseded.SupersededBy.String() != second.str("id") {
		t.Fatalf("the earlier proposal: %+v", superseded)
	}
	if r := e.call(http.MethodGet, "/v1/approvals/"+earlier.ApprovalRequest.String(), weiling, nil); r.str("status") != "void" {
		t.Errorf("the earlier request: %d %s", r.Status, r.Raw)
	}
	if r := f.decide(weiling, earlier.ApprovalRequest.String(), "approve", hashOf(shown)); r.Status < 400 {
		t.Errorf("the superseded proposal's request was signed: %d %s", r.Status, r.Raw)
	}
	trail := auditTrail(t, e)
	if !strings.Contains(trail, `"action":"ai_proposal.superseded"`) || !strings.Contains(trail, `"action":"approval.void"`) {
		t.Errorf("the supersession and the void are not both audited")
	}
	if p := get(second.str("id")); p.Status != storage.ProposalPending || p.ApprovalRequest == nil {
		t.Errorf("the revision: %+v", p)
	}

	// Only the person's own proposal, of the same task type, still waiting, may be revised.
	other := propose(f.weiling, "test.annotate_note", map[string]any{"note": "Weiling's"}, "")
	for name, c := range map[string]struct {
		taskType, revises string
	}{
		"one superseded already":   {"test.annotate_note", first.str("id")},
		"another person's":         {"test.annotate_note", other.str("id")},
		"one of another task type": {"test.tag_note", second.str("id")},
		"one that does not exist":  {"test.annotate_note", uuid.NewString()},
		"a word that is not an ID": {"test.annotate_note", "the last one"},
	} {
		r := propose(f.ahkow, c.taskType, map[string]any{"note": "Book the yard"}, c.revises)
		if r.Status != http.StatusUnprocessableEntity || (r.problemCode() != CodeProposalRevises && r.problemCode() != CodeInvalid) {
			t.Errorf("revising %s: %d %s", name, r.Status, r.Raw)
		}
	}
	if p := get(second.str("id")); p.Status != storage.ProposalPending {
		t.Errorf("a refused revision moved the proposal it named: %+v", p)
	}
}
