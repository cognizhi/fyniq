package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// recurringJournals keeps the recurring journals (FR-GL-02; PRD-001 rev 0.60) and makes the scheduler's drafts.
type recurringJournals struct{ u *unitOfWork }

func (u *unitOfWork) RecurringJournals() storage.RecurringJournals { return recurringJournals{u} }

func toRecurring(row sqlcdb.GetRecurringJournalRow) storage.RecurringJournal {
	return storage.RecurringJournal{ID: row.ID, Name: row.Name, Status: storage.RecurringStatus(row.Status),
		Schedule: storage.Schedule{EveryMonths: int(row.EveryMonths), DayOfMonth: int(row.DayOfMonth), StartsOn: row.StartsOn, EndsOn: nullDate(row.EndsOn)},
		NextOn:   nullDate(row.NextOn), Version: int(row.Version), ContentVersion: int(row.ContentVersion), Content: row.Content,
		EditedBy: row.EditedBy, EditedAt: row.EditedAt, LastOn: nullDate(row.LastOn), LastJournalID: row.LastJournalID}
}

// optDay is a date for an audit change: nil when there is none.
func optDay(t *time.Time) any {
	if t == nil {
		return nil
	}
	return day(*t)
}

// recurringChanges are the audited differences between two states of a recurring journal; content only when it
// changed, as its new version.
func recurringChanges(a, b storage.RecurringJournal) (map[string][2]any, error) {
	out := map[string][2]any{}
	diff := func(k string, x, y any) {
		if x != y {
			out[k] = [2]any{x, y}
		}
	}
	var zero storage.RecurringJournal
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

func (r recurringJournals) Create(ctx context.Context, d storage.RecurringJournalDetails) (storage.RecurringJournal, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringJournal{}, err
	}
	row, err := r.u.q.InsertRecurringJournal(ctx, sqlcdb.InsertRecurringJournalParams{Name: d.Name, Status: string(d.Status),
		EveryMonths: int32(d.Schedule.EveryMonths), DayOfMonth: int32(d.Schedule.DayOfMonth), // #nosec G115 -- 1 to 31, checked by the database
		StartsOn: d.Schedule.StartsOn, EndsOn: pgDate(d.Schedule.EndsOn), NextOn: pgDate(d.NextOn)})
	if err != nil {
		return storage.RecurringJournal{}, mapError(err)
	}
	if err := r.u.q.InsertRecurringJournalVersion(ctx, sqlcdb.InsertRecurringJournalVersionParams{RecurringJournalID: row.ID,
		Version: row.ContentVersion, Content: d.Content, EditedBy: r.u.actor(ctx)}); err != nil {
		return storage.RecurringJournal{}, mapError(err)
	}
	out, err := r.Get(ctx, row.ID)
	if err != nil {
		return storage.RecurringJournal{}, err
	}
	changes, err := recurringChanges(storage.RecurringJournal{}, out)
	if err != nil {
		return storage.RecurringJournal{}, err
	}
	if err := r.u.event(ctx, "recurring_journal.created", "recurring_journal", out.ID, &out.Version, changes); err != nil {
		return storage.RecurringJournal{}, err
	}
	return out, nil
}

func (r recurringJournals) Get(ctx context.Context, id uuid.UUID) (storage.RecurringJournal, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringJournal{}, err
	}
	row, err := r.u.q.GetRecurringJournal(ctx, id)
	if err != nil {
		return storage.RecurringJournal{}, mapError(err)
	}
	return toRecurring(row), nil
}

func (r recurringJournals) Lock(ctx context.Context, id uuid.UUID) (storage.RecurringJournal, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringJournal{}, err
	}
	row, err := r.u.q.LockRecurringJournal(ctx, id)
	if err != nil {
		return storage.RecurringJournal{}, mapError(err)
	}
	return toRecurring(sqlcdb.GetRecurringJournalRow(row)), nil
}

