-- Account recovery (PRD-001 SEC-35; rev 0.198; p1-users-roles task 2.5), on the owner's decisions of 6 Nov 2026: each
-- person keeps 10 one-time recovery codes, shown once and kept only as keyed hashes (the API's field key, never in the
-- database); a person who lost every authenticator recovers with one of them plus a confirmation from an Owner or
-- Administrator of one of their organisations, given in person (a one-time code, 24 hours). Recovery gives the session
-- a second factor (method recovery) so they can register a new passkey, ends their other sessions, replaces their codes
-- and starts a cooling-off (24 hours) during which the API holds approvals, payment releases, bank-detail, role and
-- sign-in changes and break-glass. A person whose provider failed signs in with another one they linked (FR-ADM-08).
-- Email alone or a support request never recovers an account: nothing here takes either.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.user_recovery_code (
  id         uuid        NOT NULL DEFAULT uuidv7(),
  user_id    uuid        NOT NULL,
  code_hash  bytea       NOT NULL,
  created_at timestamptz NOT NULL,
  used_at    timestamptz,
  CONSTRAINT user_recovery_code_pkey PRIMARY KEY (id),
  CONSTRAINT user_recovery_code_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT user_recovery_code_hash_length CHECK (length(code_hash) = 32),
  CONSTRAINT user_recovery_code_used_after CHECK (used_at IS NULL OR used_at >= created_at)
);
CREATE UNIQUE INDEX user_recovery_code_hash ON fyniq.user_recovery_code (code_hash);
CREATE INDEX user_recovery_code_user ON fyniq.user_recovery_code (user_id) WHERE used_at IS NULL;
REVOKE ALL ON fyniq.user_recovery_code FROM PUBLIC;

