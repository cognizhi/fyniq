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
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// companySecretary is a member of e's organisation holding the Company secretary role, as the founders invite them
// (FM-1): their stepped-up token and their user.
func companySecretary(e *env, f founders) (string, string) {
	e.t.Helper()
	invited := e.call(http.MethodPost, "/v1/members/invitations", f.as(f.ahkow), map[string]any{"display_name": "Secretary", "roles": []string{"company_secretary"}})
	if a := f.decide(f.as(f.weiling), invited.str("id"), "approve", hashOf(invited)); a.Status != http.StatusOK {
		e.t.Fatalf("approving the invitation: %d %s", a.Status, a.Raw)
	}
	joined := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "secretary-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": invited.str("invitation_code"), "display_name": "Secretary"})
	user := joined.str("user_id")
	if user == "" {
		e.t.Fatalf("the Company secretary joining: %s, then %d %s", invited.Raw, joined.Status, joined.Raw)
	}
	return e.principal(authn.Principal{Subject: "secretary", UserID: uuid.MustParse(user), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.CompanySecretary}, StepUpAt: e.clock.Now()}), user
}

// sec.create_task, the release's own (Appendix E; p1-scheduled-agents task 6.1, design D7): the AI proposes a compliance
// task of its own finding with an owner and a due date, tier 1; the Company secretary approves it under Appendix D's rule
// for every sec.* proposal; the executor runs CreateComplianceTask as them, raising the task from the proposal. A
// finding with a task proposed or open gets no second, a deterministic finding's key is refused, and a task opened for
// the finding meanwhile leaves the proposal Stale.
// req: ai/task-catalogue sec.create_task Create a compliance task
// req: AI-65
// req: AI-10
// asvs: V8.2.2, V8.3.1
func TestTheAIProposesATaskTheSecretaryApproves(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	enableTaskTypes(e, "sec.create_task", 1)
	secretary, _ := companySecretary(e, f)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	turn := proposeTurn(e, f.ahkow)
	propose := func(key, title string) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "sec.create_task", "payload": map[string]any{
			"finding_key": key, "title": title, "detail": "Board approval is needed first.", "owner_role": "company_secretary", "due_on": "2027-06-30"},
			"rationale": "The AGM must be held by 30 June 2027.", "model": "claude-opus-5-5", "prompt": "chat@3"}, ServiceTokenHeader, service)
	}
	executor := proposal.Executor{Store: executorStore(t), Catalogue: proposal.Default(), Commands: proposal.Release, Clock: e.clock}
	execute := func(pr storage.AIProposal) storage.AIProposal {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"proposal_id": pr.ID.String()})
		if err := executor.Work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind,
			Payload: body}); err != nil {
			t.Fatal(err)
		}
		var out storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIProposals().Get(ctx, pr.ID)
			return err
		})
		return out
	}
	approve := func(r response) storage.AIProposal {
		t.Helper()
		var pr storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			pr, err = u.AIProposals().Get(ctx, uuid.MustParse(r.str("id")))
			return err
		})
		shown := e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), secretary, nil)
		if shown.Body["you_may_approve"] != true {
			t.Fatalf("the Company secretary's view of the request: %s", shown.Raw)
		}
		if d := f.decide(secretary, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK || d.str("status") != "approved" {
			t.Fatalf("the Company secretary approving it: %d %s", d.Status, d.Raw)
		}
		return pr
	}

	// Pending, tier 1, with the platform's words; a second for the same finding is refused while the first waits.
	first := propose("agent:agm-room-2027", "Book the room for the AGM")
	if first.Status != http.StatusCreated || first.str("status") != storage.ProposalPending ||
		first.str("summary") != `Creates the task "Book the room for the AGM" for the company secretary, due 30 June 2027` {
		t.Fatalf("the proposal: %d %s", first.Status, first.Raw)
	}
	if again := propose("agent:agm-room-2027", "Book a room"); again.str("status") != storage.ProposalInvalid || !strings.Contains(again.str("reason"), "proposed already") {
		t.Errorf("a second proposal for the finding: %s", again.Raw)
	}
	if theirs := propose("calendar:annual_return:2027-07-31", "File the annual return"); theirs.str("status") != storage.ProposalInvalid ||
		!strings.Contains(theirs.str("reason"), "agent:") {
		t.Errorf("a proposal for a deterministic finding: %s", theirs.Raw)
	}

	// Approved by the Company secretary, it runs as them: the task, raised from the proposal.
	pr := execute(approve(first))
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "compliance_task:") {
		t.Fatalf("executed: %+v %s", pr, pr.Execution)
	}
	var task storage.ComplianceTask
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		task, err = u.ComplianceTasks().OpenFor(ctx, "agent:agm-room-2027")
		return err
	})
	if task.Source != "ai_proposal" || task.Proposal == nil || *task.Proposal != pr.ID || task.OwnerRole != "company_secretary" ||
		task.DueOn == nil || task.DueOn.Format("2006-01-02") != "2027-06-30" || task.Title != "Book the room for the AGM" {
		t.Fatalf("the task: %+v", task)
	}
	if open := propose("agent:agm-room-2027", "Book the room again"); open.str("status") != storage.ProposalInvalid || !strings.Contains(open.str("reason"), "open task") {
		t.Errorf("a proposal for a finding with a task open: %s", open.Raw)
	}

	// The Company secretary signs no other request: another member's invitation waits for a founder.
	other := e.call(http.MethodPost, "/v1/members/invitations", f.as(f.ahkow), map[string]any{"display_name": "Auditor", "roles": []string{"auditor"}})
	if d := f.decide(secretary, other.str("id"), "approve", hashOf(other)); d.Status != http.StatusForbidden && d.Status != http.StatusNotFound {
		t.Errorf("the Company secretary approving an invitation: %d %s", d.Status, d.Raw)
	}

	// A task opened for the finding between the approval and the run: Stale, and no second task.
	late := approve(propose("agent:auditor-letter-2027", "Send the auditor's engagement letter"))
	raiseTasks(e, f.ahkow, "agent:auditor-letter-2027")
	if s := execute(late); s.Status != storage.ProposalStale {
		t.Errorf("a proposal whose finding gained a task meanwhile: %+v", s)
	}
}

