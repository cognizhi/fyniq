-- A scheduled agent's run reads and proposes through a grant of its own (PRD-001 AI-63, AI-89 rev 0.281; p1-scheduled-agents
-- task 5.2, design D6).
--
-- A delegated grant is a turn's or a run's, never both. A run's reads as the Owner the run acts for, for at most 30
-- minutes, and holds the agent's tools and task types: the API refuses any other. A run's proposal is requested for that
-- Owner, authenticated by the run's grant: the requester method ai_run, which the database allows for an ai_proposal
-- request whose proposal came from a run alone, as ai_turn for one from a turn. The person may sign their own tier-1
-- proposal as before (000377): their agent's too.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A run's grant has no turn: ai_grant_origin keeps exactly one of the two, and only the API reads the column.
-- squawk-ignore ban-drop-not-null
ALTER TABLE fyniq.ai_grant ALTER COLUMN turn_id DROP NOT NULL;
ALTER TABLE fyniq.ai_grant ADD COLUMN run_id uuid;
ALTER TABLE fyniq.ai_grant ADD COLUMN tools text[];
ALTER TABLE fyniq.ai_grant ADD COLUMN task_types text[];
ALTER TABLE fyniq.ai_grant ADD CONSTRAINT ai_grant_origin CHECK (num_nonnulls(turn_id, run_id) = 1
  AND (run_id IS NULL) = (tools IS NULL) AND (run_id IS NULL) = (task_types IS NULL)) NOT VALID;
ALTER TABLE fyniq.ai_grant ADD CONSTRAINT ai_grant_run_fkey FOREIGN KEY (organisation_id, run_id)
  REFERENCES fyniq.agent_run (organisation_id, id) NOT VALID;
ALTER TABLE fyniq.ai_grant DROP CONSTRAINT ai_grant_lifetime;
ALTER TABLE fyniq.ai_grant ADD CONSTRAINT ai_grant_lifetime CHECK (expires_at > issued_at
  AND expires_at <= issued_at + CASE WHEN run_id IS NULL THEN interval '5 minutes' ELSE interval '30 minutes' END) NOT VALID;
-- One grant per run. ai_grant holds a grant per turn, ended within 5 minutes, so building the index in this transaction
-- holds it for moments (squawk: the concurrent form cannot run in a transaction).
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX ai_grant_run ON fyniq.ai_grant (organisation_id, run_id) WHERE run_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.ai_grant_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.organisation_id, NEW.id, NEW.token_hash, NEW.user_id, NEW.turn_id, NEW.run_id, NEW.issued_at, NEW.expires_at,
       NEW.propose, NEW.tools, NEW.task_types)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.token_hash, OLD.user_id, OLD.turn_id, OLD.run_id, OLD.issued_at, OLD.expires_at,
       OLD.propose, OLD.tools, OLD.task_types)
     OR OLD.ended_at IS NOT NULL OR NOT NEW.classes @> OLD.classes THEN
    RAISE EXCEPTION 'A delegated read grant only ends, once, and what it read only widens before then' USING ERRCODE = 'FY340';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (run_id, tools, task_types) ON fyniq.ai_grant TO fyniq_api;

DROP FUNCTION fyniq.ai_grant_for(uuid, bytea, timestamptz);

