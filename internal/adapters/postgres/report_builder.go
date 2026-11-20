package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- the report builder's definitions and schedules (FR-RPT-04, FR-RPT-07, rev 0.540)

func (u *unitOfWork) ReportDefinitions() storage.ReportDefinitions { return reportDefinitions{u} }
func (u *unitOfWork) ReportSchedules() storage.ReportSchedules     { return reportSchedules{u} }

type reportDefinitions struct{ u *unitOfWork }

func toReportDefinition(r sqlcdb.FyniqReportDefinition) storage.ReportDefinition {
	return storage.ReportDefinition{ID: r.ID, Name: r.Name, Description: r.Description, Definition: json.RawMessage(r.Definition), Visibility: r.Visibility,
		OwnerID: r.OwnerID, Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt}
}

func (r reportDefinitions) Get(ctx context.Context, id uuid.UUID) (storage.ReportDefinition, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportDefinition{}, err
	}
	row, err := r.u.q.GetReportDefinition(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ReportDefinition{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ReportDefinition{}, mapError(err)
	}
	return toReportDefinition(row), nil
}

func (r reportDefinitions) List(ctx context.Context, archived bool) ([]storage.ReportDefinition, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListReportDefinitions(ctx, archived)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReportDefinition, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReportDefinition(row))
	}
	return out, nil
}

func (r reportDefinitions) Create(ctx context.Context, d storage.ReportDefinition) (storage.ReportDefinition, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportDefinition{}, err
	}
	row, err := r.u.q.InsertReportDefinition(ctx, sqlcdb.InsertReportDefinitionParams{Name: d.Name, Description: d.Description, Definition: d.Definition,
		Visibility: d.Visibility, OwnerID: d.OwnerID, CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt})
	if err != nil {
		return storage.ReportDefinition{}, mapError(err)
	}
	out := toReportDefinition(row)
	v := out.Version
	return out, r.u.event(ctx, "report_definition.created", "report_definition", out.ID, &v, map[string][2]any{"name": {nil, out.Name},
		"visibility": {nil, out.Visibility}, "definition": {nil, string(out.Definition)}})
}

func (r reportDefinitions) Replace(ctx context.Context, d storage.ReportDefinition, version int) (storage.ReportDefinition, error) {
	before, err := r.Get(ctx, d.ID)
	if err != nil {
		return storage.ReportDefinition{}, err
	}
	row, err := r.u.q.UpdateReportDefinition(ctx, sqlcdb.UpdateReportDefinitionParams{ID: d.ID, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Name: d.Name, Description: d.Description, Definition: d.Definition, Visibility: d.Visibility, UpdatedBy: d.UpdatedBy, UpdatedAt: d.UpdatedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ReportDefinition{}, &storage.Error{Code: storage.CodeVersion, Message: "the definition changed since it was read"}
	}
	if err != nil {
		return storage.ReportDefinition{}, mapError(err)
	}
	out := toReportDefinition(row)
	v := out.Version
	return out, r.u.event(ctx, "report_definition.changed", "report_definition", out.ID, &v, map[string][2]any{"name": {before.Name, out.Name},
		"visibility": {before.Visibility, out.Visibility}, "definition": {string(before.Definition), string(out.Definition)}})
}

func (r reportDefinitions) Archive(ctx context.Context, id uuid.UUID, by string, at time.Time, version int) (storage.ReportDefinition, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportDefinition{}, err
	}
	row, err := r.u.q.ArchiveReportDefinition(ctx, sqlcdb.ArchiveReportDefinitionParams{ID: id, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		UpdatedBy: by, At: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ReportDefinition{}, &storage.Error{Code: storage.CodeVersion, Message: "the definition changed since it was read, or is archived"}
	}
	if err != nil {
		return storage.ReportDefinition{}, mapError(err)
	}
	out := toReportDefinition(row)
	v := out.Version
	return out, r.u.event(ctx, "report_definition.archived", "report_definition", out.ID, &v, map[string][2]any{"archived": {false, true}})
}

type reportSchedules struct{ u *unitOfWork }

func scheduleDay(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}

func smallInt(p *int16) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

func toSmall(p *int) *int16 {
	if p == nil {
		return nil
	}
	v := int16(min(max(*p, 0), 31)) // #nosec G115 -- bounded
	return &v
}

func toReportSchedule(r sqlcdb.FyniqReportSchedule) storage.ReportSchedule {
	s := storage.ReportSchedule{ID: r.ID, DefinitionID: r.DefinitionID, Cadence: r.Cadence, Weekday: smallInt(r.Weekday), DayOfMonth: smallInt(r.DayOfMonth),
		PeriodRule: r.PeriodRule, Channel: r.Channel, Recipients: r.Recipients, Active: r.Active, NextRunOn: r.NextRunOn, LastRunOn: scheduleDay(r.LastRunOn),
		Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if r.SlackChannel != nil {
		s.SlackChannel = *r.SlackChannel
	}
	return s
}

func (r reportSchedules) slack(s storage.ReportSchedule) *string {
	if s.Channel != storage.ChannelSlack {
		return nil
	}
	return &s.SlackChannel
}

func (r reportSchedules) Get(ctx context.Context, id uuid.UUID) (storage.ReportSchedule, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportSchedule{}, err
	}
	row, err := r.u.q.GetReportSchedule(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ReportSchedule{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ReportSchedule{}, mapError(err)
	}
	return toReportSchedule(row), nil
}

func (r reportSchedules) ForDefinition(ctx context.Context, definition uuid.UUID) ([]storage.ReportSchedule, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ReportSchedulesOf(ctx, definition)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReportSchedule, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReportSchedule(row))
	}
	return out, nil
}

