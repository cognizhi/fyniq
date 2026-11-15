package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// A proposal is an approval request (PRD-001 AI-89, AI-P4, AI-P5, AI-10; p1-ai-proposals-core design D4; rev 0.280): made
// as it becomes Pending, its requester the person who asked, authenticated by their live turn; its envelope signs the
// payload's hash; its one step names the task type's approver roles, and a signer must also hold the right its command
// needs, as if they did it themselves, or be the Company secretary for a sec.* proposal (Appendix D rev 0.280); the
// person who asked may sign their own tier-1 proposal. No organisation changes
// its policy (approval.Policies, Fixed). The final approval marks it Approved and queues it for the executor, in the same
// transaction; the API never executes it.

// turnMFA and runMFA are how an AI proposal's requester is authenticated: by their live turn, or the live run of the
// scheduled agent that acts for them (AI-89 rev 0.281), which approval_request_mfa_known lets ai_proposal requests alone
// hold, each only for a proposal from a turn or a run (approval_request_ai_requester).
const (
	turnMFA = "ai_turn"
	runMFA  = "ai_run"
)

// proposalRule is who signs an AI proposal's request, as its change holds it, signed in its envelope: its task type, the
// operation whose right a signer holds as a person (AI-10), and whether the person who asked may sign (tier 1).
type proposalRule struct {
	TaskType string
	Command  authz.Policy
	SelfSign bool
}

// proposalRuleOf is r's rule, when r is an AI proposal's request.
func proposalRuleOf(r storage.ApprovalRequest) (proposalRule, bool) {
	if r.ObjectType != approval.ObjectAIProposal {
		return proposalRule{}, false
	}
	var st stored
	if json.Unmarshal(r.Change, &st) != nil {
		return proposalRule{}, true // a rule that cannot be read lets no one sign
	}
	op, _ := st.Execute["command"].(string)
	taskType, _ := st.Execute["task_type"].(string)
	tier, _ := st.Execute["tier"].(float64)
	return proposalRule{TaskType: taskType, Command: proposal.CommandPolicy(op), SelfSign: tier == proposal.Tier1}, true
}

// approverOf reports whether p holds the right to sign r: A on its object's row, or for an AI proposal the right its
// command needs, as a person and never as the AI; a sec.* proposal, the Company secretary too (proposal.Approves).
func approverOf(p authn.Principal, r storage.ApprovalRequest) bool {
	if rule, ok := proposalRuleOf(r); ok {
		return !p.AI() && proposal.Approves(rule.TaskType, rule.Command, p.Roles)
	}
	return mayApprove(p, r.ObjectType)
}

// approvesRequest reports whether a member's roles hold the right to sign r, for the recheck before it executes.
func approvesRequest(roles []string, r storage.ApprovalRequest) bool {
	if rule, ok := proposalRuleOf(r); ok {
		rs := make([]authz.Role, len(roles))
		for i, x := range roles {
			rs[i] = authz.Role(x)
		}
		return proposal.Approves(rule.TaskType, rule.Command, rs)
	}
	return approves(roles, r.ObjectType)
}

// seesProposal reports whether p may see an AI proposal's request: the person who asked, one who may sign it, and the
// Owner and the DPO every one (design D5).
func seesProposal(p authn.Principal, r storage.ApprovalRequest) bool {
	if p.AI() {
		return false
	}
	if slices.Contains(p.Roles, authz.Owner) || slices.Contains(p.Roles, authz.DPO) {
		return true
	}
	steps := domainSteps(r.PolicySteps)
	return approverOf(p, r) && slices.ContainsFunc(steps, func(s approval.Step) bool { return approval.Eligible(s, p.UserID, rolesOf(p.Roles)) })
}

