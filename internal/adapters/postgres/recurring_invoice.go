package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// recurringInvoices keeps the recurring invoices (FR-AR-07; PRD-001 rev 0.135) and makes the scheduler's draft invoices.
type recurringInvoices struct{ u *unitOfWork }

func (u *unitOfWork) RecurringInvoices() storage.RecurringInvoices { return recurringInvoices{u} }

func toRecurringInvoice(row sqlcdb.GetRecurringInvoiceRow) storage.RecurringInvoice {
	out := storage.RecurringInvoice{ID: row.ID, Name: row.Name, Status: storage.RecurringStatus(row.Status),
		Schedule: storage.Schedule{EveryMonths: int(row.EveryMonths), EveryWeeks: int(row.EveryWeeks), DayOfMonth: int(row.DayOfMonth), StartsOn: row.StartsOn,
			EndsOn: nullDate(row.EndsOn)},
		NextOn: nullDate(row.NextOn), Version: int(row.Version), ContentVersion: int(row.ContentVersion), Content: row.Content,
		EditedBy: row.EditedBy, EditedAt: row.EditedAt, LastOn: nullDate(row.LastOn), LastInvoiceID: row.LastInvoiceID,
		Mode: row.Mode, ApprovedBy: str(row.ApprovedBy), ApprovedAt: row.ApprovedAt}
	if row.ApprovedVersion != nil {
		v := int(*row.ApprovedVersion)
		out.ApprovedVersion = &v
	}
	return out
}

// recurringInvoiceChanges are the audited differences between two states of a recurring invoice; content only when it
// changed, as its new version.
func recurringInvoiceChanges(a, b storage.RecurringInvoice) (map[string][2]any, error) {
	out := map[string][2]any{}
	diff := func(k string, x, y any) {
		if x != y {
			out[k] = [2]any{x, y}
		}
	}
	var zero storage.RecurringInvoice
	if a.ID == zero.ID {
		diff("name", nil, b.Name)
		diff("status", nil, string(b.Status))
		diff("every_months", nil, b.Schedule.EveryMonths)
		diff("every_weeks", nil, b.Schedule.EveryWeeks)
		diff("day_of_month", nil, b.Schedule.DayOfMonth)
		diff("starts_on", nil, b.Schedule.StartsOn.Format(time.DateOnly))
		diff("ends_on", nil, optDay(b.Schedule.EndsOn))
		diff("next_on", nil, optDay(b.NextOn))
		diff("mode", nil, b.Mode)
	} else {
		diff("name", a.Name, b.Name)
		diff("status", string(a.Status), string(b.Status))
		diff("every_months", a.Schedule.EveryMonths, b.Schedule.EveryMonths)
		diff("every_weeks", a.Schedule.EveryWeeks, b.Schedule.EveryWeeks)
		diff("day_of_month", a.Schedule.DayOfMonth, b.Schedule.DayOfMonth)
		diff("starts_on", a.Schedule.StartsOn.Format(time.DateOnly), b.Schedule.StartsOn.Format(time.DateOnly))
		diff("ends_on", optDay(a.Schedule.EndsOn), optDay(b.Schedule.EndsOn))
		diff("next_on", optDay(a.NextOn), optDay(b.NextOn))
		diff("mode", a.Mode, b.Mode)
		diff("version", a.Version, b.Version)
	}
	if a.ContentVersion != b.ContentVersion {
		c, err := jsonValue(b.Content)
		if err != nil {
			return nil, err
		}
		out["content_version"] = [2]any{a.ContentVersion, b.ContentVersion}
		out["content"] = [2]any{nil, c}
	}
	return out, nil
}

func (r recurringInvoices) Create(ctx context.Context, d storage.RecurringInvoiceDetails) (storage.RecurringInvoice, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringInvoice{}, err
	}
	row, err := r.u.q.InsertRecurringInvoice(ctx, sqlcdb.InsertRecurringInvoiceParams{Name: d.Name, Status: string(d.Status),
		EveryMonths: int32(d.Schedule.EveryMonths), EveryWeeks: int32(d.Schedule.EveryWeeks), DayOfMonth: int32(d.Schedule.DayOfMonth), // #nosec G115 -- small, checked by the database
		StartsOn: d.Schedule.StartsOn, EndsOn: pgDate(d.Schedule.EndsOn), NextOn: pgDate(d.NextOn), Mode: modeOf(d.Mode)})
	if err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	if err := r.u.q.InsertRecurringInvoiceVersion(ctx, sqlcdb.InsertRecurringInvoiceVersionParams{RecurringInvoiceID: row.ID,
		Version: row.ContentVersion, Content: d.Content, EditedBy: r.u.actor(ctx)}); err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	out, err := r.Get(ctx, row.ID)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	changes, err := recurringInvoiceChanges(storage.RecurringInvoice{}, out)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	if err := r.u.event(ctx, "recurring_invoice.created", "recurring_invoice", out.ID, &out.Version, changes); err != nil {
		return storage.RecurringInvoice{}, err
	}
	return out, nil
}

