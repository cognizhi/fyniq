-- The audit chain's head for every service (PRD-001 AUD-04; p1-audit-explorer slice 4, design D4).
--
-- A period's close and a reopening record the chain's head in their snapshot, read under the ledger lock (ENG-47) by
-- the API, which may not read the trail itself (000006). This function answers the transaction's organisation's head:
-- its sequence number, its hash and the head event's action, nothing else of the trail.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_chain_head(OUT seq bigint, OUT head_hash bytea, OUT action text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT c.seq, c.head_hash,
         coalesce((SELECT e.action FROM fyniq.audit_event e WHERE e.organisation_id = c.organisation_id AND e.seq = c.seq), '')
    FROM fyniq.audit_chain c
   WHERE c.organisation_id = fyniq.require_org()
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.audit_chain_head() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.audit_chain_head() TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