func (r recurringJournals) Update(ctx context.Context, id uuid.UUID, version int, d storage.RecurringJournalDetails) (storage.RecurringJournal, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringJournal{}, err
	}
	cur, err := r.u.q.LockRecurringJournal(ctx, id)
	if err != nil {
		return storage.RecurringJournal{}, mapError(err)
	}
	before := toRecurring(sqlcdb.GetRecurringJournalRow(cur))
	if before.Version != version {
		return storage.RecurringJournal{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the recurring journal changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	contentVersion := before.ContentVersion
	if d.Content != nil {
		contentVersion++
		if err := r.u.q.InsertRecurringJournalVersion(ctx, sqlcdb.InsertRecurringJournalVersionParams{RecurringJournalID: id,
			Version: int32(contentVersion), Content: d.Content, EditedBy: r.u.actor(ctx)}); err != nil { // #nosec G115 -- a version count
			return storage.RecurringJournal{}, mapError(err)
		}
	}
	if err := r.u.q.UpdateRecurringJournal(ctx, sqlcdb.UpdateRecurringJournalParams{ID: id, Name: d.Name, Status: string(d.Status),
		EveryMonths: int32(d.Schedule.EveryMonths), DayOfMonth: int32(d.Schedule.DayOfMonth), // #nosec G115 -- 1 to 31, checked by the database
		StartsOn: d.Schedule.StartsOn, EndsOn: pgDate(d.Schedule.EndsOn), NextOn: pgDate(d.NextOn),
		Version: int32(before.Version + 1), ContentVersion: int32(contentVersion)}); err != nil { // #nosec G115 -- version counts
		return storage.RecurringJournal{}, mapError(err)
	}
	after, err := r.Get(ctx, id)
	if err != nil {
		return storage.RecurringJournal{}, err
	}
	changes, err := recurringChanges(before, after)
	if err != nil {
		return storage.RecurringJournal{}, err
	}
	if err := r.u.event(ctx, "recurring_journal.updated", "recurring_journal", id, &after.Version, changes); err != nil {
		return storage.RecurringJournal{}, err
	}
	return after, nil
}

func (r recurringJournals) Page(ctx context.Context, afterName string, afterID *uuid.UUID, limit int) ([]storage.RecurringJournal, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PageRecurringJournals(ctx, sqlcdb.PageRecurringJournalsParams{AfterName: afterName, AfterID: afterID,
		PageSize: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.RecurringJournal, len(rows))
	for i, row := range rows {
		out[i] = toRecurring(sqlcdb.GetRecurringJournalRow(row))
	}
	return out, nil
}

func (r recurringJournals) Version(ctx context.Context, id uuid.UUID, version int) (storage.RecurringJournalVersion, error) {
	if err := r.u.check(); err != nil {
		return storage.RecurringJournalVersion{}, err
	}
	row, err := r.u.q.GetRecurringJournalVersion(ctx, sqlcdb.GetRecurringJournalVersionParams{RecurringJournalID: id,
		Version: int32(version)}) // #nosec G115 -- a version count
	if err != nil {
		return storage.RecurringJournalVersion{}, mapError(err)
	}
	return storage.RecurringJournalVersion{Version: int(row.Version), Content: row.Content, EditedBy: row.EditedBy, EditedAt: row.EditedAt}, nil
}

func (r recurringJournals) Due(ctx context.Context, on string, limit int) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	d, err := time.Parse(time.DateOnly, on)
	if err != nil {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("%q is not a date", on), Hint: "Send YYYY-MM-DD"}
	}
	ids, err := r.u.q.DueRecurringJournals(ctx, sqlcdb.DueRecurringJournalsParams{OnDay: d, MaxRows: int32(min(max(limit, 1), 10000))}) // #nosec G115 -- bounded
	return ids, mapError(err)
}

func (r recurringJournals) MakeDraft(ctx context.Context, id uuid.UUID, on time.Time, next *time.Time) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}
	docID, err := r.u.q.CreateScheduledDraft(ctx, sqlcdb.CreateScheduledDraftParams{RecurringJournalID: id, OnDay: on})
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
	if err := r.u.q.SetRecurringNext(ctx, sqlcdb.SetRecurringNextParams{NextOn: pgDate(next), ID: id}); err != nil {
		return storage.Document{}, mapError(err)
	}
	var lastJournal any
	if before.LastJournalID != nil {
		lastJournal = before.LastJournalID.String()
	}
	if err := r.u.event(ctx, "recurring_journal.ran", "recurring_journal", id, &before.Version, map[string][2]any{
		"last_on": {optDay(before.LastOn), on.Format(time.DateOnly)}, "last_journal_id": {lastJournal, doc.ID.String()},
		"next_on": {optDay(before.NextOn), optDay(next)}}); err != nil {
		return storage.Document{}, err
	}
	return doc, nil
}
