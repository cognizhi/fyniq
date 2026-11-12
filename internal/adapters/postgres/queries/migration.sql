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
