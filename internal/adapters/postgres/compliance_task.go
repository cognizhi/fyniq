package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) ComplianceTasks() storage.ComplianceTasks { return complianceTasks{u} }

type complianceTasks struct{ u *unitOfWork }

func (r complianceTasks) RaiseForRun(ctx context.Context, run uuid.UUID, t storage.NewComplianceTask) (storage.ComplianceTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ComplianceTask{}, err
	}
	row, err := r.u.q.RaiseRunTask(ctx, sqlcdb.RaiseRunTaskParams{FindingKey: t.FindingKey, Title: t.Title, Detail: t.Detail, OwnerRole: optText(t.OwnerRole),
		OwnerUser: t.OwnerUser, DueOn: pgDate(t.DueOn), Escalated: t.Escalated, RunID: &run, CreatedAt: t.CreatedAt})
	return r.raised(ctx, row, err)
}

func (r complianceTasks) RaiseForProposal(ctx context.Context, proposal uuid.UUID, t storage.NewComplianceTask) (storage.ComplianceTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ComplianceTask{}, err
	}
	row, err := r.u.q.RaiseProposalTask(ctx, sqlcdb.RaiseProposalTaskParams{FindingKey: t.FindingKey, Title: t.Title, Detail: t.Detail,
		OwnerRole: optText(t.OwnerRole), OwnerUser: t.OwnerUser, DueOn: pgDate(t.DueOn), ProposalID: &proposal, CreatedAt: t.CreatedAt})
	return r.raised(ctx, row, err)
}

func (r complianceTasks) RaiseSystem(ctx context.Context, t storage.NewComplianceTask) (storage.ComplianceTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ComplianceTask{}, err
	}
	row, err := r.u.q.RaiseSystemTask(ctx, sqlcdb.RaiseSystemTaskParams{FindingKey: t.FindingKey, Title: t.Title, Detail: t.Detail, OwnerRole: optText(t.OwnerRole),
		OwnerUser: t.OwnerUser, DueOn: pgDate(t.DueOn), Escalated: t.Escalated, CreatedAt: t.CreatedAt})
	return r.raised(ctx, row, err)
}

func (r complianceTasks) raised(ctx context.Context, row sqlcdb.FyniqComplianceTask, err error) (storage.ComplianceTask, error) {
	if err != nil {
		return storage.ComplianceTask{}, mapError(err)
	}
	out := toComplianceTask(row)
	return out, r.audit(ctx, "raised", out, map[string][2]any{"task": {nil, map[string]any{"finding_key": out.FindingKey, "title": out.Title,
		"owner": ownerOf(out), "due_on": dateText(out.DueOn), "source": out.Source}}})
}

func (r complianceTasks) Get(ctx context.Context, id uuid.UUID) (storage.ComplianceTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ComplianceTask{}, err
	}
	row, err := r.u.q.GetComplianceTask(ctx, id)
	if err != nil {
		return storage.ComplianceTask{}, mapError(err)
	}
	return toComplianceTask(row), nil
}

func (r complianceTasks) OpenFor(ctx context.Context, findingKey string) (storage.ComplianceTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ComplianceTask{}, err
	}
	row, err := r.u.q.OpenComplianceTask(ctx, findingKey)
	if err != nil {
		return storage.ComplianceTask{}, mapError(err)
	}
	return toComplianceTask(row), nil
}

func (r complianceTasks) List(ctx context.Context, q storage.ComplianceTaskQuery) ([]storage.ComplianceTask, error) {
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
	rows, err := r.u.q.ComplianceTasksPage(ctx, sqlcdb.ComplianceTasksPageParams{Statuses: statuses, KeyPrefix: q.KeyPrefix, After: q.After,
		MaxRows: int32(q.Limit)}) // #nosec G115 -- at most 500
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ComplianceTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toComplianceTask(row))
	}
	return out, nil
}

