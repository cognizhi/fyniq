-- What Settings › Members and roles shows besides a member's roles (PRD-001 FR-ADM-01, SEC-06; p1-users-roles task 4.1): when they
-- last signed in, and whether they hold a passkey. Both live in global tables (sessions and passkeys), so the API reads them
-- only through this narrow definer function, which names the members of the current organisation alone and returns a time and
-- a count: no token, no key, no address.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.member_activity() RETURNS TABLE (user_id uuid, last_sign_in_at timestamptz, passkeys bigint)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT m.user_id,
         (SELECT max(s.created_at) FROM fyniq.user_session s WHERE s.user_id = m.user_id),
         (SELECT count(*) FROM fyniq.webauthn_credential w WHERE w.user_id = m.user_id AND w.revoked_at IS NULL)
    FROM fyniq.user_membership m WHERE m.organisation_id = fyniq.current_org()
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.member_activity() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.member_activity() TO fyniq_api;
