//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// A proposal undecided past its 72 hours expires (§9.5; design D9): the worker's job moves it to Expired, audited, and
// the database voids its approval request in the same transaction (000382), audited too, so it can no longer be
// signed. One not yet past its expiry waits on.
// req: ai/proposals Proposal lifecycle
// req: ai/proposals Proposal tier, originator, approvals and execution record
func TestAProposalUndecidedExpires(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	e.api.deps.Catalogue = fixtureCatalogue(t, "test.annotate_note")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	enableTaskTypes(e, "test.annotate_note", 1)
	propose := func(note string) storage.AIProposal {
		t.Helper()
		// The tokens are the moment's: a turn's lives 5 minutes, and the AI service's own is short-lived too.
		service, err := e.services.Token(ServiceAI)
		if err != nil {
			t.Fatal(err)
		}
		turn := proposeTurn(e, f.ahkow)
		r := e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "test.annotate_note", "payload": map[string]any{"note": note},
			"rationale": "x", "model": "m", "prompt": "chat@3"}, ServiceTokenHeader, service)
		if r.Status != http.StatusCreated {
			t.Fatalf("proposing %q: %d %s", note, r.Status, r.Raw)
		}
		var pr storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			pr, err = u.AIProposals().Get(ctx, uuid.MustParse(r.str("id")))
			return err
		})
		return pr
	}
	old := propose("Old")
	e.clock.Advance(time.Hour)
	young := propose("Young")
	weiling := f.as(f.weiling)
	shown := e.call(http.MethodGet, "/v1/approvals/"+old.ApprovalRequest.String(), weiling, nil)

	e.clock.Advance(71 * time.Hour) // the first is past its 72 hours, the second is not
	// This organisation's alone: the other tests' organisations share the database, and theirs run on their own clocks.
	org := e.books.Org
	if n, err := proposal.ExpireIn(context.Background(), workerStore(t), org, e.clock); err != nil || n != 1 {
		t.Fatalf("expiring: %d %v", n, err)
	}
	get := func(id uuid.UUID) (p storage.AIProposal) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			p, err = u.AIProposals().Get(ctx, id)
			return err
		})
		return p
	}
	if p := get(old.ID); p.Status != storage.ProposalExpired || p.DecidedAt == nil {
		t.Errorf("past its expiry: %+v", p)
	}
	if p := get(young.ID); p.Status != storage.ProposalPending {
		t.Errorf("not yet past its expiry: %+v", p)
	}
	weiling = f.as(f.weiling)
	if r := e.call(http.MethodGet, "/v1/approvals/"+old.ApprovalRequest.String(), weiling, nil); r.str("status") != "void" {
		t.Errorf("the expired proposal's request: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/approvals/"+young.ApprovalRequest.String(), weiling, nil); r.str("status") != "pending" {
		t.Errorf("the waiting proposal's request: %d %s", r.Status, r.Raw)
	}
	e.problemIs(f.decide(weiling, old.ApprovalRequest.String(), "approve", hashOf(shown)), "approveApproval", 409, approval.CodeChanged)
	if trail := auditTrail(t, e); !strings.Contains(trail, `"action":"approval.void"`) {
		t.Errorf("the void is not audited")
	}
	if n, err := proposal.ExpireIn(context.Background(), workerStore(t), org, e.clock); err != nil || n != 0 {
		t.Errorf("expiring again: %d %v", n, err)
	}
}
