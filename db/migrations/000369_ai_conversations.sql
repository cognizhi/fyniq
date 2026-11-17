-- The assistant's conversations (PRD-001 AI-12, AI-P8, FR-UX-17; p1-ai-assistant task 6.1, design D11).
--
-- ai_conversation is a person's conversation with the assistant, theirs alone; ai_turn each question in it and its
-- answer: the words, the segments and facts it cites, the prompt and its hash, the model calls' runs and what grounding
-- did; ai_tool_call each read a turn made, with an output hash and its counts; ai_feedback a person's thumbs up or down
-- with a reason, reviewed before it joins the golden set. Each goes only forward (FY341): a conversation is deleted
-- once, a turn answered once, feedback reviewed once; nothing is removed but by the purge after retention (task 6.3).
-- The API writes them in the turn's unit of work, audited; the AI's role holds nothing on them (CMP-AI-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_conversation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  title           text        NOT NULL DEFAULT '',
  created_at      timestamptz NOT NULL,
  deleted_at      timestamptz,
  CONSTRAINT ai_conversation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_conversation_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_conversation_title_length CHECK (length(title) <= 200),
  CONSTRAINT ai_conversation_deleted CHECK (deleted_at IS NULL OR deleted_at >= created_at)
);
CREATE INDEX ai_conversation_of_user ON fyniq.ai_conversation (organisation_id, user_id, created_at DESC);

CREATE TABLE fyniq.ai_turn (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  conversation_id uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  question        text        NOT NULL,
  context         jsonb,                                  -- what the person asked about (FR-UX-17)
  status          text        NOT NULL DEFAULT 'answering',
  code            text        NOT NULL DEFAULT '',        -- a refusal's or a failure's code
  answer          text        NOT NULL DEFAULT '',
  segments        jsonb       NOT NULL DEFAULT '[]',
  cited           text[]      NOT NULL DEFAULT '{}',      -- the facts the answer cites, by ID
  facts           jsonb       NOT NULL DEFAULT '[]',      -- every fact the turn read
  prompt          text        NOT NULL DEFAULT '',        -- chat@2
  prompt_sha256   text        NOT NULL DEFAULT '',
  runs            uuid[]      NOT NULL DEFAULT '{}',      -- the model calls' runs in ai_call
  input_tokens    bigint      NOT NULL DEFAULT 0,
  output_tokens   bigint      NOT NULL DEFAULT 0,
  regenerated     boolean     NOT NULL DEFAULT false,
  withheld        integer     NOT NULL DEFAULT 0,
  asked_at        timestamptz NOT NULL,
  answered_at     timestamptz,
  CONSTRAINT ai_turn_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_turn_conversation_fkey FOREIGN KEY (organisation_id, conversation_id) REFERENCES fyniq.ai_conversation (organisation_id, id),
  CONSTRAINT ai_turn_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_turn_question_length CHECK (length(question) BETWEEN 1 AND 4000),
  CONSTRAINT ai_turn_status CHECK (status IN ('answering', 'answered', 'refused', 'failed') AND (status = 'answering') = (answered_at IS NULL)),
  CONSTRAINT ai_turn_code CHECK (code ~ '^([A-Z][A-Z0-9-]{1,40})?$' AND (status IN ('refused', 'failed')) = (code <> '')),
  CONSTRAINT ai_turn_prompt CHECK (prompt ~ '^([a-z][a-z_]{1,30}@[1-9][0-9]{0,3})?$' AND prompt_sha256 ~ '^([0-9a-f]{64})?$'),
  CONSTRAINT ai_turn_counts CHECK (input_tokens >= 0 AND output_tokens >= 0 AND withheld >= 0),
  CONSTRAINT ai_turn_answer_length CHECK (length(answer) <= 40000),
  CONSTRAINT ai_turn_answered CHECK (answered_at IS NULL OR answered_at >= asked_at)
);
CREATE INDEX ai_turn_of_conversation ON fyniq.ai_turn (organisation_id, conversation_id, asked_at);
CREATE INDEX ai_turn_of_user ON fyniq.ai_turn (organisation_id, user_id, asked_at);

CREATE TABLE fyniq.ai_tool_call (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  turn_id         uuid        NOT NULL,
  n               integer     NOT NULL,                   -- its place in the turn, from 1
  tool            text        NOT NULL,
  version         integer     NOT NULL,
  input           jsonb       NOT NULL,
  output_sha256   text        NOT NULL DEFAULT '',        -- what the model read, hashed
  outcome         text        NOT NULL,
  code            text        NOT NULL DEFAULT '',
  facts           integer     NOT NULL DEFAULT 0,
  omitted         integer     NOT NULL DEFAULT 0,
  withheld        boolean     NOT NULL DEFAULT false,
  duration_ms     integer     NOT NULL DEFAULT 0,
  CONSTRAINT ai_tool_call_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_tool_call_turn_fkey FOREIGN KEY (organisation_id, turn_id) REFERENCES fyniq.ai_turn (organisation_id, id),
  CONSTRAINT ai_tool_call_place UNIQUE (organisation_id, turn_id, n),
  CONSTRAINT ai_tool_call_n CHECK (n BETWEEN 1 AND 100),
  CONSTRAINT ai_tool_call_tool CHECK (tool ~ '^[a-z_]+(\.[a-z_]+)?$' AND version >= 0),
  CONSTRAINT ai_tool_call_outcome CHECK (outcome IN ('read', 'refused') AND (outcome = 'refused') = (code <> '')),
  CONSTRAINT ai_tool_call_code CHECK (code ~ '^([A-Z][A-Z0-9-]{1,40})?$' AND output_sha256 ~ '^([0-9a-f]{64})?$'),
  CONSTRAINT ai_tool_call_counts CHECK (facts >= 0 AND omitted >= 0 AND duration_ms >= 0),
  CONSTRAINT ai_tool_call_input_size CHECK (pg_column_size(input) <= 16384)
);

