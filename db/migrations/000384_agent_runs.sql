-- A scheduled agent's runs (PRD-001 §9.12, AI-62, AI-64, AI-65, AI-67; p1-scheduled-agents task 3.1, design D3).
--
-- A run is queued for its slot: a schedule's entry, an event or Run now, once per agent, trigger and slot. The worker
-- runs it: it starts, then is done with its report, failed or skipped with why; nothing changes once it ends. Its report
-- records what it read (the ledger revision, the pack versions, the knowledge base's snapshot and its inputs'
-- fingerprint), its findings, Fyniq's summary and the model's words as written. A run is kept: it is never deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.agent_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  agent           text        NOT NULL,              -- the skill's name: company-secretary
  skill_version   integer     NOT NULL,
  skill_sha256    text        NOT NULL,              -- as agents/agents.lock.json pins it
  trigger         text        NOT NULL,              -- schedule:<entry>, event:<kind>:<key> or run_now
  slot_at         timestamptz NOT NULL,              -- when it was due
  acts_for        uuid        NOT NULL,              -- the Owner who turned the agent on (AI-63)
  requested_by    uuid,                              -- who chose Run now
  status          text        NOT NULL DEFAULT 'queued',
  reason          text        NOT NULL DEFAULT '',   -- why it failed or was skipped
  queued_at       timestamptz NOT NULL,
  started_at      timestamptz,
  finished_at     timestamptz,
  fingerprint     text,                              -- its inputs' SHA-256 (AI-64)
  ledger_revision bigint,                            -- ENG-46
  pack_versions   jsonb,                             -- each pack read, at its version
  kb_snapshot     text,                              -- the knowledge base's snapshot, or none
  findings        jsonb,                             -- the deterministic findings, each with its status, rule and source
  summary         text,                              -- Fyniq's words, never "all clear" with anything open (AI-65)
  model_used      boolean,
  model           text,
  model_text      text,                              -- the model's words as written, shown as AI
  tokens          integer,
  tool_calls      integer,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT agent_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT agent_run_slot UNIQUE (organisation_id, agent, trigger, slot_at),
  CONSTRAINT agent_run_acts_for_fkey FOREIGN KEY (acts_for) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT agent_run_requested_by_fkey FOREIGN KEY (requested_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT agent_run_agent CHECK (agent ~ '^[a-z][a-z-]{1,40}$' AND skill_version >= 1 AND skill_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT agent_run_trigger CHECK (trigger ~ '^(schedule:[a-z_]{1,40}|event:[a-z_]{1,40}:[A-Za-z0-9:._-]{1,100}|run_now)$'
    AND (requested_by IS NOT NULL) = (trigger = 'run_now')),
  CONSTRAINT agent_run_status CHECK (status IN ('queued', 'running', 'done', 'failed', 'skipped')),
  CONSTRAINT agent_run_words CHECK (length(reason) <= 1000 AND length(summary) <= 4000 AND length(model_text) <= 20000
    AND length(model) <= 200 AND length(kb_snapshot) <= 100 AND pg_column_size(findings) <= 262144),
  CONSTRAINT agent_run_counts CHECK (ledger_revision >= 0 AND tokens >= 0 AND tool_calls >= 0),
  -- What each state holds: when it started and ended; why it failed or was skipped; a report once done.
  CONSTRAINT agent_run_state CHECK (
    (started_at IS NOT NULL) = (status IN ('running', 'done', 'failed'))
    AND (finished_at IS NOT NULL) = (status IN ('done', 'failed', 'skipped'))
    AND (reason <> '') = (status IN ('failed', 'skipped'))
    AND (status <> 'done' OR (fingerprint ~ '^[0-9a-f]{64}$' AND ledger_revision IS NOT NULL AND pack_versions IS NOT NULL
      AND kb_snapshot IS NOT NULL AND jsonb_typeof(findings) = 'array' AND summary <> '' AND model_used IS NOT NULL))
    AND ((model_used IS TRUE) = (model IS NOT NULL)) AND (model_text IS NULL OR model_used IS TRUE)
    AND (started_at IS NULL OR started_at >= queued_at) AND (finished_at IS NULL OR finished_at >= queued_at)
    AND (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at)),
  CONSTRAINT agent_run_version CHECK (version >= 1)
);
CREATE INDEX agent_run_latest ON fyniq.agent_run (organisation_id, agent, queued_at DESC);
CREATE INDEX agent_run_open ON fyniq.agent_run (organisation_id, status, slot_at) WHERE status IN ('queued', 'running');

-- The steps, and the role that takes each: the worker queues, starts and ends a run; the API queues one for Run now.
-- What a run is never changes; its report is written once, as it ends.
-- +goose StatementBegin
CREATE FUNCTION fyniq.agent_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  step text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An agent''s run is kept: it is never deleted' USING ERRCODE = 'FY350';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'queued' OR NEW.version <> 1 OR NEW.started_at IS NOT NULL OR NEW.finished_at IS NOT NULL OR NEW.findings IS NOT NULL
       OR NEW.summary IS NOT NULL OR NEW.model_used IS NOT NULL THEN
      RAISE EXCEPTION 'An agent''s run starts queued, at version 1, with no report' USING ERRCODE = 'FY350';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.agent, NEW.skill_version, NEW.skill_sha256, NEW.trigger, NEW.slot_at, NEW.acts_for,
      NEW.requested_by, NEW.queued_at)
     IS DISTINCT FROM
     (OLD.organisation_id, OLD.id, OLD.agent, OLD.skill_version, OLD.skill_sha256, OLD.trigger, OLD.slot_at, OLD.acts_for,
      OLD.requested_by, OLD.queued_at) THEN
    RAISE EXCEPTION 'What an agent''s run is never changes' USING ERRCODE = 'FY350';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Each step of an agent''s run is its next version' USING ERRCODE = 'FY350';
  END IF;
  step := OLD.status || '>' || NEW.status;
  IF step NOT IN ('queued>running', 'queued>skipped', 'running>done', 'running>failed', 'running>skipped') THEN
    RAISE EXCEPTION 'An agent''s run cannot go from % to %', OLD.status, NEW.status USING ERRCODE = 'FY350';
  END IF;
  IF NOT pg_has_role(current_user, 'fyniq_worker', 'MEMBER') THEN
    RAISE EXCEPTION 'Only the worker runs an agent''s run' USING ERRCODE = 'FY350';
  END IF;
  -- Its report is written once, as it ends.
  IF step = 'queued>running' AND (NEW.findings, NEW.summary, NEW.fingerprint, NEW.model_used) IS DISTINCT FROM
     (OLD.findings, OLD.summary, OLD.fingerprint, OLD.model_used) THEN
    RAISE EXCEPTION 'An agent''s run reports as it ends' USING ERRCODE = 'FY350';
  END IF;
  IF NEW.started_at IS DISTINCT FROM OLD.started_at AND step <> 'queued>running' THEN
    RAISE EXCEPTION 'An agent''s run starts once' USING ERRCODE = 'FY350';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER agent_run_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.agent_run
  FOR EACH ROW EXECUTE FUNCTION fyniq.agent_run_guard();
CREATE TRIGGER agent_run_no_truncate BEFORE TRUNCATE ON fyniq.agent_run
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- Every step is audited in its transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.agent_run
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('agent_run', 'id');

ALTER TABLE fyniq.agent_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.agent_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.agent_run
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.agent_run FROM PUBLIC;
GRANT SELECT ON fyniq.agent_run TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (agent, skill_version, skill_sha256, trigger, slot_at, acts_for, requested_by, queued_at)
  ON fyniq.agent_run TO fyniq_api, fyniq_worker;
GRANT UPDATE (status, reason, started_at, finished_at, fingerprint, ledger_revision, pack_versions, kb_snapshot, findings, summary,
  model_used, model, model_text, tokens, tool_calls, version)
  ON fyniq.agent_run TO fyniq_worker;
