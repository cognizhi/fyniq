-- name: InsertControlRun :one
INSERT INTO fyniq.control_run (control, frequency, check_how, since, ran_at, ok, item_count, items, detail)
VALUES (sqlc.arg(control), sqlc.arg(frequency), sqlc.arg(check_how), sqlc.arg(since), sqlc.arg(ran_at), sqlc.arg(ok),
        sqlc.arg(item_count), sqlc.arg(items), sqlc.arg(detail))
RETURNING *;

-- name: LatestControlRuns :many
-- The latest run of each verification of each control.
SELECT DISTINCT ON (r.control, r.frequency, r.check_how) r.*
  FROM fyniq.control_run r
 ORDER BY r.control, r.frequency, r.check_how, r.ran_at DESC, r.id DESC;

-- name: DualControlApprovals :many
-- CTL-FIN-08: each approval of a payment batch or of a payee's or claimant's bank change decided since the window began,
-- with how many signed it and how many of them prepared it or asked for it.
SELECT r.id, r.object_type, r.object_key, r.decided_at::timestamptz AS decided_at,
       (SELECT count(*) FROM fyniq.approval_signature s
         WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id)::integer AS signatures,
       (SELECT count(*) FROM fyniq.approval_signature s
         WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
           AND (s.signer = r.requested_by OR 'user:' || s.signer::text = (
                 SELECT v.created_by FROM fyniq.document d
                   JOIN fyniq.document_version v
                     ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
                  WHERE d.organisation_id = r.organisation_id AND d.id::text = r.object_key)))::integer AS own_signatures
  FROM fyniq.approval_request r
 WHERE r.operation IN ('payment_batch.approve', 'payee_bank_change.approve', 'claimant_bank_change.approve')
   AND r.status = 'approved' AND r.decided_at >= sqlc.arg(since)
 ORDER BY r.decided_at, r.id;

-- name: DualControlReleases :many
-- CTL-FIN-08: each payment batch released since the window began, with its approval's signatures (all of them, and those
-- of whoever prepared the batch or asked for its approval) or whether its break-glass's Owner used it for the batch.
SELECT rel.batch_id, rel.released_at, rel.approval_id, rel.break_glass_id,
       coalesce((SELECT r.status = 'approved' FROM fyniq.approval_request r
                  WHERE r.organisation_id = rel.organisation_id AND r.id = rel.approval_id), false)::boolean AS approved,
       (SELECT count(*) FROM fyniq.approval_signature s
         WHERE s.organisation_id = rel.organisation_id AND s.request_id = rel.approval_id)::integer AS signatures,
       (SELECT count(*) FROM fyniq.approval_signature s
          JOIN fyniq.approval_request r ON r.organisation_id = s.organisation_id AND r.id = s.request_id
         WHERE s.organisation_id = rel.organisation_id AND s.request_id = rel.approval_id
           AND (s.signer = r.requested_by OR 'user:' || s.signer::text = (
                 SELECT v.created_by FROM fyniq.document_version v
                  WHERE v.organisation_id = rel.organisation_id AND v.document_id = rel.batch_id
                    AND v.version = rel.batch_version)))::integer AS own_signatures,
       EXISTS (SELECT 1 FROM fyniq.break_glass_use u
                WHERE u.organisation_id = rel.organisation_id AND u.break_glass_id = rel.break_glass_id
                  AND u.batch_id = rel.batch_id AND u.used_by = rel.released_by)::boolean AS break_glass_used
  FROM fyniq.payment_batch_release rel
 WHERE rel.released_at >= sqlc.arg(since)
 ORDER BY rel.released_at, rel.id;

-- name: MembersSecondFactor :many
SELECT f.user_id::uuid AS user_id, f.passkeys::integer AS passkeys, f.sessions_with::integer AS sessions_with,
       f.sessions_without::integer AS sessions_without,
       coalesce(f.last_without, 'epoch'::timestamptz)::timestamptz AS last_without -- read only when sessions_without > 0
  FROM fyniq.members_second_factor(sqlc.arg(since)::timestamptz) AS f;

-- name: ExportsWithoutTheirRecords :many
-- CTL-AUD-05: each report export asked for since the window began without its audit event, and each of the latest
-- sqlc.arg(sample) events of each audited read since then (an export, a download, an opening or a reveal) without the
-- request details kept beside the chain.
SELECT 'report_export'::text AS kind, e.id::text AS object_key, e.requested_at::timestamptz AS at,
       'asked for, with no audit event'::text AS reason
  FROM fyniq.report_export e
 WHERE e.requested_at >= sqlc.arg(since)
   AND NOT EXISTS (SELECT 1 FROM fyniq.audit_event a
                    WHERE a.organisation_id = e.organisation_id AND a.object_type = 'report_export' AND a.object_id = e.id
                      AND a.action = 'report_export.requested')
UNION ALL
SELECT x.action, x.id::text, x.at, 'recorded without the request it came from'
  FROM unnest(sqlc.arg(actions)::text[]) AS k(action)
 CROSS JOIN LATERAL (
       SELECT a.id, a.action, a.organisation_id, ((a.body::jsonb) ->> 'occurred_at')::timestamptz AS at
         FROM fyniq.audit_event a
        WHERE a.action = k.action
        ORDER BY a.seq DESC
        LIMIT sqlc.arg(sample)::integer) AS x
 WHERE x.at >= sqlc.arg(since)
   AND NOT EXISTS (SELECT 1 FROM fyniq.audit_request_detail d WHERE d.organisation_id = x.organisation_id AND d.id = x.id)
 ORDER BY 3, 2
 LIMIT 1000;

-- name: ListControlStates :many
SELECT * FROM fyniq.control_state ORDER BY control;

-- name: GetControlState :one
SELECT * FROM fyniq.control_state WHERE control = sqlc.arg(control);

-- name: InsertControlState :one
INSERT INTO fyniq.control_state (control, status, detail, checks, failing_since, run_at, changed_at)
VALUES (sqlc.arg(control), sqlc.arg(status), sqlc.arg(detail), sqlc.arg(checks), sqlc.narg(failing_since), sqlc.arg(run_at),
        sqlc.arg(changed_at))
RETURNING *;

-- name: UpdateControlState :one
UPDATE fyniq.control_state
   SET status = sqlc.arg(status), detail = sqlc.arg(detail), checks = sqlc.arg(checks), failing_since = sqlc.narg(failing_since),
       run_at = sqlc.arg(run_at), changed_at = sqlc.arg(changed_at), version = version + 1
 WHERE control = sqlc.arg(control) AND version = sqlc.arg(version)
RETURNING *;
