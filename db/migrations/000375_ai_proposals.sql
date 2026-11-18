-- The AI's proposals (PRD-001 §9.5, AI-P3 to AI-P5; p1-ai-proposals-core task 2.1, design D2): the one way an AI changes
-- anything. A proposal is a catalogue task type's payload, kept as the exact JSON text that will execute with its SHA-256,
-- which the database checks; the AI's rationale and evidence; the dry run's impact and the versions of the records it
-- read; its tier, the approvals it needs, who and what made it; its decision; and how it ran.
--
-- It moves only by its lifecycle, each step by its own role: the API makes a Draft and moves it to Invalid or Pending;
-- the approval engine, in the API, to Approved or Rejected; a revision to Superseded; the worker's job to Expired once
-- past expires_at; the executor, alone, from Approved to Executing and on to Executed, Stale or Failed. What it proposed
-- never changes once made, and nothing is deleted (FY342). The AI's role holds nothing on it (CMP-AI-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_proposal (
  organisation_id     uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                  uuid        NOT NULL DEFAULT uuidv7(),
  task_type           text        NOT NULL,
  task_version        integer     NOT NULL,
  task_sha256         text        NOT NULL,              -- the catalogue definition it was made under
  payload             text        NOT NULL,              -- the exact JSON that executes
  payload_sha256      text        NOT NULL,
  item_count          integer     NOT NULL DEFAULT 0,    -- a batch's items; 0 when it is not a batch
  rationale           text        NOT NULL DEFAULT '',   -- the AI's words, shown apart as AI text
  evidence            jsonb       NOT NULL DEFAULT '[]', -- the records and documents it relied on
  impact              jsonb,                             -- the dry run's: ledger lines, balances, checks, documents
  base_versions       jsonb       NOT NULL DEFAULT '{}', -- each record read or changed, at its version
  risk_tier           smallint    NOT NULL,
  required_approvals  smallint    NOT NULL DEFAULT 1,
  agent               text        NOT NULL,              -- the AI principal: the agent that proposed it
  requested_by        uuid        NOT NULL,              -- the person it acted for
  turn_id             uuid,                              -- the conversation's turn (kept once the turn is purged), or
  run_id              uuid,                              -- the scheduled agent's run
  model               text        NOT NULL,
  prompt              text        NOT NULL,              -- chat@2
  tools_sha256        text        NOT NULL,              -- the tools it was offered, as pinned
  status              text        NOT NULL DEFAULT 'draft',
  reason              text        NOT NULL DEFAULT '',   -- why it is Invalid, Rejected, Stale or Failed
  approval_request_id uuid,
  decided_by          uuid,
  decided_at          timestamptz,
  approved_items      integer[],                         -- a batch's items approved, from 1
  approved_payload    text,                              -- what was approved: the payload, or its items' subset
  approved_sha256     text,
  superseded_by       uuid,
  execution           jsonb,                             -- the executor's run, the records and entries made, any error
  started_at          timestamptz,
  finished_at         timestamptz,
  proposed_at         timestamptz NOT NULL,
  expires_at          timestamptz NOT NULL,
  version             integer     NOT NULL DEFAULT 1,
  CONSTRAINT ai_proposal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_proposal_requested_by_fkey FOREIGN KEY (requested_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_proposal_decided_by_fkey FOREIGN KEY (decided_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_proposal_approval_fkey FOREIGN KEY (organisation_id, approval_request_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT ai_proposal_superseded_fkey FOREIGN KEY (organisation_id, superseded_by) REFERENCES fyniq.ai_proposal (organisation_id, id),
  CONSTRAINT ai_proposal_task CHECK (task_type ~ '^[a-z]+\.[a-z]+(_[a-z]+)*$' AND length(task_type) <= 56 AND task_version >= 1
    AND task_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT ai_proposal_payload CHECK (length(payload) BETWEEN 2 AND 65536 AND jsonb_typeof(payload::jsonb) = 'object'
    AND payload_sha256 = encode(sha256(convert_to(payload, 'UTF8')), 'hex')),
  CONSTRAINT ai_proposal_approved_payload CHECK ((approved_payload IS NULL) = (approved_sha256 IS NULL)
    AND (approved_payload IS NULL OR (length(approved_payload) BETWEEN 2 AND 65536 AND jsonb_typeof(approved_payload::jsonb) = 'object'
      AND approved_sha256 = encode(sha256(convert_to(approved_payload, 'UTF8')), 'hex')))),
  CONSTRAINT ai_proposal_items CHECK (item_count BETWEEN 0 AND 100 AND (approved_items IS NULL OR (item_count > 0
    AND cardinality(approved_items) BETWEEN 1 AND item_count AND 1 <= ALL (approved_items) AND item_count >= ALL (approved_items)))),
  CONSTRAINT ai_proposal_words CHECK (length(rationale) <= 4000 AND length(reason) <= 1000 AND jsonb_typeof(evidence) = 'array'
    AND jsonb_array_length(evidence) <= 50 AND jsonb_typeof(base_versions) = 'object' AND pg_column_size(impact) <= 262144),
  CONSTRAINT ai_proposal_tier CHECK (risk_tier BETWEEN 1 AND 3 AND required_approvals BETWEEN 1 AND 2),
  CONSTRAINT ai_proposal_originator CHECK (agent ~ '^[a-z][a-z_]{1,40}$' AND num_nonnulls(turn_id, run_id) = 1
    AND length(model) BETWEEN 1 AND 200 AND prompt ~ '^[a-z][a-z_]{1,30}@[1-9][0-9]{0,3}$' AND tools_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT ai_proposal_expiry CHECK (expires_at > proposed_at AND expires_at <= proposed_at + interval '72 hours'),
  CONSTRAINT ai_proposal_status CHECK (status IN ('draft', 'invalid', 'pending', 'approved', 'rejected', 'expired', 'superseded',
    'executing', 'executed', 'stale', 'failed')),
  -- What each state holds: the dry run's impact from Pending on; a decision's time and who made it; the approved payload
  -- from Approved on; a reason where one is due; the execution's times and record.
  CONSTRAINT ai_proposal_state CHECK (
    (impact IS NOT NULL) = (status NOT IN ('draft', 'invalid'))
    AND (decided_at IS NOT NULL) = (status NOT IN ('draft', 'invalid', 'pending'))
    AND (decided_by IS NOT NULL) = (status IN ('approved', 'rejected', 'executing', 'executed', 'stale', 'failed'))
    AND (approved_payload IS NOT NULL) = (status IN ('approved', 'executing', 'executed', 'stale', 'failed'))
    AND (reason <> '') = (status IN ('invalid', 'rejected', 'stale', 'failed'))
    AND (superseded_by IS NOT NULL) = (status = 'superseded')
    AND (started_at IS NOT NULL) = (status IN ('executing', 'executed', 'stale', 'failed'))
    AND (finished_at IS NOT NULL) = (status IN ('executed', 'stale', 'failed'))
    AND (execution IS NOT NULL) = (status IN ('executed', 'stale', 'failed'))
    AND (status <> 'expired' OR decided_at >= expires_at)
    AND (status NOT IN ('approved', 'rejected') OR decided_at < expires_at)
    AND (decided_at IS NULL OR decided_at >= proposed_at)
    AND (started_at IS NULL OR started_at >= decided_at) AND (finished_at IS NULL OR finished_at >= started_at)),
  CONSTRAINT ai_proposal_version CHECK (version >= 1)
);
CREATE INDEX ai_proposal_by_status ON fyniq.ai_proposal (organisation_id, status, proposed_at);
CREATE INDEX ai_proposal_of_requester ON fyniq.ai_proposal (organisation_id, requested_by, proposed_at DESC);
CREATE INDEX ai_proposal_of_turn ON fyniq.ai_proposal (organisation_id, turn_id) WHERE turn_id IS NOT NULL;
CREATE INDEX ai_proposal_of_approval ON fyniq.ai_proposal (organisation_id, approval_request_id) WHERE approval_request_id IS NOT NULL;

-- The lifecycle, and the role that takes each step (design D2). A step not in the list, a step by another role, a change to
-- what was proposed, or a delete is refused.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_proposal_guard() RETURNS trigger
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

CREATE TRIGGER ai_proposal_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_proposal
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_proposal_guard();
CREATE TRIGGER ai_proposal_no_truncate BEFORE TRUNCATE ON fyniq.ai_proposal
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- Every step is audited in its transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.ai_proposal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_proposal', 'id');

ALTER TABLE fyniq.ai_proposal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_proposal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_proposal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_proposal FROM PUBLIC;
GRANT SELECT ON fyniq.ai_proposal TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (task_type, task_version, task_sha256, payload, payload_sha256, item_count, rationale, evidence, risk_tier,
  required_approvals, agent, requested_by, turn_id, run_id, model, prompt, tools_sha256, proposed_at, expires_at),
  UPDATE (status, reason, impact, base_versions, approval_request_id, decided_by, decided_at, approved_items, approved_payload,
  approved_sha256, superseded_by, version)
  ON fyniq.ai_proposal TO fyniq_api;
GRANT UPDATE (status, decided_at, version) ON fyniq.ai_proposal TO fyniq_worker;
GRANT UPDATE (status, reason, execution, started_at, finished_at, version) ON fyniq.ai_proposal TO fyniq_executor;
