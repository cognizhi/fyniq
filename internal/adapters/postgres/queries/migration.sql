-- name: AnyJournalEntry :one
-- Whether the organisation holds a posted entry: a workbook import goes only into one that holds none (rev 0.254).
SELECT EXISTS (SELECT 1 FROM fyniq.journal_entry) AS posted;

-- name: InsertMigrationRun :one
INSERT INTO fyniq.migration_run (started_by, started_at) VALUES (sqlc.arg(started_by), sqlc.arg(started_at))
RETURNING *;

-- name: GetMigrationRun :one
SELECT * FROM fyniq.migration_run WHERE id = sqlc.arg(id);

-- name: ListMigrationRuns :many
-- The organisation's migration: at most one (rev 0.254).
SELECT * FROM fyniq.migration_run ORDER BY started_at, id;

-- name: InsertMigrationFile :one
INSERT INTO fyniq.migration_file (run_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, storage_key, data_class,
                                  added_by, added_at)
VALUES (sqlc.arg(run_id), sqlc.arg(kind), sqlc.arg(file_name), sqlc.arg(media_type), sqlc.arg(size_bytes), sqlc.arg(sha256),
        sqlc.arg(original_sha256), sqlc.arg(storage_key), sqlc.arg(data_class), sqlc.arg(added_by), sqlc.arg(added_at))
RETURNING *;

-- name: ListMigrationFiles :many
SELECT * FROM fyniq.migration_file WHERE run_id = sqlc.arg(run_id) ORDER BY added_at, id;

-- name: InsertMigrationStep :one
INSERT INTO fyniq.migration_step (run_id, step, state, workbook_file_id, started_by, started_at)
VALUES (sqlc.arg(run_id), sqlc.arg(step), 'validating', sqlc.arg(workbook_file_id), sqlc.arg(started_by), sqlc.arg(started_at))
RETURNING *;

-- name: GetMigrationStep :one
SELECT * FROM fyniq.migration_step WHERE id = sqlc.arg(id);

-- name: ListMigrationSteps :many
SELECT * FROM fyniq.migration_step WHERE run_id = sqlc.arg(run_id) ORDER BY started_at, id;

-- name: ValidateMigrationStep :one
-- A validating step's report; none when the step is no longer validating.
UPDATE fyniq.migration_step
   SET state = sqlc.arg(state), checks_file_id = sqlc.arg(checks_file_id), report = sqlc.arg(report)::jsonb,
       report_sha256 = sqlc.arg(report_sha256), errors = sqlc.arg(errors), warnings = sqlc.arg(warnings),
       validated_at = sqlc.arg(validated_at)
 WHERE id = sqlc.arg(id) AND state = 'validating'
RETURNING *;

-- name: LatestMigrationMapping :one
SELECT * FROM fyniq.migration_mapping WHERE run_id = sqlc.arg(run_id) ORDER BY version DESC LIMIT 1;

-- name: InsertMigrationMapping :one
INSERT INTO fyniq.migration_mapping (run_id, version, step_id, mapping, mapping_sha256, made_by, made_at)
VALUES (sqlc.arg(run_id), sqlc.arg(version), sqlc.arg(step_id), sqlc.arg(mapping)::jsonb, sqlc.arg(mapping_sha256), sqlc.arg(made_by), sqlc.arg(made_at))
RETURNING *;

-- name: ListMigrationSignoffs :many
SELECT * FROM fyniq.migration_signoff WHERE run_id = sqlc.arg(run_id) ORDER BY signed_at, id;

-- name: InsertMigrationSignoff :one
INSERT INTO fyniq.migration_signoff (run_id, subject, mapping_id, subject_sha256, signed_by, signed_at)
VALUES (sqlc.arg(run_id), sqlc.arg(subject), sqlc.narg(mapping_id), sqlc.arg(subject_sha256), sqlc.arg(signed_by), sqlc.arg(signed_at))
RETURNING *;

-- name: GetMigrationMapping :one
SELECT * FROM fyniq.migration_mapping WHERE id = sqlc.arg(id);

-- name: GetMigrationCommit :one
-- A migration's commit: at most one (rev 0.254).
SELECT * FROM fyniq.migration_commit WHERE run_id = sqlc.arg(run_id);