func (r recurringInvoices) Get(ctx context.Context, id uuid.UUID) (storage.RecurringInvoice, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringInvoice{}, err
	}
	row, err := r.u.q.GetRecurringInvoice(ctx, id)
	if err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	return toRecurringInvoice(row), nil
}

func (r recurringInvoices) Lock(ctx context.Context, id uuid.UUID) (storage.RecurringInvoice, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringInvoice{}, err
	}
	row, err := r.u.q.LockRecurringInvoice(ctx, id)
	if err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	return toRecurringInvoice(sqlcdb.GetRecurringInvoiceRow(row)), nil
}

func (r recurringInvoices) Update(ctx context.Context, id uuid.UUID, version int, d storage.RecurringInvoiceDetails) (storage.RecurringInvoice, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringInvoice{}, err
	}
	cur, err := r.u.q.LockRecurringInvoice(ctx, id)
	if err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	before := toRecurringInvoice(sqlcdb.GetRecurringInvoiceRow(cur))
	if before.Version != version {
		return storage.RecurringInvoice{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the recurring invoice changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	contentVersion := before.ContentVersion
	if d.Content != nil {
		contentVersion++
		if err := r.u.q.InsertRecurringInvoiceVersion(ctx, sqlcdb.InsertRecurringInvoiceVersionParams{RecurringInvoiceID: id,
			Version: int32(contentVersion), Content: d.Content, EditedBy: r.u.actor(ctx)}); err != nil { // #nosec G115 -- a version count
			return storage.RecurringInvoice{}, mapError(err)
		}
	}
	if err := r.u.q.UpdateRecurringInvoice(ctx, sqlcdb.UpdateRecurringInvoiceParams{ID: id, Name: d.Name, Status: string(d.Status),
		EveryMonths: int32(d.Schedule.EveryMonths), EveryWeeks: int32(d.Schedule.EveryWeeks), DayOfMonth: int32(d.Schedule.DayOfMonth), // #nosec G115 -- small, checked by the database
		StartsOn: d.Schedule.StartsOn, EndsOn: pgDate(d.Schedule.EndsOn), NextOn: pgDate(d.NextOn),
		Version: int32(before.Version + 1), ContentVersion: int32(contentVersion), Mode: modeOf(d.Mode)}); err != nil { // #nosec G115 -- version counts
		return storage.RecurringInvoice{}, mapError(err)
	}
	after, err := r.Get(ctx, id)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	changes, err := recurringInvoiceChanges(before, after)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	if err := r.u.event(ctx, "recurring_invoice.updated", "recurring_invoice", id, &after.Version, changes); err != nil {
		return storage.RecurringInvoice{}, err
	}
	return after, nil
}

func (r recurringInvoices) Page(ctx context.Context, afterName string, afterID *uuid.UUID, limit int) ([]storage.RecurringInvoice, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PageRecurringInvoices(ctx, sqlcdb.PageRecurringInvoicesParams{AfterName: afterName, AfterID: afterID,
		PageSize: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.RecurringInvoice, len(rows))
	for i, row := range rows {
		out[i] = toRecurringInvoice(sqlcdb.GetRecurringInvoiceRow(row))
	}
	return out, nil
}

func (r recurringInvoices) Version(ctx context.Context, id uuid.UUID, version int) (storage.RecurringInvoiceVersion, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringInvoiceVersion{}, err
	}
	row, err := r.u.q.GetRecurringInvoiceVersion(ctx, sqlcdb.GetRecurringInvoiceVersionParams{RecurringInvoiceID: id,
		Version: int32(version)}) // #nosec G115 -- a version count
	if err != nil {
		return storage.RecurringInvoiceVersion{}, mapError(err)
	}
	return storage.RecurringInvoiceVersion{Version: int(row.Version), Content: row.Content, EditedBy: row.EditedBy, EditedAt: row.EditedAt}, nil
}

func (r recurringInvoices) Due(ctx context.Context, on string, limit int) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	d, err := time.Parse(time.DateOnly, on)
	if err != nil {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("%q is not a date", on), Hint: "Send YYYY-MM-DD"}
	}
	ids, err := r.u.q.DueRecurringInvoices(ctx, sqlcdb.DueRecurringInvoicesParams{OnDay: d, MaxRows: int32(min(max(limit, 1), 10000))}) // #nosec G115 -- bounded
	return ids, mapError(err)
}

