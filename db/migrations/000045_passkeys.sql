-- Passkeys (WebAuthn) as a person's second factor and for step-up (PRD-001 FR-ADM-02, SEC-01, SEC-05, FR-APR-04,
-- rev 0.43; p1-users-roles task 2.2).
--
-- TOTP stays with the sign-in provider (Identity Platform; locally, the sign-in page's second factor). A passkey is
-- a person's own credential, outside any organisation: registered by a person whose session passed a second factor in
-- the last 5 minutes, and used to step a session up, which then dates its step-up from the passkey's assertion.
--
-- Every ceremony answers a challenge: 32 random bytes, bound to the session that asked for it and to its purpose,
-- taken once before it expires (at most 5 minutes) and answered once. A credential exists only as the answer to a
-- registration challenge of a session that passed a second factor in the last 5 minutes; a session is stepped up by a
-- passkey only as the answer to a step-up challenge of that session, by a live passkey of its user. A credential's
-- signature counter never goes back (a cloned authenticator, WebAuthn §6.1.1), its key never changes, and it is
-- revoked once (FY140); a challenge never changes but to be taken, then answered (FY141). Adding and revoking a
-- passkey is audited in the installation's trail.
--
-- A session's step-up now records how it was passed: at sign-in (step_up_at no later than the session's start, as
-- before) or by a passkey (no earlier than its start). The check that encoded "step-up only at sign-in" is replaced by
-- that pair NOT VALID, and 000046 validates it (squawk: constraint-missing-not-valid).
--
-- Global tables with no grant, reached only through the functions below (db/README.md).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.webauthn_credential (
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  credential_id   bytea       NOT NULL,
  public_key      bytea       NOT NULL,  -- the COSE key the authenticator gave at registration
  sign_count      bigint      NOT NULL,
  aaguid          bytea       NOT NULL,
  transports      text[]      NOT NULL,
  backup_eligible boolean     NOT NULL,
  backup_state    boolean     NOT NULL,
  name            text        NOT NULL,
  created_at      timestamptz NOT NULL,
  last_used_at    timestamptz,
  revoked_at      timestamptz,
  CONSTRAINT webauthn_credential_pkey PRIMARY KEY (id),
  CONSTRAINT webauthn_credential_once UNIQUE (credential_id),
  CONSTRAINT webauthn_credential_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT webauthn_credential_id_length CHECK (length(credential_id) BETWEEN 16 AND 1023),
  CONSTRAINT webauthn_credential_key_length CHECK (length(public_key) BETWEEN 16 AND 4096),
  CONSTRAINT webauthn_credential_count_range CHECK (sign_count BETWEEN 0 AND 4294967295),
  CONSTRAINT webauthn_credential_aaguid_length CHECK (length(aaguid) = 16),
  CONSTRAINT webauthn_credential_transports_few CHECK (cardinality(transports) <= 8),
  CONSTRAINT webauthn_credential_backup_flags CHECK (backup_eligible OR NOT backup_state),
  CONSTRAINT webauthn_credential_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT webauthn_credential_used_after_created CHECK (last_used_at >= created_at),
  CONSTRAINT webauthn_credential_revoked_after_created CHECK (revoked_at >= created_at)
);
CREATE INDEX webauthn_credential_user_idx ON fyniq.webauthn_credential (user_id) WHERE revoked_at IS NULL;

