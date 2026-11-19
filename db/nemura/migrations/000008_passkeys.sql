-- Passkeys and step-up (PRD-002 NM-ARC-06, NM-PPL-04, NM-ROLE-05; PRD-001 SEC-01; question 10).
--
-- A member registers passkeys; a session is stepped up by an assertion with one of them, within five minutes of an approval. The relying-party ID is
-- the registrable domain Fyniq and Nemura share (question 10), so one passkey works in both; Nemura keeps its own rows. A challenge is
-- 32 random bytes bound to one session and one purpose, used once, valid for five minutes. A signature counter never goes back.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.passkey (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  member_id       uuid        NOT NULL,
  credential_id   bytea       NOT NULL,
  public_key      bytea       NOT NULL,
  sign_count      bigint      NOT NULL DEFAULT 0,
  aaguid          bytea       NOT NULL,
  transports      text[]      NOT NULL DEFAULT '{}',
  backup_eligible boolean     NOT NULL DEFAULT false,
  backup_state    boolean     NOT NULL DEFAULT false,
  name            text        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  last_used_at    timestamptz,
  revoked_at      timestamptz,
  CONSTRAINT passkey_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT passkey_credential_key UNIQUE (organisation_id, credential_id),
  CONSTRAINT passkey_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT passkey_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT passkey_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT passkey_aaguid_16 CHECK (length(aaguid) = 16),
  CONSTRAINT passkey_sign_count_range CHECK (sign_count BETWEEN 0 AND 4294967295)
);
CREATE INDEX passkey_member_idx ON nemura.passkey (organisation_id, member_id);

-- A passkey's key never changes; its counter only goes up; it is used, then revoked once; nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.passkey_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.member_id IS DISTINCT FROM OLD.member_id OR NEW.credential_id IS DISTINCT FROM OLD.credential_id
     OR NEW.public_key IS DISTINCT FROM OLD.public_key OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
    RAISE EXCEPTION 'A passkey can only be used or revoked, once' USING ERRCODE = 'NM102';
  END IF;
  IF NEW.sign_count < OLD.sign_count AND NEW.sign_count <> 0 THEN
    RAISE EXCEPTION 'A passkey''s signature counter never goes back: the authenticator may be cloned' USING ERRCODE = 'NM103';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER passkey_guard BEFORE UPDATE OR DELETE ON nemura.passkey
  FOR EACH ROW EXECUTE FUNCTION nemura.passkey_guard();

CREATE TABLE nemura.passkey_challenge (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  session_id      uuid        NOT NULL,
  member_id       uuid        NOT NULL,
  purpose         text        NOT NULL,
  challenge       bytea       NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  used_at         timestamptz,
  CONSTRAINT passkey_challenge_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT passkey_challenge_session_fkey FOREIGN KEY (organisation_id, session_id)
    REFERENCES nemura.session (organisation_id, id),
  CONSTRAINT passkey_challenge_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT passkey_challenge_purpose_known CHECK (purpose IN ('register', 'step_up')),
  CONSTRAINT passkey_challenge_32 CHECK (length(challenge) = 32),
  CONSTRAINT passkey_challenge_expiry CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes')
);
CREATE INDEX passkey_challenge_session_idx ON nemura.passkey_challenge (organisation_id, session_id, purpose);

-- A challenge only gets used, once.
-- +goose StatementBegin
CREATE FUNCTION nemura.passkey_challenge_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.used_at IS NOT NULL OR NEW.challenge IS DISTINCT FROM OLD.challenge OR NEW.session_id IS DISTINCT FROM OLD.session_id
     OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
    RAISE EXCEPTION 'A passkey challenge is used once and never changed' USING ERRCODE = 'NM102';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER passkey_challenge_guard BEFORE UPDATE OR DELETE ON nemura.passkey_challenge
  FOR EACH ROW EXECUTE FUNCTION nemura.passkey_challenge_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.passkey
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('passkey', 'id');

ALTER TABLE nemura.passkey ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.passkey FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.passkey
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.passkey_challenge ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.passkey_challenge FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.passkey_challenge
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.passkey, nemura.passkey_challenge TO nemura_api, nemura_readonly;
GRANT INSERT (id, member_id, credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state, name),
      UPDATE (sign_count, backup_state, last_used_at, revoked_at) ON nemura.passkey TO nemura_api;
GRANT INSERT (id, session_id, member_id, purpose, challenge, created_at, expires_at), UPDATE (used_at) ON nemura.passkey_challenge TO nemura_api;
