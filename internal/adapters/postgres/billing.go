package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Billing() storage.Billing { return billing{u} }

type billing struct{ u *unitOfWork }

func (b billing) Billed(ctx context.Context, project uuid.UUID) ([]storage.BilledItem, error) {
	if err := b.u.check(); err != nil {
		return nil, err
	}
	rows, err := b.u.q.BilledItems(ctx, project)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BilledItem, len(rows))
	for i, r := range rows {
		out[i] = storage.BilledItem{Kind: r.Kind, RefID: r.RefID, Line: int(r.LineNo), InvoiceID: r.InvoiceID}
	}
	return out, nil
}

func (b billing) BillableDocuments(ctx context.Context, project uuid.UUID, after *uuid.UUID, limit int) ([]uuid.UUID, error) {
	if err := b.u.check(); err != nil {
		return nil, err
	}
	ids, err := b.u.q.BillableDocuments(ctx, sqlcdb.BillableDocumentsParams{ProjectID: project.String(), AfterID: after,
		MaxRows: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	return ids, mapError(err)
}

func toMilestone(id, project uuid.UUID, kind, name string, amount string, due, accepted *time.Time, version int32, created time.Time, billed *uuid.UUID) storage.Milestone {
	return storage.Milestone{ID: id, ProjectID: project, Kind: kind, Name: name, Amount: storage.Decimal(amount), DueOn: due, AcceptedOn: accepted, Version: int(version),
		InvoiceID: billed, CreatedAt: created}
}

func (b billing) Milestones(ctx context.Context, project uuid.UUID) ([]storage.Milestone, error) {
	if err := b.u.check(); err != nil {
		return nil, err
	}
	rows, err := b.u.q.ProjectMilestones(ctx, project)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Milestone, len(rows))
	for i, r := range rows {
		out[i] = toMilestone(r.ID, r.ProjectID, r.Kind, r.Name, r.Amount, nullDate(r.DueOn), nullDate(r.AcceptedOn), r.Version, r.CreatedAt, r.BilledOn)
	}
	return out, nil
}

func (b billing) GetMilestone(ctx context.Context, id uuid.UUID) (storage.Milestone, error) {
	if err := b.u.check(); err != nil {
		return storage.Milestone{}, err
	}
	r, err := b.u.q.ProjectMilestone(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Milestone{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.Milestone{}, mapError(err)
	}
	return toMilestone(r.ID, r.ProjectID, r.Kind, r.Name, r.Amount, nullDate(r.DueOn), nullDate(r.AcceptedOn), r.Version, r.CreatedAt, r.BilledOn), nil
}

func (b billing) CreateMilestone(ctx context.Context, m storage.NewMilestone) (storage.Milestone, error) {
	if err := b.u.check(); err != nil {
		return storage.Milestone{}, err
	}
	id, err := b.u.q.InsertProjectMilestone(ctx, sqlcdb.InsertProjectMilestoneParams{ProjectID: m.ProjectID, Kind: m.Kind, Name: m.Name, Amount: string(m.Amount), DueOn: pgDate(m.DueOn)})
	if err != nil {
		return storage.Milestone{}, mapError(err)
	}
	out, err := b.GetMilestone(ctx, id)
	if err != nil {
		return out, err
	}
	return out, b.u.event(ctx, "project.milestone_added", "project", m.ProjectID, nil, map[string][2]any{"milestone_id": {nil, id.String()}, "kind": {nil, m.Kind},
		"name": {nil, m.Name}, "amount": {nil, string(m.Amount)}})
}

func (b billing) AcceptMilestone(ctx context.Context, id uuid.UUID, version int, on time.Time) (storage.Milestone, error) {
	before, err := b.GetMilestone(ctx, id)
	if err != nil {
		return before, err
	}
	_, err = b.u.q.AcceptProjectMilestone(ctx, sqlcdb.AcceptProjectMilestoneParams{AcceptedOn: pgDate(&on), ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Milestone{}, &storage.Error{Code: storage.CodeVersion, Message: "the milestone " + before.Name + " changed since it was read"}
	}
	if err != nil {
		return storage.Milestone{}, mapError(err)
	}
	after, err := b.GetMilestone(ctx, id)
	if err != nil {
		return after, err
	}
	return after, b.u.event(ctx, "project.milestone_accepted", "project", before.ProjectID, nil, map[string][2]any{"milestone_id": {id.String(), id.String()},
		"version": {before.Version, after.Version}, "accepted_on": {nil, on.Format(time.DateOnly)}})
}
