package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Operations() storage.Operations { return operations{u} }

type operations struct{ u *unitOfWork }

func (r operations) Create(ctx context.Context, n storage.NewOperation) (storage.Operation, error) {
	if err := r.u.check(); err != nil {
		return storage.Operation{}, err
	}
	id, err := r.u.q.InsertOperation(ctx, sqlcdb.InsertOperationParams{Kind: n.Kind, JobID: n.JobID, Resource: n.Resource, RequestedBy: n.RequestedBy,
		RequestedAt: n.RequestedAt})
	if err != nil {
		return storage.Operation{}, mapError(err)
	}
	if err := r.u.event(ctx, "operation.asked", "operation", id, nil, map[string][2]any{"kind": {nil, n.Kind}, "job": {nil, n.JobID.String()},
		"resource": {nil, n.Resource}}); err != nil {
		return storage.Operation{}, err
	}
	return r.Get(ctx, id)
}

func (r operations) Get(ctx context.Context, id uuid.UUID) (storage.Operation, error) {
	if err := r.u.check(); err != nil {
		return storage.Operation{}, err
	}
	row, err := r.u.q.GetOperation(ctx, id)
	if err != nil {
		return storage.Operation{}, mapError(err)
	}
	return toOperation(sqlcdb.OperationsOfRow(row)), nil
}

func (r operations) Mine(ctx context.Context, requestedBy string, limit int) ([]storage.Operation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.OperationsOf(ctx, sqlcdb.OperationsOfParams{RequestedBy: requestedBy, PageSize: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Operation, 0, len(rows))
	for _, row := range rows {
		out = append(out, toOperation(row))
	}
	return out, nil
}

func toOperation(row sqlcdb.OperationsOfRow) storage.Operation {
	return storage.Operation{ID: row.ID, Kind: row.Kind, JobID: row.JobID, Resource: row.Resource, RequestedBy: row.RequestedBy,
		RequestedAt: row.RequestedAt, JobState: storage.OutboxState(row.JobState), JobUpdated: row.JobUpdated.In(time.UTC)}
}
