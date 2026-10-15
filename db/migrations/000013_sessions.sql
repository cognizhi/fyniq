-- The web app's first-party sessions (PRD-001 SEC-05, SEC-08, FR-ADM-08; ADR-T3;
-- p0-identity-oauth-first-run, slice 4a).
--
-- The browser holds an opaque session token in an HttpOnly cookie; this table keeps only its SHA-256.
-- A session belongs to the sign-in identity it began with, (issuer, subject), and records the provider
-- used (FR-ADM-08). The user behind the identity, their organisation and their live roles are looked up
-- on every request, so a role change or a recovery applies at the next request. A session ends when its
-- holder signs out, when it has been idle longer than the API's idle timeout, at its absolute expiry
-- (SEC-05: 30 minutes and 12 hours), or when a recovery replaces its user's sign-ins (SEC-35).
--
-- A global table with no grant, reached only through the functions below (db/README.md).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.user_session (
  token_hash   bytea       NOT NULL,
  issuer       text        NOT NULL,
  subject      text        NOT NULL,
  provider     text        NOT NULL,
  -- The identity's user, once it has one: recorded at the start or at the first request after, so a
  -- recovery can end the user's sessions (end_user_sessions).
  user_id      uuid,
  created_at   timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  expires_at   timestamptz NOT NULL,
  -- When the sign-in passed a second factor (TOTP or a passkey, SEC-01); NULL when it passed none.
  step_up_at   timestamptz,
  ended_at     timestamptz,
  CONSTRAINT user_session_pkey PRIMARY KEY (token_hash),
  CONSTRAINT user_session_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT user_session_token_sha256 CHECK (length(token_hash) = 32),
  CONSTRAINT user_session_issuer_length CHECK (length(issuer) BETWEEN 1 AND 500),
  CONSTRAINT user_session_subject_length CHECK (length(subject) BETWEEN 1 AND 500),
  CONSTRAINT user_session_provider_length CHECK (length(provider) BETWEEN 1 AND 100),
  CONSTRAINT user_session_expiry_after_start CHECK (expires_at > created_at),
  CONSTRAINT user_session_seen_after_start CHECK (last_seen_at >= created_at),
  CONSTRAINT user_session_step_up_before_start CHECK (step_up_at <= created_at),
  CONSTRAINT user_session_ended_after_start CHECK (ended_at >= created_at)
);
CREATE INDEX user_session_user_idx ON fyniq.user_session (user_id) WHERE ended_at IS NULL;

-- ------------------------------------------------------------------ functions
-- Begin a session for a sign-in identity.
-- +goose StatementBegin
CREATE FUNCTION fyniq.start_session(p_hash bytea, p_issuer text, p_subject text, p_provider text,
  p_now timestamptz, p_expires timestamptz, p_step_up_at timestamptz) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  INSERT INTO fyniq.user_session (token_hash, issuer, subject, provider, user_id, created_at, last_seen_at, expires_at, step_up_at)
  VALUES (p_hash, p_issuer, p_subject, p_provider, fyniq.user_for_identity(p_issuer, p_subject), p_now, p_now, p_expires, p_step_up_at)
$$;
-- +goose StatementEnd

