-- The active Super admins, for the worker to tell them of what is the deployment's to fix (PRD-001 FR-NTF-01,
-- FR-ADM-07, rev 0.111): a sending domain that failed its nightly check. The installation organisation has no members,
-- so a notice there reached nobody; a Super admin is told in each organisation they belong to.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.super_admin_users() RETURNS SETOF uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.user_id FROM fyniq.super_admin s JOIN fyniq.app_user u USING (user_id)
   WHERE s.removed_at IS NULL AND u.status = 'active'
   ORDER BY s.added_at, s.user_id
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.super_admin_users() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.super_admin_users() TO fyniq_worker;
