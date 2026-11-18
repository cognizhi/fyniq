-- An approval of an AI proposal's items, signed (PRD-001 AI-32, AI-P5; p1-ai-proposals-core task 4.2, design D8).
--
-- An approver of a batch proposal may approve some of its items: the selection, re-hashed, is signed with the request's
-- envelope (approval_signature.selection_sha256), an ai_proposal's approval's alone. The proposal is approved only as
-- its signer signed: what is approved hashes to the payload's hash, or to the selection's. A selection never changes
-- once signed.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_signature ADD COLUMN selection_sha256 text;
ALTER TABLE fyniq.approval_signature ADD CONSTRAINT approval_signature_selection
  CHECK (selection_sha256 IS NULL OR selection_sha256 ~ '^[0-9a-f]{64}$') NOT VALID;
GRANT INSERT (selection_sha256) ON fyniq.approval_signature TO fyniq_api;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.approval_signature_check() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r fyniq.approval_request;
  c record;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL
       OR (NEW.request_id, NEW.step, NEW.signer, NEW.decision, NEW.envelope_hash, NEW.mfa_method, NEW.mfa_at, NEW.signed_at, NEW.reason,
           NEW.passkey_challenge_id, NEW.passkey_id, NEW.authenticator_data, NEW.client_data, NEW.assertion_signature, NEW.selection_sha256)
          IS DISTINCT FROM (OLD.request_id, OLD.step, OLD.signer, OLD.decision, OLD.envelope_hash, OLD.mfa_method, OLD.mfa_at, OLD.signed_at,
           OLD.reason, OLD.passkey_challenge_id, OLD.passkey_id, OLD.authenticator_data, OLD.client_data, OLD.assertion_signature,
           OLD.selection_sha256) THEN
      RAISE EXCEPTION 'A signed approval is never changed, only voided once' USING ERRCODE = 'FY102';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  IF r.envelope_hash IS DISTINCT FROM NEW.envelope_hash THEN
    RAISE EXCEPTION 'The signed hash is not the hash of the request''s envelope' USING ERRCODE = 'FY100',
      HINT = 'Sign the envelope the request holds, exactly (FR-APR-03, FR-APR-10).';
  END IF;
  -- The person who asked may sign their own tier-1 AI proposal (AI-10, AI-89): ai_proposal's alone.
  IF r.requested_by = NEW.signer AND NOT (r.object_type = 'ai_proposal' AND EXISTS (
       SELECT 1 FROM fyniq.ai_proposal p
        WHERE p.organisation_id = r.organisation_id AND p.approval_request_id = r.id AND p.risk_tier = 1
          AND p.requested_by = NEW.signer)) THEN
    RAISE EXCEPTION 'The requester never signs their own request' USING ERRCODE = 'FY101',
      HINT = 'Another member who holds the approval right signs it (FM-1).';
  END IF;
  -- A selection is the items of an AI proposal its approver approves, re-hashed (AI-32): an ai_proposal's approval's alone.
  IF NEW.selection_sha256 IS NOT NULL AND (r.object_type <> 'ai_proposal' OR NEW.decision <> 'approve') THEN
    RAISE EXCEPTION 'Only an approval of an AI proposal selects its items' USING ERRCODE = 'FY342';
  END IF;
  IF r.status <> 'pending' OR NEW.signed_at >= r.expires_at OR NEW.signed_at < r.requested_at OR NEW.voided_at IS NOT NULL THEN
    RAISE EXCEPTION 'The request is not pending, or it expired at %', r.expires_at USING ERRCODE = 'FY103',
      HINT = 'Ask for the change again (FR-APR-05).';
  END IF;
  IF NEW.step > jsonb_array_length(r.policy_steps)
     OR NOT fyniq.approval_steps_met(r.organisation_id, r.id, r.envelope_hash, r.policy_steps, NEW.step - 1)
     OR (SELECT count(*) FROM fyniq.approval_signature s
          WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.step = NEW.step AND s.decision = 'approve'
            AND s.voided_at IS NULL AND s.envelope_hash = r.envelope_hash) >= (r.policy_steps -> (NEW.step - 1) ->> 'quorum')::integer THEN
    RAISE EXCEPTION 'Step % is not the request''s current step', NEW.step USING ERRCODE = 'FY106',
      HINT = 'Sign the first step whose quorum is not met yet; the steps are signed in order (FR-APR-01).';
  END IF;
  IF NEW.mfa_method = 'passkey' THEN
    SELECT * INTO c FROM fyniq.webauthn_approval_proof(NEW.passkey_challenge_id);
    IF c.organisation_id IS DISTINCT FROM NEW.organisation_id OR c.request_id IS DISTINCT FROM NEW.request_id
       OR c.envelope_hash IS DISTINCT FROM NEW.envelope_hash OR c.user_id IS DISTINCT FROM NEW.signer
       OR c.answered_by IS DISTINCT FROM NEW.passkey_id OR c.taken_at IS NULL OR NEW.signed_at - c.taken_at > interval '5 minutes'
       OR EXISTS (SELECT 1 FROM fyniq.approval_signature s
                   WHERE s.organisation_id = NEW.organisation_id AND s.passkey_challenge_id = NEW.passkey_challenge_id) THEN
      RAISE EXCEPTION 'A passkey-signed approval is the signer''s answer to a challenge over this request''s envelope'
        USING ERRCODE = 'FY142', HINT = 'Ask for a new challenge for this request and confirm it''s you again (FR-APR-03).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

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
