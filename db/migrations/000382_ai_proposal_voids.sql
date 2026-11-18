-- A proposal that ends undecided voids its approval request (PRD-001 §9.5, AI-89; p1-ai-proposals-core design D9, task
-- 8.2).
--
-- When a Pending proposal is Superseded by a revision, or Expires, the database voids its pending approval request in
-- the same transaction, as it voids a setting's or a member's (migration 000030): the request can no longer be signed,
-- and voiding it voids its signatures. The unit of work that moved the proposal audits the void (require_audit).
-- The trigger runs as its owner, so neither the worker that expires a proposal nor the API that supersedes one gains a
-- grant to void requests: only a proposal's own move to Superseded or Expired, which its guard allows those roles alone,
-- voids its own request.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_proposal_voids() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.approval_void_pending('ai_proposal', NEW.id::text,
    CASE NEW.status WHEN 'superseded' THEN 'a revision superseded the proposal' ELSE 'the proposal expired undecided' END);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_proposal_voids AFTER UPDATE OF status ON fyniq.ai_proposal
  FOR EACH ROW WHEN (OLD.status = 'pending' AND NEW.status IN ('superseded', 'expired')) EXECUTE FUNCTION fyniq.ai_proposal_voids();
REVOKE ALL ON FUNCTION fyniq.ai_proposal_voids() FROM PUBLIC;
