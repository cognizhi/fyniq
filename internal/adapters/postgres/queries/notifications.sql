-- name: InsertNotificationEvent :one
INSERT INTO fyniq.notification_event (type, record_type, record_id, title, link, actor, audience, occurred_at)
VALUES (sqlc.arg(type), sqlc.narg(record_type), sqlc.narg(record_id), sqlc.arg(title), sqlc.narg(link), sqlc.arg(actor), sqlc.arg(audience),
  sqlc.arg(occurred_at))
RETURNING id, type, record_type, record_id, title, link, actor, audience, occurred_at, fanned_out_at;

-- name: GetNotificationEvent :one
SELECT id, type, record_type, record_id, title, link, actor, audience, occurred_at, fanned_out_at
FROM fyniq.notification_event WHERE id = sqlc.arg(id);

-- name: NotificationEventEmitted :one
SELECT EXISTS (SELECT 1 FROM fyniq.notification_event WHERE type = sqlc.arg(type) AND record_id = sqlc.arg(record_id));

-- name: FannedOutNotificationEvent :execrows
UPDATE fyniq.notification_event SET fanned_out_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND fanned_out_at IS NULL;

-- name: InsertNotification :one
INSERT INTO fyniq.notification (event_id, user_id, type, severity, actionable, email, created_at)
VALUES (sqlc.arg(event_id), sqlc.arg(user_id), sqlc.arg(type), sqlc.arg(severity), sqlc.arg(actionable), sqlc.arg(email), sqlc.arg(created_at))
ON CONFLICT ON CONSTRAINT notification_once DO NOTHING
RETURNING id;

-- name: EmailedNotification :execrows
UPDATE fyniq.notification SET email_message_id = sqlc.arg(message) WHERE id = sqlc.arg(id) AND email_message_id IS NULL;

-- name: GetNotification :one
SELECT n.id, n.event_id, n.user_id, n.type, n.severity, n.actionable, n.email, n.email_message_id, n.digest_id, n.created_at, n.read_at,
  n.archived_at, e.title, e.link
FROM fyniq.notification n JOIN fyniq.notification_event e ON e.organisation_id = n.organisation_id AND e.id = n.event_id
WHERE n.id = sqlc.arg(id);

-- name: Notifications :many
SELECT n.id, n.event_id, n.user_id, n.type, n.severity, n.actionable, n.email, n.email_message_id, n.digest_id, n.created_at, n.read_at,
  n.archived_at, e.title, e.link
FROM fyniq.notification n JOIN fyniq.notification_event e ON e.organisation_id = n.organisation_id AND e.id = n.event_id
WHERE n.user_id = sqlc.arg(user_id)
  AND (sqlc.arg(archived)::boolean = (n.archived_at IS NOT NULL))
  AND (NOT sqlc.arg(unread)::boolean OR n.read_at IS NULL)
  AND (sqlc.narg(before)::uuid IS NULL OR n.id < sqlc.narg(before)::uuid)
ORDER BY n.id DESC
LIMIT sqlc.arg(page_size);

-- name: UnreadNotifications :one
SELECT count(*) FROM fyniq.notification WHERE user_id = sqlc.arg(user_id) AND read_at IS NULL AND archived_at IS NULL;

-- name: MarkNotification :one
UPDATE fyniq.notification SET read_at = sqlc.narg(read_at)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING id;

-- name: ArchiveNotification :one
UPDATE fyniq.notification SET archived_at = coalesce(archived_at, sqlc.arg(at)::timestamptz)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING id;

-- name: MarkAllNotificationsRead :execrows
UPDATE fyniq.notification SET read_at = sqlc.arg(at) WHERE user_id = sqlc.arg(user_id) AND read_at IS NULL AND archived_at IS NULL;

-- name: HeldNotifications :many
SELECT n.id, n.event_id, n.user_id, n.type, n.severity, n.actionable, n.email, n.email_message_id, n.digest_id, n.created_at, n.read_at,
  n.archived_at, e.title, e.link
FROM fyniq.notification n JOIN fyniq.notification_event e ON e.organisation_id = n.organisation_id AND e.id = n.event_id
WHERE n.email = sqlc.arg(cadence) AND n.email_message_id IS NULL AND n.digest_id IS NULL
ORDER BY n.id
LIMIT 5000;

-- name: InsertNotificationDigest :one
INSERT INTO fyniq.notification_digest (user_id, cadence, email_message_id, sent_at)
VALUES (sqlc.arg(user_id), sqlc.arg(cadence), sqlc.arg(email_message_id), sqlc.arg(sent_at))
RETURNING id;

-- name: DigestNotifications :execrows
UPDATE fyniq.notification SET digest_id = sqlc.arg(digest_id) WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND digest_id IS NULL;

-- name: InsertNotificationPreference :one
INSERT INTO fyniq.notification_preference (user_id, type, email, quiet_from, quiet_to, set_at)
VALUES (sqlc.arg(user_id), sqlc.arg(type), sqlc.narg(email), sqlc.narg(quiet_from), sqlc.narg(quiet_to), sqlc.arg(set_at))
RETURNING id, user_id, type, email, quiet_from, quiet_to, set_at;

-- name: NotificationPreferences :many
-- The latest preference of each type: the one in force.
SELECT DISTINCT ON (type) id, user_id, type, email, quiet_from, quiet_to, set_at
FROM fyniq.notification_preference WHERE user_id = sqlc.arg(user_id)
ORDER BY type, set_at DESC, id DESC;
