package api

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// A proposal is an approval request (PRD-001 AI-89, AI-P4, AI-P5, AI-10; p1-ai-proposals-core design D4; rev 0.280): made
// as it becomes Pending, its requester the person who asked, authenticated by their live turn; its envelope signs the
// payload's hash; its one step names the task type's approver roles, and a signer must also hold the right its command
// needs, as if they did it themselves; the person who asked may sign their own tier-1 proposal. No organisation changes
// its policy (approval.Policies, Fixed). The final approval marks it Approved and queues it for the executor, in the same
// transaction; the API never executes it.

// ExecutorKind is the executor's work of an approved proposal (design D6).
const ExecutorKind = "ai_proposal.execute"

// turnMFA is how an AI proposal's requester is authenticated: by their live turn (AI-89), which approval_request_mfa_known
// lets ai_proposal requests alone hold.
const turnMFA = "ai_turn"

// proposalRule is who signs an AI proposal's request, as its change holds it, signed in its envelope: the operation whose
// right a signer holds as a person (AI-10), and whether the person who asked may sign (tier 1).
type proposalRule struct {
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
	tier, _ := st.Execute["tier"].(float64)
	return proposalRule{Command: authz.Policies[op], SelfSign: tier == proposal.Tier1}, true
}

// approverOf reports whether p holds the right to sign r: A on its object's row, or for an AI proposal the right its
// command needs, as a person and never as the AI.
func approverOf(p authn.Principal, r storage.ApprovalRequest) bool {
	if rule, ok := proposalRuleOf(r); ok {
		return !p.AI() && rule.Command.Operation != "" && authz.Decide(rule.Command, p.Roles, false).Allowed
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
		return rule.Command.Operation != "" && authz.Decide(rule.Command, rs, false).Allowed
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
// live turn now, its envelope holding the payload's hash, its step the task type's approver roles.
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
	now := a.deps.Clock.Now()
	steps := []approval.Step{{Quorum: 1, Roles: roles}}
	r, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: c.ObjectType, ObjectKey: c.ObjectKey, Operation: c.Operation,
		Summary: c.Summary, Change: change, Envelope: string(envelope), EnvelopeHash: approval.Hash(envelope), PolicyVersion: policy.Version,
		PolicySteps: storageSteps(steps), RequestedBy: p.UserID, RequestedAt: now, RequesterMFAMethod: turnMFA, RequesterMFAAt: now,
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
	approved, err := u.AIProposals().Approve(ctx, pr.ID, pr.Version, signer, nil, pr.Payload, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"proposal_id": approved.ID.String(), "version": approved.Version, "approval_request_id": r.ID.String()})
	if err != nil {
		return err
	}
	if _, err := u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassExecutor, Kind: ExecutorKind, Payload: body,
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
