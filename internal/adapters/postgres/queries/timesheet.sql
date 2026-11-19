-- Time entries, weekly submissions and the running timer (p1-projects task 2.2).

-- name: TimeEntries :many
SELECT * FROM fyniq.time_entry
 WHERE (sqlc.narg(user_id)::uuid IS NULL OR user_id = sqlc.narg(user_id)::uuid)
   AND (sqlc.narg(project_id)::uuid IS NULL OR project_id = sqlc.narg(project_id)::uuid)
   AND (sqlc.narg(from_day)::date IS NULL OR work_date >= sqlc.narg(from_day)::date)
   AND (sqlc.narg(to_day)::date IS NULL OR work_date <= sqlc.narg(to_day)::date)
   AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
   AND (sqlc.narg(submission_id)::uuid IS NULL OR submission_id = sqlc.narg(submission_id)::uuid)
 ORDER BY work_date, created_at, id
 LIMIT sqlc.arg(max_rows);

-- name: TimeEntry :one
SELECT * FROM fyniq.time_entry WHERE id = sqlc.arg(id);

-- name: InsertTimeEntry :one
INSERT INTO fyniq.time_entry (user_id, project_id, task_id, work_date, minutes, billable, note, reverses_id)
VALUES (sqlc.arg(user_id), sqlc.arg(project_id), sqlc.arg(task_id), sqlc.arg(work_date), sqlc.arg(minutes), sqlc.arg(billable), sqlc.arg(note),
        sqlc.narg(reverses_id))
RETURNING *;

-- name: UpdateTimeEntry :one
UPDATE fyniq.time_entry
   SET task_id = sqlc.arg(task_id), work_date = sqlc.arg(work_date), minutes = sqlc.arg(minutes), billable = sqlc.arg(billable),
       note = sqlc.arg(note), version = version + 1, changed_at = now()
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: DeleteTimeEntry :one
DELETE FROM fyniq.time_entry WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) RETURNING week_id;

-- name: SubmitTimeEntries :many
-- A person's draft and returned entries of a project's week join a submission.
UPDATE fyniq.time_entry
   SET status = 'submitted', submission_id = sqlc.arg(submission_id), version = version + 1, changed_at = now()
 WHERE user_id = sqlc.arg(user_id) AND project_id = sqlc.arg(project_id) AND status IN ('draft', 'returned')
   AND work_date BETWEEN sqlc.arg(week_start)::date AND sqlc.arg(week_start)::date + 6
RETURNING *;

-- name: DecideTimeEntries :many
-- The entries of a decided submission follow it.
UPDATE fyniq.time_entry SET status = sqlc.arg(status), version = version + 1, changed_at = now()
 WHERE submission_id = sqlc.arg(submission_id) AND status = 'submitted'
RETURNING *;

-- name: InsertSubmission :one
INSERT INTO fyniq.timesheet_submission (user_id, project_id, week_start, submitted_at) VALUES (sqlc.arg(user_id), sqlc.arg(project_id), sqlc.arg(week_start), sqlc.arg(submitted_at))
RETURNING *;

-- name: DecideSubmission :one
UPDATE fyniq.timesheet_submission
   SET status = sqlc.arg(status), decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at), comment = sqlc.arg(comment), version = version + 1
 WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: Submission :one
SELECT * FROM fyniq.timesheet_submission WHERE id = sqlc.arg(id);

-- name: Submissions :many
SELECT * FROM fyniq.timesheet_submission
 WHERE (sqlc.narg(user_id)::uuid IS NULL OR user_id = sqlc.narg(user_id)::uuid)
   AND (sqlc.narg(project_id)::uuid IS NULL OR project_id = sqlc.narg(project_id)::uuid)
   AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
 ORDER BY week_start DESC, submitted_at DESC, id
 LIMIT sqlc.arg(max_rows);

-- name: TimeTimer :one
SELECT * FROM fyniq.time_timer WHERE user_id = sqlc.arg(user_id);

-- name: InsertTimeTimer :exec
INSERT INTO fyniq.time_timer (user_id, project_id, task_id, billable, note, started_at)
VALUES (sqlc.arg(user_id), sqlc.arg(project_id), sqlc.arg(task_id), sqlc.arg(billable), sqlc.arg(note), sqlc.arg(started_at));

-- name: DeleteTimeTimer :execrows
DELETE FROM fyniq.time_timer WHERE user_id = sqlc.arg(user_id);

-- name: TimeWeekID :one
SELECT fyniq.time_week_id(sqlc.arg(user_id)::uuid, sqlc.arg(day)::date)::uuid;
