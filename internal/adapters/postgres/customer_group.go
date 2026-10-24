package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) CustomerGroups() storage.CustomerGroups { return customerGroups{u} }

func (u *unitOfWork) SalesReminders() storage.SalesReminders { return salesReminders{u} }

type customerGroups struct{ u *unitOfWork }

// customerGroupRow is every customer group query's row: they all select the same columns.
type customerGroupRow = sqlcdb.GetCustomerGroupRow

func toCustomerGroup(r customerGroupRow) storage.CustomerGroup {
	days := make([]int, 0, len(r.ReminderDays))
	for _, d := range r.ReminderDays {
		days = append(days, int(d))
	}
	return storage.CustomerGroup{ID: r.ID, Name: r.Name, ReminderDays: days, Active: r.Active, Version: int(r.Version)}
}

// reminderDays is a schedule as the column holds it, refused outside the column's range (the domain checks the rest).
func reminderDays(days []int) ([]int32, error) {
	out := make([]int32, 0, len(days))
	for _, d := range days {
		if d < -30 || d > 365 {
			return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a reminder %d days from the due date", d), Hint: "Use -30 to 365 days"}
		}
		out = append(out, int32(d)) // #nosec G115 -- -30 to 365, checked above
	}
	return out, nil
}

func (r customerGroups) List(ctx context.Context) ([]storage.CustomerGroup, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListCustomerGroups(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CustomerGroup, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCustomerGroup(customerGroupRow(row)))
	}
	return out, nil
}

func (r customerGroups) Get(ctx context.Context, id uuid.UUID) (storage.CustomerGroup, error) {
	if err := r.u.check(); err != nil {
		return storage.CustomerGroup{}, err
	}
	row, err := r.u.q.GetCustomerGroup(ctx, id)
	if err != nil {
		return storage.CustomerGroup{}, mapError(err)
	}
	return toCustomerGroup(row), nil
}

func (r customerGroups) Create(ctx context.Context, name string, days []int) (storage.CustomerGroup, error) {
	if err := r.u.check(); err != nil {
		return storage.CustomerGroup{}, err
	}
	d, err := reminderDays(days)
	if err != nil {
		return storage.CustomerGroup{}, err
	}
	row, err := r.u.q.InsertCustomerGroup(ctx, sqlcdb.InsertCustomerGroupParams{Name: name, ReminderDays: d})
	if err != nil {
		return storage.CustomerGroup{}, mapError(err)
	}
	g := toCustomerGroup(customerGroupRow(row))
	if err := r.u.event(ctx, "customer_group.created", "customer_group", g.ID, &g.Version, map[string][2]any{
		"name": {nil, g.Name}, "reminder_days": {nil, g.ReminderDays}, "active": {nil, g.Active}}); err != nil {
		return storage.CustomerGroup{}, err
	}
	return g, nil
}

func (r customerGroups) Update(ctx context.Context, id uuid.UUID, version int, name string, days []int, active bool) (storage.CustomerGroup, error) {
	if err := r.u.check(); err != nil {
		return storage.CustomerGroup{}, err
	}
	d, err := reminderDays(days)
	if err != nil {
		return storage.CustomerGroup{}, err
	}
	cur, err := r.u.q.LockCustomerGroup(ctx, id)
	if err != nil {
		return storage.CustomerGroup{}, mapError(err)
	}
	before := toCustomerGroup(customerGroupRow(cur))
	if before.Version != version {
		return storage.CustomerGroup{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the customer group changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	row, err := r.u.q.UpdateCustomerGroup(ctx, sqlcdb.UpdateCustomerGroupParams{ID: id, Name: name, ReminderDays: d, Active: active})
	if err != nil {
		return storage.CustomerGroup{}, mapError(err)
	}
	after := toCustomerGroup(customerGroupRow(row))
	changes := map[string][2]any{"version": {before.Version, after.Version}}
	if before.Name != after.Name {
		changes["name"] = [2]any{before.Name, after.Name}
	}
	if !slices.Equal(before.ReminderDays, after.ReminderDays) {
		changes["reminder_days"] = [2]any{before.ReminderDays, after.ReminderDays}
	}
	if before.Active != after.Active {
		changes["active"] = [2]any{before.Active, after.Active}
	}
	if err := r.u.event(ctx, "customer_group.updated", "customer_group", after.ID, &after.Version, changes); err != nil {
		return storage.CustomerGroup{}, err
	}
	return after, nil
}

type salesReminders struct{ u *unitOfWork }

func toSalesReminder(r sqlcdb.InsertSalesReminderRow) storage.SalesReminder {
	return storage.SalesReminder{ID: r.ID, DocumentID: r.DocumentID, Step: int(r.Step), Outcome: r.Outcome, SendID: r.SendID, Reason: str(r.Reason),
		DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt}
}

func (r salesReminders) Record(ctx context.Context, n storage.NewSalesReminder) (storage.SalesReminder, error) {
	if err := r.u.check(); err != nil {
		return storage.SalesReminder{}, err
	}
	if n.Step < -30 || n.Step > 365 {
		return storage.SalesReminder{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a reminder %d days from the due date", n.Step),
			Hint: "Use -30 to 365 days"}
	}
	actor := r.u.actor(ctx)
	row, err := r.u.q.InsertSalesReminder(ctx, sqlcdb.InsertSalesReminderParams{DocumentID: n.DocumentID, Step: int32(n.Step), // #nosec G115 -- -30 to 365, checked above
		Outcome: n.Outcome, SendID: n.SendID, Reason: opt(n.Reason), DecidedBy: actor})
	if err != nil {
		return storage.SalesReminder{}, mapError(err)
	}
	out := toSalesReminder(row)
	send := any(nil)
	if n.SendID != nil {
		send = n.SendID.String()
	}
	if err := r.u.event(ctx, "sales_reminder."+n.Outcome, "sales_reminder", out.ID, nil, map[string][2]any{
		"document_id": {nil, n.DocumentID.String()}, "step": {nil, n.Step}, "outcome": {nil, n.Outcome}, "send_id": {nil, send},
		"reason": {nil, nilIfEmpty(n.Reason)}}); err != nil {
		return storage.SalesReminder{}, err
	}
	return out, nil
}

func (r salesReminders) Of(ctx context.Context, documents []uuid.UUID) (map[uuid.UUID][]storage.SalesReminder, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]storage.SalesReminder{}
	if len(documents) == 0 {
		return out, nil
	}
	rows, err := r.u.q.SalesRemindersOf(ctx, documents)
	if err != nil {
		return nil, mapError(err)
	}
	for _, row := range rows {
		out[row.DocumentID] = append(out[row.DocumentID], toSalesReminder(sqlcdb.InsertSalesReminderRow(row)))
	}
	return out, nil
}
