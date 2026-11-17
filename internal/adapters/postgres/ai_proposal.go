package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIProposals() storage.AIProposals { return aiProposals{u} }

type aiProposals struct{ u *unitOfWork }

func (r aiProposals) Create(ctx context.Context, p storage.AIProposal) (storage.AIProposal, error) {
	if err := r.u.check(); err != nil {
		return storage.AIProposal{}, err
	}
	evidence, err := json.Marshal(evidenceOf(p.Evidence))
	if err != nil {
		return storage.AIProposal{}, err
	}
	row, err := r.u.q.InsertAIProposal(ctx, sqlcdb.InsertAIProposalParams{TaskType: p.TaskType, TaskVersion: int32(min(max(p.TaskVersion, 0), 1<<30)), // #nosec G115 -- capped
		TaskSha256: p.TaskSHA256, Payload: p.Payload, PayloadSha256: p.PayloadSHA256, ItemCount: int32(min(max(p.ItemCount, 0), 1<<30)), // #nosec G115 -- capped
		Rationale: p.Rationale, Evidence: evidence, RiskTier: int16(min(max(p.RiskTier, 0), 9)), // #nosec G115 -- capped
		RequiredApprovals: int16(min(max(p.RequiredApprovals, 0), 9)), Agent: p.Agent, RequestedBy: p.RequestedBy, TurnID: p.Turn, // #nosec G115 -- capped
		RunID: p.Run, Model: p.Model, Prompt: p.Prompt, ToolsSha256: p.ToolsSHA256, ProposedAt: p.ProposedAt, ExpiresAt: p.ExpiresAt})
	if err != nil {
		return storage.AIProposal{}, mapError(err)
	}
	out, err := toAIProposal(row)
	if err != nil {
		return storage.AIProposal{}, err
	}
	return out, r.audit(ctx, "proposed", out, map[string][2]any{"proposal": {nil, map[string]any{
		"task_type": out.TaskType, "task_version": out.TaskVersion, "payload_sha256": out.PayloadSHA256, "items": out.ItemCount,
		"risk_tier": out.RiskTier, "agent": out.Agent, "requested_by": out.RequestedBy.String(), "model": out.Model, "prompt": out.Prompt,
		"expires_at": out.ExpiresAt.UTC().Format(time.RFC3339)}}})
}

func (r aiProposals) Get(ctx context.Context, id uuid.UUID) (storage.AIProposal, error) {
	if err := r.u.check(); err != nil {
		return storage.AIProposal{}, err
	}
	row, err := r.u.q.GetAIProposal(ctx, id)
	if err != nil {
		return storage.AIProposal{}, mapError(err)
	}
	return toAIProposal(row)
}