func (r recurringInvoices) MakeDraft(ctx context.Context, id uuid.UUID, on time.Time, next *time.Time) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}
	docID, err := r.u.q.CreateScheduledInvoiceDraft(ctx, sqlcdb.CreateScheduledInvoiceDraftParams{RecurringInvoiceID: id, OnDay: on})
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	row, err := r.u.q.GetDocument(ctx, docID)
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	doc := storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
		Version: int(row.CurrentVersion), Payload: row.Payload, SavedBy: str(row.CreatedBy)}
	payload, err := jsonValue(row.Payload)
	if err != nil {
		return storage.Document{}, err
	}
	if err := r.u.event(ctx, "document.created", "document", doc.ID, &doc.Version, map[string][2]any{
		"type": {nil, row.Type}, "status": {nil, row.Status}, "payload": {nil, payload}}); err != nil {
		return storage.Document{}, err
	}
	if err := r.u.q.SetRecurringInvoiceNext(ctx, sqlcdb.SetRecurringInvoiceNextParams{NextOn: pgDate(next), ID: id}); err != nil {
		return storage.Document{}, mapError(err)
	}
	var lastInvoice any
	if before.LastInvoiceID != nil {
		lastInvoice = before.LastInvoiceID.String()
	}
	if err := r.u.event(ctx, "recurring_invoice.ran", "recurring_invoice", id, &before.Version, map[string][2]any{
		"last_on": {optDay(before.LastOn), on.Format(time.DateOnly)}, "last_invoice_id": {lastInvoice, doc.ID.String()},
		"next_on": {optDay(before.NextOn), optDay(next)}}); err != nil {
		return storage.Document{}, err
	}
	return doc, nil
}

// modeOf is a recurring invoice's mode as stored: a draft when none is named.
func modeOf(mode string) string {
	if mode == "" {
		return storage.RecurringDraft
	}
	return mode
}

func (r recurringInvoices) Approve(ctx context.Context, id uuid.UUID, version int) (storage.RecurringInvoice, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringInvoice{}, err
	}
	cur, err := r.u.q.LockRecurringInvoice(ctx, id)
	if err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	before := toRecurringInvoice(sqlcdb.GetRecurringInvoiceRow(cur))
	if before.ContentVersion != version {
		return storage.RecurringInvoice{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the recurring invoice's content is at version %d, not the %d approved", before.ContentVersion, version)}
	}
	by := r.u.actor(ctx)
	v := int32(version) // #nosec G115 -- a version count
	if err := r.u.q.ApproveRecurringInvoice(ctx, sqlcdb.ApproveRecurringInvoiceParams{ID: id, ApprovedVersion: &v, ApprovedBy: &by}); err != nil {
		return storage.RecurringInvoice{}, mapError(err)
	}
	after, err := r.Get(ctx, id)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	var was any
	if before.ApprovedVersion != nil {
		was = *before.ApprovedVersion
	}
	if err := r.u.event(ctx, "recurring_invoice.approved", "recurring_invoice", id, &after.Version, map[string][2]any{
		"approved_version": {was, version}, "approved_by": {nilIfEmpty(before.ApprovedBy), by}, "version": {before.Version, after.Version}}); err != nil {
		return storage.RecurringInvoice{}, err
	}
	return after, nil
}

func (r recurringInvoices) Unsent(ctx context.Context, limit int) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.UnsentScheduledInvoices(ctx, sqlcdb.UnsentScheduledInvoicesParams{Since: r.u.store.clock.Now().Add(-storage.ScheduledSendFor),
		MaxRows: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- bounded
	return ids, mapError(err)
}
