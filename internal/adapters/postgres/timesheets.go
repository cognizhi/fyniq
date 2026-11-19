package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// entryMinutes is minutes as the column takes them; one beyond a day is sent as 0, which the table's check refuses, rather than
// wrapped into another number.
func entryMinutes(m int) int32 {
	if m < -1440 || m > 1440 {
		return 0
	}
	return int32(m)
}

func (u *unitOfWork) Timesheets() storage.Timesheets { return timesheets{u} }

type timesheets struct{ u *unitOfWork }

func toTimeEntry(r sqlcdb.FyniqTimeEntry) storage.TimeEntry {
	return storage.TimeEntry{ID: r.ID, UserID: r.UserID, ProjectID: r.ProjectID, TaskID: r.TaskID, WorkDate: r.WorkDate, Minutes: int(r.Minutes),
		Billable: r.Billable, Note: r.Note, Status: r.Status, SubmissionID: r.SubmissionID, ReversesID: r.ReversesID, Version: int(r.Version),
		CreatedAt: r.CreatedAt, ChangedAt: r.ChangedAt}
}

func toSubmission(r sqlcdb.FyniqTimesheetSubmission) storage.Submission {
	return storage.Submission{ID: r.ID, UserID: r.UserID, ProjectID: r.ProjectID, WeekStart: r.WeekStart, Status: r.Status, SubmittedAt: r.SubmittedAt,
		DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt, Comment: r.Comment, Version: int(r.Version)}
}

// entryText is an entry as the audit trail records it.
func entryText(e storage.TimeEntry) map[string]any {
	out := map[string]any{"id": e.ID.String(), "project_id": e.ProjectID.String(), "task_id": e.TaskID.String(), "work_date": e.WorkDate.Format(time.DateOnly),
		"minutes": e.Minutes, "billable": e.Billable, "note": e.Note}
	if e.ReversesID != nil {
		out["reverses_id"] = e.ReversesID.String()
	}
	return out
}

func (r timesheets) weekID(ctx context.Context, user uuid.UUID, day time.Time) (uuid.UUID, error) {
	id, err := r.u.q.TimeWeekID(ctx, sqlcdb.TimeWeekIDParams{UserID: user, Day: day})
	return id, mapError(err)
}

func (r timesheets) Entries(ctx context.Context, f storage.TimeFilter) ([]storage.TimeEntry, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	p := sqlcdb.TimeEntriesParams{MaxRows: 500}
	if f.Limit > 0 {
		p.MaxRows = small(f.Limit)
	}
	if f.UserID != uuid.Nil {
		p.UserID = &f.UserID
	}
	if f.ProjectID != uuid.Nil {
		p.ProjectID = &f.ProjectID
	}
	if !f.From.IsZero() {
		p.FromDay = pgtype.Date{Time: f.From, Valid: true}
	}
	if !f.To.IsZero() {
		p.ToDay = pgtype.Date{Time: f.To, Valid: true}
	}
	if f.Status != "" {
		p.Status = &f.Status
	}
	if f.SubmissionID != uuid.Nil {
		p.SubmissionID = &f.SubmissionID
	}
	rows, err := r.u.q.TimeEntries(ctx, p)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.TimeEntry, 0, len(rows))
	for _, x := range rows {
		out = append(out, toTimeEntry(x))
	}
	return out, nil
}

