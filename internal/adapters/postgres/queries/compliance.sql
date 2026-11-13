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

-- name: InsertRisk :one
INSERT INTO fyniq.risk (ref, created_at) VALUES (sqlc.arg(ref), sqlc.arg(created_at)) RETURNING *;

-- name: NextRiskRef :one
-- The next reference of a new risk: R and one more than the highest number used.
SELECT ('R' || (coalesce(max(substring(r.ref FROM 2)::integer), 0) + 1))::text AS ref FROM fyniq.risk r;

-- name: InsertRiskVersion :one
INSERT INTO fyniq.risk_version (risk_id, version, title, description, owner_role, likelihood, impact, mitigation, review_months,
                                next_review_on, state, saved_by, saved_at)
VALUES (sqlc.arg(risk_id), sqlc.arg(version), sqlc.arg(title), sqlc.arg(description), sqlc.arg(owner_role), sqlc.arg(likelihood),
        sqlc.arg(impact), sqlc.arg(mitigation), sqlc.arg(review_months), sqlc.arg(next_review_on), sqlc.arg(state), sqlc.arg(saved_by),
        sqlc.arg(saved_at))
RETURNING *;

-- name: InsertRiskReview :one
INSERT INTO fyniq.risk_review (risk_id, version, reviewed_by, reviewed_at, note, next_review_on)
VALUES (sqlc.arg(risk_id), sqlc.arg(version), sqlc.arg(reviewed_by), sqlc.arg(reviewed_at), sqlc.arg(note), sqlc.arg(next_review_on))
RETURNING *;

-- name: ListRisks :many
-- Every risk at its latest version, in the order of their references.
SELECT r.id, r.ref, r.created_at, v.version, v.title, v.description, v.owner_role, v.likelihood, v.impact, v.mitigation,
       v.review_months, v.next_review_on, v.state, v.saved_by, v.saved_at
  FROM fyniq.risk r
  JOIN LATERAL (SELECT * FROM fyniq.risk_version x WHERE x.organisation_id = r.organisation_id AND x.risk_id = r.id
                 ORDER BY x.version DESC LIMIT 1) v ON true
 ORDER BY substring(r.ref FROM 2)::integer;

-- name: LatestRiskReviews :many
-- Each risk's latest review.
SELECT DISTINCT ON (y.risk_id) y.*
  FROM fyniq.risk_review y
 ORDER BY y.risk_id, y.reviewed_at DESC, y.id DESC;

-- name: LatestPolicyDrafts :many
-- Each policy's latest draft revision.
SELECT DISTINCT ON (d.key) d.* FROM fyniq.policy_draft d ORDER BY d.key, d.revision DESC;

-- name: ListPolicyVersions :many
SELECT * FROM fyniq.policy_version ORDER BY key, version;

-- name: ListPolicyAcknowledgements :many
SELECT * FROM fyniq.policy_acknowledgement ORDER BY key, version, acknowledged_at;

-- name: InsertPolicyDraft :one
INSERT INTO fyniq.policy_draft (key, revision, body, sha256, saved_by, saved_at)
VALUES (sqlc.arg(key), sqlc.arg(revision), sqlc.arg(body), sha256(convert_to(sqlc.arg(body)::text, 'UTF8')), sqlc.arg(saved_by), sqlc.arg(saved_at))
RETURNING *;

-- name: InsertPolicyVersion :one
INSERT INTO fyniq.policy_version (key, version, title, body, sha256, from_draft, published_by, published_at)
VALUES (sqlc.arg(key), sqlc.arg(version), sqlc.arg(title), sqlc.arg(body), sha256(convert_to(sqlc.arg(body)::text, 'UTF8')), sqlc.arg(from_draft),
        sqlc.arg(published_by), sqlc.arg(published_at))
RETURNING *;

