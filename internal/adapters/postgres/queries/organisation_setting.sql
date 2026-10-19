-- name: OrganisationSettings :many
SELECT id, key, value, effective_from, version, recorded_at FROM fyniq.organisation_setting
WHERE sqlc.arg(key)::text = '' OR key = sqlc.arg(key)::text
ORDER BY key, version;

-- name: InsertOrganisationSetting :one
INSERT INTO fyniq.organisation_setting (key, value, effective_from, version)
VALUES (sqlc.arg(key), sqlc.arg(value), sqlc.arg(effective_from), sqlc.arg(version))
RETURNING id, key, value, effective_from, version, recorded_at;
