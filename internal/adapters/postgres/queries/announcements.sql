-- name: InsertAnnouncement :one
INSERT INTO fyniq.announcement (message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at)
VALUES (sqlc.arg(message), sqlc.narg(link), sqlc.arg(severity), sqlc.arg(roles)::text[], sqlc.arg(starts_at), sqlc.arg(ends_at),
  sqlc.arg(dismissible), sqlc.arg(published_by), sqlc.arg(published_at))
RETURNING id, message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at, withdrawn_at, withdrawn_by;

-- name: GetAnnouncement :one
SELECT id, message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at, withdrawn_at, withdrawn_by
FROM fyniq.announcement WHERE id = sqlc.arg(id);

-- name: ListAnnouncements :many
SELECT id, message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at, withdrawn_at, withdrawn_by
FROM fyniq.announcement ORDER BY published_at DESC, id DESC LIMIT 500;

-- name: WithdrawAnnouncement :one
UPDATE fyniq.announcement SET withdrawn_at = sqlc.arg(at), withdrawn_by = sqlc.arg(by)
WHERE id = sqlc.arg(id) AND withdrawn_at IS NULL
RETURNING id, message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at, withdrawn_at, withdrawn_by;

-- name: PublishInstallationAnnouncement :one
SELECT fyniq.publish_installation_announcement(sqlc.arg(by), sqlc.arg(message), sqlc.narg(link), sqlc.arg(severity), sqlc.arg(roles)::text[],
  sqlc.arg(starts_at), sqlc.arg(ends_at), sqlc.arg(dismissible), sqlc.arg(at))::uuid AS id;

-- name: WithdrawInstallationAnnouncement :exec
SELECT fyniq.withdraw_installation_announcement(sqlc.arg(by), sqlc.arg(id), sqlc.arg(at));

-- name: InstallationAnnouncements :many
SELECT r.id::uuid AS id, r.message::text AS message, coalesce(r.link, '')::text AS link, r.severity::text AS severity,
  r.roles::text[] AS roles, r.starts_at::timestamptz AS starts_at, r.ends_at::timestamptz AS ends_at, r.dismissible::boolean AS dismissible,
  r.published_by::uuid AS published_by, r.published_at::timestamptz AS published_at, (r.withdrawn_at IS NOT NULL)::boolean AS withdrawn,
  coalesce(r.withdrawn_at, r.published_at)::timestamptz AS withdrawn_at, coalesce(r.withdrawn_by::text, '')::text AS withdrawn_by
FROM fyniq.installation_announcements() r;

-- name: InsertAnnouncementDismissal :exec
INSERT INTO fyniq.announcement_dismissal (announcement_id, installation, user_id, dismissed_at)
VALUES (sqlc.arg(announcement_id), sqlc.arg(installation), sqlc.arg(user_id), sqlc.arg(dismissed_at))
ON CONFLICT ON CONSTRAINT announcement_dismissal_once DO NOTHING;

-- name: AnnouncementDismissals :many
SELECT announcement_id FROM fyniq.announcement_dismissal WHERE user_id = sqlc.arg(user_id);
