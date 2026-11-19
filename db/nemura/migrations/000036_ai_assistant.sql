-- The people assistant: its settings and its proposals (PRD-002 NM-AI-01, NM-AI-04, NM-ARC-06, Appendix D; p1-nemura-assistant-external slice 1).
--
-- The assistant is off for an organisation until its Owner turns it on. It holds no write credential on people data: its database role (`nemura_assistant`) may only INSERT a proposal and its audit event.
-- A proposal is decided by a named human (a tier 1 draft: the person who asked may decide it, as Fyniq's AI-10 allows, and both are in the audit trail), applied by the executor's role (`nemura_executor`) only after approval and with the approved text and its hash, and
-- expires after 72 hours (a pending proposal past its time cannot be approved). A proposal's text and its audit event never hold a pay figure or a bank account.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.ai_settings (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  enabled         boolean     NOT NULL DEFAULT false,
  route           text        NOT NULL DEFAULT 'none',
  task_types      text[]      NOT NULL DEFAULT '{}',
  own_pay         boolean     NOT NULL DEFAULT false,
  updated_by      uuid        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT ai_settings_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT ai_settings_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT ai_settings_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT ai_settings_route_known CHECK (route IN ('none', 'scripted')),
  CONSTRAINT ai_settings_task_types_known CHECK (task_types <@ ARRAY['people.draft_letter', 'people.draft_fwa_reply']::text[]),
  -- Own-pay explanations send the asking person's own payslip figures to a model: they need the Owner and a second approver, which arrives with the Fyniq link (NM-AI-06); until then no row may turn them on.
  CONSTRAINT ai_settings_own_pay_needs_a_second_approver CHECK (NOT own_pay)
);
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.ai_settings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('ai_settings', 'organisation_id');
ALTER TABLE nemura.ai_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.ai_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.ai_settings USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
GRANT SELECT ON nemura.ai_settings TO nemura_api, nemura_worker, nemura_readonly, nemura_assistant, nemura_executor;
GRANT INSERT (enabled, route, task_types, own_pay, updated_by, updated_at), UPDATE (enabled, route, task_types, own_pay, updated_by, updated_at) ON nemura.ai_settings TO nemura_api;

CREATE TABLE nemura.ai_proposal (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  task_type       text        NOT NULL,
  task_version    integer     NOT NULL,
  asker           uuid        NOT NULL,   -- the member the assistant was acting for
  payload         jsonb       NOT NULL,   -- what the assistant proposed
  payload_hash    bytea       NOT NULL,   -- SHA-256 of its canonical JSON
  rationale       text,
  model_id        text        NOT NULL,
  prompt_version  text        NOT NULL,
  state           text        NOT NULL DEFAULT 'pending',
  created_at      timestamptz NOT NULL,
  expires_at      timestamptz NOT NULL,
  decided_by      uuid,
  decided_at      timestamptz,
  decision_reason text,
  final_payload   jsonb,                  -- what the human approved, after editing: the executor applies this and nothing else
  approved_hash   bytea,                  -- SHA-256 of the final payload
  applied_at      timestamptz,
  CONSTRAINT ai_proposal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_proposal_asker_fkey FOREIGN KEY (organisation_id, asker) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT ai_proposal_decider_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT ai_proposal_task_known CHECK (task_type IN ('people.draft_letter', 'people.draft_fwa_reply')),
  CONSTRAINT ai_proposal_state_known CHECK (state IN ('pending', 'approved', 'rejected', 'applied')),
  CONSTRAINT ai_proposal_hash CHECK (octet_length(payload_hash) = 32 AND (approved_hash IS NULL OR octet_length(approved_hash) = 32)),
  CONSTRAINT ai_proposal_expiry CHECK (expires_at = created_at + interval '72 hours'),
  CONSTRAINT ai_proposal_rationale_length CHECK (rationale IS NULL OR length(rationale) <= 500),
  CONSTRAINT ai_proposal_decision_whole CHECK (
       (state = 'pending' AND decided_by IS NULL AND decided_at IS NULL AND final_payload IS NULL AND approved_hash IS NULL AND applied_at IS NULL)
    OR (state = 'rejected' AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND decision_reason IS NOT NULL AND length(btrim(decision_reason)) > 0 AND final_payload IS NULL AND applied_at IS NULL)
    OR (state = 'approved' AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND final_payload IS NOT NULL AND approved_hash IS NOT NULL AND applied_at IS NULL)
    OR (state = 'applied' AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND final_payload IS NOT NULL AND approved_hash IS NOT NULL AND applied_at IS NOT NULL))
);
CREATE INDEX ai_proposal_state_idx ON nemura.ai_proposal (organisation_id, state, created_at);

