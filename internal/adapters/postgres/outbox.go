package postgres

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type outbox struct{ u *unitOfWork }

func (u *unitOfWork) Outbox() storage.Outbox { return outbox{u} }

// AfterCommit implements storage.UnitOfWork.
func (u *unitOfWork) AfterCommit(fn func(ctx context.Context)) {
	u.afterCommit = append(u.afterCommit, fn)
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func toOutbox(id uuid.UUID, class, kind string, payload []byte, key *string, state string, attempts, max, dispatches int32,
	task, holder *string, until, deadline *time.Time, lastErr *string) storage.OutboxRow {
	return storage.OutboxRow{ID: id, Class: storage.WorkerClass(class), Kind: kind, Payload: payload, BusinessKey: str(key),
		State: storage.OutboxState(state), Attempts: int(attempts), MaxAttempts: int(max), Dispatches: int(dispatches),
		TaskName: str(task), LeaseHolder: str(holder), LeaseUntil: until, Deadline: deadline, LastError: str(lastErr)}
}

func (r outbox) Enqueue(ctx context.Context, item storage.NewOutboxItem) (storage.OutboxRow, error) {
	if err := r.u.check(); err != nil {
		return storage.OutboxRow{}, err
	}
	var key *string
	if item.BusinessKey != "" {
		key = &item.BusinessKey
	}
	payload := item.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	row, err := r.u.q.InsertOutbox(ctx, sqlcdb.InsertOutboxParams{WorkerClass: string(item.Class), Kind: item.Kind,
		Payload: payload, BusinessKey: key, MaxAttempts: int32(cmp.Or(item.MaxAttempts, 5))})
	if err != nil {
		return storage.OutboxRow{}, mapError(err)
	}
	return toOutbox(row.ID, row.WorkerClass, row.Kind, row.Payload, row.BusinessKey, row.State, row.Attempts, row.MaxAttempts,
		row.Dispatches, row.TaskName, row.LeaseHolder, row.LeaseExpiresAt, row.DeadlineAt, row.LastError), nil
}

func (r outbox) Get(ctx context.Context, id uuid.UUID) (storage.OutboxRow, error) {
	if err := r.u.check(); err != nil {
		return storage.OutboxRow{}, err
	}
	row, err := r.u.q.GetOutbox(ctx, id)
	if err != nil {
		return storage.OutboxRow{}, mapError(err)
	}
	return toOutbox(row.ID, row.WorkerClass, row.Kind, row.Payload, row.BusinessKey, row.State, row.Attempts, row.MaxAttempts,
		row.Dispatches, row.TaskName, row.LeaseHolder, row.LeaseExpiresAt, row.DeadlineAt, row.LastError), nil
}

func (r outbox) MarkDispatched(ctx context.Context, id uuid.UUID, taskName string, deadline time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	_, err := r.u.q.MarkOutboxDispatched(ctx, sqlcdb.MarkOutboxDispatchedParams{ID: id, TaskName: &taskName, DeadlineAt: &deadline})
	if errors.Is(err, pgx.ErrNoRows) {
		return &storage.Error{Code: storage.CodeOutbox, Message: "the outbox row is done, dead or gone, so it cannot be dispatched"}
	}
	return mapError(err)
}

func (r outbox) Lease(ctx context.Context, id uuid.UUID, holder string, now, until time.Time) (storage.OutboxRow, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.OutboxRow{}, false, err
	}
	row, err := r.u.q.LeaseOutbox(ctx, sqlcdb.LeaseOutboxParams{ID: id, Holder: &holder, Until: &until, Now: &now})
	if errors.Is(err, pgx.ErrNoRows) {
		cur, err := r.Get(ctx, id)
		return cur, false, err
	}
	if err != nil {
		return storage.OutboxRow{}, false, mapError(err)
	}
	return toOutbox(row.ID, row.WorkerClass, row.Kind, row.Payload, row.BusinessKey, row.State, row.Attempts, row.MaxAttempts,
		row.Dispatches, row.TaskName, row.LeaseHolder, row.LeaseExpiresAt, row.DeadlineAt, row.LastError), true, nil
}

func (r outbox) Finish(ctx context.Context, id uuid.UUID, holder string, state storage.OutboxState, lastError string) error {
	if err := r.u.check(); err != nil {
		return err
	}
	var msg *string
	if lastError != "" {
		if len(lastError) > 2000 {
			lastError = lastError[:2000]
		}
		msg = &lastError
	}
	_, err := r.u.q.FinishOutbox(ctx, sqlcdb.FinishOutboxParams{ID: id, Holder: &holder, State: string(state), LastError: msg})
	if errors.Is(err, pgx.ErrNoRows) {
		return &storage.Error{Code: storage.CodeOutbox, Message: "the lease on the outbox row was lost to another holder",
			Hint: "Another delivery took the row after this lease expired; its outcome stands"}
	}
	return mapError(err)
}