-- ai_grant_for resolves a token's hash in its organisation to the person and their live roles there, whether the grant
-- may propose, and, for a run's, the run, its agent, and its tools and task types: nothing when the grant ended or
-- expired, or the person is no longer an active member.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_grant_for(p_org uuid, p_hash bytea, p_now timestamptz,
  OUT user_id uuid, OUT turn_id uuid, OUT roles text[], OUT expires_at timestamptz, OUT propose boolean,
  OUT run_id uuid, OUT tools text[], OUT task_types text[], OUT agent text)
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
    ai_grant_for.run_id := g.run_id;
    ai_grant_for.tools := g.tools;
    ai_grant_for.task_types := g.task_types;
    ai_grant_for.expires_at := g.expires_at;
    ai_grant_for.propose := g.propose;
    -- The run's agent as the AI's agents name it (ai_agent, AI-74): its skill company-secretary is company_secretary.
    SELECT replace(r.agent, '-', '_') INTO ai_grant_for.agent FROM fyniq.agent_run r WHERE r.organisation_id = p_org AND r.id = g.run_id;
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.ai_proposal_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  step text;
  role text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An AI proposal is kept: it is never deleted' USING ERRCODE = 'FY342';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'draft' OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'An AI proposal starts as a Draft, at version 1' USING ERRCODE = 'FY342';
    END IF;
    -- The turn it came from exists as it is made. No foreign key keeps it: the conversation's purge after retention
    -- removes the turn (000372), and the proposal, a record of what was decided, keeps the turn's ID.
    IF NEW.turn_id IS NOT NULL AND NOT EXISTS (
         SELECT 1 FROM fyniq.ai_turn t WHERE t.organisation_id = NEW.organisation_id AND t.id = NEW.turn_id) THEN
      RAISE EXCEPTION 'An AI proposal comes from a turn of the organisation''s' USING ERRCODE = 'FY342';
    END IF;
    -- A scheduled agent's proposal comes from its run while it runs, for the person the run acts for (AI-63, AI-89).
    IF NEW.run_id IS NOT NULL AND NOT EXISTS (
         SELECT 1 FROM fyniq.agent_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'running'
            AND r.acts_for = NEW.requested_by) THEN
      RAISE EXCEPTION 'A scheduled agent''s proposal comes from its running run, for the person it acts for' USING ERRCODE = 'FY342';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.task_type, NEW.task_version, NEW.task_sha256, NEW.payload, NEW.payload_sha256, NEW.item_count,
      NEW.rationale, NEW.evidence, NEW.risk_tier, NEW.required_approvals, NEW.agent, NEW.requested_by, NEW.turn_id, NEW.run_id,
      NEW.model, NEW.prompt, NEW.tools_sha256, NEW.proposed_at, NEW.expires_at)
     IS DISTINCT FROM
     (OLD.organisation_id, OLD.id, OLD.task_type, OLD.task_version, OLD.task_sha256, OLD.payload, OLD.payload_sha256, OLD.item_count,
      OLD.rationale, OLD.evidence, OLD.risk_tier, OLD.required_approvals, OLD.agent, OLD.requested_by, OLD.turn_id, OLD.run_id,
      OLD.model, OLD.prompt, OLD.tools_sha256, OLD.proposed_at, OLD.expires_at) THEN
    RAISE EXCEPTION 'What an AI proposal proposed never changes: a revision is a new proposal' USING ERRCODE = 'FY342';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Each step of an AI proposal is its next version' USING ERRCODE = 'FY342';
  END IF;
  step := OLD.status || '>' || NEW.status;
  role := CASE step
    WHEN 'draft>invalid' THEN 'fyniq_api'
    WHEN 'draft>pending' THEN 'fyniq_api'
    WHEN 'pending>approved' THEN 'fyniq_api'
    WHEN 'pending>rejected' THEN 'fyniq_api'
    WHEN 'pending>superseded' THEN 'fyniq_api'
    WHEN 'pending>expired' THEN 'fyniq_worker'
    WHEN 'approved>executing' THEN 'fyniq_executor'
    WHEN 'executing>executed' THEN 'fyniq_executor'
    WHEN 'executing>stale' THEN 'fyniq_executor'
    WHEN 'executing>failed' THEN 'fyniq_executor'
  END;
  IF role IS NULL THEN
    RAISE EXCEPTION 'An AI proposal cannot go from % to %', OLD.status, NEW.status USING ERRCODE = 'FY342';
  END IF;
  IF NOT pg_has_role(current_user, role, 'MEMBER') THEN
    RAISE EXCEPTION 'Only % takes an AI proposal from % to %', role, OLD.status, NEW.status USING ERRCODE = 'FY342';
  END IF;
  -- An approval is exactly what its signer signed (AI-P5, AI-32): what is approved hashes to the payload's hash their
  -- signature of the proposal's request signs, or to the items they selected, re-hashed.
  IF step = 'pending>approved' AND NOT EXISTS (
       SELECT 1 FROM fyniq.approval_signature s
        WHERE s.organisation_id = NEW.organisation_id AND s.request_id = NEW.approval_request_id AND s.signer = NEW.decided_by
          AND s.decision = 'approve' AND s.voided_at IS NULL AND coalesce(s.selection_sha256, NEW.payload_sha256) = NEW.approved_sha256) THEN
    RAISE EXCEPTION 'An AI proposal is approved only as its approver signed it' USING ERRCODE = 'FY342';
  END IF;
  -- Each step sets only its own: the dry run's at Pending; the decision's at Approved; the execution's on the way out.
  IF (NEW.impact, NEW.base_versions, NEW.approval_request_id) IS DISTINCT FROM (OLD.impact, OLD.base_versions, OLD.approval_request_id)
     AND step <> 'draft>pending' THEN
    RAISE EXCEPTION 'An AI proposal''s dry run is kept as it was made' USING ERRCODE = 'FY342';
  END IF;
  IF (NEW.decided_by, NEW.approved_items, NEW.approved_payload) IS DISTINCT FROM (OLD.decided_by, OLD.approved_items, OLD.approved_payload)
     AND step NOT IN ('pending>approved', 'pending>rejected') THEN
    RAISE EXCEPTION 'An AI proposal''s decision is kept as it was made' USING ERRCODE = 'FY342';
  END IF;
  IF NEW.decided_at IS DISTINCT FROM OLD.decided_at AND OLD.decided_at IS NOT NULL THEN
    RAISE EXCEPTION 'An AI proposal''s decision is kept as it was made' USING ERRCODE = 'FY342';
  END IF;
  IF NEW.started_at IS DISTINCT FROM OLD.started_at AND step <> 'approved>executing' THEN
    RAISE EXCEPTION 'An AI proposal starts executing once' USING ERRCODE = 'FY342';
  END IF;
  IF NEW.reason IS DISTINCT FROM OLD.reason AND OLD.reason <> '' THEN
    RAISE EXCEPTION 'An AI proposal''s reason is kept as it was given' USING ERRCODE = 'FY342';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- An AI proposal's request is asked by the person the proposal is for, authenticated by the turn or the run it came from.
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_mfa_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_mfa_known CHECK (
  requester_mfa_method IN ('session_step_up', 'passkey', 'totp')
  OR (object_type = 'ai_proposal' AND requester_mfa_method IN ('ai_turn', 'ai_run'))
) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_request_ai_requester() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  -- Any other object's request authenticated so is approval_request_mfa_known's to refuse.
  IF NEW.object_type = 'ai_proposal' AND NEW.requester_mfa_method IN ('ai_turn', 'ai_run') AND NOT EXISTS (
       SELECT 1 FROM fyniq.ai_proposal p
        WHERE p.organisation_id = NEW.organisation_id AND p.id::text = NEW.object_key AND p.requested_by = NEW.requested_by
          AND ((NEW.requester_mfa_method = 'ai_turn' AND p.turn_id IS NOT NULL) OR (NEW.requester_mfa_method = 'ai_run' AND p.run_id IS NOT NULL))) THEN
    RAISE EXCEPTION 'An AI proposal''s request is asked by the person it is for, authenticated by the turn or the run it came from'
      USING ERRCODE = 'FY342';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_request_ai_requester BEFORE INSERT ON fyniq.approval_request
  FOR EACH ROW EXECUTE FUNCTION fyniq.approval_request_ai_requester();