func (r complianceTasks) Update(ctx context.Context, id uuid.UUID, version int, c storage.TaskChange) (storage.ComplianceTask, error) {
	return r.step(ctx, id, "updated", func() (sqlcdb.FyniqComplianceTask, error) {
		return r.u.q.UpdateComplianceTask(ctx, sqlcdb.UpdateComplianceTaskParams{Title: c.Title, Detail: c.Detail, OwnerRole: optText(c.OwnerRole),
			OwnerUser: c.OwnerUser, DueOn: pgDate(c.DueOn), Escalated: c.Escalated, ID: id, Version: v32(version)})
	}, func(was storage.ComplianceTask) map[string][2]any {
		return map[string][2]any{"title": {was.Title, c.Title}, "owner": {ownerOf(was), ownerOf(storage.ComplianceTask{OwnerRole: c.OwnerRole, OwnerUser: c.OwnerUser})},
			"due_on": {dateText(was.DueOn), dateText(c.DueOn)}, "escalated": {was.Escalated, c.Escalated}}
	})
}

func (r complianceTasks) Clear(ctx context.Context, id uuid.UUID, version int, at time.Time) (storage.ComplianceTask, error) {
	return r.step(ctx, id, "cleared", func() (sqlcdb.FyniqComplianceTask, error) {
		return r.u.q.ClearComplianceTask(ctx, sqlcdb.ClearComplianceTaskParams{ClosedAt: &at, ID: id, Version: v32(version)})
	}, func(storage.ComplianceTask) map[string][2]any { return map[string][2]any{"status": {storage.TaskOpen, storage.TaskCleared}} })
}

func (r complianceTasks) Close(ctx context.Context, id uuid.UUID, version int, status, reason string, by uuid.UUID, at time.Time) (storage.ComplianceTask, error) {
	return r.step(ctx, id, status, func() (sqlcdb.FyniqComplianceTask, error) {
		return r.u.q.CloseComplianceTask(ctx, sqlcdb.CloseComplianceTaskParams{Status: status, Reason: reason, ClosedAt: &at, ClosedBy: &by, ID: id,
			Version: v32(version)})
	}, func(storage.ComplianceTask) map[string][2]any {
		return map[string][2]any{"status": {storage.TaskOpen, status}, "reason": {nil, reason}}
	})
}

func (r complianceTasks) step(ctx context.Context, id uuid.UUID, to string, run func() (sqlcdb.FyniqComplianceTask, error),
	changes func(was storage.ComplianceTask) map[string][2]any) (storage.ComplianceTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ComplianceTask{}, err
	}
	before, err := r.u.q.GetComplianceTask(ctx, id)
	if err != nil {
		return storage.ComplianceTask{}, mapError(err)
	}
	row, err := run()
	if notFound(err) {
		return storage.ComplianceTask{}, &storage.Error{Code: storage.CodeVersion, Message: "the task changed since it was read"}
	}
	if err != nil {
		return storage.ComplianceTask{}, mapError(err)
	}
	out := toComplianceTask(row)
	return out, r.audit(ctx, to, out, changes(toComplianceTask(before)))
}

func (r complianceTasks) audit(ctx context.Context, to string, t storage.ComplianceTask, changes map[string][2]any) error {
	version := t.Version
	return r.u.event(ctx, "compliance_task."+to, "compliance_task", t.ID, &version, changes)
}

func toComplianceTask(row sqlcdb.FyniqComplianceTask) storage.ComplianceTask {
	return storage.ComplianceTask{ID: row.ID, FindingKey: row.FindingKey, Title: row.Title, Detail: row.Detail, OwnerRole: deref(row.OwnerRole),
		OwnerUser: row.OwnerUser, DueOn: nullDate(row.DueOn), Escalated: row.Escalated, Source: row.Source, Run: row.RunID, Proposal: row.ProposalID,
		Status: row.Status, Reason: row.Reason, CreatedAt: row.CreatedAt, ClosedAt: row.ClosedAt, ClosedBy: row.ClosedBy, Version: int(row.Version)}
}

func ownerOf(t storage.ComplianceTask) string {
	if t.OwnerUser != nil {
		return "user:" + t.OwnerUser.String()
	}
	return "role:" + t.OwnerRole
}

func dateText(d *time.Time) any {
	if d == nil {
		return nil
	}
	return d.Format(time.DateOnly)
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
