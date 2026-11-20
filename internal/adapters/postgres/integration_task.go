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

// ----------------------------------------------- tasks made by integrations (FR-INT-24; p2-api-third-parties task 2)

func (u *unitOfWork) IntegrationTasks() storage.IntegrationTasks { return integrationTasks{u} }

type integrationTasks struct{ u *unitOfWork }

func toIntegrationTask(r sqlcdb.FyniqIntegrationTask) storage.IntegrationTask {
	return storage.IntegrationTask{ID: r.ID, ClientID: r.ClientID, ExternalRef: r.ExternalRef, Title: r.Title, Detail: r.Detail, DueOn: nullDate(r.DueOn),
		LinkURL: deref(r.LinkUrl), Status: r.Status, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ClosedAt: r.ClosedAt, Version: int(r.Version)}
}

func (r integrationTasks) Make(ctx context.Context, n storage.NewIntegrationTask) (storage.IntegrationTask, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.IntegrationTask{}, false, err
	}
	row, err := r.u.q.InsertIntegrationTask(ctx, sqlcdb.InsertIntegrationTaskParams{ClientID: n.ClientID, ExternalRef: n.ExternalRef, Title: n.Title, Detail: n.Detail,
		DueOn: pgDate(n.DueOn), LinkUrl: optText(n.LinkURL), CreatedBy: n.CreatedBy, At: n.At})
	if errors.Is(err, pgx.ErrNoRows) { // the client made it already: a retried create finds its task
		again, err := r.u.q.GetIntegrationTaskByRef(ctx, sqlcdb.GetIntegrationTaskByRefParams{ClientID: n.ClientID, ExternalRef: n.ExternalRef})
		if err != nil {
			return storage.IntegrationTask{}, false, mapError(err)
		}
		return toIntegrationTask(again), false, nil
	}
	if err != nil {
		return storage.IntegrationTask{}, false, mapError(err)
	}
	t := toIntegrationTask(row)
	v := t.Version
	return t, true, r.u.event(ctx, "integration_task.created", "integration_task", t.ID, &v, map[string][2]any{"client_id": {nil, t.ClientID},
		"external_ref": {nil, t.ExternalRef}, "title": {nil, t.Title}, "due_on": {nil, dateText(t.DueOn)}})
}

func (r integrationTasks) Get(ctx context.Context, id uuid.UUID) (storage.IntegrationTask, error) {
	if err := r.u.check(); err != nil {
		return storage.IntegrationTask{}, err
	}
	row, err := r.u.q.GetIntegrationTask(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.IntegrationTask{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.IntegrationTask{}, mapError(err)
	}
	return toIntegrationTask(row), nil
}

func (r integrationTasks) List(ctx context.Context, q storage.IntegrationTaskQuery) ([]storage.IntegrationTask, error) {
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
	rows, err := r.u.q.IntegrationTasksPage(ctx, sqlcdb.IntegrationTasksPageParams{Statuses: statuses, ClientID: q.ClientID, After: q.After, MaxRows: int32(q.Limit)}) // #nosec G115 -- at most 500
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.IntegrationTask, len(rows))
	for i, row := range rows {
		out[i] = toIntegrationTask(row)
	}
	return out, nil
}

func (r integrationTasks) Update(ctx context.Context, client string, id uuid.UUID, version int, c storage.IntegrationTaskChange) (storage.IntegrationTask, error) {
	if err := r.u.check(); err != nil {
		return storage.IntegrationTask{}, err
	}
	before, err := r.u.q.GetIntegrationTask(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && before.ClientID != client) { // another client's task is not found, as a task of no one's is
		return storage.IntegrationTask{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.IntegrationTask{}, mapError(err)
	}
	var closed *time.Time
	if c.Status != storage.IntegrationTaskOpen {
		at := c.At
		closed = &at
	}
	row, err := r.u.q.UpdateIntegrationTask(ctx, sqlcdb.UpdateIntegrationTaskParams{Title: c.Title, Detail: c.Detail, DueOn: pgDate(c.DueOn), LinkUrl: optText(c.LinkURL),
		Status: c.Status, ClosedAt: closed, At: c.At, ID: id, ClientID: client, Version: v32(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.IntegrationTask{}, &storage.Error{Code: storage.CodeVersion, Message: "the task changed since it was read"}
	}
	if err != nil {
		return storage.IntegrationTask{}, mapError(err)
	}
	t := toIntegrationTask(row)
	v := t.Version
	was, now := toIntegrationTask(before), t
	changes := map[string][2]any{}
	if was.Title != now.Title {
		changes["title"] = [2]any{was.Title, now.Title}
	}
	if was.Detail != now.Detail {
		changes["detail"] = [2]any{was.Detail, now.Detail}
	}
	if dateText(was.DueOn) != dateText(now.DueOn) {
		changes["due_on"] = [2]any{dateText(was.DueOn), dateText(now.DueOn)}
	}
	if was.LinkURL != now.LinkURL {
		changes["link_url"] = [2]any{was.LinkURL, now.LinkURL}
	}
	if was.Status != now.Status {
		changes["status"] = [2]any{was.Status, now.Status}
	}
	return t, r.u.event(ctx, "integration_task."+map[string]string{storage.IntegrationTaskOpen: "updated", storage.IntegrationTaskDone: "done", storage.IntegrationTaskCancelled: "cancelled"}[t.Status], "integration_task", t.ID, &v, changes)
}
