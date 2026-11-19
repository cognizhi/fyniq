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
	"github.com/cognizhi/fyniq/internal/proposal"
)

// undoNote is the test-only task type's command made undoable: its undo reverses the note, unless the note was "moved", a record that changed since.
type undoNote struct{ noteCommand }

func (undoNote) Undo(_ context.Context, _ storage.UnitOfWork, done proposal.Execution, as proposal.Actor) (proposal.Execution, error) {
	for _, r := range done.Records {
		if r == "note:moved" {
			return proposal.Execution{}, proposal.ErrStale
		}
	}
	return proposal.Execution{Records: append([]string{"undone-by:" + as.User.String()}, done.Records...), Entries: []string{}}, nil
}

// An executed proposal is undone by a person in one action (AI-31, AI-91): only one that executed, whose task type can be undone, by someone who holds the
// right its command needs, once, while its records are as the execution left them; the undo runs as that person, is kept in an append-only record, is
// audited, and the proposal stays Executed. The AI reaches none of it. The month's approval quality is read by the Owner and the DPO from the
// proposals' own records, with the decider's first look; the review is signed once, after the month, after step-up (CTL-AI-07, AI-92).
// req: AI-31
// req: AI-91
// req: CTL-AI-07
// req: AI-92
// asvs: V8.2.2, V8.3.1
func TestAnExecutedProposalIsUndoneByAPersonAndItsQualityIsReviewed(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	catalogue := fixtureCatalogue(t, "test.annotate_note")
	e.api.deps.Catalogue = catalogue
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": undoNote{}}
	enableTaskTypes(e, "test.annotate_note", 1)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	weiling, ahkow := f.as(f.weiling), f.as(f.ahkow)
	turn := proposeTurn(e, f.ahkow)
	executor := proposal.Executor{Store: executorStore(t), Catalogue: catalogue, Commands: e.api.deps.Commands, Clock: e.clock}
	proposed := func(note string) storage.AIProposal {
		t.Helper()
		r := e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "test.annotate_note", "payload": map[string]any{"note": note},
			"rationale": "It is due.", "model": "m", "prompt": "chat@3"}, ServiceTokenHeader, service)
		var pr storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			pr, err = u.AIProposals().Get(ctx, uuid.MustParse(r.str("id")))
			return err
		})
		return pr
	}
	executed := func(note string) storage.AIProposal {
		t.Helper()
		pr := proposed(note)
		// Weiling opens the review, then approves it at once.
		if r := e.call(http.MethodGet, "/v1/ai/proposals/"+pr.ID.String(), weiling, nil); r.Status != http.StatusOK {
			t.Fatalf("opening the review: %d %s", r.Status, r.Raw)
		}
		shown := e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), weiling, nil)
		if d := f.decide(weiling, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK {
			t.Fatalf("approving: %d %s", d.Status, d.Raw)
		}
		var row *storage.OutboxRow
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			row, err = u.Outbox().ByKey(ctx, proposal.ExecutorKind, pr.ID.String())
			return err
		})
		if err := executor.Work(context.Background(), e.books.Org, *row); err != nil {
			t.Fatal(err)
		}
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			pr, err = u.AIProposals().Get(ctx, pr.ID)
			return err
		})
		if pr.Status != storage.ProposalExecuted {
			t.Fatalf("%s: %s %s", note, pr.Status, pr.Reason)
		}
		return pr
	}
	undo := func(tok string, pr storage.AIProposal, why string) response {
		return e.call(http.MethodPost, "/v1/ai/proposal-undos", tok, map[string]any{"proposal_id": pr.ID.String(), "reason": why}, "Idempotency-Key", uuid.NewString())
	}

	done := executed("Book the room")
	// Someone who may not do what it did cannot undo it; a pending proposal has nothing to undo.
	if r := undo(e.as(authz.Auditor), done, "x"); r.Status != http.StatusForbidden && r.Status != http.StatusNotFound {
		t.Errorf("an auditor undoing it: %d %s", r.Status, r.Raw)
	}
	if r := undo(weiling, proposed("Waiting"), "x"); r.Status != http.StatusConflict || r.str("code") != CodeUndoNotExecuted {
		t.Errorf("undoing a pending proposal: %d %s", r.Status, r.Raw)
	}
	// Undone by Weiling, who holds the right: as them, once, with the proposal still Executed and an audit event.
	r := undo(weiling, done, "The room was double-booked")
	if r.Status != http.StatusCreated || r.str("undone_by") != f.weiling || !strings.Contains(r.Raw, "undone-by:"+f.weiling) {
		t.Fatalf("the undo: %d %s", r.Status, r.Raw)
	}
	var after storage.AIProposal
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		after, err = u.AIProposals().Get(ctx, done.ID)
		return err
	})
	if after.Status != storage.ProposalExecuted || after.Version != done.Version {
		t.Errorf("the proposal after its undo: %s v%d (was v%d)", after.Status, after.Version, done.Version)
	}
	if again := undo(weiling, done, "again"); again.Status != http.StatusConflict || again.str("code") != CodeUndoAlready {
		t.Errorf("undoing twice: %d %s", again.Status, again.Raw)
	}
	if !strings.Contains(auditTrail(t, e), `"action":"ai_proposal.undone"`) {
		t.Error("the undo is not in the audit trail")
	}
	// A record that moved since the execution is not undone.
	if moved := undo(weiling, executed("moved"), "x"); moved.Status != http.StatusConflict || moved.str("code") != CodeUndoMoved {
		t.Errorf("undoing what moved: %d %s", moved.Status, moved.Raw)
	}
	// A task type that cannot be undone offers no undo: a command with no Undo.
	plain := executed("Plain")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	if r := undo(weiling, plain, "x"); r.Status != http.StatusConflict || r.str("code") != CodeUndoNotUndoable {
		t.Errorf("undoing what cannot be undone: %d %s", r.Status, r.Raw)
	}
	// The AI's tokens reach neither the undo nor the metrics.
	if r := e.call(http.MethodPost, "/v1/ai/proposal-undos", turn, map[string]any{"proposal_id": done.ID.String(), "reason": "x"}); r.Status < 400 {
		t.Errorf("the AI's turn token undid a proposal: %d", r.Status)
	}

	// Approval quality: the Owner reads the month; another role may not; the review is signed once, after the month, after step-up.
	month := e.clock.Now().Format("2006-01")
	q := e.call(http.MethodGet, "/v1/ai/approval-quality?month="+month, ahkow, nil)
	if q.Status != http.StatusOK || q.Body["metrics"].(map[string]any)["made"].(float64) < 4 || q.Body["metrics"].(map[string]any)["approved"].(float64) < 3 || q.Body["metrics"].(map[string]any)["quick_share"].(float64) == 0 {
		t.Fatalf("the month's metrics: %d %s", q.Status, q.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/ai/approval-quality?month="+month, f.as(uuid.NewString(), authz.Accountant), nil); r.Status != http.StatusForbidden || r.str("code") != CodeQualityNotReader {
		t.Errorf("an accountant reading the metrics: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/ai/approval-reviews", steppedUp(e, ahkow), map[string]any{"month": month}, "Idempotency-Key", uuid.NewString()); r.Status != http.StatusUnprocessableEntity || r.str("code") != CodeQualityMonth {
		t.Errorf("signing a month that has not ended: %d %s", r.Status, r.Raw)
	}
	past := e.clock.Now().AddDate(0, -1, 0).Format("2006-01")
	owner := f.as(f.ahkow)
	signed := e.call(http.MethodPost, "/v1/ai/approval-reviews", owner, map[string]any{"month": past, "note": "Nothing to follow up"}, "Idempotency-Key", uuid.NewString())
	if signed.Status != http.StatusCreated || signed.str("signed_by") != f.ahkow || signed.str("note") != "Nothing to follow up" {
		t.Fatalf("signing the review: %d %s", signed.Status, signed.Raw)
	}
	if twice := e.call(http.MethodPost, "/v1/ai/approval-reviews", owner, map[string]any{"month": past}, "Idempotency-Key", uuid.NewString()); twice.Status != http.StatusPreconditionFailed {
		t.Errorf("signing the month twice: %d %s", twice.Status, twice.Raw)
	}
}

// matrixUndo are the authorisation matrix's rows for undo and approval quality: decided past the policy, by the record or the role (pastPolicy).
var matrixUndo = map[string]func(e *env, tok string) (response, func() bool){
	"undoAIProposal": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/proposal-undos", steppedUp(e, tok), map[string]any{"proposal_id": uuid.NewString(), "reason": "matrix"}, "Idempotency-Key", uuid.NewString()), nil
	},
	"getAIApprovalQuality": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/approval-quality?month=2026-10", steppedUp(e, tok), nil), nil
	},
	"signAIApprovalReview": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/approval-reviews", steppedUp(e, tok), map[string]any{"month": "2099-01"}, "Idempotency-Key", uuid.NewString()), nil
	},
}