-- The live session behind a token's hash: its identity, provider and step-up, the identity's user now,
-- the user's organisation (the first they are an active member of; choosing among several arrives with
-- organisations, P3) and their live roles in it. Every column is NULL when there is no live session:
-- unknown, ended, idle for longer than p_idle, or expired. Memberships and roles are tenant rows, read one
-- organisation at a time under that organisation's row-level security, as require_audit does.
-- +goose StatementBegin
CREATE FUNCTION fyniq.session_for(p_hash bytea, p_now timestamptz, p_idle interval,
  OUT issuer text, OUT subject text, OUT provider text, OUT user_id uuid, OUT organisation_id uuid,
  OUT roles text[], OUT step_up_at timestamptz, OUT expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  s        fyniq.user_session;
  org      uuid;
  previous text := current_setting('app.org_id', true);
BEGIN
  SELECT * INTO s FROM fyniq.user_session u
   WHERE u.token_hash = p_hash AND u.ended_at IS NULL AND u.expires_at > p_now AND u.last_seen_at > p_now - p_idle;
  IF NOT FOUND THEN
    RETURN;
  END IF;
  session_for.issuer := s.issuer;
  session_for.subject := s.subject;
  session_for.provider := s.provider;
  session_for.step_up_at := s.step_up_at;
  session_for.expires_at := s.expires_at;
  session_for.roles := '{}';
  session_for.user_id := fyniq.user_for_identity(s.issuer, s.subject);
  -- Seen: written at most once a minute, so the idle timeout holds to the minute without a write per
  -- request; and the user, once the identity has one (first-run setup or an invitation in this session).
  IF s.last_seen_at < p_now - interval '1 minute' OR (s.user_id IS NULL AND session_for.user_id IS NOT NULL) THEN
    UPDATE fyniq.user_session u SET last_seen_at = greatest(u.last_seen_at, p_now), user_id = coalesce(u.user_id, session_for.user_id)
     WHERE u.token_hash = p_hash;
  END IF;
  IF session_for.user_id IS NULL THEN
    RETURN;
  END IF;
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.created_at, t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    IF EXISTS (SELECT 1 FROM fyniq.user_membership m
                WHERE m.organisation_id = org AND m.user_id = session_for.user_id AND m.status = 'active') THEN
      session_for.organisation_id := org;
      SELECT coalesce(array_agg(DISTINCT r.role ORDER BY r.role), '{}') INTO session_for.roles
        FROM fyniq.role_assignment r
        JOIN fyniq.user_membership m ON m.organisation_id = r.organisation_id AND m.id = r.membership_id
       WHERE r.organisation_id = org AND m.user_id = session_for.user_id AND m.status = 'active'
         AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > p_now);
      EXIT;
    END IF;
  END LOOP;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd

-- Sign out: end one session.
-- +goose StatementBegin
CREATE FUNCTION fyniq.end_session(p_hash bytea, p_now timestamptz) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.user_session SET ended_at = greatest(p_now, created_at) WHERE token_hash = p_hash AND ended_at IS NULL
$$;
-- +goose StatementEnd

-- End every session of a user except those of the identity kept (SEC-35: a recovery revokes all
-- sessions but the one recovering): the sessions recorded as the user's, and those of the identities
-- the recovery removed (p_removed_subjects, gone from user_identity by now), which may have begun before
-- the identity had a user and never been used since. Returns how many it ended.
-- +goose StatementBegin
CREATE FUNCTION fyniq.end_user_sessions(p_user uuid, p_keep_issuer text, p_keep_subject text, p_removed_subjects text[],
  p_now timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  ended integer;
BEGIN
  UPDATE fyniq.user_session s SET ended_at = greatest(p_now, s.created_at)
   WHERE s.ended_at IS NULL AND NOT (s.issuer = p_keep_issuer AND s.subject = p_keep_subject)
     AND (s.user_id = p_user
          OR (s.user_id IS NULL AND s.subject = ANY (p_removed_subjects)
              AND NOT EXISTS (SELECT 1 FROM fyniq.user_identity i WHERE i.issuer = s.issuer AND i.subject = s.subject)));
  GET DIAGNOSTICS ended = ROW_COUNT;
  RETURN ended;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ grants (db/README.md)
REVOKE ALL ON FUNCTION fyniq.start_session(bytea, text, text, text, timestamptz, timestamptz, timestamptz),
  fyniq.session_for(bytea, timestamptz, interval), fyniq.end_session(bytea, timestamptz),
  fyniq.end_user_sessions(uuid, text, text, text[], timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.start_session(bytea, text, text, text, timestamptz, timestamptz, timestamptz),
  fyniq.session_for(bytea, timestamptz, interval), fyniq.end_session(bytea, timestamptz),
  fyniq.end_user_sessions(uuid, text, text, text[], timestamptz) TO fyniq_api;
