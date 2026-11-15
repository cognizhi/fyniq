package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AgentRuns() storage.AgentRuns { return agentRuns{u} }

type agentRuns struct{ u *unitOfWork }

func (r agentRuns) Queue(ctx context.Context, n storage.NewAgentRun) (storage.AgentRun, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.AgentRun{}, false, err
	}
	row, err := r.u.q.QueueAgentRun(ctx, sqlcdb.QueueAgentRunParams{Agent: n.Agent, SkillVersion: v32(n.SkillVersion), SkillSha256: n.SkillSHA256,
		Trigger: n.Trigger, SlotAt: n.SlotAt, ActsFor: n.ActsFor, RequestedBy: n.RequestedBy, QueuedAt: n.QueuedAt})
	if notFound(err) { // queued already: the slot's run is the one there
		row, err := r.u.q.AgentRunBySlot(ctx, sqlcdb.AgentRunBySlotParams{Agent: n.Agent, Trigger: n.Trigger, SlotAt: n.SlotAt})
		if err != nil {
			return storage.AgentRun{}, false, mapError(err)
		}
		out, err := toAgentRun(row)
		return out, false, err
	}
	if err != nil {
		return storage.AgentRun{}, false, mapError(err)
	}
	out, err := toAgentRun(row)
	if err != nil {
		return storage.AgentRun{}, false, err
	}
	return out, true, r.audit(ctx, "queued", out, map[string][2]any{"run": {nil, map[string]any{"agent": out.Agent,
		"skill_version": out.SkillVersion, "skill_sha256": out.SkillSHA256, "trigger": out.Trigger, "slot_at": out.SlotAt.UTC().Format(time.RFC3339),
		"acts_for": out.ActsFor.String()}}})
}

func (r agentRuns) Get(ctx context.Context, id uuid.UUID) (storage.AgentRun, error) {
	if err := r.u.check(); err != nil {
		return storage.AgentRun{}, err
	}
	row, err := r.u.q.GetAgentRun(ctx, id)
	if err != nil {
		return storage.AgentRun{}, mapError(err)
	}
	return toAgentRun(row)
}

