-- What an approved proposal becomes (PRD-002 NM-AI-02, NM-AI-04; p1-nemura-assistant-external slice 3).
--
-- The assistant drafts; a human approves; the executor attaches the approved text to the request as a draft for the person who decides it. The executor never checks, issues or signs a letter and never
-- replies to a flexible-work request: those are the people team's own commands, and a draft only sits beside the request they work on. A draft is written once, from the approved text and its hash.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.ai_draft (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  proposal_id     uuid        NOT NULL,
  kind            text        NOT NULL,
  request_id      uuid        NOT NULL,
  payload         jsonb       NOT NULL,   -- the approved text, exactly
  approved_hash   bytea       NOT NULL,
  applied_at      timestamptz NOT NULL,
  CONSTRAINT ai_draft_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_draft_proposal_fkey FOREIGN KEY (organisation_id, proposal_id) REFERENCES nemura.ai_proposal (organisation_id, id),
  CONSTRAINT ai_draft_once UNIQUE (organisation_id, proposal_id),
  CONSTRAINT ai_draft_kind CHECK (kind IN ('letter', 'flex_reply')),
  CONSTRAINT ai_draft_hash CHECK (octet_length(approved_hash) = 32)
);
CREATE INDEX ai_draft_request_idx ON nemura.ai_draft (organisation_id, request_id);
CREATE TRIGGER ai_draft_append_only BEFORE UPDATE OR DELETE ON nemura.ai_draft FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A draft');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.ai_draft DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('ai_draft', 'id');
ALTER TABLE nemura.ai_draft ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.ai_draft FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.ai_draft USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.ai_draft TO nemura_api, nemura_worker, nemura_readonly;
-- The executor attaches a draft and reads the request it is for: it holds no write on a request.
GRANT INSERT (id, proposal_id, kind, request_id, payload, approved_hash, applied_at) ON nemura.ai_draft TO nemura_executor;
GRANT SELECT ON nemura.letter_request, nemura.flex_request TO nemura_executor;
