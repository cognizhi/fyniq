//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// executorStore is the database as the executor reaches it, under its own role.
func executorStore(t *testing.T) *postgres.Store {
	t.Helper()
	s, err := postgres.Open(context.Background(), db.URL("fyniq_executor"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// The executor runs what was approved, as the person who approved it (AI-11, AI-P5, AI-30; design D6): only a proposal
// it finds Approved, whose approved payload hashes as approved, under the task type's version it was proposed with, and
// while the approver may still do it; in one unit of work with the proposal Executed and its record, audited as the
// approver on behalf of the person who asked, carrying out the AI's proposal. A record that moved since the dry run is
// Stale; a refusal as it runs, a task type changed since or an approver who may no longer do it is Failed, with why,
// and nothing else changes. Running it twice runs it once.
// req: AI-11
// req: AI-P5
// req: AI-30
// req: ai/proposals The executor runs approved proposals as the approving human
// asvs: V8.2.2, V8.3.1
func TestTheExecutorRunsWhatWasApproved(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	catalogue := fixtureCatalogue(t, "test.annotate_note")
	e.api.deps.Catalogue = catalogue
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	enableTaskTypes(e, "test.annotate_note", 1)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	weiling := f.as(f.weiling)
	turn := proposeTurn(e, f.ahkow)
	executor := proposal.Executor{Store: executorStore(t), Catalogue: catalogue, Commands: e.api.deps.Commands, Clock: e.clock}
	approved := func(note string) storage.AIProposal {
		t.Helper()
		r := e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "test.annotate_note", "payload": map[string]any{"note": note},
			"rationale": "It is due.", "model": "m", "prompt": "chat@3"}, ServiceTokenHeader, service)
		id := uuid.MustParse(r.str("id"))
		var pr storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			pr, err = u.AIProposals().Get(ctx, id)
			return err
		})
		shown := e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), weiling, nil)
		if d := f.decide(weiling, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK {
			t.Fatalf("approving %q: %d %s", note, d.Status, d.Raw)
		}
		return pr
	}
	run := func(pr storage.AIProposal) storage.AIProposal {
		t.Helper()
		var row *storage.OutboxRow
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			row, err = u.Outbox().ByKey(ctx, proposal.ExecutorKind, pr.ID.String())
			return err
		})
		if row == nil {
			body, _ := json.Marshal(map[string]any{"proposal_id": pr.ID.String()})
			row = &storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind, Payload: body}
		}
		if err := executor.Work(context.Background(), e.books.Org, *row); err != nil {
			t.Fatalf("running %s: %v", pr.ID, err)
		}
		var out storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIProposals().Get(ctx, pr.ID)
			return err
		})
		return out
	}

	// Approved, it runs as the person who approved it, once.
	pr := approved("Book the room")
	done := run(pr)
	if done.Status != storage.ProposalExecuted || done.StartedAt == nil || done.FinishedAt == nil ||
		!strings.Contains(string(done.Execution), "as:"+f.weiling) || !strings.Contains(string(done.Execution), "note:Book the room") {
		t.Fatalf("executed: %+v %s", done, done.Execution)
	}
	if again := run(pr); again.Version != done.Version || again.Status != storage.ProposalExecuted {
		t.Fatalf("run twice: %+v", again)
	}
	trail := auditTrail(t, e)
	executed := ""
	for _, line := range strings.Split(trail, "\n") {
		if strings.Contains(line, `"action":"ai_proposal.executed"`) {
			executed = line
		}
	}
	if !strings.Contains(executed, `"actor":{"type":"user","id":"`+f.weiling+`"}`) || !strings.Contains(executed, `"on_behalf_of":{"type":"user","id":"`+f.ahkow+`"}`) ||
		!strings.Contains(executed, `"originator":"ai_proposal:`+pr.ID.String()+`"`) || !strings.Contains(executed, pr.ApprovalRequest.String()) {
		t.Errorf("the execution's audit event: %s", executed)
	}

	// Not approved: nothing runs.
	r := e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "test.annotate_note", "payload": map[string]any{"note": "Wait"},
		"rationale": "x", "model": "m", "prompt": "chat@3"}, ServiceTokenHeader, service)
	if waiting := run(storage.AIProposal{ID: uuid.MustParse(r.str("id"))}); waiting.Status != storage.ProposalPending {
		t.Fatalf("a pending proposal ran: %+v", waiting)
	}

	// A record that moved: Stale; a refusal as it runs: Failed; each with why.
	if s := run(approved("stale")); s.Status != storage.ProposalStale || s.Reason == "" {
		t.Errorf("stale: %+v", s)
	}
	if x := run(approved("refuse")); x.Status != storage.ProposalFailed || !strings.Contains(x.Reason, "refused as it ran") {
		t.Errorf("refused: %+v", x)
	}

	// A task type changed since: Failed.
	changed := approved("Changed")
	b, _ := json.Marshal(map[string]any{
		"id": "test.annotate_note", "version": 1, "name": "Annotate a note", "description": "Adds a note to a record, differently.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"note"},
			"properties": map[string]any{"note": map[string]any{"type": "string", "maxLength": 200, "description": "The note."}}},
		"command": "AnnotateNote", "tier": 1, "limits": map[string]any{"items": 5}, "approvers": []any{"owner"}, "expiry_hours": 72,
	})
	other, err := proposal.Load(fstest.MapFS{"tasks/test.annotate_note.json": {Data: b}})
	if err != nil {
		t.Fatal(err)
	}
	executor.Catalogue = other
	if x := run(changed); x.Status != storage.ProposalFailed || !strings.Contains(x.Reason, "not the version") {
		t.Errorf("a changed task type: %+v", x)
	}
	executor.Catalogue = catalogue

	// An approver who may no longer do it: Failed.
	lost := approved("Lost")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		roles, err := u.Members().Roles(ctx, uuid.MustParse(f.weiling), e.clock.Now())
		if err != nil {
			return err
		}
		i := slices.IndexFunc(roles, func(a storage.RoleAssignment) bool { return a.Role == string(authz.Controller) })
		return u.Members().Revoke(ctx, roles[i].ID)
	})
	if x := run(lost); x.Status != storage.ProposalFailed || !strings.Contains(x.Reason, "may no longer do it") {
		t.Errorf("an approver who lost the right: %+v", x)
	}
}