func (r aiProposals) List(ctx context.Context, q storage.AIProposalQuery) ([]storage.AIProposal, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIProposalsPage(ctx, sqlcdb.AIProposalsPageParams{Statuses: statusesOf(q.Statuses), RequestedBy: q.RequestedBy, TurnID: q.Turn,
		After: q.After, MaxRows: int32(min(max(q.Limit, 1), 500))}) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIProposal, 0, len(rows))
	for _, row := range rows {
		p, err := toAIProposal(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r aiProposals) Invalid(ctx context.Context, id uuid.UUID, version int, reason string) (storage.AIProposal, error) {
	return r.step(ctx, id, version, "invalid", map[string][2]any{"reason": {nil, reason}}, func() (sqlcdb.FyniqAiProposal, error) {
		return r.u.q.InvalidAIProposal(ctx, sqlcdb.InvalidAIProposalParams{Reason: reason, ID: id, Version: v32(version)})
	})
}

func (r aiProposals) Pending(ctx context.Context, id uuid.UUID, version int, impact json.RawMessage, base map[string]int64,
	request *uuid.UUID) (storage.AIProposal, error) {
	versions, err := json.Marshal(versionsOf(base))
	if err != nil {
		return storage.AIProposal{}, err
	}
	changes := map[string][2]any{"impact_sha256": {nil, hashOf([]byte(impact))}, "base_versions": {nil, versionsOf(base)}}
	if request != nil {
		changes["approval_request_id"] = [2]any{nil, request.String()}
	}
	return r.step(ctx, id, version, "pending", changes, func() (sqlcdb.FyniqAiProposal, error) {
		return r.u.q.PendingAIProposal(ctx, sqlcdb.PendingAIProposalParams{Impact: impact, BaseVersions: versions, ApprovalRequestID: request,
			ID: id, Version: v32(version)})
	})
}

func (r aiProposals) Approve(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, items []int, payload string,
	at time.Time) (storage.AIProposal, error) {
	sum := hashOf([]byte(payload))
	var chosen []int32
	for _, n := range items {
		chosen = append(chosen, int32(min(max(n, 0), 1<<30))) // #nosec G115 -- capped
	}
	changes := map[string][2]any{"decided_by": {nil, by.String()}, "approved_sha256": {nil, sum}}
	if len(items) > 0 {
		changes["approved_items"] = [2]any{nil, items}
	}
	return r.step(ctx, id, version, "approved", changes, func() (sqlcdb.FyniqAiProposal, error) {
		return r.u.q.ApproveAIProposal(ctx, sqlcdb.ApproveAIProposalParams{DecidedBy: &by, DecidedAt: &at, ApprovedItems: chosen,
			ApprovedPayload: &payload, ApprovedSha256: &sum, ID: id, Version: v32(version)})
	})
}

func (r aiProposals) Reject(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, reason string, at time.Time) (storage.AIProposal, error) {
	return r.step(ctx, id, version, "rejected", map[string][2]any{"decided_by": {nil, by.String()}, "reason": {nil, reason}},
		func() (sqlcdb.FyniqAiProposal, error) {
			return r.u.q.RejectAIProposal(ctx, sqlcdb.RejectAIProposalParams{DecidedBy: &by, DecidedAt: &at, Reason: reason, ID: id, Version: v32(version)})
		})
}

func (r aiProposals) Supersede(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time) (storage.AIProposal, error) {
	return r.step(ctx, id, version, "superseded", map[string][2]any{"superseded_by": {nil, by.String()}}, func() (sqlcdb.FyniqAiProposal, error) {
		return r.u.q.SupersedeAIProposal(ctx, sqlcdb.SupersedeAIProposalParams{SupersededBy: &by, DecidedAt: &at, ID: id, Version: v32(version)})
	})
}

func (r aiProposals) Expire(ctx context.Context, id uuid.UUID, version int, at time.Time) (storage.AIProposal, error) {
	return r.step(ctx, id, version, "expired", map[string][2]any{"expired_at": {nil, at.UTC().Format(time.RFC3339)}},
		func() (sqlcdb.FyniqAiProposal, error) {
			return r.u.q.ExpireAIProposal(ctx, sqlcdb.ExpireAIProposalParams{DecidedAt: &at, ID: id, Version: v32(version)})
		})
}

func (r aiProposals) Start(ctx context.Context, id uuid.UUID, version int, at time.Time) (storage.AIProposal, error) {
	return r.step(ctx, id, version, "executing", map[string][2]any{"started_at": {nil, at.UTC().Format(time.RFC3339)}},
		func() (sqlcdb.FyniqAiProposal, error) {
			return r.u.q.StartAIProposal(ctx, sqlcdb.StartAIProposalParams{StartedAt: &at, ID: id, Version: v32(version)})
		})
}

func (r aiProposals) Finish(ctx context.Context, id uuid.UUID, version int, status, reason string, execution json.RawMessage,
	at time.Time) (storage.AIProposal, error) {
	changes := map[string][2]any{"execution_sha256": {nil, hashOf([]byte(execution))}}
	if reason != "" {
		changes["reason"] = [2]any{nil, reason}
	}
	return r.step(ctx, id, version, status, changes, func() (sqlcdb.FyniqAiProposal, error) {
		return r.u.q.FinishAIProposal(ctx, sqlcdb.FinishAIProposalParams{Status: status, Reason: reason, Execution: execution, FinishedAt: &at,
			ID: id, Version: v32(version)})
	})
}

// step runs one step of the lifecycle at the version read: none matching is a conflict, or the proposal does not exist.
// The database's guard refuses a step out of the lifecycle, or by the wrong role.
func (r aiProposals) step(ctx context.Context, id uuid.UUID, version int, to string, changes map[string][2]any,
	run func() (sqlcdb.FyniqAiProposal, error)) (storage.AIProposal, error) {
	if err := r.u.check(); err != nil {
		return storage.AIProposal{}, err
	}
	row, err := run()
	if notFound(err) {
		if _, err := r.u.q.GetAIProposal(ctx, id); err != nil {
			return storage.AIProposal{}, mapError(err)
		}
		return storage.AIProposal{}, &storage.Error{Code: storage.CodeVersion, Message: "the proposal moved since it was read"}
	}
	if err != nil {
		return storage.AIProposal{}, mapError(err)
	}
	out, err := toAIProposal(row)
	if err != nil {
		return storage.AIProposal{}, err
	}
	changes["status"] = [2]any{nil, out.Status}
	if err := r.audit(ctx, to, out, changes); err != nil {
		return storage.AIProposal{}, err
	}
	if to == storage.ProposalSuperseded || to == storage.ProposalExpired { // the database voided its pending request (000382)
		if err := r.u.auditVoids(ctx); err != nil {
			return storage.AIProposal{}, err
		}
	}
	return out, nil
}

func (r aiProposals) audit(ctx context.Context, to string, p storage.AIProposal, changes map[string][2]any) error {
	version := p.Version
	return r.u.event(ctx, "ai_proposal."+to, "ai_proposal", p.ID, &version, changes)
}

func toAIProposal(row sqlcdb.FyniqAiProposal) (storage.AIProposal, error) {
	p := storage.AIProposal{ID: row.ID, TaskType: row.TaskType, TaskVersion: int(row.TaskVersion), TaskSHA256: row.TaskSha256, Payload: row.Payload,
		PayloadSHA256: row.PayloadSha256, ItemCount: int(row.ItemCount), Rationale: row.Rationale, RiskTier: int(row.RiskTier),
		RequiredApprovals: int(row.RequiredApprovals), Agent: row.Agent, RequestedBy: row.RequestedBy, Turn: row.TurnID, Run: row.RunID,
		Model: row.Model, Prompt: row.Prompt, ToolsSHA256: row.ToolsSha256, Status: row.Status, Reason: row.Reason,
		ApprovalRequest: row.ApprovalRequestID, DecidedBy: row.DecidedBy, DecidedAt: row.DecidedAt, SupersededBy: row.SupersededBy,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, ProposedAt: row.ProposedAt, ExpiresAt: row.ExpiresAt, Version: int(row.Version)}
	if row.ApprovedPayload != nil {
		p.ApprovedPayload = *row.ApprovedPayload
	}
	if row.ApprovedSha256 != nil {
		p.ApprovedSHA256 = *row.ApprovedSha256
	}
	for _, n := range row.ApprovedItems {
		p.ApprovedItems = append(p.ApprovedItems, int(n))
	}
	if len(row.Impact) > 0 {
		p.Impact = json.RawMessage(row.Impact)
	}
	if len(row.Execution) > 0 {
		p.Execution = json.RawMessage(row.Execution)
	}
	if err := json.Unmarshal(row.Evidence, &p.Evidence); err != nil {
		return storage.AIProposal{}, err
	}
	if err := json.Unmarshal(row.BaseVersions, &p.BaseVersions); err != nil {
		return storage.AIProposal{}, err
	}
	return p, nil
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func v32(version int) int32 { return int32(min(max(version, 0), 1<<30)) } // #nosec G115 -- capped

// versionsOf, evidenceOf and statusesOf are their argument, or empty rather than nil, as the columns and queries take them.
func versionsOf(m map[string]int64) map[string]int64 {
	if m == nil {
		return map[string]int64{}
	}
	return m
}

func evidenceOf(e []storage.AIEvidence) []storage.AIEvidence {
	if e == nil {
		return []storage.AIEvidence{}
	}
	return e
}

func statusesOf(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
