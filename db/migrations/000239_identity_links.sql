-- Linking sign-in identities (PRD-001 FR-ADM-08; rev 0.196; p1-users-roles task 2.4): a person adds a sign-in with
-- another provider to their account, from a session that passed a second factor in the last 5 minutes and after signing
-- in with that provider, and removes one, never the last nor the one they are signed in with; its sessions end with it.
-- A matching email never links: an identity is its (issuer, subject). Each identity records the provider it came from.
-- The API writes each link and removal to the installation's trail, in the same transaction.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.user_identity ADD COLUMN provider text, ADD COLUMN linked_at timestamptz;
ALTER TABLE fyniq.user_identity ADD CONSTRAINT user_identity_provider_format
  CHECK (provider IS NULL OR provider ~ '^[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;

-- A person's sign-in identities, the one they joined or set up with first, then those linked in the order they were: the
-- provider each came from (recorded when linked, else the last session's), when it was linked (NULL for the first) and
-- when it last signed in.
-- +goose StatementBegin
CREATE FUNCTION fyniq.user_identities(p_user uuid)
  RETURNS TABLE (issuer text, subject text, provider text, created_at timestamptz, linked_at timestamptz, last_used_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT i.issuer, i.subject,
         coalesce(i.provider, (SELECT s.provider FROM fyniq.user_session s WHERE s.issuer = i.issuer AND s.subject = i.subject
                                ORDER BY s.created_at DESC LIMIT 1)),
         i.created_at, i.linked_at,
         (SELECT max(s.created_at) FROM fyniq.user_session s WHERE s.issuer = i.issuer AND s.subject = i.subject)
    FROM fyniq.user_identity i
   WHERE i.user_id = p_user
   ORDER BY i.linked_at NULLS FIRST, i.created_at, i.issuer, i.subject
$$;

-- Link (issuer, subject), just signed in with provider, to the user of a live session that passed a second factor in the
-- last 5 minutes (FY270), unless any user has it already (FY271). Returns the user.
CREATE FUNCTION fyniq.link_identity(p_session bytea, p_issuer text, p_subject text, p_provider text, p_now timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who uuid := fyniq.live_session_user(p_session, p_now);
BEGIN
  IF who IS NULL OR NOT EXISTS (SELECT 1 FROM fyniq.user_session s WHERE s.token_hash = p_session AND s.step_up_at > p_now - interval '5 minutes') THEN
    RAISE EXCEPTION 'A sign-in is linked only from a session that confirmed it is you in the last 5 minutes'
      USING ERRCODE = 'FY270', HINT = 'Confirm it''s you, then link the sign-in again.';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.user_identity i WHERE i.issuer = p_issuer AND i.subject = p_subject) THEN
    RAISE EXCEPTION 'That sign-in is linked already'
      USING ERRCODE = 'FY271', HINT = 'A sign-in belongs to one person only: sign in with an account of that provider that is not linked yet.';
  END IF;
  INSERT INTO fyniq.user_identity (issuer, subject, user_id, provider, created_at, linked_at)
  VALUES (p_issuer, p_subject, who, p_provider, p_now, p_now);
  RETURN who;
END
$$;

-- Remove one of the identities of the user of a live session that passed a second factor in the last 5 minutes (FY270):
-- never the one the session signed in with, nor the user's last (FY271); its sessions end. Returns the user and the
-- provider the identity came from; no row when it is not theirs.
CREATE FUNCTION fyniq.unlink_identity(p_session bytea, p_issuer text, p_subject text, p_now timestamptz)
  RETURNS TABLE (unlinked_user uuid, unlinked_provider text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who uuid := fyniq.live_session_user(p_session, p_now);
  s   fyniq.user_session;
BEGIN
  SELECT * INTO s FROM fyniq.user_session x WHERE x.token_hash = p_session;
  IF who IS NULL OR s.step_up_at IS NULL OR s.step_up_at <= p_now - interval '5 minutes' THEN
    RAISE EXCEPTION 'A sign-in is removed only from a session that confirmed it is you in the last 5 minutes'
      USING ERRCODE = 'FY270', HINT = 'Confirm it''s you, then remove the sign-in again.';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.user_identity i WHERE i.issuer = p_issuer AND i.subject = p_subject AND i.user_id = who) THEN
    RETURN;
  END IF;
  IF s.issuer = p_issuer AND s.subject = p_subject
     OR (SELECT count(*) FROM fyniq.user_identity i WHERE i.user_id = who) <= 1 THEN
    RAISE EXCEPTION 'The sign-in you are using, or your last one, cannot be removed'
      USING ERRCODE = 'FY271', HINT = 'Sign in with another of your sign-ins to remove this one, or link another first.';
  END IF;
  -- Its sessions end with it: they would sign nobody in.
  UPDATE fyniq.user_session x SET ended_at = p_now WHERE x.issuer = p_issuer AND x.subject = p_subject AND x.ended_at IS NULL;
  RETURN QUERY
    DELETE FROM fyniq.user_identity i WHERE i.issuer = p_issuer AND i.subject = p_subject AND i.user_id = who
    RETURNING i.user_id, coalesce(i.provider, (SELECT x.provider FROM fyniq.user_session x WHERE x.issuer = i.issuer
                                               AND x.subject = i.subject ORDER BY x.created_at DESC LIMIT 1));
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.user_identities(uuid), fyniq.link_identity(bytea, text, text, text, timestamptz),
  fyniq.unlink_identity(bytea, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.user_identities(uuid), fyniq.link_identity(bytea, text, text, text, timestamptz),
  fyniq.unlink_identity(bytea, text, text, timestamptz) TO fyniq_api;
