-- The assistant's delegated read grants (PRD-001 §9.3 rev 0.277, AI-01, AI-36; p1-ai-assistant task 1.1, design D2).
--
-- For each turn the API hands the AI service a bearer token that reads as the person, never the person's session. The
-- token is opaque: its organisation in the clear and 32 random bytes; only its SHA-256 is kept here. A grant lives at
-- most 5 minutes and ends with its turn; ended or expired, it reads nothing. ai_grant_for resolves a token to the
-- person and their live roles in the grant's organisation, so a role taken away applies at the next call. The API
-- issues and ends grants; the AI's role holds nothing on them (CMP-AI-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_grant (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  token_hash      bytea       NOT NULL,
  user_id         uuid        NOT NULL,
  turn_id         uuid        NOT NULL,
  issued_at       timestamptz NOT NULL,
  expires_at      timestamptz NOT NULL,
  ended_at        timestamptz,
  CONSTRAINT ai_grant_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_grant_token UNIQUE (organisation_id, token_hash),
  CONSTRAINT ai_grant_turn UNIQUE (organisation_id, turn_id),
  CONSTRAINT ai_grant_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_grant_token_sha256 CHECK (length(token_hash) = 32),
  CONSTRAINT ai_grant_lifetime CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '5 minutes'),
  CONSTRAINT ai_grant_ended CHECK (ended_at IS NULL OR ended_at >= issued_at)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_grant_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.organisation_id, NEW.id, NEW.token_hash, NEW.user_id, NEW.turn_id, NEW.issued_at, NEW.expires_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.token_hash, OLD.user_id, OLD.turn_id, OLD.issued_at, OLD.expires_at)
     OR OLD.ended_at IS NOT NULL THEN
    RAISE EXCEPTION 'A delegated read grant only ends, once' USING ERRCODE = 'FY340';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_grant_guard BEFORE UPDATE OR DELETE ON fyniq.ai_grant
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_grant_guard();
CREATE TRIGGER ai_grant_no_truncate BEFORE TRUNCATE ON fyniq.ai_grant
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

ALTER TABLE fyniq.ai_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_grant
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_grant FROM PUBLIC;
GRANT SELECT ON fyniq.ai_grant TO fyniq_api;
GRANT INSERT (token_hash, user_id, turn_id, issued_at, expires_at), UPDATE (ended_at) ON fyniq.ai_grant TO fyniq_api;

-- ai_grant_for resolves a token's hash in its organisation to the person and their live roles there: nothing when the
-- grant ended or expired, or the person is no longer an active member.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_grant_for(p_org uuid, p_hash bytea, p_now timestamptz,
  OUT user_id uuid, OUT turn_id uuid, OUT roles text[], OUT expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  g        fyniq.ai_grant;
  previous text := current_setting('app.org_id', true);
BEGIN
  PERFORM set_config('app.org_id', p_org::text, true);
  SELECT * INTO g FROM fyniq.ai_grant a
   WHERE a.organisation_id = p_org AND a.token_hash = p_hash AND a.ended_at IS NULL AND a.expires_at > p_now AND a.issued_at <= p_now;
  IF FOUND AND EXISTS (SELECT 1 FROM fyniq.user_membership m
                        WHERE m.organisation_id = p_org AND m.user_id = g.user_id AND m.status = 'active') THEN
    ai_grant_for.user_id := g.user_id;
    ai_grant_for.turn_id := g.turn_id;
    ai_grant_for.expires_at := g.expires_at;
    SELECT coalesce(array_agg(DISTINCT r.role ORDER BY r.role), '{}') INTO ai_grant_for.roles
      FROM fyniq.role_assignment r
      JOIN fyniq.user_membership m ON m.organisation_id = r.organisation_id AND m.id = r.membership_id
     WHERE r.organisation_id = p_org AND m.user_id = g.user_id AND m.status = 'active'
       AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > p_now);
  END IF;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.ai_grant_for(uuid, bytea, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.ai_grant_for(uuid, bytea, timestamptz) TO fyniq_api;

