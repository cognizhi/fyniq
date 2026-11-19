-- The super-admin console's two reads (PRD-001 FR-ADM-07; docs/design/screens SuperAdminConsole; p1-users-roles task 4.1): the
-- installation's health across every organisation, and its Super admins. Both are narrow definer functions the API's role may
-- call, since the tables they read are global or belong to every organisation; the API refuses a caller who is no Super admin
-- before it calls them. They return counts, times and names only: no organisation's record, and no secret.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The platform's own jobs' latest beats: the scheduler tick, audit anchoring and verification, the restore drill (§12.11).
-- +goose StatementBegin
CREATE FUNCTION fyniq.platform_heartbeats() RETURNS TABLE (name text, last_beat timestamptz, detail text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT h.name, h.last_beat, left(h.detail, 200) FROM fyniq.heartbeat h ORDER BY h.name
$$;
-- +goose StatementEnd

-- The outbox's backlog across organisations: what waits, what is dead, and since when the oldest waits.
-- +goose StatementBegin
CREATE FUNCTION fyniq.platform_outbox() RETURNS TABLE (pending bigint, dead bigint, oldest_pending timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT count(*) FILTER (WHERE o.state IN ('pending', 'dispatched', 'running')),
         count(*) FILTER (WHERE o.state = 'dead'),
         min(o.created_at) FILTER (WHERE o.state = 'pending')
  FROM fyniq.outbox o
$$;
-- +goose StatementEnd

-- The active Super admins, longest-serving first.
-- +goose StatementBegin
CREATE FUNCTION fyniq.super_admin_list() RETURNS TABLE (user_id uuid, display_name text, added_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.user_id, u.display_name, s.added_at FROM fyniq.super_admin s JOIN fyniq.app_user u USING (user_id)
   WHERE s.removed_at IS NULL AND u.status = 'active'
   ORDER BY s.added_at, s.user_id
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.platform_heartbeats(), fyniq.platform_outbox(), fyniq.super_admin_list() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.platform_heartbeats(), fyniq.platform_outbox(), fyniq.super_admin_list() TO fyniq_api;
