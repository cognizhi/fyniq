-- The idle timeout is the organisation's setting (PRD-001 SEC-05; rev 0.180, the owner's decision of 6 Nov 2026):
-- security.idle_timeout_minutes, 5 to 60 minutes, under dual control, 30 when the organisation has not changed it. A
-- session ends after that long without a request; it ends 12 hours after sign-in whatever happens.
--
-- idle_timeout reads the value in force on the organisation's own date, as the settings do (settings.On: the latest
-- change taking effect on or before the day, the later version of a day winning), and falls back to the default for a
-- value outside 5 to 60 minutes. session_for finds the session's organisation first, then refuses a session idle for
-- longer than that organisation's timeout; its signature is unchanged.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.idle_timeout(p_org uuid, p_now timestamptz, p_default interval) RETURNS interval
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous text := current_setting('app.org_id', true);
  zone     text;
  minutes  text;
BEGIN
  IF p_org IS NULL THEN
    RETURN p_default;
  END IF;
  PERFORM set_config('app.org_id', p_org::text, true);
  SELECT o.time_zone INTO zone FROM fyniq.organisation o WHERE o.organisation_id = p_org;
  SELECT s.value INTO minutes FROM fyniq.organisation_setting s
   WHERE s.organisation_id = p_org AND s.key = 'security.idle_timeout_minutes'
     AND s.effective_from <= (p_now AT TIME ZONE coalesce(nullif(zone, ''), 'UTC'))::date
   ORDER BY s.effective_from DESC, s.version DESC LIMIT 1;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  IF minutes IS NULL OR minutes !~ '^[0-9]{1,2}$' OR minutes::integer NOT BETWEEN 5 AND 60 THEN
    RETURN p_default;
  END IF;
  RETURN minutes::integer * interval '1 minute';
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.session_for(p_hash bytea, p_now timestamptz, p_idle interval,
  OUT issuer text, OUT subject text, OUT provider text, OUT user_id uuid, OUT organisation_id uuid,
  OUT roles text[], OUT step_up_at timestamptz, OUT expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  s        fyniq.user_session;
  org      uuid;
  found_in uuid;
  who      uuid;
  held     text[] := '{}';
  previous text := current_setting('app.org_id', true);
BEGIN
  -- A live session at most 60 minutes idle: the organisation's timeout decides below.
  SELECT * INTO s FROM fyniq.user_session u
   WHERE u.token_hash = p_hash AND u.ended_at IS NULL AND u.expires_at > p_now
     AND u.last_seen_at > p_now - greatest(p_idle, interval '60 minutes');
  IF NOT FOUND THEN
    RETURN;
  END IF;
  who := fyniq.user_for_identity(s.issuer, s.subject);
  IF who IS NOT NULL THEN
    FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.created_at, t.organisation_id LOOP
      PERFORM set_config('app.org_id', org::text, true);
      IF EXISTS (SELECT 1 FROM fyniq.user_membership m
                  WHERE m.organisation_id = org AND m.user_id = who AND m.status = 'active') THEN
        found_in := org;
        SELECT coalesce(array_agg(DISTINCT r.role ORDER BY r.role), '{}') INTO held
          FROM fyniq.role_assignment r
          JOIN fyniq.user_membership m ON m.organisation_id = r.organisation_id AND m.id = r.membership_id
         WHERE r.organisation_id = org AND m.user_id = who AND m.status = 'active'
           AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > p_now);
        EXIT;
      END IF;
    END LOOP;
    PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  END IF;
  -- Idle for longer than the organisation's timeout (SEC-05): no session.
  IF s.last_seen_at <= p_now - fyniq.idle_timeout(found_in, p_now, p_idle) THEN
    RETURN;
  END IF;
  session_for.issuer := s.issuer;
  session_for.subject := s.subject;
  session_for.provider := s.provider;
  session_for.step_up_at := s.step_up_at;
  session_for.expires_at := s.expires_at;
  session_for.user_id := who;
  session_for.organisation_id := found_in;
  session_for.roles := held;
  -- Seen: written at most once a minute, so the idle timeout holds to the minute without a write per
  -- request; and the user, once the identity has one (first-run setup or an invitation in this session).
  IF s.last_seen_at < p_now - interval '1 minute' OR (s.user_id IS NULL AND who IS NOT NULL) THEN
    UPDATE fyniq.user_session u SET last_seen_at = greatest(u.last_seen_at, p_now), user_id = coalesce(u.user_id, who)
     WHERE u.token_hash = p_hash;
  END IF;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.idle_timeout(uuid, timestamptz, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.idle_timeout(uuid, timestamptz, interval) TO fyniq_api;
