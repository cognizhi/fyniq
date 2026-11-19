-- name: LivePeopleLink :one
SELECT id, state, epoch, nemura_organisation_id, nemura_origin, link_key, client_id, approval_id, version, created_by, created_at, changed_at
FROM fyniq.people_link WHERE state <> 'unlinked';

-- name: LastPeopleLinkEpoch :one
SELECT coalesce(max(epoch), 0)::int AS epoch FROM fyniq.people_link;

-- name: InsertPeopleLink :one
INSERT INTO fyniq.people_link (state, epoch, nemura_organisation_id, nemura_origin, link_key, client_id, approval_id, created_by, created_at, changed_at)
VALUES ('active', sqlc.arg(epoch), sqlc.arg(nemura_organisation_id), sqlc.arg(nemura_origin), sqlc.arg(link_key), sqlc.arg(client_id), sqlc.arg(approval_id),
        sqlc.arg(created_by), sqlc.arg(at), sqlc.arg(at))
RETURNING id, state, epoch, nemura_organisation_id, nemura_origin, link_key, client_id, approval_id, version, created_by, created_at, changed_at;

-- name: PeopleLinkForUpdate :one
SELECT id, state, epoch, nemura_organisation_id, nemura_origin, link_key, client_id, approval_id, version, created_by, created_at, changed_at
FROM fyniq.people_link WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: SetPeopleLinkState :one
UPDATE fyniq.people_link SET state = sqlc.arg(state), client_id = sqlc.arg(client_id), version = version + 1, changed_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, state, epoch, nemura_organisation_id, nemura_origin, link_key, client_id, approval_id, version, created_by, created_at, changed_at;

-- name: InsertPeopleSource :exec
INSERT INTO fyniq.people_source (feature, source, effective_from, approval_id, set_at)
VALUES (sqlc.arg(feature), sqlc.arg(source), sqlc.arg(effective_from), sqlc.arg(approval_id), sqlc.arg(set_at));

-- name: PeopleSourcesInForce :many
SELECT DISTINCT ON (feature) id, feature, source, effective_from, approval_id, set_at
FROM fyniq.people_source ORDER BY feature, set_at DESC, id DESC;

-- name: InsertPeopleInbox :execrows
INSERT INTO fyniq.people_link_inbox (event_id, link_id, epoch, sequence, type, object_type, object_id, object_version, payload_sha256, outcome, code, received_at)
VALUES (sqlc.arg(event_id), sqlc.arg(link_id), sqlc.arg(epoch), sqlc.arg(sequence), sqlc.arg(type), sqlc.arg(object_type), sqlc.arg(object_id),
        sqlc.arg(object_version), sqlc.arg(payload_sha256), sqlc.arg(outcome), sqlc.arg(code), sqlc.arg(received_at))
ON CONFLICT (organisation_id, event_id) DO NOTHING;

-- name: PeopleInboxCounts :many
SELECT outcome, count(*)::int AS n, max(received_at) AS last_at FROM fyniq.people_link_inbox WHERE link_id = sqlc.arg(link_id) GROUP BY outcome;

-- name: PersonProjectionForUpdate :one
SELECT person_id, version, display_name, work_email, department_value_id, cost_centre_value_id, started_on, left_on, status, line_manager_person_id,
       removed, hold_version, hold_id, held, hold_from, bank_version, bank_record_id, user_id, updated_at
FROM fyniq.person_projection WHERE person_id = sqlc.arg(person_id) FOR UPDATE;

-- name: PersonProjections :many
SELECT person_id, version, display_name, work_email, department_value_id, cost_centre_value_id, started_on, left_on, status, line_manager_person_id,
       removed, hold_version, hold_id, held, hold_from, bank_version, bank_record_id, user_id, updated_at
FROM fyniq.person_projection ORDER BY display_name NULLS LAST, person_id;

-- name: PersonProjectionByUser :one
SELECT person_id, version, display_name, work_email, department_value_id, cost_centre_value_id, started_on, left_on, status, line_manager_person_id,
       removed, hold_version, hold_id, held, hold_from, bank_version, bank_record_id, user_id, updated_at
FROM fyniq.person_projection WHERE user_id = sqlc.arg(user_id);

-- name: UpsertPersonProjection :exec
INSERT INTO fyniq.person_projection (person_id, version, display_name, work_email, department_value_id, cost_centre_value_id, started_on, left_on, status,
                                     line_manager_person_id, removed, hold_version, hold_id, held, hold_from, bank_version, bank_record_id, user_id, updated_at)
VALUES (sqlc.arg(person_id), sqlc.arg(version), sqlc.arg(display_name), sqlc.arg(work_email), sqlc.arg(department_value_id), sqlc.arg(cost_centre_value_id),
        sqlc.arg(started_on), sqlc.arg(left_on), sqlc.arg(status), sqlc.arg(line_manager_person_id), sqlc.arg(removed), sqlc.arg(hold_version), sqlc.arg(hold_id),
        sqlc.arg(held), sqlc.arg(hold_from), sqlc.arg(bank_version), sqlc.arg(bank_record_id), sqlc.arg(user_id), sqlc.arg(updated_at))
ON CONFLICT (organisation_id, person_id) DO UPDATE SET version = EXCLUDED.version, display_name = EXCLUDED.display_name, work_email = EXCLUDED.work_email,
  department_value_id = EXCLUDED.department_value_id, cost_centre_value_id = EXCLUDED.cost_centre_value_id, started_on = EXCLUDED.started_on,
  left_on = EXCLUDED.left_on, status = EXCLUDED.status, line_manager_person_id = EXCLUDED.line_manager_person_id, removed = EXCLUDED.removed,
  hold_version = EXCLUDED.hold_version, hold_id = EXCLUDED.hold_id, held = EXCLUDED.held, hold_from = EXCLUDED.hold_from,
  bank_version = EXCLUDED.bank_version, bank_record_id = EXCLUDED.bank_record_id, user_id = EXCLUDED.user_id, updated_at = EXCLUDED.updated_at;
