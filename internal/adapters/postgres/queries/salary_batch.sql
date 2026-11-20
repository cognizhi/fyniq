-- name: NextSalaryBatchSeq :one
-- The next number takes the organisation's batches under a lock, so two batches never share one.
SELECT COALESCE(max(seq), 0)::integer + 1 AS seq FROM fyniq.salary_batch;

-- name: InsertSalaryBatch :one
INSERT INTO fyniq.salary_batch (seq, link_id, source_system, run_id, run_version, receipt_id, wage_month, lines_version, created_at, changed_at)
VALUES (sqlc.arg(seq), sqlc.arg(link_id), sqlc.arg(source_system), sqlc.arg(run_id), sqlc.arg(run_version), sqlc.arg(receipt_id), sqlc.arg(wage_month),
        sqlc.arg(lines_version), sqlc.arg(created_at), sqlc.arg(created_at))
RETURNING *;

-- name: SalaryBatch :one
SELECT * FROM fyniq.salary_batch WHERE id = sqlc.arg(id);

-- name: SalaryBatchForUpdate :one
SELECT * FROM fyniq.salary_batch WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: SalaryBatchOfRun :one
SELECT * FROM fyniq.salary_batch WHERE source_system = sqlc.arg(source_system) AND run_id = sqlc.arg(run_id) AND run_version = sqlc.arg(run_version) FOR UPDATE;

-- name: SalaryBatches :many
SELECT * FROM fyniq.salary_batch ORDER BY seq DESC;

-- name: SalaryBatchesOfPerson :many
SELECT DISTINCT b.* FROM fyniq.salary_batch b JOIN fyniq.salary_batch_line l ON l.organisation_id = b.organisation_id AND l.batch_id = b.id
WHERE l.person_id = sqlc.arg(person_id) ORDER BY b.seq DESC;

-- name: PrepareSalaryBatch :one
UPDATE fyniq.salary_batch SET pay_date = sqlc.arg(pay_date), money_account_id = sqlc.arg(money_account_id), method = sqlc.arg(method), prepared_by = sqlc.arg(prepared_by), version = version + 1, changed_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version) AND status <> 'cancelled' RETURNING *;

-- name: ReviseSalaryBatch :one
-- A revised line is a new version of it: the batch's version goes up, so an approval of the old one is stale.
UPDATE fyniq.salary_batch SET version = version + 1, lines_version = sqlc.arg(lines_version), changed_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND status <> 'cancelled' RETURNING *;

-- name: ApproveSalaryBatch :one
UPDATE fyniq.salary_batch SET status = 'approved', changed_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND status <> 'cancelled' RETURNING *;

-- name: CancelSalaryBatch :one
UPDATE fyniq.salary_batch SET status = 'cancelled', cancel_reason = sqlc.arg(reason), version = version + 1, changed_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND status <> 'cancelled' RETURNING *;

-- name: InsertSalaryBatchLine :one
INSERT INTO fyniq.salary_batch_line (batch_id, line_ref, line_version, person_id, amount, bank_detail_id, bank_detail_version, bank_detail_digest, sealed, event_id, created_at)
VALUES (sqlc.arg(batch_id), sqlc.arg(line_ref), sqlc.arg(line_version), sqlc.arg(person_id), sqlc.arg(amount)::numeric, sqlc.arg(bank_detail_id), sqlc.arg(bank_detail_version),
        sqlc.arg(bank_detail_digest), sqlc.arg(sealed), sqlc.arg(event_id), sqlc.arg(created_at))
RETURNING *;

-- name: SalaryBatchLines :many
-- The current version of each line: the highest.
SELECT l.* FROM fyniq.salary_batch_line l
WHERE l.batch_id = sqlc.arg(batch_id)
  AND l.line_version = (SELECT max(x.line_version) FROM fyniq.salary_batch_line x WHERE x.organisation_id = l.organisation_id AND x.batch_id = l.batch_id AND x.line_ref = l.line_ref)
ORDER BY l.created_at, l.line_ref;

-- name: SalaryBatchLine :one
SELECT * FROM fyniq.salary_batch_line WHERE id = sqlc.arg(id);

-- name: InsertSalaryLineEvent :one
INSERT INTO fyniq.salary_line_event (batch_id, line_ref, person_id, run_id, state, source, instruction_id, amount, on_date, bank_line_id, evidence_id, recorded_by, at)
VALUES (sqlc.arg(batch_id), sqlc.arg(line_ref), sqlc.arg(person_id), sqlc.arg(run_id), sqlc.arg(state), sqlc.arg(source), sqlc.narg(instruction_id), sqlc.narg(amount)::numeric,
        sqlc.narg(on_date)::date, sqlc.narg(bank_line_id)::uuid, sqlc.narg(evidence_id)::uuid, sqlc.arg(recorded_by), sqlc.arg(at))
RETURNING *;

-- name: SalaryLineEvents :many
SELECT * FROM fyniq.salary_line_event WHERE batch_id = sqlc.arg(batch_id) ORDER BY at, id;

-- name: SalaryLatestStatesOfRunPerson :many
-- The latest event of each line of a run for a person, across every batch of the run: an unknown outcome blocks by run and person (FR-PPL-12).
SELECT DISTINCT ON (batch_id, line_ref) batch_id, line_ref, state FROM fyniq.salary_line_event
WHERE run_id = sqlc.arg(run_id) AND person_id = sqlc.arg(person_id) ORDER BY batch_id, line_ref, at DESC, id DESC;

-- name: InsertSalaryRelease :one
INSERT INTO fyniq.salary_release (batch_id, batch_version, approval_id, released_by, released_at, writer, file_name, media_type, file_sealed, file_sha256, lines, total)
VALUES (sqlc.arg(batch_id), sqlc.arg(batch_version), sqlc.arg(approval_id), sqlc.arg(released_by), sqlc.arg(released_at), sqlc.arg(writer), sqlc.arg(file_name), sqlc.arg(media_type),
        sqlc.arg(file_sealed), sqlc.arg(file_sha256), sqlc.arg(lines), sqlc.arg(total)::numeric)
RETURNING *;

-- name: InsertSalaryReleaseLine :exec
INSERT INTO fyniq.salary_release_line (release_id, batch_id, line_ref, line_id, instruction_id)
VALUES (sqlc.arg(release_id), sqlc.arg(batch_id), sqlc.arg(line_ref), sqlc.arg(line_id), sqlc.arg(instruction_id));

-- name: SalaryReleases :many
SELECT * FROM fyniq.salary_release WHERE batch_id = sqlc.arg(batch_id) ORDER BY released_at, id;

-- name: SalaryRelease :one
SELECT * FROM fyniq.salary_release WHERE id = sqlc.arg(id);

-- name: SalaryReleaseLines :many
SELECT * FROM fyniq.salary_release_line WHERE batch_id = sqlc.arg(batch_id) ORDER BY id;

-- name: SalaryReleaseLinesOf :many
SELECT * FROM fyniq.salary_release_line WHERE release_id = sqlc.arg(release_id) ORDER BY id;

-- name: SalaryIsRunParty :one
-- Whether a Fyniq user is, by issuer and subject, the preparer or approver of the batch's pay run in Nemura (SOD-4).
SELECT fyniq.salary_is_run_party(b.organisation_id, b.receipt_id, sqlc.arg(who)::uuid)::boolean AS party FROM fyniq.salary_batch b WHERE b.id = sqlc.arg(batch_id);