func (r timesheets) GetEntry(ctx context.Context, id uuid.UUID) (storage.TimeEntry, error) {
	if err := r.u.check(); err != nil {
		return storage.TimeEntry{}, err
	}
	row, err := r.u.q.TimeEntry(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.TimeEntry{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.TimeEntry{}, mapError(err)
	}
	return toTimeEntry(row), nil
}

func (r timesheets) SaveWeek(ctx context.Context, user uuid.UUID, week time.Time, e storage.TimeWeekEdits) ([]storage.TimeEntry, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	inWeek := func(d time.Time) bool { return !d.Before(week) && !d.After(week.AddDate(0, 0, 6)) }
	var added, before, after, removed []any
	for _, n := range e.Remove {
		old, err := r.GetEntry(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		if old.UserID != user {
			return nil, &storage.Error{Code: storage.CodeTime, Message: "an entry of another person cannot be removed"}
		}
		if _, err := r.u.q.DeleteTimeEntry(ctx, sqlcdb.DeleteTimeEntryParams{ID: n.ID, Version: small(n.Version)}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, &storage.Error{Code: storage.CodeVersion, Message: "an entry changed since it was read"}
			}
			return nil, mapError(err)
		}
		removed = append(removed, entryText(old))
	}
	for _, c := range e.Change {
		old, err := r.GetEntry(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if old.UserID != user || !inWeek(c.WorkDate) {
			return nil, &storage.Error{Code: storage.CodeTime, Message: "an entry is the person's own, on a day of the week"}
		}
		row, err := r.u.q.UpdateTimeEntry(ctx, sqlcdb.UpdateTimeEntryParams{TaskID: c.TaskID, WorkDate: c.WorkDate, Minutes: entryMinutes(c.Minutes),
			Billable: c.Billable, Note: c.Note, ID: c.ID, Version: small(c.Version)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &storage.Error{Code: storage.CodeVersion, Message: "an entry changed since it was read"}
		}
		if err != nil {
			return nil, mapError(err)
		}
		before, after = append(before, entryText(old)), append(after, entryText(toTimeEntry(row)))
	}
	for _, n := range e.Add {
		if n.UserID != user || !inWeek(n.WorkDate) {
			return nil, &storage.Error{Code: storage.CodeTime, Message: "an entry is the person's own, on a day of the week"}
		}
		row, err := r.u.q.InsertTimeEntry(ctx, sqlcdb.InsertTimeEntryParams{UserID: n.UserID, ProjectID: n.ProjectID, TaskID: n.TaskID, WorkDate: n.WorkDate,
			Minutes: entryMinutes(n.Minutes), Billable: n.Billable, Note: n.Note, ReversesID: n.ReversesID})
		if err != nil {
			return nil, mapError(err)
		}
		added = append(added, entryText(toTimeEntry(row)))
	}
	if len(added)+len(before)+len(removed) > 0 {
		id, err := r.weekID(ctx, user, week)
		if err != nil {
			return nil, err
		}
		changes := map[string][2]any{"week_start": {nil, week.Format(time.DateOnly)}}
		if len(added) > 0 {
			changes["added"] = [2]any{nil, added}
		}
		if len(before) > 0 {
			changes["changed"] = [2]any{before, after}
		}
		if len(removed) > 0 {
			changes["removed"] = [2]any{removed, nil}
		}
		if err := r.u.event(ctx, "time.week_saved", "time_week", id, nil, changes); err != nil {
			return nil, err
		}
	}
	return r.Entries(ctx, storage.TimeFilter{UserID: user, From: week, To: week.AddDate(0, 0, 6)})
}

func (r timesheets) Reverse(ctx context.Context, id uuid.UUID) (storage.TimeEntry, error) {
	orig, err := r.GetEntry(ctx, id)
	if err != nil {
		return orig, err
	}
	row, err := r.u.q.InsertTimeEntry(ctx, sqlcdb.InsertTimeEntryParams{UserID: orig.UserID, ProjectID: orig.ProjectID, TaskID: orig.TaskID, WorkDate: orig.WorkDate,
		Minutes: entryMinutes(-orig.Minutes), Billable: orig.Billable, Note: "Reverses an approved entry", ReversesID: &orig.ID})
	if err != nil {
		return storage.TimeEntry{}, mapError(err)
	}
	out := toTimeEntry(row)
	wid, err := r.weekID(ctx, out.UserID, out.WorkDate)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "time.entry_reversed", "time_week", wid, nil, map[string][2]any{"reverses": {nil, orig.ID.String()}, "entry": {nil, entryText(out)}})
}

func (r timesheets) Submit(ctx context.Context, user, project uuid.UUID, week, at time.Time) (storage.Submission, []storage.TimeEntry, error) {
	if err := r.u.check(); err != nil {
		return storage.Submission{}, nil, err
	}
	sub, err := r.u.q.InsertSubmission(ctx, sqlcdb.InsertSubmissionParams{UserID: user, ProjectID: project, WeekStart: week, SubmittedAt: at})
	if err != nil {
		return storage.Submission{}, nil, mapError(err)
	}
	rows, err := r.u.q.SubmitTimeEntries(ctx, sqlcdb.SubmitTimeEntriesParams{SubmissionID: &sub.ID, UserID: user, ProjectID: project, WeekStart: week})
	if err != nil {
		return storage.Submission{}, nil, mapError(err)
	}
	if len(rows) == 0 {
		return storage.Submission{}, nil, &storage.Error{Code: storage.CodeTime, Message: "there is nothing to submit on this project for this week", Hint: "Enter the time first"}
	}
	entries := make([]storage.TimeEntry, 0, len(rows))
	ids := make([]string, 0, len(rows))
	total := 0
	for _, x := range rows {
		e := toTimeEntry(x)
		entries = append(entries, e)
		ids = append(ids, e.ID.String())
		total += e.Minutes
	}
	out := toSubmission(sub)
	wid, err := r.weekID(ctx, user, week)
	if err != nil {
		return out, entries, err
	}
	return out, entries, r.u.event(ctx, "time.week_submitted", "time_week", wid, nil, map[string][2]any{"submission_id": {nil, out.ID.String()},
		"project_id": {nil, project.String()}, "week_start": {nil, week.Format(time.DateOnly)}, "entries": {nil, ids}, "minutes": {nil, total}})
}

func (r timesheets) Decide(ctx context.Context, id uuid.UUID, version int, decider uuid.UUID, approve bool, comment string, at time.Time) (storage.Submission, error) {
	if err := r.u.check(); err != nil {
		return storage.Submission{}, err
	}
	status, action := storage.TimeReturned, "time.week_returned"
	if approve {
		status, action = storage.TimeApproved, "time.week_approved"
	}
	row, err := r.u.q.DecideSubmission(ctx, sqlcdb.DecideSubmissionParams{Status: status, DecidedBy: &decider, DecidedAt: &at, Comment: comment, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Submission{}, &storage.Error{Code: storage.CodeVersion, Message: "the week was decided, or changed, since it was read"}
	}
	if err != nil {
		return storage.Submission{}, mapError(err)
	}
	entries, err := r.u.q.DecideTimeEntries(ctx, sqlcdb.DecideTimeEntriesParams{Status: status, SubmissionID: &id})
	if err != nil {
		return storage.Submission{}, mapError(err)
	}
	out := toSubmission(row)
	ids := make([]string, 0, len(entries))
	total := 0
	for _, x := range entries {
		ids = append(ids, x.ID.String())
		total += int(x.Minutes)
	}
	wid, err := r.weekID(ctx, out.UserID, out.WeekStart)
	if err != nil {
		return out, err
	}
	changes := map[string][2]any{"submission_id": {id.String(), id.String()}, "project_id": {nil, out.ProjectID.String()}, "week_start": {nil, out.WeekStart.Format(time.DateOnly)},
		"status": {storage.TimeSubmitted, status}, "decided_by": {nil, decider.String()}, "entries": {nil, ids}, "minutes": {nil, total}}
	if comment != "" {
		changes["comment"] = [2]any{nil, comment}
	}
	return out, r.u.event(ctx, action, "time_week", wid, nil, changes)
}

func (r timesheets) Submissions(ctx context.Context, f storage.SubmissionFilter) ([]storage.Submission, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	p := sqlcdb.SubmissionsParams{MaxRows: 500}
	if f.Limit > 0 {
		p.MaxRows = small(f.Limit)
	}
	if f.UserID != uuid.Nil {
		p.UserID = &f.UserID
	}
	if f.ProjectID != uuid.Nil {
		p.ProjectID = &f.ProjectID
	}
	if f.Status != "" {
		p.Status = &f.Status
	}
	rows, err := r.u.q.Submissions(ctx, p)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Submission, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSubmission(x))
	}
	return out, nil
}

func (r timesheets) GetSubmission(ctx context.Context, id uuid.UUID) (storage.Submission, error) {
	if err := r.u.check(); err != nil {
		return storage.Submission{}, err
	}
	row, err := r.u.q.Submission(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Submission{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.Submission{}, mapError(err)
	}
	return toSubmission(row), nil
}

func toTimer(r sqlcdb.FyniqTimeTimer) storage.Timer {
	return storage.Timer{UserID: r.UserID, ProjectID: r.ProjectID, TaskID: r.TaskID, Billable: r.Billable, Note: r.Note, StartedAt: r.StartedAt}
}

func (r timesheets) Timer(ctx context.Context, user uuid.UUID) (storage.Timer, error) {
	if err := r.u.check(); err != nil {
		return storage.Timer{}, err
	}
	row, err := r.u.q.TimeTimer(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Timer{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.Timer{}, mapError(err)
	}
	return toTimer(row), nil
}

func (r timesheets) StartTimer(ctx context.Context, t storage.Timer) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if _, err := r.Timer(ctx, t.UserID); err == nil {
		return &storage.Error{Code: storage.CodeTime, Message: "a timer is running already", Hint: "Stop it first"}
	}
	if err := r.u.q.InsertTimeTimer(ctx, sqlcdb.InsertTimeTimerParams{UserID: t.UserID, ProjectID: t.ProjectID, TaskID: t.TaskID, Billable: t.Billable, Note: t.Note,
		StartedAt: t.StartedAt}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "time.timer_started", "time_timer", t.UserID, nil, map[string][2]any{"project_id": {nil, t.ProjectID.String()}, "task_id": {nil, t.TaskID.String()},
		"started_at": {nil, t.StartedAt.UTC().Format(time.RFC3339)}})
}

func (r timesheets) DropTimer(ctx context.Context, user uuid.UUID) error {
	if err := r.u.check(); err != nil {
		return err
	}
	n, err := r.u.q.DeleteTimeTimer(ctx, user)
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return r.u.event(ctx, "time.timer_dropped", "time_timer", user, nil, map[string][2]any{"dropped": {false, true}})
}

func (r timesheets) StopTimer(ctx context.Context, user uuid.UUID, day time.Time, minutes int) (storage.TimeEntry, error) {
	t, err := r.Timer(ctx, user)
	if err != nil {
		return storage.TimeEntry{}, err
	}
	if _, err := r.u.q.DeleteTimeTimer(ctx, user); err != nil {
		return storage.TimeEntry{}, mapError(err)
	}
	row, err := r.u.q.InsertTimeEntry(ctx, sqlcdb.InsertTimeEntryParams{UserID: user, ProjectID: t.ProjectID, TaskID: t.TaskID, WorkDate: day, Minutes: entryMinutes(minutes),
		Billable: t.Billable, Note: t.Note})
	if err != nil {
		return storage.TimeEntry{}, mapError(err)
	}
	e := toTimeEntry(row)
	if err := r.u.event(ctx, "time.timer_stopped", "time_timer", user, nil, map[string][2]any{"entry_id": {nil, e.ID.String()}, "minutes": {nil, minutes}}); err != nil {
		return e, err
	}
	wid, err := r.weekID(ctx, user, day)
	if err != nil {
		return e, err
	}
	return e, r.u.event(ctx, "time.week_saved", "time_week", wid, nil, map[string][2]any{"week_start": {nil, weekOf(day).Format(time.DateOnly)}, "added": {nil, []any{entryText(e)}}})
}

// weekOf is the Monday of a day's ISO week.
func weekOf(d time.Time) time.Time {
	back := (int(d.Weekday()) + 6) % 7
	return time.Date(d.Year(), d.Month(), d.Day()-back, 0, 0, 0, 0, time.UTC)
}
