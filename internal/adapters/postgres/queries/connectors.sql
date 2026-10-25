-- name: InstallationConnectors :many
SELECT c.id, c.connector, c.manifest_version, c.manifest_hash, c.secret_names, c.consent, c.state, c.configuration, c.position, c.test_passed,
  c.test_reason, c.tested_at, c.installed_by, c.installed_at, c.changed_by, c.changed_at, c.version
FROM fyniq.installation_connectors() c;

-- name: InstallationConnectorSecrets :many
SELECT s.id, s.connector_id, s.name, s.ref, s.set_by, s.set_at, s.valid_until, s.destroyed_at
FROM fyniq.installation_connector_secrets() s;

-- name: InstallInstallationConnector :one
SELECT fyniq.install_installation_connector(sqlc.arg(by), sqlc.narg(change)::uuid, sqlc.arg(connector), sqlc.arg(manifest_version),
  sqlc.arg(manifest_hash), sqlc.arg(secret_names)::text[], sqlc.arg(consent)::jsonb, sqlc.arg(configuration)::jsonb, sqlc.arg(position)::integer,
  sqlc.arg(at))::uuid AS id;

-- name: BootstrapInstallationConnectors :one
SELECT fyniq.bootstrap_installation_connectors(sqlc.arg(rows)::jsonb, sqlc.arg(at))::integer AS installed;

-- name: ChangeInstallationConnector :one
SELECT fyniq.change_installation_connector(sqlc.arg(by), sqlc.narg(change)::uuid, sqlc.arg(step), sqlc.arg(id), sqlc.arg(version)::integer,
  sqlc.arg(state), sqlc.arg(configuration)::jsonb, sqlc.arg(position)::integer, sqlc.narg(test_passed)::boolean, sqlc.narg(test_reason)::text,
  sqlc.narg(tested_at)::timestamptz, sqlc.arg(manifest_version), sqlc.arg(manifest_hash), sqlc.arg(secret_names)::text[],
  sqlc.arg(consent)::jsonb, sqlc.arg(at))::integer AS version;

-- name: SetInstallationConnectorSecret :exec
SELECT fyniq.set_installation_connector_secret(sqlc.arg(by), sqlc.narg(change)::uuid, sqlc.arg(id), sqlc.arg(name), sqlc.arg(ref),
  sqlc.arg(valid_until), sqlc.arg(at));

-- name: DestroyedInstallationConnectorSecret :exec
SELECT fyniq.destroyed_installation_connector_secret(sqlc.arg(ref), sqlc.arg(at));

-- name: RequestInstallationChange :one
SELECT fyniq.request_installation_change(sqlc.arg(by), sqlc.narg(connector_id)::uuid, sqlc.arg(connector), sqlc.arg(step), sqlc.arg(summary),
  sqlc.arg(change)::jsonb, sqlc.arg(envelope), sqlc.arg(envelope_hash)::bytea, sqlc.arg(mfa_at), sqlc.arg(at), sqlc.arg(expires_at))::uuid AS id;

-- name: DecideInstallationChange :exec
SELECT fyniq.decide_installation_change(sqlc.arg(by), sqlc.arg(id), sqlc.arg(envelope_hash)::bytea, sqlc.arg(approve)::boolean,
  sqlc.arg(mfa_at), sqlc.narg(reason)::text, sqlc.arg(at));

-- name: ExpireInstallationChanges :many
SELECT e.id::uuid AS id FROM fyniq.expire_installation_changes(sqlc.arg(at)) AS e(id);

-- name: InstallationChanges :many
SELECT c.id, c.connector_id, c.connector, c.step, c.summary, c.change, c.envelope, c.envelope_hash, c.requested_by, c.requested_at,
  c.requester_mfa_at, c.expires_at, c.status, c.decided_by, c.decided_at, c.decider_mfa_at, c.reason, c.applied_at
FROM fyniq.installation_changes() c;

-- name: OrganisationConnectors :many
SELECT c.id, c.connector, c.manifest_version, c.manifest_hash, c.secret_names, c.consent, c.state, c.configuration, c.position, c.test_passed,
  c.test_reason, c.tested_at, c.installed_by, c.installed_at, c.changed_by, c.changed_at, c.version
FROM fyniq.organisation_connector c
ORDER BY c.position, c.installed_at, c.id;

-- name: OrganisationConnectorSecrets :many
SELECT s.id, s.connector_id, s.name, s.ref, s.set_by, s.set_at, s.valid_until, s.destroyed_at
FROM fyniq.organisation_connector_secret s
ORDER BY s.connector_id, s.name, s.set_at, s.id;

-- name: InsertOrganisationConnector :one
INSERT INTO fyniq.organisation_connector (connector, manifest_version, manifest_hash, secret_names, consent, state, configuration, position,
  installed_by, installed_at, changed_by, changed_at)
VALUES (sqlc.arg(connector), sqlc.arg(manifest_version), sqlc.arg(manifest_hash), sqlc.arg(secret_names)::text[], sqlc.arg(consent)::jsonb, 'installed',
  sqlc.arg(configuration)::jsonb, sqlc.arg(position), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING id;

-- name: UpdateOrganisationConnector :execrows
UPDATE fyniq.organisation_connector SET state = sqlc.arg(state), configuration = sqlc.arg(configuration)::jsonb,
  position = sqlc.arg(position)::integer, test_passed = sqlc.narg(test_passed)::boolean, test_reason = sqlc.narg(test_reason)::text,
  tested_at = sqlc.narg(tested_at)::timestamptz, manifest_version = sqlc.arg(manifest_version), manifest_hash = sqlc.arg(manifest_hash),
  secret_names = sqlc.arg(secret_names)::text[], consent = sqlc.arg(consent)::jsonb, changed_by = sqlc.arg(by), changed_at = sqlc.arg(at),
  version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)::integer;

-- name: ReplaceOrganisationConnectorSecret :exec
UPDATE fyniq.organisation_connector_secret SET valid_until = sqlc.arg(valid_until)
WHERE connector_id = sqlc.arg(connector_id) AND name = sqlc.arg(name) AND valid_until IS NULL AND destroyed_at IS NULL;

-- name: InsertOrganisationConnectorSecret :exec
INSERT INTO fyniq.organisation_connector_secret (connector_id, name, ref, set_by, set_at)
VALUES (sqlc.arg(connector_id), sqlc.arg(name), sqlc.arg(ref), sqlc.arg(by), sqlc.arg(at));

-- name: DestroyedOrganisationConnectorSecret :execrows
UPDATE fyniq.organisation_connector_secret s SET destroyed_at = sqlc.arg(at)
WHERE s.ref = sqlc.arg(ref) AND s.destroyed_at IS NULL
  AND (s.valid_until <= sqlc.arg(at) OR EXISTS (SELECT 1 FROM fyniq.organisation_connector c
                                                 WHERE c.organisation_id = s.organisation_id AND c.id = s.connector_id AND c.state = 'uninstalled'));