-- name: InsertMigrationCommit :one
INSERT INTO fyniq.migration_commit (run_id, approval_id, path, step_id, mapping_id, records, approved_by, approved_at)
VALUES (sqlc.arg(run_id), sqlc.arg(approval_id), sqlc.arg(path), sqlc.arg(step_id), sqlc.arg(mapping_id), sqlc.arg(records),
        sqlc.arg(approved_by), sqlc.arg(approved_at))
RETURNING *;

-- name: CommitMigrationCommit :one
-- A running commit, committed; none when it is not running.
UPDATE fyniq.migration_commit SET status = 'committed', committed_at = sqlc.arg(committed_at)
 WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING *;

-- name: ListMigrationRecords :many
SELECT * FROM fyniq.migration_record WHERE commit_id = sqlc.arg(commit_id) ORDER BY imported_at, id;

-- name: InsertMigrationRecord :one
INSERT INTO fyniq.migration_record (run_id, commit_id, key, kind, sheet, row_number, record_type, record_id, imported_by, imported_at)
VALUES (sqlc.arg(run_id), sqlc.arg(commit_id), sqlc.arg(key), sqlc.arg(kind), sqlc.arg(sheet), sqlc.arg(row_number), sqlc.arg(record_type),
        sqlc.arg(record_id), sqlc.arg(imported_by), sqlc.arg(imported_at))
RETURNING *;

-- name: MigrationRecordOf :one
-- The row a record came from.
SELECT * FROM fyniq.migration_record WHERE record_type = sqlc.arg(record_type) AND record_id = sqlc.arg(record_id) ORDER BY imported_at LIMIT 1;

-- name: LockMigrationCommit :one
-- A commit, locked until the unit of work ends: its batches import one record at a time.
SELECT * FROM fyniq.migration_commit WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: GetMigrationRecordByKey :one
SELECT * FROM fyniq.migration_record WHERE commit_id = sqlc.arg(commit_id) AND key = sqlc.arg(key);

-- name: ListMigrationDifferences :many
SELECT * FROM fyniq.migration_difference WHERE run_id = sqlc.arg(run_id) ORDER BY line_key;

-- name: InsertMigrationDifference :one
INSERT INTO fyniq.migration_difference (run_id, line_key, class, note, fyniq_figure, workbook_figure, decided_by, decided_at)
VALUES (sqlc.arg(run_id), sqlc.arg(line_key), sqlc.arg(class), sqlc.arg(note), sqlc.arg(fyniq_figure), sqlc.arg(workbook_figure),
  sqlc.arg(decided_by), sqlc.arg(decided_at))
ON CONFLICT (organisation_id, run_id, line_key) DO NOTHING
RETURNING *;

-- name: UpdateMigrationDifference :one
UPDATE fyniq.migration_difference
SET class = sqlc.arg(class), note = sqlc.arg(note), fyniq_figure = sqlc.arg(fyniq_figure), workbook_figure = sqlc.arg(workbook_figure),
  version = version + 1, decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at)
WHERE run_id = sqlc.arg(run_id) AND line_key = sqlc.arg(line_key) AND version = sqlc.arg(version)
RETURNING *;

-- name: GetMigrationDifference :one
SELECT * FROM fyniq.migration_difference WHERE run_id = sqlc.arg(run_id) AND line_key = sqlc.arg(line_key);

-- name: InsertMigrationArchive :one
INSERT INTO fyniq.migration_archive (run_id, file_id, locked_key, sha256, retain_until, archived_by, archived_at)
VALUES (sqlc.arg(run_id), sqlc.arg(file_id), sqlc.arg(locked_key), sqlc.arg(sha256), sqlc.arg(retain_until), sqlc.arg(archived_by),
  sqlc.arg(archived_at))
RETURNING *;

-- name: ListMigrationArchive :many
SELECT * FROM fyniq.migration_archive WHERE run_id = sqlc.arg(run_id) ORDER BY archived_at, id;

-- name: CloseMigrationRun :one
UPDATE fyniq.migration_run SET status = 'closed' WHERE id = sqlc.arg(id) AND status = 'open' RETURNING *;