// requestProposal makes a Pending proposal's approval request (AI-89): as the person who asked, authenticated by their
// live turn now, or the live run that acts for them, its envelope holding the payload's hash, its step the task type's
// approver roles.
func (a *API) requestProposal(ctx context.Context, u storage.UnitOfWork, p authn.Principal, made storage.AIProposal, tt proposal.TaskType,
	cmd proposal.Command, impact proposal.Impact) (storage.ApprovalRequest, error) {
	policy := approval.Policies[approval.ObjectAIProposal]
	roles := make([]string, len(tt.Approvers))
	for i, r := range tt.Approvers {
		roles[i] = string(r)
	}
	c := pendingChange{ObjectType: approval.ObjectAIProposal, ObjectKey: made.ID.String(), Operation: approval.OpAIProposalExecute,
		Summary: cut(impact.Summary, 200), Execute: map[string]any{"proposal": made.ID.String(), "task_type": tt.ID, "task_version": tt.Version,
			"payload_sha256": made.PayloadSHA256, "tier": tt.Tier, "items": made.ItemCount, "command": cmd.Policy().Operation},
		Diff: []ApprovalDiff{{Field: "proposal", After: impact.Summary}}}
	if c.Summary == "" {
		c.Summary = "An AI proposal of " + tt.ID
	}
	envelope, err := a.envelopeOf(ctx, u, p.Organisation, c, p.UserID, policy.Version)
	if err != nil {
		return storage.ApprovalRequest{}, err
	}
	change, err := json.Marshal(stored{Execute: c.Execute, Diff: c.Diff, Amounts: []approval.Amount{}})
	if err != nil {
		return storage.ApprovalRequest{}, err
	}
	now, method := a.deps.Clock.Now(), turnMFA
	if p.Run != uuid.Nil {
		method = runMFA
	}
	steps := []approval.Step{{Quorum: 1, Roles: roles}}
	r, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: c.ObjectType, ObjectKey: c.ObjectKey, Operation: c.Operation,
		Summary: c.Summary, Change: change, Envelope: string(envelope), EnvelopeHash: approval.Hash(envelope), PolicyVersion: policy.Version,
		PolicySteps: storageSteps(steps), RequestedBy: p.UserID, RequestedAt: now, RequesterMFAMethod: method, RequesterMFAAt: now,
		ExpiresAt: made.ExpiresAt})
	if err != nil {
		return storage.ApprovalRequest{}, err
	}
	return r, nil
}

// aiProposalDependencies are a proposal's envelope dependencies: its status and payload's hash, at its version.
func aiProposalDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	pr, err := u.AIProposals().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	// The request is made in the same unit of work as the proposal becomes Pending: it is Pending as signed.
	status := pr.Status
	if status == storage.ProposalDraft {
		status, pr.Version = storage.ProposalPending, pr.Version+1
	}
	return pr.Version, map[string]any{"status": status, "payload_sha256": pr.PayloadSHA256}, nil
}

// executeAIProposal is ai_proposal.execute: the proposal Approved, with what the final signer approved, and queued for
// the executor in this transaction (AI-P5, design D4). Nothing of it runs here.
func (a *API) executeAIProposal(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	pr, err := u.AIProposals().Get(ctx, id)
	if err != nil {
		return err
	}
	items, payload := []int(nil), pr.Payload
	if sel, ok := selectionOf(ctx); ok {
		items, payload = sel.items, sel.payload
	}
	approved, err := u.AIProposals().Approve(ctx, pr.ID, pr.Version, signer, items, payload, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"proposal_id": approved.ID.String(), "version": approved.Version, "approval_request_id": r.ID.String()})
	if err != nil {
		return err
	}
	if _, err := u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassExecutor, Kind: proposal.ExecutorKind, Payload: body,
		BusinessKey: approved.ID.String()}); err != nil {
		return fmt.Errorf("queueing the approved proposal for the executor: %w", err)
	}
	return nil
}

// rejectAIProposal marks a rejected request's proposal Rejected, with the person's reason, in the same transaction.
func (a *API) rejectAIProposal(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, by uuid.UUID, reason string, at time.Time) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	pr, err := u.AIProposals().Get(ctx, id)
	if err != nil {
		return err
	}
	_, err = u.AIProposals().Reject(ctx, pr.ID, pr.Version, by, cut(reason, 1000), at)
	return err
}

