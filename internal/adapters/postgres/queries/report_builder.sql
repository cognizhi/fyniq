-- name: GetReportDefinition :one
SELECT * FROM fyniq.report_definition WHERE id = sqlc.arg(id);

-- name: ListReportDefinitions :many
SELECT * FROM fyniq.report_definition WHERE sqlc.arg(include_archived)::boolean OR archived_at IS NULL ORDER BY lower(name), id;

-- name: InsertReportDefinition :one
INSERT INTO fyniq.report_definition (name, description, definition, visibility, owner_id, created_by, created_at, updated_by, updated_at)
VALUES (sqlc.arg(name), sqlc.arg(description), sqlc.arg(definition), sqlc.arg(visibility), sqlc.arg(owner_id), sqlc.arg(created_by), sqlc.arg(created_at),
        sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;

-- name: UpdateReportDefinition :one
UPDATE fyniq.report_definition SET name = sqlc.arg(name), description = sqlc.arg(description), definition = sqlc.arg(definition),
  visibility = sqlc.arg(visibility), updated_by = sqlc.arg(updated_by), updated_at = sqlc.arg(updated_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND archived_at IS NULL
RETURNING *;

-- name: ArchiveReportDefinition :one
UPDATE fyniq.report_definition SET archived_at = sqlc.arg(at), updated_by = sqlc.arg(updated_by), updated_at = sqlc.arg(at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND archived_at IS NULL
RETURNING *;

-- name: GetReportSchedule :one
SELECT * FROM fyniq.report_schedule WHERE id = sqlc.arg(id);

-- name: ReportSchedulesOf :many
SELECT * FROM fyniq.report_schedule WHERE definition_id = sqlc.arg(definition_id) ORDER BY created_at, id;

-- name: DueReportSchedules :many
SELECT s.* FROM fyniq.report_schedule s JOIN fyniq.report_definition d ON d.organisation_id = s.organisation_id AND d.id = s.definition_id
WHERE s.active AND s.next_run_on <= sqlc.arg(day)::date AND d.archived_at IS NULL ORDER BY s.next_run_on, s.id;

-- name: InsertReportSchedule :one
INSERT INTO fyniq.report_schedule (definition_id, cadence, weekday, day_of_month, period_rule, channel, recipients, slack_channel, active, next_run_on,
  created_by, created_at, updated_at)
VALUES (sqlc.arg(definition_id), sqlc.arg(cadence), sqlc.narg(weekday), sqlc.narg(day_of_month), sqlc.arg(period_rule), sqlc.arg(channel),
  sqlc.arg(recipients)::uuid[], sqlc.narg(slack_channel), sqlc.arg(active), sqlc.arg(next_run_on), sqlc.arg(created_by), sqlc.arg(created_at), sqlc.arg(created_at))
RETURNING *;

-- name: UpdateReportSchedule :one
UPDATE fyniq.report_schedule SET cadence = sqlc.arg(cadence), weekday = sqlc.narg(weekday), day_of_month = sqlc.narg(day_of_month),
  period_rule = sqlc.arg(period_rule), channel = sqlc.arg(channel), recipients = sqlc.arg(recipients)::uuid[], slack_channel = sqlc.narg(slack_channel),
  active = sqlc.arg(active), next_run_on = sqlc.arg(next_run_on), updated_at = sqlc.arg(updated_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: MoveReportScheduleOn :one
UPDATE fyniq.report_schedule SET last_run_on = sqlc.arg(last_run_on), next_run_on = sqlc.arg(next_run_on), updated_at = sqlc.arg(updated_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: InsertReportScheduleRun :one
INSERT INTO fyniq.report_schedule_run (schedule_id, ran_on, period_label, ledger_revision, sent, skipped, ran_at)
VALUES (sqlc.arg(schedule_id), sqlc.arg(ran_on), sqlc.arg(period_label), sqlc.arg(ledger_revision), sqlc.arg(sent), sqlc.arg(skipped), sqlc.arg(ran_at))
RETURNING *;

-- name: ReportScheduleRuns :many
SELECT * FROM fyniq.report_schedule_run WHERE schedule_id = sqlc.arg(schedule_id) ORDER BY ran_on DESC, id DESC LIMIT sqlc.arg(max_rows);
