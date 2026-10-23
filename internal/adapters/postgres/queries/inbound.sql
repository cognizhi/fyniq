-- name: RecordIdentityEmail :exec
SELECT fyniq.record_identity_email(sqlc.arg(issuer)::text, sqlc.arg(subject)::text, sqlc.arg(email)::text, sqlc.arg(now)::timestamptz);

-- name: MemberEmails :many
SELECT e.user_id::uuid AS user_id, e.email::text AS email FROM fyniq.member_emails() AS e;

-- name: ContactEmails :many
SELECT DISTINCT lower(x.email)::text AS email FROM (
  SELECT billing_email AS email FROM fyniq.contact WHERE is_active AND billing_email IS NOT NULL
  UNION ALL
  SELECT remittance_email FROM fyniq.contact WHERE is_active AND remittance_email IS NOT NULL
) AS x;

-- name: InsertInboundMessage :one
INSERT INTO fyniq.inbound_message (address_id, provider, provider_id, sender, subject, received_at)
VALUES (sqlc.arg(address_id), sqlc.arg(provider), sqlc.arg(provider_id), sqlc.arg(sender), sqlc.narg(subject), sqlc.arg(received_at))
ON CONFLICT ON CONSTRAINT inbound_message_once DO NOTHING
RETURNING id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error;

-- name: GetInboundMessage :one
SELECT id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error FROM fyniq.inbound_message WHERE id = sqlc.arg(id);

-- name: FetchedInboundMessage :one
UPDATE fyniq.inbound_message SET status = sqlc.arg(status), spf = sqlc.narg(spf), dkim = sqlc.narg(dkim), dmarc = sqlc.narg(dmarc),
  fetched_at = sqlc.arg(fetched_at), expires_at = sqlc.narg(expires_at), last_error = sqlc.narg(last_error)
WHERE id = sqlc.arg(id) AND status = 'received'
RETURNING id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error;

-- name: DecideInboundMessage :one
UPDATE fyniq.inbound_message SET status = sqlc.arg(status), decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at)
WHERE id = sqlc.arg(id) AND status = 'quarantined'
RETURNING id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error;

-- name: ExpireInboundMessage :one
UPDATE fyniq.inbound_message SET status = 'expired'
WHERE id = sqlc.arg(id) AND status = 'quarantined'
RETURNING id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error;

-- name: InboundMessages :many
SELECT id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error FROM fyniq.inbound_message
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.narg(address_id)::uuid IS NULL OR address_id = sqlc.narg(address_id)::uuid)
  AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(page_size);

-- name: QuarantinedInboundFrom :many
SELECT id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error FROM fyniq.inbound_message
WHERE status = 'quarantined' AND sender = sqlc.arg(sender) AND address_id = sqlc.arg(address_id) ORDER BY id;

-- name: InboundDueToExpire :many
SELECT id, address_id, provider, provider_id, sender, subject, received_at, status, spf, dkim, dmarc, fetched_at, expires_at, decided_by,
  decided_at, last_error FROM fyniq.inbound_message
WHERE status = 'quarantined' AND expires_at <= sqlc.arg(now) ORDER BY id LIMIT 500;

-- name: InsertInboundFile :one
INSERT INTO fyniq.inbound_file (message_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, blob_key, stored_at)
VALUES (sqlc.arg(message_id), sqlc.arg(kind), sqlc.arg(file_name), sqlc.arg(media_type), sqlc.arg(size_bytes), sqlc.arg(sha256),
  sqlc.arg(original_sha256), sqlc.arg(blob_key), sqlc.arg(stored_at))
RETURNING id, message_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, blob_key, stored_at, removed_at;

-- name: InboundFiles :many
SELECT id, message_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, blob_key, stored_at, removed_at
FROM fyniq.inbound_file WHERE message_id = sqlc.arg(message_id) ORDER BY kind DESC, stored_at, id;

-- name: RemoveInboundFiles :many
UPDATE fyniq.inbound_file SET removed_at = sqlc.arg(removed_at)
WHERE message_id = sqlc.arg(message_id) AND removed_at IS NULL
RETURNING id, message_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, blob_key, stored_at, removed_at;
