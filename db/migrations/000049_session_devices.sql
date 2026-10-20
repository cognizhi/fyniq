-- A person's devices: their live sessions, which they can end, and a sign-in alert by email (PRD-001 FR-ADM-03,
-- SEC-05, rev 0.45; p1-users-roles task 2.3).
--
-- A session records the browser that began it (its User-Agent, at most 300 characters), so a person can tell their
-- devices apart. A person lists their own live sessions and ends any of them, through the functions below; nobody else
-- does. A sign-in alert is an email of its own purpose. The checks are added NOT VALID and validated by 000050 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.user_session ADD COLUMN user_agent text;
ALTER TABLE fyniq.user_session ADD CONSTRAINT user_session_user_agent_length CHECK (length(user_agent) <= 300) NOT VALID;

-- Begin a session for a sign-in identity from a browser; a step-up at sign-in is recorded as such.
-- +goose StatementBegin
CREATE FUNCTION fyniq.start_session_from(p_hash bytea, p_issuer text, p_subject text, p_provider text,
  p_now timestamptz, p_expires timestamptz, p_step_up_at timestamptz, p_user_agent text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  INSERT INTO fyniq.user_session (token_hash, issuer, subject, provider, user_id, created_at, last_seen_at, expires_at, step_up_at,
                                  step_up_method, user_agent)
  VALUES (p_hash, p_issuer, p_subject, p_provider, fyniq.user_for_identity(p_issuer, p_subject), p_now, p_now, p_expires, p_step_up_at,
          CASE WHEN p_step_up_at IS NOT NULL THEN 'sign_in' END, nullif(left(p_user_agent, 300), ''))
$$;
-- +goose StatementEnd

-- A user's live sessions: not ended, not expired, not idle for longer than p_idle; newest first.
-- +goose StatementBegin
CREATE FUNCTION fyniq.user_sessions(p_user uuid, p_now timestamptz, p_idle interval)
RETURNS SETOF fyniq.user_session
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.* FROM fyniq.user_session s
   WHERE fyniq.user_for_identity(s.issuer, s.subject) = p_user AND (s.user_id IS NULL OR s.user_id = p_user)
     AND s.ended_at IS NULL AND s.expires_at > p_now
     AND s.last_seen_at > p_now - p_idle
   ORDER BY s.created_at DESC, s.token_hash
$$;
-- +goose StatementEnd

-- End one of a user's sessions; returns whether there was one to end.
-- +goose StatementBegin
CREATE FUNCTION fyniq.end_user_session(p_user uuid, p_hash bytea, p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.user_session s SET ended_at = greatest(p_now, s.created_at)
   WHERE s.token_hash = p_hash AND s.ended_at IS NULL AND fyniq.user_for_identity(s.issuer, s.subject) = p_user
     AND (s.user_id IS NULL OR s.user_id = p_user);
  RETURN FOUND;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.start_session_from(bytea, text, text, text, timestamptz, timestamptz, timestamptz, text),
  fyniq.user_sessions(uuid, timestamptz, interval), fyniq.end_user_session(uuid, bytea, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.start_session_from(bytea, text, text, text, timestamptz, timestamptz, timestamptz, text),
  fyniq.user_sessions(uuid, timestamptz, interval), fyniq.end_user_session(uuid, bytea, timestamptz) TO fyniq_api;

-- ------------------------------------------------------------------ sign-in alerts are email of their own purpose

ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known CHECK (purpose IN ('test', 'sign_in_alert')) NOT VALID;
