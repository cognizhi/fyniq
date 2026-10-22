package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// recurringBills keeps the recurring bills (FR-AP-09; PRD-001 rev 0.84) and makes the scheduler's draft bills.
type recurringBills struct{ u *unitOfWork }

func (u *unitOfWork) RecurringBills() storage.RecurringBills { return recurringBills{u} }

func toRecurringBill(row sqlcdb.GetRecurringBillRow) storage.RecurringBill {
	return storage.RecurringBill{ID: row.ID, Name: row.Name, Status: storage.RecurringStatus(row.Status),
		Schedule: storage.Schedule{EveryMonths: int(row.EveryMonths), DayOfMonth: int(row.DayOfMonth), StartsOn: row.StartsOn, EndsOn: nullDate(row.EndsOn)},
		NextOn:   nullDate(row.NextOn), Version: int(row.Version), ContentVersion: int(row.ContentVersion), Content: row.Content,
		EditedBy: row.EditedBy, EditedAt: row.EditedAt, LastOn: nullDate(row.LastOn), LastBillID: row.LastBillID}
}

// recurringBillChanges are the audited differences between two states of a recurring bill; content only when it
// changed, as its new version.
func recurringBillChanges(a, b storage.RecurringBill) (map[string][2]any, error) {
	out := map[string][2]any{}
	diff := func(k string, x, y any) {
		if x != y {
			out[k] = [2]any{x, y}
		}
	}
	var zero storage.RecurringBill
	if a.ID == zero.ID {
		diff("name", nil, b.Name)
		diff("status", nil, string(b.Status))
		diff("every_months", nil, b.Schedule.EveryMonths)
		diff("day_of_month", nil, b.Schedule.DayOfMonth)
		diff("starts_on", nil, b.Schedule.StartsOn.Format(time.DateOnly))
		diff("ends_on", nil, optDay(b.Schedule.EndsOn))
		diff("next_on", nil, optDay(b.NextOn))
	} else {
		diff("name", a.Name, b.Name)
		diff("status", string(a.Status), string(b.Status))
		diff("every_months", a.Schedule.EveryMonths, b.Schedule.EveryMonths)
		diff("day_of_month", a.Schedule.DayOfMonth, b.Schedule.DayOfMonth)
		diff("starts_on", a.Schedule.StartsOn.Format(time.DateOnly), b.Schedule.StartsOn.Format(time.DateOnly))
		diff("ends_on", optDay(a.Schedule.EndsOn), optDay(b.Schedule.EndsOn))
		diff("next_on", optDay(a.NextOn), optDay(b.NextOn))
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

func (r recurringBills) Create(ctx context.Context, d storage.RecurringBillDetails) (storage.RecurringBill, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringBill{}, err
	}
	row, err := r.u.q.InsertRecurringBill(ctx, sqlcdb.InsertRecurringBillParams{Name: d.Name, Status: string(d.Status),
		EveryMonths: int32(d.Schedule.EveryMonths), DayOfMonth: int32(d.Schedule.DayOfMonth), // #nosec G115 -- 1 to 31, checked by the database
		StartsOn: d.Schedule.StartsOn, EndsOn: pgDate(d.Schedule.EndsOn), NextOn: pgDate(d.NextOn)})
	if err != nil {
		return storage.RecurringBill{}, mapError(err)
	}
	if err := r.u.q.InsertRecurringBillVersion(ctx, sqlcdb.InsertRecurringBillVersionParams{RecurringBillID: row.ID,
		Version: row.ContentVersion, Content: d.Content, EditedBy: r.u.actor(ctx)}); err != nil {
		return storage.RecurringBill{}, mapError(err)
	}
	out, err := r.Get(ctx, row.ID)
	if err != nil {
		return storage.RecurringBill{}, err
	}
	changes, err := recurringBillChanges(storage.RecurringBill{}, out)
	if err != nil {
		return storage.RecurringBill{}, err
	}
	if err := r.u.event(ctx, "recurring_bill.created", "recurring_bill", out.ID, &out.Version, changes); err != nil {
		return storage.RecurringBill{}, err
	}
	return out, nil
}

func (r recurringBills) Get(ctx context.Context, id uuid.UUID) (storage.RecurringBill, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringBill{}, err
	}
	row, err := r.u.q.GetRecurringBill(ctx, id)
	if err != nil {
		return storage.RecurringBill{}, mapError(err)
	}
	return toRecurringBill(row), nil
}

func (r recurringBills) Lock(ctx context.Context, id uuid.UUID) (storage.RecurringBill, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringBill{}, err
	}
	row, err := r.u.q.LockRecurringBill(ctx, id)
	if err != nil {
		return storage.RecurringBill{}, mapError(err)
	}
	return toRecurringBill(sqlcdb.GetRecurringBillRow(row)), nil
}