// Who approves a proposal (rev 0.280, Appendix D): one who holds its command's right as a person; for a sec.* proposal
// the Company secretary too, and for no other task type.
// req: ai/task-catalogue sec.create_task Create a compliance task
// asvs: V8.2.1
func TestTheSecretaryApprovesTheSecProposalsAlone(t *testing.T) {
	cases := []struct {
		taskType string
		policy   authz.Policy
		roles    []authz.Role
		want     bool
	}{
		{"sec.create_task", proposal.CreateComplianceTaskPolicy, []authz.Role{authz.Owner}, true},
		{"sec.create_task", proposal.CreateComplianceTaskPolicy, []authz.Role{authz.Administrator}, true},
		{"sec.create_task", proposal.CreateComplianceTaskPolicy, []authz.Role{authz.CompanySecretary}, true},
		{"sec.create_task", proposal.CreateComplianceTaskPolicy, []authz.Role{authz.Controller}, false},
		{"sec.create_task", proposal.CreateComplianceTaskPolicy, []authz.Role{authz.BoardViewer}, false},
		{"test.annotate_note", authz.CreateContact, []authz.Role{authz.CompanySecretary}, false},
		{"sec.create_task", authz.Policy{}, []authz.Role{authz.Owner, authz.CompanySecretary}, false},
	}
	for _, c := range cases {
		if got := proposal.Approves(c.taskType, c.policy, c.roles); got != c.want {
			t.Errorf("%s as %v: %v, want %v", c.taskType, c.roles, got, c.want)
		}
	}
	if p := proposal.CommandPolicy("createComplianceTask"); p.Operation != "createComplianceTask" || p.Capability != authz.AICompanySecretary {
		t.Errorf("the command's policy: %+v", p)
	}
	if p := proposal.CommandPolicy("createContact"); p.Operation != authz.CreateContact.Operation {
		t.Errorf("an operation's policy: %+v", p)
	}
}
