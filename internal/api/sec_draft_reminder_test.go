//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// sec.draft_reminder (Appendix E; p1-scheduled-agents task 6.2, design D8): the AI drafts a reminder about an obligation to
// the members holding a role; the dry run renders the email and sends nothing; once the Owner or the Company secretary
// approves it, the executor queues it through the email outbox, once, to each member of that role with a sign-in email,
// saying the AI drafted it and a person approved it. A role no member can be emailed in is refused.
// req: ai/task-catalogue sec.draft_reminder Draft a reminder about an obligation
// req: AI-65
// req: AI-11
// asvs: V8.2.2
func TestTheAIDraftsAReminderSentOnceApproved(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	enableTaskTypes(e, "sec.draft_reminder", 1)
	secretary, secretaryID := companySecretary(e, f)
	verifiedEmail(t, e, secretaryID, "secretary@example.test")
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	turn := proposeTurn(e, f.ahkow)
	propose := func(role string) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "sec.draft_reminder", "payload": map[string]any{
			"to_role": role, "subject": "The annual return is due", "message": "The annual return is due by 31 July 2027. Please prepare it.",
			"obligation": "calendar:annual_return:2027-07-31"}, "rationale": "It is due within a month.", "model": "claude-opus-5-5", "prompt": "chat@3"},
			ServiceTokenHeader, service)
	}
	sent := func() []storage.EmailMessage {
		t.Helper()
		var out []storage.EmailMessage
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.EmailMessages().List(ctx, storage.EmailMessageFilter{Purpose: email.PurposeSecretaryReminder, Limit: 10})
			return err
		})
		return out
	}

	// The dry run renders it and sends nothing.
	made := propose("company_secretary")
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending ||
		made.str("summary") != `Emails the reminder "The annual return is due" to the company secretary (1 person)` {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	if len(sent()) != 0 {
		t.Fatal("a reminder went before anyone approved it")
	}
	if none := propose("auditor"); none.Status < 400 && none.str("status") != storage.ProposalInvalid {
		t.Errorf("a reminder to a role the assistant does not tell: %s", none.Raw)
	}
	if none := propose("board_viewer"); none.str("status") != storage.ProposalInvalid || !strings.Contains(none.str("reason"), "sign-in email") {
		t.Errorf("a reminder to a role no one can be emailed in: %s", none.Raw)
	}

	// Approved by the Company secretary, the executor queues it once, saying who wrote it.
	var pr storage.AIProposal
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr, err = u.AIProposals().Get(ctx, uuid.MustParse(made.str("id")))
		return err
	})
	shown := e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), secretary, nil)
	if d := f.decide(secretary, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK {
		t.Fatalf("approving the reminder: %d %s", d.Status, d.Raw)
	}
	executor := proposal.Executor{Store: executorStore(t), Catalogue: proposal.Default(), Commands: proposal.Release, Clock: e.clock,
		Outbox: outbox.Dispatcher{Store: executorStore(t), Queue: e.queue, Clock: e.clock}}
	body, _ := json.Marshal(map[string]any{"proposal_id": pr.ID.String()})
	for range 2 {
		if err := executor.Work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind,
			Payload: body}); err != nil {
			t.Fatal(err)
		}
	}
	msgs := sent()
	if len(msgs) == 0 {
		var now storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			now, err = u.AIProposals().Get(ctx, pr.ID)
			return err
		})
		t.Fatalf("no reminder queued: the proposal is %s: %s", now.Status, now.Reason)
	}
	if len(msgs) != 1 || msgs[0].Recipient != "secretary@example.test" || msgs[0].Subject != "The annual return is due" {
		t.Fatalf("the reminders queued: %+v", msgs)
	}
	var job *storage.OutboxRow
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		job, err = u.Outbox().ByKey(ctx, email.Kind, "email:"+msgs[0].ID.String())
		return err
	})
	if job == nil || !strings.Contains(string(job.Payload), "drafted by Fyniq's AI assistant and approved by a person") ||
		!strings.Contains(string(job.Payload), "calendar:annual_return:2027-07-31") {
		t.Fatalf("the reminder's send: %+v", job)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr, err = u.AIProposals().Get(ctx, pr.ID)
		return err
	})
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "email_message:"+msgs[0].ID.String()) {
		t.Errorf("the proposal executed: %+v %s", pr, pr.Execution)
	}
}
