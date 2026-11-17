-- A turn's grant may propose (PRD-001 AI-P3, AI-32; p1-ai-proposals-core task 3.1, design D3).
--
-- A turn's delegated read grant gains one capability, propose, set as it is issued and only while the organisation has a
-- task type turned on (ai.task_types). It permits making proposals through the internal API and nothing else: no
-- approval, no execution, no other write. It never changes once issued; ai_grant_for answers it with the rest.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_grant ADD COLUMN propose boolean NOT NULL DEFAULT false;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.ai_grant_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.organisation_id, NEW.id, NEW.token_hash, NEW.user_id, NEW.turn_id, NEW.issued_at, NEW.expires_at, NEW.propose)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.token_hash, OLD.user_id, OLD.turn_id, OLD.issued_at, OLD.expires_at, OLD.propose)
     OR OLD.ended_at IS NOT NULL OR NOT NEW.classes @> OLD.classes THEN
    RAISE EXCEPTION 'A delegated read grant only ends, once, and what it read only widens before then' USING ERRCODE = 'FY340';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (propose) ON fyniq.ai_grant TO fyniq_api;

DROP FUNCTION fyniq.ai_grant_for(uuid, bytea, timestamptz);

-- ai_grant_for resolves a token's hash in its organisation to the person and their live roles there, and whether the
-- grant may propose: nothing when the grant ended or expired, or the person is no longer an active member.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_grant_for(p_org uuid, p_hash bytea, p_now timestamptz,
  OUT user_id uuid, OUT turn_id uuid, OUT roles text[], OUT expires_at timestamptz, OUT propose boolean)
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
    ai_grant_for.propose := g.propose;
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