-- A confirmation, by an Owner or Administrator of confirming_organisation (the API checks the role), that they spoke to
-- the person: the second proof. One live confirmation per person; never their own. A global row: the organisation is
-- where the confirmation came from, not a tenant key.
CREATE TABLE fyniq.recovery_confirmation (
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  confirmed_by    uuid        NOT NULL,
  confirming_organisation uuid NOT NULL,
  code_hash       bytea       NOT NULL,
  created_at      timestamptz NOT NULL,
  expires_at      timestamptz NOT NULL,
  used_at         timestamptz,
  CONSTRAINT recovery_confirmation_pkey PRIMARY KEY (id),
  CONSTRAINT recovery_confirmation_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT recovery_confirmation_by_fkey FOREIGN KEY (confirmed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT recovery_confirmation_another CHECK (confirmed_by <> user_id),
  CONSTRAINT recovery_confirmation_hash_length CHECK (length(code_hash) = 32),
  CONSTRAINT recovery_confirmation_expiry CHECK (expires_at > created_at),
  CONSTRAINT recovery_confirmation_used_window CHECK (used_at IS NULL OR (used_at >= created_at AND used_at <= expires_at))
);
CREATE UNIQUE INDEX recovery_confirmation_hash ON fyniq.recovery_confirmation (code_hash);
REVOKE ALL ON fyniq.recovery_confirmation FROM PUBLIC;

-- Each recovery, with what proved it and the cooling-off it started; never changed.
CREATE TABLE fyniq.account_recovery (
  id                uuid        NOT NULL DEFAULT uuidv7(),
  user_id           uuid        NOT NULL,
  recovery_code_id  uuid        NOT NULL,
  confirmation_id   uuid        NOT NULL,
  recovered_at      timestamptz NOT NULL,
  cooling_off_until timestamptz NOT NULL,
  CONSTRAINT account_recovery_pkey PRIMARY KEY (id),
  CONSTRAINT account_recovery_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT account_recovery_code_fkey FOREIGN KEY (recovery_code_id) REFERENCES fyniq.user_recovery_code (id),
  CONSTRAINT account_recovery_confirmation_fkey FOREIGN KEY (confirmation_id) REFERENCES fyniq.recovery_confirmation (id),
  CONSTRAINT account_recovery_cooling_off CHECK (cooling_off_until > recovered_at)
);
CREATE INDEX account_recovery_user ON fyniq.account_recovery (user_id, cooling_off_until);
REVOKE ALL ON fyniq.account_recovery FROM PUBLIC;

-- A recovered session passed its second factor by recovery.
ALTER TABLE fyniq.user_session DROP CONSTRAINT user_session_step_up_method;
ALTER TABLE fyniq.user_session ADD CONSTRAINT user_session_step_up_method CHECK (
  (step_up_at IS NULL AND step_up_method IS NULL)
  OR (step_up_method = 'sign_in' AND step_up_at <= created_at)
  OR (step_up_method IN ('passkey', 'recovery') AND step_up_at >= created_at)) NOT VALID;

-- A recovery enters the weekly review of the organisation the person's session is in (FM-3).
ALTER TABLE fyniq.review_log_entry DROP CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.review_log_entry ADD CONSTRAINT review_log_entry_kind_known CHECK (
  kind IN ('approved_change', 'founder_bootstrap', 'self_approved', 'scheduled_issue', 'auto_reconciled', 'release_conflict',
           'break_glass', 'account_recovery')) NOT VALID;

-- +goose StatementBegin
-- An account recovery is never changed or removed (FY279).
CREATE FUNCTION fyniq.account_recovery_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'An account recovery is kept as it happened' USING ERRCODE = 'FY279';
END
$$;

-- Until when user p_user's latest recovery holds them in cooling-off, if it still does at p_now.
CREATE FUNCTION fyniq.user_cooling_off_until(p_user uuid, p_now timestamptz) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT max(r.cooling_off_until) FROM fyniq.account_recovery r WHERE r.user_id = p_user AND r.cooling_off_until > p_now
$$;

-- How many of a person's recovery codes are unused, and when they were made.
CREATE FUNCTION fyniq.user_recovery_codes(p_user uuid) RETURNS TABLE (unused integer, created_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT count(*) FILTER (WHERE c.used_at IS NULL)::integer, max(c.created_at) FROM fyniq.user_recovery_code c WHERE c.user_id = p_user
$$;

-- The user of a live session that passed a second factor in the last 5 minutes replaces their recovery codes with ten
-- new ones (FY275 otherwise). Returns the user.
CREATE FUNCTION fyniq.set_user_recovery_codes(p_session bytea, p_hashes bytea[], p_now timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who uuid := fyniq.live_session_user(p_session, p_now);
BEGIN
  IF who IS NULL OR NOT EXISTS (SELECT 1 FROM fyniq.user_session s WHERE s.token_hash = p_session AND s.step_up_at > p_now - interval '5 minutes') THEN
    RAISE EXCEPTION 'Recovery codes are made only by a session that confirmed it is you in the last 5 minutes' USING ERRCODE = 'FY275',
      HINT = 'Confirm it''s you, then make the codes again.';
  END IF;
  IF cardinality(p_hashes) <> 10 THEN
    RAISE EXCEPTION 'A person keeps ten recovery codes, not %', cardinality(p_hashes) USING ERRCODE = 'FY275';
  END IF;
  DELETE FROM fyniq.user_recovery_code c WHERE c.user_id = who AND c.used_at IS NULL; -- a used code stays, with its recovery
  INSERT INTO fyniq.user_recovery_code (user_id, code_hash, created_at) SELECT who, h, p_now FROM unnest(p_hashes) AS h;
  RETURN who;
END
$$;

-- p_by, whom the API found an Owner or Administrator of p_org that user p_for belongs to, confirms p_for's recovery
-- with the one-time code whose SHA-256 is p_hash, until p_expires; it replaces an unused confirmation. Never their own
-- (FY276).
CREATE FUNCTION fyniq.confirm_account_recovery(p_by uuid, p_for uuid, p_org uuid, p_hash bytea, p_now timestamptz, p_expires timestamptz)
  RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  out uuid;
BEGIN
  IF p_by = p_for THEN
    RAISE EXCEPTION 'Nobody confirms their own recovery' USING ERRCODE = 'FY276',
      HINT = 'Another Owner or Administrator confirms it, after speaking to the person.';
  END IF;
  IF fyniq.user_cooling_off_until(p_by, p_now) IS NOT NULL THEN
    RAISE EXCEPTION 'An account in its cooling-off after a recovery confirms nobody''s recovery' USING ERRCODE = 'FY276',
      HINT = 'Another Owner or Administrator confirms it.';
  END IF;
  DELETE FROM fyniq.recovery_confirmation c WHERE c.user_id = p_for AND c.used_at IS NULL;
  INSERT INTO fyniq.recovery_confirmation (user_id, confirmed_by, confirming_organisation, code_hash, created_at, expires_at)
  VALUES (p_for, p_by, p_org, p_hash, p_now, p_expires) RETURNING id INTO out;
  RETURN out;
END
$$;

-- The person signed in to live session p_session, with or without a second factor, recovers with one of their unused
-- recovery codes (keyed hash p_code) and an unused, unexpired confirmation of their recovery (SHA-256 p_confirmation):
-- both are used; the session passes its second factor by recovery; their other sessions end; their codes become
-- p_new_hashes; the cooling-off lasts p_cooling. Any proof wrong, used or expired changes nothing (FY277, the same for
-- each, so a guess learns nothing).
CREATE FUNCTION fyniq.recover_account(p_session bytea, p_code bytea, p_confirmation bytea, p_new_hashes bytea[], p_now timestamptz,
  p_cooling interval)
  RETURNS TABLE (recovery_id uuid, user_id uuid, confirmed_by uuid, confirming_organisation uuid, cooling_off_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who  uuid := fyniq.live_session_user(p_session, p_now);
  code fyniq.user_recovery_code;
  conf fyniq.recovery_confirmation;
  rid  uuid;
BEGIN
  SELECT * INTO conf FROM fyniq.recovery_confirmation c
   WHERE c.code_hash = p_confirmation AND c.user_id = who AND c.used_at IS NULL AND c.expires_at > p_now FOR UPDATE;
  IF who IS NOT NULL AND conf.id IS NOT NULL THEN
    SELECT * INTO code FROM fyniq.user_recovery_code c WHERE c.code_hash = p_code AND c.user_id = who AND c.used_at IS NULL FOR UPDATE;
  END IF;
  IF who IS NULL OR conf.id IS NULL OR code.id IS NULL THEN
    RAISE EXCEPTION 'The recovery is not valid, used or expired' USING ERRCODE = 'FY277',
      HINT = 'Check both codes, or ask the Owner or Administrator for a new confirmation.';
  END IF;
  IF cardinality(p_new_hashes) <> 10 THEN
    RAISE EXCEPTION 'A person keeps ten recovery codes, not %', cardinality(p_new_hashes) USING ERRCODE = 'FY277';
  END IF;
  UPDATE fyniq.recovery_confirmation c SET used_at = p_now WHERE c.id = conf.id;
  UPDATE fyniq.user_recovery_code c SET used_at = p_now WHERE c.user_id = who AND c.used_at IS NULL;
  INSERT INTO fyniq.user_recovery_code (user_id, code_hash, created_at) SELECT who, h, p_now FROM unnest(p_new_hashes) AS h;
  UPDATE fyniq.user_session s SET step_up_at = p_now, step_up_method = 'recovery' WHERE s.token_hash = p_session;
  UPDATE fyniq.user_session s SET ended_at = greatest(p_now, s.created_at)
   WHERE s.ended_at IS NULL AND s.token_hash <> p_session
     AND EXISTS (SELECT 1 FROM fyniq.user_identity i WHERE i.issuer = s.issuer AND i.subject = s.subject AND i.user_id = who);
  INSERT INTO fyniq.account_recovery (user_id, recovery_code_id, confirmation_id, recovered_at, cooling_off_until)
  VALUES (who, code.id, conf.id, p_now, p_now + p_cooling) RETURNING id INTO rid;
  RETURN QUERY SELECT rid, who, conf.confirmed_by, conf.confirming_organisation, p_now + p_cooling;
END
$$;
-- Linking a sign-in (000239), now held while its user is in a cooling-off after a recovery (FY278).
CREATE OR REPLACE FUNCTION fyniq.link_identity(p_session bytea, p_issuer text, p_subject text, p_provider text, p_now timestamptz) RETURNS uuid
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
  IF fyniq.user_cooling_off_until(who, p_now) IS NOT NULL THEN
    RAISE EXCEPTION 'Sign-ins are not linked during the cooling-off after a recovery' USING ERRCODE = 'FY278',
      HINT = 'Link it once the cooling-off has ended.';
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
-- +goose StatementEnd

CREATE TRIGGER account_recovery_immutable BEFORE UPDATE OR DELETE ON fyniq.account_recovery
  FOR EACH ROW EXECUTE FUNCTION fyniq.account_recovery_immutable();

REVOKE ALL ON FUNCTION fyniq.user_cooling_off_until(uuid, timestamptz), fyniq.user_recovery_codes(uuid),
  fyniq.set_user_recovery_codes(bytea, bytea[], timestamptz),
  fyniq.confirm_account_recovery(uuid, uuid, uuid, bytea, timestamptz, timestamptz),
  fyniq.recover_account(bytea, bytea, bytea, bytea[], timestamptz, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.user_cooling_off_until(uuid, timestamptz), fyniq.user_recovery_codes(uuid),
  fyniq.set_user_recovery_codes(bytea, bytea[], timestamptz),
  fyniq.confirm_account_recovery(uuid, uuid, uuid, bytea, timestamptz, timestamptz),
  fyniq.recover_account(bytea, bytea, bytea, bytea[], timestamptz, interval) TO fyniq_api;
