package proposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ExecutorKind is the executor's work of an approved proposal (design D6), which the final approval queues.
const ExecutorKind = "ai_proposal.execute"

// Executor runs approved proposals (AI-11, AI-P5; design D6) as cmd/executor's work of kind ExecutorKind, under the
// executor's own identity and database role. It runs a proposal only when it finds it Approved, its approved payload
// hashing as approved and its request signing the payload's hash, under the task type's version it was proposed with;
// as the approving person, who must still hold the command's right, with the AI as originator and the requester as
// on-behalf-of; in one unit of work with the proposal Executed. A refusal changes nothing but the proposal, Stale or
// Failed with why, in a unit of work of its own. Running it twice runs it once.
type Executor struct {
	Store     storage.Store
	Catalogue *Catalogue
	Commands  Commands
	Clock     ports.Clock
}

var (
	errDone    = errors.New("the proposal is not approved: done already, or never approved")
	errRefused = errors.New("the proposal was refused")
)

// Work is the executor's work of a queued proposal.
func (x Executor) Work(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	var in struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	}
	if err := json.Unmarshal(row.Payload, &in); err != nil || in.ProposalID == uuid.Nil {
		return fmt.Errorf("the executor's work names no proposal: %w", err)
	}
	var status, why string
	err := x.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		pr, err := u.AIProposals().Get(ctx, in.ProposalID)
		if err != nil {
			return err
		}
		if pr.Status != storage.ProposalApproved {
			return errDone
		}
		ctx = asApprover(ctx, pr)
		cmd, as, refusal, err := x.check(ctx, u, pr)
		if err != nil {
			return err
		}
		if refusal != "" {
			status, why = storage.ProposalFailed, refusal
			return errRefused
		}
		now := x.Clock.Now()
		started, err := u.AIProposals().Start(ctx, pr.ID, pr.Version, now)
		if err != nil {
			return err
		}
		done, err := cmd.Execute(ctx, u, json.RawMessage(pr.ApprovedPayload), pr.BaseVersions, as)
		switch {
		case errors.Is(err, ErrStale):
			status, why = storage.ProposalStale, err.Error()
			return errRefused
		case refused(err):
			status, why = storage.ProposalFailed, "it was refused as it ran: "+err.Error()
			return errRefused
		case err != nil:
			return err
		}
		body, err := json.Marshal(map[string]any{"run": row.ID.String(), "records": nonEmpty(done.Records), "entries": nonEmpty(done.Entries)})
		if err != nil {
			return err
		}
		_, err = u.AIProposals().Finish(ctx, started.ID, started.Version, storage.ProposalExecuted, "", body, now)
		return err
	})
	switch {
	case errors.Is(err, errDone):
		return nil
	case errors.Is(err, errRefused):
		return x.refuse(ctx, org, in.ProposalID, row.ID, status, why)
	}
	return err
}

// check is what running pr needs, or why it may not run: its approved payload hashing as approved, its approval
// request approved and signing the payload's hash, its task type at the version it was proposed with and built, and the
// approving person holding the command's right now.
func (x Executor) check(ctx context.Context, u storage.UnitOfWork, pr storage.AIProposal) (Command, Actor, string, error) {
	sum := sha256.Sum256([]byte(pr.ApprovedPayload))
	if pr.ApprovedSHA256 == "" || hex.EncodeToString(sum[:]) != pr.ApprovedSHA256 {
		return nil, Actor{}, "what was approved does not hash as approved", nil
	}
	if pr.ApprovalRequest == nil || pr.DecidedBy == nil {
		return nil, Actor{}, "it has no approval request or approver", nil
	}
	r, err := u.Approvals().Get(ctx, *pr.ApprovalRequest)
	if err != nil {
		return nil, Actor{}, "", err
	}
	var envelope struct {
		Change struct {
			PayloadSHA256 string `json:"payload_sha256"`
		} `json:"change"`
	}
	if r.Status != "approved" || json.Unmarshal([]byte(r.Envelope), &envelope) != nil || envelope.Change.PayloadSHA256 != pr.PayloadSHA256 {
		return nil, Actor{}, "its approval request is not approved, or does not sign its payload's hash", nil
	}
	tt, ok := x.Catalogue.Get(pr.TaskType)
	if !ok || tt.Version != pr.TaskVersion || tt.SHA256 != pr.TaskSHA256 {
		return nil, Actor{}, fmt.Sprintf("the task type %s is not the version %d it was proposed under", pr.TaskType, pr.TaskVersion), nil
	}
	if why := Refuse(tt.ID, tt.Command); why != "" {
		return nil, Actor{}, tt.ID + " can never be proposed: it is " + why, nil
	}
	cmd, ok := x.Commands[tt.Command]
	if !ok {
		return nil, Actor{}, tt.ID + " is not built in this release", nil
	}
	now := x.Clock.Now()
	assignments, err := u.Members().Roles(ctx, *pr.DecidedBy, now)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return nil, Actor{}, "", err
	}
	roles := make([]authz.Role, 0, len(assignments))
	for _, a := range assignments {
		roles = append(roles, authz.Role(a.Role))
	}
	if !Approves(tt.ID, cmd.Policy(), roles) {
		return nil, Actor{}, "the person who approved it may no longer do it themselves", nil
	}
	return cmd, Actor{User: *pr.DecidedBy, Roles: roles, Today: now, Proposal: pr.ID}, "", nil
}

// refuse records why pr did not run, Stale or Failed, in a unit of work of its own: nothing else changed.
func (x Executor) refuse(ctx context.Context, org, id, run uuid.UUID, status, why string) error {
	return x.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		pr, err := u.AIProposals().Get(ctx, id)
		if err != nil {
			return err
		}
		if pr.Status != storage.ProposalApproved {
			return nil
		}
		ctx = asApprover(ctx, pr)
		now := x.Clock.Now()
		started, err := u.AIProposals().Start(ctx, pr.ID, pr.Version, now)
		if err != nil {
			return err
		}
		if len(why) > 1000 {
			why = why[:1000]
		}
		body, err := json.Marshal(map[string]any{"run": run.String(), "records": []string{}, "entries": []string{}, "error": why})
		if err != nil {
			return err
		}
		_, err = u.AIProposals().Finish(ctx, started.ID, started.Version, status, why, body, now)
		return err
	})
}

// asApprover is ctx audited as the approving person, on behalf of the person who asked, carrying out the AI's proposal
// and its approval (AI-10, AUD-01).
func asApprover(ctx context.Context, pr storage.AIProposal) context.Context {
	a := storage.AuditFrom(ctx)
	if pr.DecidedBy != nil {
		a.Actor = &storage.Actor{Type: "user", ID: pr.DecidedBy.String()}
	}
	a.OnBehalfOf = &storage.Actor{Type: "user", ID: pr.RequestedBy.String()}
	a.Originator = "ai_proposal:" + pr.ID.String()
	if pr.ApprovalRequest != nil {
		a.ApprovalIDs = []string{pr.ApprovalRequest.String()}
	}
	return storage.WithAudit(ctx, a)
}

// refused reports whether a command's error refuses the change, a validation or a lock, rather than failing for a moment.
func refused(err error) bool {
	var se *storage.Error
	if !errors.As(err, &se) {
		return false
	}
	return se.Code != storage.CodeRetry && se.Code != storage.CodeLockTimeout
}

func nonEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
