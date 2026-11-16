-- Every attempt of a model call (PRD-001 AI-76, AI-83, AI-87, AI-12; p1-ai-providers task 5.2, design D7, D9, D11).
--
-- ai_call keeps each attempt the router makes along an agent's route, as the AI activity log reads it: the run it
-- belongs to, the agent, the route's version and step and the step it fell back from, the connection, the model
-- requested and the model and version the provider reports (AI-87), who it ran for, the union of the data classes it
-- carried (AI-83), its outcome (served, skipped and why, busy, timed out, not found, refused, failed or stopped), the
-- tokens by kind, the cost exactly in the price's currency with the price version it used (AI-77, AI-85), and how long
-- it took. It is written only through record_ai_call, which the API and the worker may run in their own organisation,
-- and is never changed or removed (FY335). The AI's role may not run it: it writes nothing, through a function either
-- (CMP-AI-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_call (
  organisation_id    uuid           NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid           NOT NULL DEFAULT uuidv7(),
  run_id             uuid           NOT NULL, -- the run its attempts belong to
  agent              text           NOT NULL,
  route_version      integer        NOT NULL,
  step               integer        NOT NULL, -- the route's step, from 1
  fell_back_from     integer,                 -- the step before it, which failed or was skipped
  connection_id      uuid           NOT NULL,
  model_requested    text           NOT NULL,
  model_reported     text,
  version_reported   text,
  actor              text           NOT NULL, -- who it ran for: a person, or the system for a job
  classes            text[]         NOT NULL,
  outcome            text           NOT NULL,
  detail             text,
  input_tokens       bigint         NOT NULL DEFAULT 0,
  output_tokens      bigint         NOT NULL DEFAULT 0,
  cache_read_tokens  bigint         NOT NULL DEFAULT 0,
  cache_write_tokens bigint         NOT NULL DEFAULT 0,
  reasoning_tokens   bigint         NOT NULL DEFAULT 0,
  cost               numeric(30,16),          -- exact: tokens times a rate of at most ten places, per million (AI-77)
  currency           text,
  price_id           uuid,                    -- the price version it was priced with (AI-85)
  latency_ms         integer        NOT NULL DEFAULT 0,
  started_at         timestamptz    NOT NULL,
  recorded_at        timestamptz    NOT NULL DEFAULT now(),
  CONSTRAINT ai_call_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_call_agent_fkey FOREIGN KEY (agent) REFERENCES fyniq.ai_agent (id),
  CONSTRAINT ai_call_connection_fkey FOREIGN KEY (organisation_id, connection_id) REFERENCES fyniq.ai_connection (organisation_id, id),
  CONSTRAINT ai_call_price_fkey FOREIGN KEY (organisation_id, price_id) REFERENCES fyniq.ai_price (organisation_id, id),
  CONSTRAINT ai_call_currency_fkey FOREIGN KEY (currency) REFERENCES fyniq.currency (code),
  CONSTRAINT ai_call_steps CHECK (route_version >= 1 AND step BETWEEN 1 AND 5 AND (fell_back_from IS NULL OR fell_back_from BETWEEN 1 AND step - 1)),
  CONSTRAINT ai_call_models CHECK (model_requested ~ '^[!-~]+$' AND length(model_requested) <= 200 AND length(model_reported) <= 200
    AND length(version_reported) <= 200),
  CONSTRAINT ai_call_actor_format CHECK (actor ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT ai_call_classes CHECK (classes <@ ARRAY['know-how', 'business-figures', 'personal-data', 'payroll']::text[]),
  CONSTRAINT ai_call_outcome_known CHECK (outcome IN ('ok', 'skipped', 'busy', 'timeout', 'not_found', 'refused', 'failed', 'stopped')),
  CONSTRAINT ai_call_served CHECK ((outcome = 'ok') = (model_reported IS NOT NULL) AND (outcome = 'ok' OR detail IS NOT NULL)),
  CONSTRAINT ai_call_detail_length CHECK (length(detail) <= 500),
  CONSTRAINT ai_call_tokens CHECK (input_tokens >= 0 AND output_tokens >= 0 AND cache_read_tokens >= 0 AND cache_write_tokens >= 0
    AND reasoning_tokens >= 0 AND reasoning_tokens <= output_tokens),
  -- An exact cost, never NaN, in the currency of the price it used (golden rule 1, QA-ENG-05).
  CONSTRAINT ai_call_cost CHECK (cost <> 'NaN' AND cost >= 0 AND (cost IS NULL) = (currency IS NULL) AND (cost IS NULL OR price_id IS NOT NULL)),
  CONSTRAINT ai_call_latency CHECK (latency_ms >= 0)
);
CREATE INDEX ai_call_by_run ON fyniq.ai_call (organisation_id, run_id);
CREATE INDEX ai_call_by_agent ON fyniq.ai_call (organisation_id, agent, started_at DESC);
CREATE INDEX ai_call_by_connection ON fyniq.ai_call (organisation_id, connection_id, started_at DESC);
CREATE INDEX ai_call_by_price ON fyniq.ai_call (organisation_id, price_id);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_call_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A model call is kept as it was recorded' USING ERRCODE = 'FY335';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_call_guard BEFORE UPDATE OR DELETE ON fyniq.ai_call
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_call_guard();
CREATE TRIGGER ai_call_no_truncate BEFORE TRUNCATE ON fyniq.ai_call
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

ALTER TABLE fyniq.ai_call ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_call FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_call
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_call FROM PUBLIC;
GRANT SELECT ON fyniq.ai_call TO fyniq_api, fyniq_worker, fyniq_readonly;

-- record_ai_call keeps one attempt in the caller's organisation and returns its id: the one way to write the log.
-- +goose StatementBegin
CREATE FUNCTION fyniq.record_ai_call(p_run uuid, p_agent text, p_route_version integer, p_step integer, p_fell_back_from integer,
  p_connection uuid, p_model_requested text, p_model_reported text, p_version_reported text, p_actor text, p_classes text[],
  p_outcome text, p_detail text, p_input bigint, p_output bigint, p_cache_read bigint, p_cache_write bigint, p_reasoning bigint,
  p_cost numeric, p_currency text, p_price uuid, p_latency_ms integer, p_started_at timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  out_id uuid;
BEGIN
  IF fyniq.current_org() IS NULL THEN
    RAISE EXCEPTION 'A model call is recorded only in an organisation' USING ERRCODE = 'FY335';
  END IF;
  INSERT INTO fyniq.ai_call (organisation_id, run_id, agent, route_version, step, fell_back_from, connection_id, model_requested, model_reported,
    version_reported, actor, classes, outcome, detail, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    cost, currency, price_id, latency_ms, started_at)
  VALUES (fyniq.current_org(), p_run, p_agent, p_route_version, p_step, p_fell_back_from, p_connection, p_model_requested, p_model_reported,
    p_version_reported, p_actor, p_classes, p_outcome, p_detail, p_input, p_output, p_cache_read, p_cache_write, p_reasoning,
    p_cost, p_currency, p_price, p_latency_ms, p_started_at)
  RETURNING id INTO out_id;
  RETURN out_id;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.record_ai_call(uuid, text, integer, integer, integer, uuid, text, text, text, text, text[], text, text,
  bigint, bigint, bigint, bigint, bigint, numeric, text, uuid, integer, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.record_ai_call(uuid, text, integer, integer, integer, uuid, text, text, text, text, text[], text, text,
  bigint, bigint, bigint, bigint, bigint, numeric, text, uuid, integer, timestamptz) TO fyniq_api, fyniq_worker;