-- A credential keeps its user, ID, key, AAGUID, transports, backup eligibility, name and creation; its counter only
-- goes up (or stays zero for an authenticator that keeps none); it is revoked once, and a revoked one is never used.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_credential_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A passkey is revoked, never deleted' USING ERRCODE = 'FY140';
  END IF;
  IF (NEW.id, NEW.user_id, NEW.credential_id, NEW.public_key, NEW.aaguid, NEW.transports, NEW.backup_eligible, NEW.name, NEW.created_at)
     IS DISTINCT FROM (OLD.id, OLD.user_id, OLD.credential_id, OLD.public_key, OLD.aaguid, OLD.transports, OLD.backup_eligible, OLD.name,
                       OLD.created_at)
     OR (OLD.revoked_at IS NOT NULL AND (NEW.revoked_at, NEW.sign_count, NEW.last_used_at, NEW.backup_state)
                                         IS DISTINCT FROM (OLD.revoked_at, OLD.sign_count, OLD.last_used_at, OLD.backup_state)) THEN
    RAISE EXCEPTION 'A passkey never changes but its counter and last use, and is revoked once' USING ERRCODE = 'FY140',
      HINT = 'Register another passkey instead.';
  END IF;
  IF NEW.sign_count IS DISTINCT FROM OLD.sign_count AND (NEW.sign_count <= OLD.sign_count) THEN
    RAISE EXCEPTION 'A passkey''s signature counter went from % to %: the authenticator may be cloned', OLD.sign_count, NEW.sign_count
      USING ERRCODE = 'FY140', HINT = 'Revoke the passkey and register a new one (WebAuthn §6.1.1).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER webauthn_credential_guard BEFORE UPDATE OR DELETE ON fyniq.webauthn_credential
  FOR EACH ROW EXECUTE FUNCTION fyniq.webauthn_credential_guard();

CREATE TABLE fyniq.webauthn_challenge (
  id           uuid        NOT NULL DEFAULT uuidv7(),
  user_id      uuid        NOT NULL,
  purpose      text        NOT NULL,
  challenge    bytea       NOT NULL,
  session_hash bytea       NOT NULL,
  created_at   timestamptz NOT NULL,
  expires_at   timestamptz NOT NULL,
  taken_at     timestamptz,
  answered_by  uuid,                   -- the credential registered by it, or that answered it
  CONSTRAINT webauthn_challenge_pkey PRIMARY KEY (id),
  CONSTRAINT webauthn_challenge_once UNIQUE (challenge),
  CONSTRAINT webauthn_challenge_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT webauthn_challenge_session_fkey FOREIGN KEY (session_hash) REFERENCES fyniq.user_session (token_hash),
  CONSTRAINT webauthn_challenge_answer_fkey FOREIGN KEY (answered_by) REFERENCES fyniq.webauthn_credential (id),
  CONSTRAINT webauthn_challenge_purpose_known CHECK (purpose IN ('register', 'step_up')),
  CONSTRAINT webauthn_challenge_random CHECK (length(challenge) = 32),
  CONSTRAINT webauthn_challenge_lifetime CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes'),
  CONSTRAINT webauthn_challenge_taken_in_time CHECK (taken_at >= created_at AND taken_at < expires_at),
  CONSTRAINT webauthn_challenge_answered_once_taken CHECK (answered_by IS NULL OR taken_at IS NOT NULL)
);
CREATE INDEX webauthn_challenge_session_idx ON fyniq.webauthn_challenge (session_hash);

-- A challenge is taken once and answered once; nothing else about it changes, and it is never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_challenge_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.id, NEW.user_id, NEW.purpose, NEW.challenge, NEW.session_hash, NEW.created_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.id, OLD.user_id, OLD.purpose, OLD.challenge, OLD.session_hash, OLD.created_at, OLD.expires_at)
     OR (OLD.taken_at IS NOT NULL AND NEW.taken_at IS DISTINCT FROM OLD.taken_at)
     OR (OLD.answered_by IS NOT NULL AND NEW.answered_by IS DISTINCT FROM OLD.answered_by) THEN
    RAISE EXCEPTION 'A passkey challenge is taken once and answered once, and never changes otherwise' USING ERRCODE = 'FY141',
      HINT = 'Ask for a new challenge.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER webauthn_challenge_guard BEFORE UPDATE OR DELETE ON fyniq.webauthn_challenge
  FOR EACH ROW EXECUTE FUNCTION fyniq.webauthn_challenge_guard();

