-- Choosing the organisation a session is in (PRD-001 FR-UX-36 and the organisation switcher of p1-app-shell 2.1): a person who is a member
-- of more than one organisation, such as the Owner of a practice organisation, works in the one they chose. A session finds its
-- organisation as before, the oldest where the person is an active member, unless it holds a choice that is still theirs: the choice
-- only ever names an organisation they are an active member of, and is checked again at every request by the same membership test.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.user_session ADD COLUMN chosen_organisation_id uuid;

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
    FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t
                ORDER BY (t.organisation_id = s.chosen_organisation_id) DESC NULLS LAST, t.created_at, t.organisation_id LOOP
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

-- The organisations a live session's person is an active member of, with their names, and whether each is a practice one: a person's
-- own memberships, nothing else of any tenant.
-- +goose StatementBegin
CREATE FUNCTION fyniq.organisations_of_session(p_hash bytea, p_now timestamptz, OUT organisation_id uuid, OUT name text, OUT practice boolean, OUT chosen boolean)
RETURNS SETOF record
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  s        fyniq.user_session;
  who      uuid;
  org      uuid;
  previous text := current_setting('app.org_id', true);
  inst     uuid := fyniq.installation_org();
BEGIN
  SELECT * INTO s FROM fyniq.user_session u WHERE u.token_hash = p_hash AND u.ended_at IS NULL AND u.expires_at > p_now;
  IF NOT FOUND THEN
    RETURN;
  END IF;
  who := fyniq.user_for_identity(s.issuer, s.subject);
  IF who IS NULL THEN
    RETURN;
  END IF;
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t WHERE t.organisation_id IS DISTINCT FROM inst ORDER BY t.created_at, t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    IF EXISTS (SELECT 1 FROM fyniq.user_membership m WHERE m.organisation_id = org AND m.user_id = who AND m.status = 'active') THEN
      organisation_id := org;
      SELECT o.name INTO name FROM fyniq.organisation o WHERE o.organisation_id = org;
      practice := fyniq.organisation_is_practice(org);
      chosen := coalesce(org = s.chosen_organisation_id, false);
      RETURN NEXT;
    END IF;
  END LOOP;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd

-- Chooses one of them for the session; false when the person is no active member there, so a choice is never of another's organisation.
-- +goose StatementBegin
CREATE FUNCTION fyniq.switch_session_organisation(p_hash bytea, p_org uuid, p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  s        fyniq.user_session;
  who      uuid;
  member   boolean;
  previous text := current_setting('app.org_id', true);
BEGIN
  SELECT * INTO s FROM fyniq.user_session u WHERE u.token_hash = p_hash AND u.ended_at IS NULL AND u.expires_at > p_now FOR UPDATE;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  who := fyniq.user_for_identity(s.issuer, s.subject);
  IF who IS NULL OR p_org IS NULL OR p_org IS NOT DISTINCT FROM fyniq.installation_org() THEN
    RETURN false;
  END IF;
  PERFORM set_config('app.org_id', p_org::text, true);
  SELECT EXISTS (SELECT 1 FROM fyniq.user_membership m WHERE m.organisation_id = p_org AND m.user_id = who AND m.status = 'active') INTO member;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  IF NOT member THEN
    RETURN false;
  END IF;
  UPDATE fyniq.user_session u SET chosen_organisation_id = p_org WHERE u.token_hash = p_hash;
  RETURN true;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.organisations_of_session(bytea, timestamptz), fyniq.switch_session_organisation(bytea, uuid, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.organisations_of_session(bytea, timestamptz), fyniq.switch_session_organisation(bytea, uuid, timestamptz) TO fyniq_api;
