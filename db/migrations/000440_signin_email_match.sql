-- A sign-in whose verified email matches an existing member's (PRD-001 FR-ADM-08, G4-061): a new provider's identity
-- with the email of someone who already signs in links nothing and reaches nothing (only a link from that person's own
-- session after step-up does, 000240), and the attempt is recorded in the trail of each organisation the matching
-- person belongs to. email_matches gives the API those members' organisations and IDs, never a name or an email.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
-- email_matches are the active members, by organisation, of whom a sign-in identity other than (p_issuer, p_subject)
-- has the verified email p_email. Each organisation's memberships are read under its own tenant, as session_for reads
-- them (000013); the tenant setting is put back afterwards.
CREATE FUNCTION fyniq.email_matches(p_email text, p_issuer text, p_subject text) RETURNS TABLE (organisation_id uuid, user_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid;
  previous text := current_setting('app.org_id', true);
BEGIN
  IF p_email IS NULL OR p_email = '' THEN
    RETURN;
  END IF;
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.created_at, t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    RETURN QUERY
      SELECT DISTINCT org, i.user_id
        FROM fyniq.user_identity i
        JOIN fyniq.app_user u ON u.user_id = i.user_id AND u.status = 'active'
        JOIN fyniq.user_membership m ON m.organisation_id = org AND m.user_id = i.user_id AND m.status = 'active'
       WHERE i.email = lower(p_email) AND NOT (i.issuer = p_issuer AND i.subject = p_subject);
  END LOOP;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.email_matches(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.email_matches(text, text, text) TO fyniq_api;