func (r agentRuns) List(ctx context.Context, q storage.AgentRunQuery) ([]storage.AgentRun, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 500
	}
	statuses := q.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := r.u.q.AgentRunsPage(ctx, sqlcdb.AgentRunsPageParams{Agent: q.Agent, Statuses: statuses, After: q.After, MaxRows: int32(q.Limit)}) // #nosec G115 -- at most 500
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AgentRun, 0, len(rows))
	for _, row := range rows {
		run, err := toAgentRun(row)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (r agentRuns) LastDone(ctx context.Context, agent string) (storage.AgentRun, error) {
	if err := r.u.check(); err != nil {
		return storage.AgentRun{}, err
	}
	row, err := r.u.q.LastDoneAgentRun(ctx, agent)
	if err != nil {
		return storage.AgentRun{}, mapError(err)
	}
	return toAgentRun(row)
}

func (r agentRuns) Start(ctx context.Context, id uuid.UUID, version int, at time.Time) (storage.AgentRun, error) {
	return r.step(ctx, id, "started", map[string][2]any{"started_at": {nil, at.UTC().Format(time.RFC3339)}}, func() (sqlcdb.FyniqAgentRun, error) {
		return r.u.q.StartAgentRun(ctx, sqlcdb.StartAgentRunParams{StartedAt: &at, ID: id, Version: v32(version)})
	})
}

func (r agentRuns) Skip(ctx context.Context, id uuid.UUID, version int, reason string, at time.Time) (storage.AgentRun, error) {
	return r.step(ctx, id, "skipped", map[string][2]any{"reason": {nil, reason}}, func() (sqlcdb.FyniqAgentRun, error) {
		return r.u.q.SkipAgentRun(ctx, sqlcdb.SkipAgentRunParams{Reason: reason, FinishedAt: &at, ID: id, Version: v32(version)})
	})
}

func (r agentRuns) Fail(ctx context.Context, id uuid.UUID, version int, reason string, at time.Time) (storage.AgentRun, error) {
	return r.step(ctx, id, "failed", map[string][2]any{"reason": {nil, reason}}, func() (sqlcdb.FyniqAgentRun, error) {
		return r.u.q.FailAgentRun(ctx, sqlcdb.FailAgentRunParams{Reason: reason, FinishedAt: &at, ID: id, Version: v32(version)})
	})
}

func (r agentRuns) Finish(ctx context.Context, id uuid.UUID, version int, rep storage.AgentReport, at time.Time) (storage.AgentRun, error) {
	packs, err := json.Marshal(nonNilMap(rep.PackVersions))
	if err != nil {
		return storage.AgentRun{}, err
	}
	findings := []byte(rep.Findings)
	if len(findings) == 0 {
		findings = []byte("[]")
	}
	var model, text *string
	if rep.ModelUsed {
		model, text = &rep.Model, &rep.ModelText
		if rep.ModelText == "" {
			text = nil
		}
	}
	tokens, calls := int32(min(max(rep.Tokens, 0), 1<<30)), int32(min(max(rep.ToolCalls, 0), 1<<30)) // #nosec G115 -- capped
	changes := map[string][2]any{"fingerprint": {nil, rep.Fingerprint}, "ledger_revision": {nil, rep.LedgerRevision},
		"findings_sha256": {nil, hashOf(findings)}, "model_used": {nil, rep.ModelUsed}}
	return r.step(ctx, id, "done", changes, func() (sqlcdb.FyniqAgentRun, error) {
		return r.u.q.FinishAgentRun(ctx, sqlcdb.FinishAgentRunParams{FinishedAt: &at, Fingerprint: &rep.Fingerprint, LedgerRevision: &rep.LedgerRevision,
			PackVersions: packs, KbSnapshot: &rep.KBSnapshot, Findings: findings, Summary: &rep.Summary, ModelUsed: &rep.ModelUsed, Model: model,
			ModelText: text, Tokens: &tokens, ToolCalls: &calls, ID: id, Version: v32(version)})
	})
}

func (r agentRuns) step(ctx context.Context, id uuid.UUID, to string, changes map[string][2]any, run func() (sqlcdb.FyniqAgentRun, error)) (storage.AgentRun, error) {
	if err := r.u.check(); err != nil {
		return storage.AgentRun{}, err
	}
	row, err := run()
	if notFound(err) {
		if _, err := r.u.q.GetAgentRun(ctx, id); err != nil {
			return storage.AgentRun{}, mapError(err)
		}
		return storage.AgentRun{}, &storage.Error{Code: storage.CodeVersion, Message: "the run moved since it was read"}
	}
	if err != nil {
		return storage.AgentRun{}, mapError(err)
	}
	out, err := toAgentRun(row)
	if err != nil {
		return storage.AgentRun{}, err
	}
	changes["status"] = [2]any{nil, out.Status}
	return out, r.audit(ctx, to, out, changes)
}

func (r agentRuns) audit(ctx context.Context, to string, run storage.AgentRun, changes map[string][2]any) error {
	version := run.Version
	return r.u.event(ctx, "agent_run."+to, "agent_run", run.ID, &version, changes)
}

func toAgentRun(row sqlcdb.FyniqAgentRun) (storage.AgentRun, error) {
	out := storage.AgentRun{ID: row.ID, Agent: row.Agent, SkillVersion: int(row.SkillVersion), SkillSHA256: row.SkillSha256, Trigger: row.Trigger,
		SlotAt: row.SlotAt, ActsFor: row.ActsFor, RequestedBy: row.RequestedBy, Status: row.Status, Reason: row.Reason, QueuedAt: row.QueuedAt,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, Version: int(row.Version)}
	if row.Status != storage.AgentRunDone {
		return out, nil
	}
	rep := storage.AgentReport{Findings: json.RawMessage(row.Findings), PackVersions: map[string]string{}}
	if err := json.Unmarshal(row.PackVersions, &rep.PackVersions); err != nil {
		return storage.AgentRun{}, err
	}
	rep.Fingerprint, rep.LedgerRevision, rep.KBSnapshot, rep.Summary = deref(row.Fingerprint), deref(row.LedgerRevision), deref(row.KbSnapshot), deref(row.Summary)
	rep.ModelUsed, rep.Model, rep.ModelText = deref(row.ModelUsed), deref(row.Model), deref(row.ModelText)
	rep.Tokens, rep.ToolCalls = int(deref(row.Tokens)), int(deref(row.ToolCalls))
	out.Report = &rep
	return out, nil
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// deref is *p, or the zero value when p is nil.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