-- Every passkey registered or revoked has its event in the installation's trail, in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION fyniq.require_passkey_audit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org      uuid := (SELECT installation_org FROM fyniq.installation WHERE singleton);
  previous text := current_setting('app.org_id', true);
  found    boolean;
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'A passkey changed before the installation''s trail exists' USING ERRCODE = 'FY140';
  END IF;
  PERFORM set_config('app.org_id', org::text, true);
  SELECT EXISTS (
    SELECT 1 FROM fyniq.audit_event e
     WHERE e.organisation_id = org AND e.created_xact = pg_current_xact_id()::text::bigint
       AND e.object_type = 'webauthn_credential' AND e.object_id = NEW.id) INTO found;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  IF NOT found THEN
    RAISE EXCEPTION 'A passkey % (%) has no event in the installation''s trail', NEW.id, TG_OP
      USING ERRCODE = 'FY140', HINT = 'Register and revoke passkeys through the passkey commands (AUD-01).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF revoked_at ON fyniq.webauthn_credential
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_passkey_audit();

-- ------------------------------------------------------------------ the session's step-up

ALTER TABLE fyniq.user_session ADD COLUMN step_up_method text;
-- The sessions before this migration passed their second factor at sign-in, if at all.
UPDATE fyniq.user_session SET step_up_method = 'sign_in' WHERE step_up_at IS NOT NULL AND step_up_method IS NULL;
ALTER TABLE fyniq.user_session DROP CONSTRAINT user_session_step_up_before_start;
ALTER TABLE fyniq.user_session ADD CONSTRAINT user_session_step_up_method CHECK (
  (step_up_at IS NULL AND step_up_method IS NULL)
  OR (step_up_method = 'sign_in' AND step_up_at <= created_at)
  OR (step_up_method = 'passkey' AND step_up_at >= created_at)) NOT VALID;

-- Begin a session for a sign-in identity; a step-up at sign-in is recorded as such.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.start_session(p_hash bytea, p_issuer text, p_subject text, p_provider text,
  p_now timestamptz, p_expires timestamptz, p_step_up_at timestamptz) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  INSERT INTO fyniq.user_session (token_hash, issuer, subject, provider, user_id, created_at, last_seen_at, expires_at, step_up_at,
                                  step_up_method)
  VALUES (p_hash, p_issuer, p_subject, p_provider, fyniq.user_for_identity(p_issuer, p_subject), p_now, p_now, p_expires, p_step_up_at,
          CASE WHEN p_step_up_at IS NOT NULL THEN 'sign_in' END)
$$;
-- +goose StatementEnd

-- How a session passed its last second factor: sign_in, passkey, or NULL.
-- +goose StatementBegin
CREATE FUNCTION fyniq.session_step_up_method(p_hash bytea) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.step_up_method FROM fyniq.user_session s WHERE s.token_hash = p_hash
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ functions

-- live_session_user is the user of a live session (not ended, not expired), or NULL.
-- +goose StatementBegin
CREATE FUNCTION fyniq.live_session_user(p_session bytea, p_now timestamptz) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT fyniq.user_for_identity(s.issuer, s.subject) FROM fyniq.user_session s
   WHERE s.token_hash = p_session AND s.ended_at IS NULL AND s.expires_at > p_now
$$;
-- +goose StatementEnd

-- Issue a challenge to a live session of a user, for a purpose; a registration challenge only to a session that passed
-- a second factor in the last 5 minutes. Returns its ID.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_challenge_issue(p_session bytea, p_purpose text, p_challenge bytea, p_now timestamptz, p_expires timestamptz)
RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who uuid := fyniq.live_session_user(p_session, p_now);
  id  uuid;