-- +goose StatementBegin
CREATE FUNCTION nemura.ai_proposal_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A proposal is evidence: it is decided and applied, never deleted' USING ERRCODE = 'NM178';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'pending' THEN
      RAISE EXCEPTION 'A proposal starts pending' USING ERRCODE = 'NM178';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.task_type IS DISTINCT FROM OLD.task_type OR NEW.task_version IS DISTINCT FROM OLD.task_version OR NEW.asker IS DISTINCT FROM OLD.asker OR NEW.payload IS DISTINCT FROM OLD.payload
     OR NEW.payload_hash IS DISTINCT FROM OLD.payload_hash OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
     OR NEW.model_id IS DISTINCT FROM OLD.model_id OR NEW.prompt_version IS DISTINCT FROM OLD.prompt_version OR NEW.rationale IS DISTINCT FROM OLD.rationale THEN
    RAISE EXCEPTION 'A proposal keeps what the assistant proposed: a human edits it into the final text' USING ERRCODE = 'NM178';
  END IF;
  IF NOT ((OLD.state = 'pending' AND NEW.state IN ('approved', 'rejected')) OR (OLD.state = 'approved' AND NEW.state = 'applied')) THEN
    RAISE EXCEPTION 'A proposal moves from pending to approved or rejected, and from approved to applied' USING ERRCODE = 'NM178';
  END IF;
  IF OLD.state = 'pending' AND NEW.state = 'approved' AND NEW.decided_at > OLD.expires_at THEN
    RAISE EXCEPTION 'A proposal that has waited 72 hours cannot be approved: ask again' USING ERRCODE = 'NM178';
  END IF;
  IF OLD.state = 'approved' AND (NEW.decided_by IS DISTINCT FROM OLD.decided_by OR NEW.decided_at IS DISTINCT FROM OLD.decided_at OR NEW.final_payload IS DISTINCT FROM OLD.final_payload OR NEW.approved_hash IS DISTINCT FROM OLD.approved_hash) THEN
    RAISE EXCEPTION 'What was approved is applied as approved' USING ERRCODE = 'NM178';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_proposal_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.ai_proposal FOR EACH ROW EXECUTE FUNCTION nemura.ai_proposal_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.ai_proposal DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('ai_proposal', 'id');
ALTER TABLE nemura.ai_proposal ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.ai_proposal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.ai_proposal USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.ai_proposal TO nemura_api, nemura_worker, nemura_readonly, nemura_executor;
-- The assistant's only write on people data is a proposal and its audit event: no SELECT, no UPDATE.
GRANT INSERT (id, task_type, task_version, asker, payload, payload_hash, rationale, model_id, prompt_version, created_at, expires_at) ON nemura.ai_proposal TO nemura_assistant;
GRANT INSERT (id, body) ON nemura.audit_event TO nemura_assistant;
GRANT UPDATE (state, decided_by, decided_at, decision_reason, final_payload, approved_hash) ON nemura.ai_proposal TO nemura_api;
GRANT UPDATE (state, applied_at) ON nemura.ai_proposal TO nemura_executor;
