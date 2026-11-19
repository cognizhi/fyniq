-- The purge of the AI apps' call trail after its retention (PRD-001 AI-44, §11.9; p1-mcp-first-party task 8.1).
--
-- mcp_call is append-only (000583). Its calls are also in the audit trail, which keeps the evidence for its own retention; the trail
-- of calls goes with the assistant's conversations, after retention.ai_conversations_days. mcp_purge_calls removes, in the
-- organisation of the transaction, every call made before p_before. Only it removes them: it sets fyniq.mcp_purge for its own work,
-- which the trail's guard reads, and sets it off again; the worker runs it and holds no DELETE of its own; the API and the MCP
-- service cannot run it. An update, and a delete outside the purge, are refused as before.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_call_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('fyniq.mcp_purge', true) = 'on' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION '% never changes or goes, but by the purge after its retention', TG_TABLE_NAME USING ERRCODE = 'FY189',
    HINT = 'Add a new one: the latest is in force (FR-NTF-02).';
END
$$;
-- +goose StatementEnd

DROP TRIGGER mcp_call_guard ON fyniq.mcp_call;
CREATE TRIGGER mcp_call_guard BEFORE UPDATE OR DELETE ON fyniq.mcp_call
  FOR EACH ROW EXECUTE FUNCTION fyniq.mcp_call_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_purge_calls(p_before timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.current_org();
  n   integer;
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'The purge of the AI apps'' calls runs in an organisation''s transaction' USING ERRCODE = 'FY341';
  END IF;
  PERFORM set_config('fyniq.mcp_purge', 'on', true);
  DELETE FROM fyniq.mcp_call c WHERE c.organisation_id = org AND c.at < p_before;
  GET DIAGNOSTICS n = ROW_COUNT;
  PERFORM set_config('fyniq.mcp_purge', 'off', true);
  RETURN n;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.mcp_purge_calls(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.mcp_purge_calls(timestamptz) TO fyniq_worker;