BEGIN
  IF who IS NULL THEN
    RAISE EXCEPTION 'No live session of a Fyniq user asked for the challenge' USING ERRCODE = 'FY141',
      HINT = 'Sign in again.';
  END IF;
  IF p_purpose = 'register' AND NOT EXISTS (
       SELECT 1 FROM fyniq.user_session s WHERE s.token_hash = p_session AND s.step_up_at > p_now - interval '5 minutes') THEN
    RAISE EXCEPTION 'A passkey is registered only by a session that passed a second factor in the last 5 minutes'
      USING ERRCODE = 'FY141', HINT = 'Confirm it''s you first (FR-ADM-02).';
  END IF;
  INSERT INTO fyniq.webauthn_challenge (user_id, purpose, challenge, session_hash, created_at, expires_at)
  VALUES (who, p_purpose, p_challenge, p_session, p_now, p_expires)
  RETURNING webauthn_challenge.id INTO id;
  RETURN id;
END
$$;
-- +goose StatementEnd

-- Take a challenge: once, by the session it was issued to, for its purpose, before it expires. Returns its bytes and
-- its user, or no row.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_challenge_take(p_id uuid, p_session bytea, p_purpose text, p_now timestamptz,
  OUT challenge bytea, OUT user_id uuid)
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.webauthn_challenge c SET taken_at = p_now
   WHERE c.id = p_id AND c.session_hash = p_session AND c.purpose = p_purpose AND c.taken_at IS NULL
     AND c.expires_at > p_now AND c.created_at <= p_now
     AND c.user_id = fyniq.live_session_user(p_session, p_now)
  RETURNING c.challenge, c.user_id
$$;
-- +goose StatementEnd

-- A user's passkeys, live ones only unless p_all.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_credentials(p_user uuid, p_all boolean)
RETURNS SETOF fyniq.webauthn_credential
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT * FROM fyniq.webauthn_credential c WHERE c.user_id = p_user AND (p_all OR c.revoked_at IS NULL) ORDER BY c.created_at, c.id
$$;
-- +goose StatementEnd