CREATE TABLE fyniq.ai_feedback (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  turn_id         uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  rating          text        NOT NULL,
  reason          text        NOT NULL DEFAULT '',
  given_at        timestamptz NOT NULL,
  reviewed_by     uuid,
  reviewed_at     timestamptz,
  decision        text,                                   -- golden: it joins the golden set (QA-AI-05); dismissed
  CONSTRAINT ai_feedback_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_feedback_turn_fkey FOREIGN KEY (organisation_id, turn_id) REFERENCES fyniq.ai_turn (organisation_id, id),
  CONSTRAINT ai_feedback_once UNIQUE (organisation_id, turn_id, user_id),
  CONSTRAINT ai_feedback_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_feedback_reviewer_fkey FOREIGN KEY (reviewed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT ai_feedback_rating CHECK (rating IN ('up', 'down') AND length(reason) <= 1000),
  CONSTRAINT ai_feedback_review CHECK ((reviewed_at IS NULL) = (decision IS NULL) AND (reviewed_at IS NULL) = (reviewed_by IS NULL)
    AND (decision IS NULL OR decision IN ('golden', 'dismissed')) AND (reviewed_at IS NULL OR reviewed_at >= given_at))
);

-- Each record goes only forward; nothing is removed but by the purge after retention, which sets fyniq.ai_purge for its
-- own transaction (task 6.3).
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_conversation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_setting('fyniq.ai_purge', true) = 'on' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'An AI conversation''s record is removed only by the purge after retention' USING ERRCODE = 'FY341';
  END IF;
  CASE TG_TABLE_NAME
  WHEN 'ai_conversation' THEN
    IF (NEW.organisation_id, NEW.id, NEW.user_id, NEW.title, NEW.created_at)
         IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.user_id, OLD.title, OLD.created_at) OR OLD.deleted_at IS NOT NULL THEN
      RAISE EXCEPTION 'A conversation is only deleted, once' USING ERRCODE = 'FY341';
    END IF;
  WHEN 'ai_turn' THEN
    IF (NEW.organisation_id, NEW.id, NEW.conversation_id, NEW.user_id, NEW.question, NEW.context, NEW.asked_at)
         IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.conversation_id, OLD.user_id, OLD.question, OLD.context, OLD.asked_at)
         OR OLD.status <> 'answering' THEN
      RAISE EXCEPTION 'A turn is only answered, once' USING ERRCODE = 'FY341';
    END IF;
  WHEN 'ai_feedback' THEN
    IF (NEW.organisation_id, NEW.id, NEW.turn_id, NEW.user_id, NEW.rating, NEW.reason, NEW.given_at)
         IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.turn_id, OLD.user_id, OLD.rating, OLD.reason, OLD.given_at)
         OR OLD.reviewed_at IS NOT NULL THEN
      RAISE EXCEPTION 'Feedback is only reviewed, once' USING ERRCODE = 'FY341';
    END IF;
  ELSE
    RAISE EXCEPTION 'A tool call is kept as it ran' USING ERRCODE = 'FY341';
  END CASE;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER ai_conversation_guard BEFORE UPDATE OR DELETE ON fyniq.ai_conversation
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_conversation_guard();
CREATE TRIGGER ai_turn_guard BEFORE UPDATE OR DELETE ON fyniq.ai_turn
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_conversation_guard();
CREATE TRIGGER ai_tool_call_guard BEFORE UPDATE OR DELETE ON fyniq.ai_tool_call
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_conversation_guard();
CREATE TRIGGER ai_feedback_guard BEFORE UPDATE OR DELETE ON fyniq.ai_feedback
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_conversation_guard();
CREATE TRIGGER ai_conversation_no_truncate BEFORE TRUNCATE ON fyniq.ai_conversation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_turn_no_truncate BEFORE TRUNCATE ON fyniq.ai_turn
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_tool_call_no_truncate BEFORE TRUNCATE ON fyniq.ai_tool_call
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_feedback_no_truncate BEFORE TRUNCATE ON fyniq.ai_feedback
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- Every saved change is audited in its transaction (AUD-01): a conversation made or deleted, a turn answered, feedback
-- given or reviewed. A turn asked and the reads it made are recorded with its answer.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.ai_conversation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_conversation', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER UPDATE ON fyniq.ai_turn
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_turn', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.ai_feedback
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_feedback', 'id');

ALTER TABLE fyniq.ai_conversation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_conversation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_conversation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_turn ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_turn FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_turn
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_tool_call ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_tool_call FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_tool_call
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_feedback ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_feedback FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_feedback
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_conversation, fyniq.ai_turn, fyniq.ai_tool_call, fyniq.ai_feedback FROM PUBLIC;
GRANT SELECT ON fyniq.ai_conversation, fyniq.ai_turn, fyniq.ai_tool_call, fyniq.ai_feedback TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, title, created_at), UPDATE (deleted_at) ON fyniq.ai_conversation TO fyniq_api;
GRANT INSERT (conversation_id, user_id, question, context, asked_at),
  UPDATE (status, code, answer, segments, cited, facts, prompt, prompt_sha256, runs, input_tokens, output_tokens, regenerated, withheld, answered_at)
  ON fyniq.ai_turn TO fyniq_api;
GRANT INSERT (turn_id, n, tool, version, input, output_sha256, outcome, code, facts, omitted, withheld, duration_ms) ON fyniq.ai_tool_call TO fyniq_api;
GRANT INSERT (turn_id, user_id, rating, reason, given_at), UPDATE (reviewed_by, reviewed_at, decision) ON fyniq.ai_feedback TO fyniq_api;