func (r reportSchedules) Due(ctx context.Context, day time.Time) ([]storage.ReportSchedule, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.DueReportSchedules(ctx, day)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReportSchedule, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReportSchedule(sqlcdb.FyniqReportSchedule(row)))
	}
	return out, nil
}

func scheduleChanges(s storage.ReportSchedule) map[string]any {
	rs := make([]string, len(s.Recipients))
	for i, x := range s.Recipients {
		rs[i] = x.String()
	}
	return map[string]any{"cadence": s.Cadence, "period_rule": s.PeriodRule, "channel": s.Channel, "recipients": rs, "active": s.Active, "next_run_on": s.NextRunOn.Format(time.DateOnly)}
}

func (r reportSchedules) Create(ctx context.Context, s storage.ReportSchedule) (storage.ReportSchedule, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportSchedule{}, err
	}
	row, err := r.u.q.InsertReportSchedule(ctx, sqlcdb.InsertReportScheduleParams{DefinitionID: s.DefinitionID, Cadence: s.Cadence, Weekday: toSmall(s.Weekday),
		DayOfMonth: toSmall(s.DayOfMonth), PeriodRule: s.PeriodRule, Channel: s.Channel, Recipients: s.Recipients, SlackChannel: r.slack(s), Active: s.Active,
		NextRunOn: s.NextRunOn, CreatedBy: s.CreatedBy, CreatedAt: s.CreatedAt})
	if err != nil {
		return storage.ReportSchedule{}, mapError(err)
	}
	out := toReportSchedule(row)
	v := out.Version
	return out, r.u.event(ctx, "report_schedule.created", "report_schedule", out.ID, &v, map[string][2]any{"definition_id": {nil, out.DefinitionID.String()},
		"schedule": {nil, scheduleChanges(out)}})
}

func (r reportSchedules) Replace(ctx context.Context, s storage.ReportSchedule, version int) (storage.ReportSchedule, error) {
	before, err := r.Get(ctx, s.ID)
	if err != nil {
		return storage.ReportSchedule{}, err
	}
	row, err := r.u.q.UpdateReportSchedule(ctx, sqlcdb.UpdateReportScheduleParams{ID: s.ID, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Cadence: s.Cadence, Weekday: toSmall(s.Weekday), DayOfMonth: toSmall(s.DayOfMonth), PeriodRule: s.PeriodRule, Channel: s.Channel, Recipients: s.Recipients,
		SlackChannel: r.slack(s), Active: s.Active, NextRunOn: s.NextRunOn, UpdatedAt: s.UpdatedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ReportSchedule{}, &storage.Error{Code: storage.CodeVersion, Message: "the schedule changed since it was read"}
	}
	if err != nil {
		return storage.ReportSchedule{}, mapError(err)
	}
	out := toReportSchedule(row)
	v := out.Version
	return out, r.u.event(ctx, "report_schedule.changed", "report_schedule", out.ID, &v, map[string][2]any{"schedule": {scheduleChanges(before), scheduleChanges(out)}})
}

func (r reportSchedules) Ran(ctx context.Context, run storage.ReportScheduleRun, next time.Time, version int) (storage.ReportSchedule, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportSchedule{}, err
	}
	skipped := run.Skipped
	if len(skipped) == 0 {
		skipped = json.RawMessage("[]")
	}
	saved, err := r.u.q.InsertReportScheduleRun(ctx, sqlcdb.InsertReportScheduleRunParams{ScheduleID: run.ScheduleID, RanOn: run.RanOn, PeriodLabel: run.PeriodLabel,
		LedgerRevision: run.LedgerRevision, Sent: int32(min(max(run.Sent, 0), 1<<30)), Skipped: skipped, RanAt: run.RanAt}) // #nosec G115 -- bounded
	if err != nil {
		return storage.ReportSchedule{}, mapError(err)
	}
	if err := r.u.event(ctx, "report_schedule_run.recorded", "report_schedule_run", saved.ID, nil, map[string][2]any{"schedule_id": {nil, run.ScheduleID.String()},
		"sent": {nil, run.Sent}, "period": {nil, run.PeriodLabel}}); err != nil {
		return storage.ReportSchedule{}, err
	}
	row, err := r.u.q.MoveReportScheduleOn(ctx, sqlcdb.MoveReportScheduleOnParams{ID: run.ScheduleID, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		LastRunOn: pgtype.Date{Time: run.RanOn, Valid: true}, NextRunOn: next, UpdatedAt: run.RanAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ReportSchedule{}, &storage.Error{Code: storage.CodeVersion, Message: "the schedule changed since it was read"}
	}
	if err != nil {
		return storage.ReportSchedule{}, mapError(err)
	}
	out := toReportSchedule(row)
	v := out.Version
	return out, r.u.event(ctx, "report_schedule.moved_on", "report_schedule", out.ID, &v, map[string][2]any{"next_run_on": {nil, next.Format(time.DateOnly)}})
}

func (r reportSchedules) Runs(ctx context.Context, schedule uuid.UUID, limit int) ([]storage.ReportScheduleRun, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ReportScheduleRuns(ctx, sqlcdb.ReportScheduleRunsParams{ScheduleID: schedule, MaxRows: int32(min(max(limit, 1), 200))}) // #nosec G115 -- bounded
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReportScheduleRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.ReportScheduleRun{ID: row.ID, ScheduleID: row.ScheduleID, RanOn: row.RanOn, PeriodLabel: row.PeriodLabel, LedgerRevision: row.LedgerRevision,
			Sent: int(row.Sent), Skipped: json.RawMessage(row.Skipped), RanAt: row.RanAt})
	}
	return out, nil
}