func (r recurringBills) Update(ctx context.Context, id uuid.UUID, version int, d storage.RecurringBillDetails) (storage.RecurringBill, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringBill{}, err
	}
	cur, err := r.u.q.LockRecurringBill(ctx, id)
	if err != nil {
		return storage.RecurringBill{}, mapError(err)
	}
	before := toRecurringBill(sqlcdb.GetRecurringBillRow(cur))
	if before.Version != version {
		return storage.RecurringBill{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the recurring bill changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	contentVersion := before.ContentVersion
	if d.Content != nil {
		contentVersion++
		if err := r.u.q.InsertRecurringBillVersion(ctx, sqlcdb.InsertRecurringBillVersionParams{RecurringBillID: id,
			Version: int32(contentVersion), Content: d.Content, EditedBy: r.u.actor(ctx)}); err != nil { // #nosec G115 -- a version count
			return storage.RecurringBill{}, mapError(err)
		}
	}
	if err := r.u.q.UpdateRecurringBill(ctx, sqlcdb.UpdateRecurringBillParams{ID: id, Name: d.Name, Status: string(d.Status),
		EveryMonths: int32(d.Schedule.EveryMonths), DayOfMonth: int32(d.Schedule.DayOfMonth), // #nosec G115 -- 1 to 31, checked by the database
		StartsOn: d.Schedule.StartsOn, EndsOn: pgDate(d.Schedule.EndsOn), NextOn: pgDate(d.NextOn),
		Version: int32(before.Version + 1), ContentVersion: int32(contentVersion)}); err != nil { // #nosec G115 -- version counts
		return storage.RecurringBill{}, mapError(err)
	}
	after, err := r.Get(ctx, id)
	if err != nil {
		return storage.RecurringBill{}, err
	}
	changes, err := recurringBillChanges(before, after)
	if err != nil {
		return storage.RecurringBill{}, err
	}
	if err := r.u.event(ctx, "recurring_bill.updated", "recurring_bill", id, &after.Version, changes); err != nil {
		return storage.RecurringBill{}, err
	}
	return after, nil
}

func (r recurringBills) Page(ctx context.Context, afterName string, afterID *uuid.UUID, limit int) ([]storage.RecurringBill, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PageRecurringBills(ctx, sqlcdb.PageRecurringBillsParams{AfterName: afterName, AfterID: afterID,
		PageSize: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.RecurringBill, len(rows))
	for i, row := range rows {
		out[i] = toRecurringBill(sqlcdb.GetRecurringBillRow(row))
	}
	return out, nil
}

func (r recurringBills) Version(ctx context.Context, id uuid.UUID, version int) (storage.RecurringBillVersion, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringBillVersion{}, err
	}
	row, err := r.u.q.GetRecurringBillVersion(ctx, sqlcdb.GetRecurringBillVersionParams{RecurringBillID: id,
		Version: int32(version)}) // #nosec G115 -- a version count
	if err != nil {
		return storage.RecurringBillVersion{}, mapError(err)
	}
	return storage.RecurringBillVersion{Version: int(row.Version), Content: row.Content, EditedBy: row.EditedBy, EditedAt: row.EditedAt}, nil
}

func (r recurringBills) Due(ctx context.Context, on string, limit int) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	d, err := time.Parse(time.DateOnly, on)
	if err != nil {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("%q is not a date", on), Hint: "Send YYYY-MM-DD"}
	}
	ids, err := r.u.q.DueRecurringBills(ctx, sqlcdb.DueRecurringBillsParams{OnDay: d, MaxRows: int32(min(max(limit, 1), 10000))}) // #nosec G115 -- bounded
	return ids, mapError(err)
}

func (r recurringBills) MakeDraft(ctx context.Context, id uuid.UUID, on time.Time, next *time.Time) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}
	docID, err := r.u.q.CreateScheduledBillDraft(ctx, sqlcdb.CreateScheduledBillDraftParams{RecurringBillID: id, OnDay: on})
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
	if err := r.u.q.SetRecurringBillNext(ctx, sqlcdb.SetRecurringBillNextParams{NextOn: pgDate(next), ID: id}); err != nil {
		return storage.Document{}, mapError(err)
	}
	var lastBill any
	if before.LastBillID != nil {
		lastBill = before.LastBillID.String()
	}
	if err := r.u.event(ctx, "recurring_bill.ran", "recurring_bill", id, &before.Version, map[string][2]any{
		"last_on": {optDay(before.LastOn), on.Format(time.DateOnly)}, "last_bill_id": {lastBill, doc.ID.String()},
		"next_on": {optDay(before.NextOn), optDay(next)}}); err != nil {
		return storage.Document{}, err
	}
	return doc, nil
}
