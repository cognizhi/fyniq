-- name: PlatformHeartbeats :many
-- The platform jobs' latest beats (the super-admin console).
SELECT r.name::text AS name, r.last_beat::timestamptz AS last_beat, coalesce(r.detail, '')::text AS detail FROM fyniq.platform_heartbeats() r;

-- name: PlatformOutbox :one
-- The outbox's backlog across organisations: counts and a time only.
SELECT r.pending::bigint AS pending, r.dead::bigint AS dead, coalesce(r.oldest_pending, 'epoch'::timestamptz)::timestamptz AS oldest_pending FROM fyniq.platform_outbox() r;

-- name: SuperAdminList :many
-- The active Super admins, longest-serving first.
SELECT r.user_id::uuid AS user_id, r.display_name::text AS display_name, r.added_at::timestamptz AS added_at FROM fyniq.super_admin_list() r;