-- Register a passkey as the answer to a registration challenge the session took in the last 5 minutes, while its
-- step-up is no older than 5 minutes either. Returns its ID; the caller audits it in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_credential_register(p_challenge uuid, p_session bytea, p_credential_id bytea, p_public_key bytea,
  p_sign_count bigint, p_aaguid bytea, p_transports text[], p_backup_eligible boolean, p_backup_state boolean, p_name text,
  p_now timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c  fyniq.webauthn_challenge;
  id uuid;
BEGIN
  SELECT * INTO c FROM fyniq.webauthn_challenge w WHERE w.id = p_challenge FOR UPDATE;
  IF NOT FOUND OR c.purpose <> 'register' OR c.session_hash <> p_session OR c.taken_at IS NULL OR c.answered_by IS NOT NULL
     OR c.taken_at < p_now - interval '5 minutes' OR c.user_id IS DISTINCT FROM fyniq.live_session_user(p_session, p_now)
     OR NOT EXISTS (SELECT 1 FROM fyniq.user_session s WHERE s.token_hash = p_session AND s.step_up_at > p_now - interval '5 minutes') THEN
    RAISE EXCEPTION 'A passkey exists only as the answer to a registration challenge of a session that passed a second factor in the last 5 minutes'
      USING ERRCODE = 'FY141', HINT = 'Confirm it''s you, then register the passkey again.';
  END IF;
  INSERT INTO fyniq.webauthn_credential (user_id, credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state,
                                         name, created_at)
  VALUES (c.user_id, p_credential_id, p_public_key, p_sign_count, p_aaguid, p_transports, p_backup_eligible, p_backup_state,
          btrim(p_name), p_now)
  RETURNING webauthn_credential.id INTO id;
  UPDATE fyniq.webauthn_challenge w SET answered_by = id WHERE w.id = p_challenge;
  RETURN id;
END
$$;
-- +goose StatementEnd

-- Step a session up by a passkey: the answer to a step-up challenge the session took in the last 5 minutes, by a live
-- passkey of its user, whose counter moves to p_sign_count (the guard refuses a counter that goes back).
-- +goose StatementBegin
CREATE FUNCTION fyniq.session_step_up(p_challenge uuid, p_session bytea, p_credential uuid, p_sign_count bigint, p_backup_state boolean,
  p_now timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.webauthn_challenge;
  k fyniq.webauthn_credential;
BEGIN
  SELECT * INTO c FROM fyniq.webauthn_challenge w WHERE w.id = p_challenge FOR UPDATE;
  SELECT * INTO k FROM fyniq.webauthn_credential w WHERE w.id = p_credential FOR UPDATE;
  IF c.id IS NULL OR c.purpose <> 'step_up' OR c.session_hash <> p_session OR c.taken_at IS NULL OR c.answered_by IS NOT NULL
     OR c.taken_at < p_now - interval '5 minutes' OR c.user_id IS DISTINCT FROM fyniq.live_session_user(p_session, p_now)
     OR k.id IS NULL OR k.user_id <> c.user_id OR k.revoked_at IS NOT NULL THEN
    RAISE EXCEPTION 'A session is stepped up by a passkey only as the answer to its own step-up challenge, by a live passkey of its user'
      USING ERRCODE = 'FY141', HINT = 'Ask for a new challenge and confirm it''s you again.';
  END IF;
  IF p_sign_count <> 0 OR k.sign_count <> 0 THEN
    UPDATE fyniq.webauthn_credential w SET sign_count = p_sign_count WHERE w.id = p_credential;
  END IF;
  UPDATE fyniq.webauthn_credential w SET last_used_at = greatest(p_now, w.created_at), backup_state = p_backup_state AND w.backup_eligible
   WHERE w.id = p_credential;
  UPDATE fyniq.webauthn_challenge w SET answered_by = p_credential WHERE w.id = p_challenge;
  UPDATE fyniq.user_session s SET step_up_at = greatest(p_now, s.created_at), step_up_method = 'passkey'
   WHERE s.token_hash = p_session;
END
$$;
-- +goose StatementEnd

-- Revoke one of a user's live passkeys; the caller audits it in the same transaction. Returns whether it revoked one.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_credential_revoke(p_user uuid, p_credential uuid, p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.webauthn_credential w SET revoked_at = greatest(p_now, w.created_at)
   WHERE w.id = p_credential AND w.user_id = p_user AND w.revoked_at IS NULL;
  RETURN FOUND;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ grants (db/README.md)
REVOKE ALL ON FUNCTION fyniq.live_session_user(bytea, timestamptz), fyniq.session_step_up_method(bytea),
  fyniq.webauthn_challenge_issue(bytea, text, bytea, timestamptz, timestamptz),
  fyniq.webauthn_challenge_take(uuid, bytea, text, timestamptz),
  fyniq.webauthn_credentials(uuid, boolean),
  fyniq.webauthn_credential_register(uuid, bytea, bytea, bytea, bigint, bytea, text[], boolean, boolean, text, timestamptz),
  fyniq.session_step_up(uuid, bytea, uuid, bigint, boolean, timestamptz),
  fyniq.webauthn_credential_revoke(uuid, uuid, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.session_step_up_method(bytea),
  fyniq.webauthn_challenge_issue(bytea, text, bytea, timestamptz, timestamptz),
  fyniq.webauthn_challenge_take(uuid, bytea, text, timestamptz),
  fyniq.webauthn_credentials(uuid, boolean),
  fyniq.webauthn_credential_register(uuid, bytea, bytea, bytea, bigint, bytea, text[], boolean, boolean, text, timestamptz),
  fyniq.session_step_up(uuid, bytea, uuid, bigint, boolean, timestamptz),
  fyniq.webauthn_credential_revoke(uuid, uuid, timestamptz) TO fyniq_api;
