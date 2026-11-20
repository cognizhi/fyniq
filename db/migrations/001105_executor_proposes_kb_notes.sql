-- The executor adds a knowledge-base note (PRD-001 AI-59, Appendix E kb.propose_page; p2-ai-expansion task 3.2, owner's decision of 23 Nov 2026).
--
-- An approved kb.propose_page proposal writes ONE note, as the approving person: a source an Owner or Administrator compiles into a page by the knowledge
-- base's own steps. The grant is the tightest the command needs: SELECT (to see that a key is free) and INSERT of a note's columns, nothing on an update or a
-- delete, nothing on a page, a version, a snapshot or a finding. A guard holds the executor to a NOTE (never a policy or a decision), of the general data
-- class, under a key that begins "proposed.", so the AI's proposals are easy to tell apart and never take a person's key. Needs human review (grants).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.kb_note TO fyniq_executor;
GRANT INSERT (key, kind, title, body, data_class, created_by, created_at, updated_by, updated_at) ON fyniq.kb_note TO fyniq_executor;

-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_note_executor_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_executor' AND (NEW.kind <> 'note' OR NEW.data_class <> 'general' OR NEW.key NOT LIKE 'proposed.%') THEN
    RAISE EXCEPTION 'The executor adds a general note under a key that begins proposed., never a policy, a decision or another class' USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER kb_note_executor_guard BEFORE INSERT ON fyniq.kb_note FOR EACH ROW EXECUTE FUNCTION fyniq.kb_note_executor_guard();

-- +goose Down
DROP TRIGGER kb_note_executor_guard ON fyniq.kb_note;
DROP FUNCTION fyniq.kb_note_executor_guard();