// selection is the items of a batch proposal an approver approves (AI-32): the payload cut to them and its hash, which
// their signature signs with the envelope.
type selection struct {
	items   []int
	payload string
	sha     string
}

type selectionKey struct{}

func withSelection(ctx context.Context, s selection) context.Context {
	return context.WithValue(ctx, selectionKey{}, s)
}

func selectionOf(ctx context.Context) (selection, bool) {
	s, ok := ctx.Value(selectionKey{}).(selection)
	return s, ok
}

// proposalSelection is what an approval of r approves when it names items (AI-32): nil for the whole of a tier-1
// proposal or for any other request. Items name an AI proposal's batch alone; a proposal above tier 1 of several items
// is never approved in bulk: its approver names each item they approve.
func (a *API) proposalSelection(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, items []int) (*selection, error) {
	if r.ObjectType != approval.ObjectAIProposal {
		if len(items) > 0 {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.items", Message: "only an AI proposal of several items has items to choose",
				Fix: "Approve the request without items"}})
		}
		return nil, nil
	}
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return nil, err
	}
	pr, err := u.AIProposals().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		if pr.ItemCount > 1 && pr.RiskTier > proposal.Tier1 {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.items", Message: fmt.Sprintf(
				"a tier-%d proposal of %d items is never approved in bulk: name each item you approve", pr.RiskTier, pr.ItemCount),
				Fix: "Name the items you approve in items"}})
		}
		return nil, nil
	}
	if pr.ItemCount == 0 {
		return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.items", Message: "this proposal is not a batch of items",
			Fix: "Approve it without items"}})
	}
	payload, sha, err := proposal.Subset(pr.Payload, items)
	if err != nil {
		return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.items", Message: err.Error(),
			Fix: fmt.Sprintf("Name items from 1 to %d, each once", pr.ItemCount)}})
	}
	chosen := slices.Clone(items)
	slices.Sort(chosen)
	return &selection{items: chosen, payload: payload, sha: sha}, nil
}

// rejectedOf are the latest proposals of a conversation's turns that a person rejected, with their reasons, for the next
// turn to read (AI-14, design D8): at most five, the latest last.
func rejectedOf(ctx context.Context, u storage.UnitOfWork, user uuid.UUID, turns []storage.AITurn) ([]assistant.Rejected, error) {
	if len(turns) == 0 {
		return nil, nil
	}
	ids := make(map[uuid.UUID]bool, len(turns))
	for _, t := range turns {
		ids[t.ID] = true
	}
	ps, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Statuses: []string{storage.ProposalRejected}, RequestedBy: &user, Limit: 100})
	if err != nil {
		return nil, err
	}
	var out []assistant.Rejected
	for _, p := range ps { // newest first
		if p.Turn == nil || !ids[*p.Turn] {
			continue
		}
		var impact struct {
			Summary string `json:"summary"`
		}
		_ = json.Unmarshal(p.Impact, &impact)
		out = append(out, assistant.Rejected{TaskType: p.TaskType, Summary: impact.Summary, Reason: p.Reason})
		if len(out) == 5 {
			break
		}
	}
	slices.Reverse(out)
	return out, nil
}

// screenRejections screens each rejection's reason on its way to the model, as a tool's text is (AI-18): a finding
// withholds that reason; a screening that did not finish refuses the turn (fail closed).
func (a *API) screenRejections(ctx context.Context, rs []assistant.Rejected) error {
	for i := range rs {
		err := a.screen(ctx, false, rs[i].Reason)
		var p *Problem
		switch {
		case err == nil:
		case errors.As(err, &p) && p.Code == CodeScreened:
			rs[i].Reason = "withheld: screening flagged the person's words" // never the finding's words, which screening reads as more
		default:
			return err
		}
	}
	return nil
}

// proposalItems are the items an approval names, as numbers.
func proposalItems(items []ProposalItem) []int {
	out := make([]int, len(items))
	for i, n := range items {
		out[i] = int(n)
	}
	return out
}