-- name: InsertPolicyAcknowledgement :one
INSERT INTO fyniq.policy_acknowledgement (key, version, acknowledged_by, acknowledged_at)
VALUES (sqlc.arg(key), sqlc.arg(version), sqlc.arg(acknowledged_by), sqlc.arg(acknowledged_at))
RETURNING *;

-- name: ListConsentPurposes :many
-- The organisation's consent purposes, oldest first (CMP-PD-03).
SELECT id, name, description, audience, needs_consent, created_by, created_at FROM fyniq.consent_purpose ORDER BY created_at, id;

-- name: InsertConsentPurpose :one
INSERT INTO fyniq.consent_purpose (name, description, audience, needs_consent, created_by, created_at)
VALUES (sqlc.arg(name), sqlc.arg(description), sqlc.arg(audience), sqlc.arg(needs_consent), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, name, description, audience, needs_consent, created_by, created_at;

-- name: ListConsentRecords :many
-- Every consent given or withdrawn, oldest first (CMP-PD-03).
SELECT id, purpose_id, person, event, at, recorded_by, note FROM fyniq.consent_record ORDER BY at, id;

-- name: InsertConsentRecord :one
INSERT INTO fyniq.consent_record (purpose_id, person, event, at, recorded_by, note)
VALUES (sqlc.arg(purpose_id), sqlc.arg(person), sqlc.arg(event), sqlc.arg(at), sqlc.arg(recorded_by), sqlc.narg(note))
RETURNING id, purpose_id, person, event, at, recorded_by, note;

-- name: ListDataInventoryEntries :many
-- Every version of the organisation's inventory entries, by entry and version (CMP-PD-02).
SELECT entry_id, version, what, why, where_held, who, retention, transfers, needs_dpia, state, saved_by, saved_at
FROM fyniq.data_inventory_entry ORDER BY entry_id, version;

-- name: InsertDataInventoryEntry :exec
INSERT INTO fyniq.data_inventory_entry (entry_id, version, what, why, where_held, who, retention, transfers, needs_dpia, state, saved_by, saved_at)
VALUES (sqlc.arg(entry_id), sqlc.arg(version), sqlc.arg(what), sqlc.arg(why), sqlc.arg(where_held), sqlc.arg(who), sqlc.arg(retention),
  sqlc.arg(transfers), sqlc.arg(needs_dpia), sqlc.arg(state), sqlc.arg(saved_by), sqlc.arg(saved_at));

-- name: ListDataInventoryReviews :many
-- The inventory's quarterly reviews, oldest first (CTL-DP-05).
SELECT id, quarter, reviewed_by, reviewed_at, note FROM fyniq.data_inventory_review ORDER BY quarter;

-- name: InsertDataInventoryReview :one
INSERT INTO fyniq.data_inventory_review (quarter, reviewed_by, reviewed_at, note)
VALUES (sqlc.arg(quarter), sqlc.arg(reviewed_by), sqlc.arg(reviewed_at), sqlc.narg(note))
RETURNING id, quarter, reviewed_by, reviewed_at, note;

-- name: ListDPIAVersions :many
-- Every version of the organisation's impact assessments, by key and version (CMP-PD-08).
SELECT id, key, version, title, body, sha256, from_draft, entry_id, saved_by, saved_at FROM fyniq.dpia_version ORDER BY key, version;

-- name: InsertDPIAVersion :one
INSERT INTO fyniq.dpia_version (key, version, title, body, sha256, from_draft, entry_id, saved_by, saved_at)
VALUES (sqlc.arg(key), sqlc.arg(version), sqlc.arg(title), sqlc.arg(body), sha256(convert_to(sqlc.arg(body)::text, 'UTF8')), sqlc.arg(from_draft),
  sqlc.narg(entry_id), sqlc.arg(saved_by), sqlc.arg(saved_at))
RETURNING id, key, version, title, body, sha256, from_draft, entry_id, saved_by, saved_at;

-- name: ListDPIAApprovals :many
SELECT id, key, version, approved_by, approved_at FROM fyniq.dpia_approval ORDER BY key, version;

-- name: InsertDPIAApproval :one
INSERT INTO fyniq.dpia_approval (key, version, approved_by, approved_at)
VALUES (sqlc.arg(key), sqlc.arg(version), sqlc.arg(approved_by), sqlc.arg(approved_at))
RETURNING id, key, version, approved_by, approved_at;

-- name: ListPDRequests :many
-- The organisation's access and correction requests, oldest first (CMP-PD-04).
SELECT id, kind, subject, requester, details, received_on, due_on, created_by, created_at FROM fyniq.pd_request ORDER BY received_on, id;

-- name: InsertPDRequest :one
INSERT INTO fyniq.pd_request (kind, subject, requester, details, received_on, due_on, created_by, created_at)
VALUES (sqlc.arg(kind), sqlc.arg(subject), sqlc.narg(requester), sqlc.arg(details), sqlc.arg(received_on), sqlc.arg(received_on)::date + 30,
  sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING id, kind, subject, requester, details, received_on, due_on, created_by, created_at;

-- name: ListPDRequestSteps :many
SELECT id, request_id, step, at, by_actor, note, new_due_on, outcome FROM fyniq.pd_request_step ORDER BY at, id;

-- name: InsertPDRequestStep :one
INSERT INTO fyniq.pd_request_step (request_id, step, at, by_actor, note, new_due_on, outcome)
VALUES (sqlc.arg(request_id), sqlc.arg(step), sqlc.arg(at), sqlc.arg(by_actor), sqlc.narg(note), sqlc.narg(new_due_on), sqlc.narg(outcome))
RETURNING id, request_id, step, at, by_actor, note, new_due_on, outcome;

-- name: ListBreaches :many
-- The organisation's breaches, oldest first (CMP-PD-05).
SELECT id, title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at FROM fyniq.pd_breach ORDER BY aware_at, id;

-- name: InsertBreach :one
-- Its assessment is due 30 days after awareness, and the database works out whether its data is prescribed.
INSERT INTO fyniq.pd_breach (title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at)
VALUES (sqlc.arg(title), sqlc.arg(description), sqlc.arg(aware_at), (sqlc.arg(aware_at)::timestamptz AT TIME ZONE 'Asia/Singapore')::date + 30,
  sqlc.arg(individuals), sqlc.arg(classes)::text[],
  (sqlc.arg(classes)::text[] && ARRAY['names', 'identification_numbers']::text[] AND sqlc.arg(classes)::text[] && ARRAY['pay', 'bank_accounts', 'card_numbers']::text[])
    OR 'credentials' = ANY(sqlc.arg(classes)::text[]),
  sqlc.arg(recorded_by), sqlc.arg(recorded_at))
RETURNING id, title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at;

-- name: ListBreachSteps :many
SELECT id, breach_id, step, at, by_actor, note, notifiable, pdpc_due_on, reference FROM fyniq.pd_breach_step ORDER BY at, id;

-- name: InsertBreachStep :one
INSERT INTO fyniq.pd_breach_step (breach_id, step, at, by_actor, note, notifiable, pdpc_due_on, reference)
VALUES (sqlc.arg(breach_id), sqlc.arg(step), sqlc.arg(at), sqlc.arg(by_actor), sqlc.narg(note), sqlc.narg(notifiable),
  CASE WHEN sqlc.arg(step)::text = 'decided' AND sqlc.narg(notifiable)::boolean THEN (sqlc.arg(at)::timestamptz AT TIME ZONE 'Asia/Singapore')::date + 3 END,
  sqlc.narg(reference))
RETURNING id, breach_id, step, at, by_actor, note, notifiable, pdpc_due_on, reference;

-- name: ListIncidents :many
-- The organisation's security incidents, oldest first (SEC-32).
SELECT id, title, summary, severity, detected_at, opened_by, opened_at FROM fyniq.security_incident ORDER BY detected_at, id;

-- name: InsertIncident :one
INSERT INTO fyniq.security_incident (title, summary, severity, detected_at, opened_by, opened_at)
VALUES (sqlc.arg(title), sqlc.arg(summary), sqlc.arg(severity), sqlc.arg(detected_at), sqlc.arg(opened_by), sqlc.arg(opened_at))
RETURNING id, title, summary, severity, detected_at, opened_by, opened_at;

-- name: ListIncidentSteps :many
SELECT id, incident_id, step, at, by_actor, note, breach_id FROM fyniq.security_incident_step ORDER BY at, id;

-- name: InsertIncidentStep :one
INSERT INTO fyniq.security_incident_step (incident_id, step, at, by_actor, note, breach_id)
VALUES (sqlc.arg(incident_id), sqlc.arg(step), sqlc.arg(at), sqlc.arg(by_actor), sqlc.arg(note), sqlc.narg(breach_id))
RETURNING id, incident_id, step, at, by_actor, note, breach_id;

-- name: ListBreachExercises :many
SELECT id, held_on, scenario, findings, recorded_by, recorded_at FROM fyniq.pd_breach_exercise ORDER BY held_on, id;

-- name: InsertBreachExercise :one
INSERT INTO fyniq.pd_breach_exercise (held_on, scenario, findings, recorded_by, recorded_at)
VALUES (sqlc.arg(held_on), sqlc.arg(scenario), sqlc.arg(findings), sqlc.arg(recorded_by), sqlc.arg(recorded_at))
RETURNING id, held_on, scenario, findings, recorded_by, recorded_at;

-- name: ListTransfers :many
-- Every version of the organisation's overseas transfers, by transfer and version (CMP-PD-06).
SELECT transfer_id, version, flow, recipient, countries, data, purpose, basis, assessed_on, assessed_by, evidence, next_review_on, state,
  saved_by, saved_at
FROM fyniq.pd_transfer ORDER BY transfer_id, version;

-- name: InsertTransfer :exec
INSERT INTO fyniq.pd_transfer (transfer_id, version, flow, recipient, countries, data, purpose, basis, assessed_on, assessed_by, evidence,
  next_review_on, state, saved_by, saved_at)
VALUES (sqlc.arg(transfer_id), sqlc.arg(version), sqlc.narg(flow), sqlc.arg(recipient), sqlc.arg(countries), sqlc.arg(data), sqlc.arg(purpose),
  sqlc.arg(basis), sqlc.arg(assessed_on), sqlc.arg(assessed_by), sqlc.arg(evidence), sqlc.arg(next_review_on), sqlc.arg(state),
  sqlc.arg(saved_by), sqlc.arg(saved_at));

-- name: ListRestoreDrills :many
-- The organisation's restore drills, oldest first (DR-06).
SELECT id, held_on, scenario, restored_from, data_lost_minutes, took_minutes, ledger, audit_chain, documents, keys, sign_in, tick, outbox,
       findings, recorded_by, recorded_at
FROM fyniq.restore_drill ORDER BY held_on, recorded_at, id;

-- name: InsertRestoreDrill :one
INSERT INTO fyniq.restore_drill (held_on, scenario, restored_from, data_lost_minutes, took_minutes, ledger, audit_chain, documents, keys,
                                 sign_in, tick, outbox, findings, recorded_by, recorded_at)
VALUES (sqlc.arg(held_on), sqlc.arg(scenario), sqlc.arg(restored_from), sqlc.arg(data_lost_minutes), sqlc.arg(took_minutes), sqlc.arg(ledger),
        sqlc.arg(audit_chain), sqlc.arg(documents), sqlc.arg(keys), sqlc.arg(sign_in), sqlc.arg(tick), sqlc.arg(outbox), sqlc.arg(findings),
        sqlc.arg(recorded_by), sqlc.arg(recorded_at))
RETURNING id, held_on, scenario, restored_from, data_lost_minutes, took_minutes, ledger, audit_chain, documents, keys, sign_in, tick, outbox,
          findings, recorded_by, recorded_at;
