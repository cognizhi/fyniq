-- The purge of the assistant's conversations (PRD-001 §11.9, AI-P8; p1-ai-assistant task 6.3, design D11).
--
-- ai_purge_conversations removes, in the organisation of the transaction, every conversation its person deleted and
-- every one whose last question was asked before p_before (its retention, retention.ai_conversations_days), with its
-- turns, their reads and their feedback. Only it removes them: it sets fyniq.ai_purge for its own work, which the guard
-- of 000369 reads, and sets it off again; the worker runs it and holds no DELETE of its own; the API and the AI's role
-- cannot run it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_purge_conversations(p_before timestamptz, OUT conversations integer, OUT turns integer)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org      uuid := fyniq.current_org();
  gone     uuid[];
  gone_turns uuid[];
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'The purge of AI conversations runs in an organisation''s transaction' USING ERRCODE = 'FY341';
  END IF;
  SELECT coalesce(array_agg(c.id), '{}') INTO gone FROM fyniq.ai_conversation c
   WHERE c.organisation_id = org
     AND (c.deleted_at IS NOT NULL
          OR coalesce((SELECT max(t.asked_at) FROM fyniq.ai_turn t WHERE t.organisation_id = org AND t.conversation_id = c.id), c.created_at) < p_before);
  SELECT coalesce(array_agg(t.id), '{}') INTO gone_turns FROM fyniq.ai_turn t WHERE t.organisation_id = org AND t.conversation_id = ANY(gone);
  PERFORM set_config('fyniq.ai_purge', 'on', true);
  DELETE FROM fyniq.ai_feedback f WHERE f.organisation_id = org AND f.turn_id = ANY(gone_turns);
  DELETE FROM fyniq.ai_tool_call x WHERE x.organisation_id = org AND x.turn_id = ANY(gone_turns);
  DELETE FROM fyniq.ai_turn t WHERE t.organisation_id = org AND t.id = ANY(gone_turns);
  GET DIAGNOSTICS turns = ROW_COUNT;
  DELETE FROM fyniq.ai_conversation c WHERE c.organisation_id = org AND c.id = ANY(gone);
  GET DIAGNOSTICS conversations = ROW_COUNT;
  PERFORM set_config('fyniq.ai_purge', 'off', true);
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.ai_purge_conversations(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.ai_purge_conversations(timestamptz) TO fyniq_worker;
