-- AI budgets as admission control (PRD-001 AI-77, AI-86, AI-13; p1-ai-providers task 5.3, design D8).
--
-- ai_budget is a monthly budget in the organisation's base currency for the whole organisation, an agent, a connection
-- or a person, set by the Owner. ai_reservation is each attempt's reservation against them: before an attempt is sent
-- the router reserves its most it could cost, in one function that locks every budget that applies and refuses what any
-- of them cannot cover; after the attempt it settles the reservation to what the attempt cost, or, when the outcome is
-- unknown (a timeout, a broken stream), keeps the whole reservation until the provider's usage reconciles it. Probes,
-- evals and retries reserve like any call. Reaching 80% of a budget in a month raises its alert once; at 100% nothing is
-- admitted that the budget cannot cover. Reservations are written and settled only through ai_reserve and ai_settle,
-- which the API and the worker may run in their own organisation, never the AI's role (CMP-AI-01), and are never
-- removed (FY336). ai_call gains each attempt's cost in the base currency, and keeps probes, the probe agent's calls
-- outside any route, as route version 0.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_budget (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  scope           text           NOT NULL,
  subject         text           NOT NULL DEFAULT '', -- the agent, the connection or the person (user:<id>); '' for the organisation
  amount          numeric(20,4),                      -- per calendar month in Singapore, in the base currency; NULL when removed
  version         integer        NOT NULL DEFAULT 1,
  alerted         text,                               -- the month whose 80% alert it raised, such as 2026-11
  set_by          text           NOT NULL,
  set_at          timestamptz    NOT NULL,
  CONSTRAINT ai_budget_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_budget_one UNIQUE (organisation_id, scope, subject),
  CONSTRAINT ai_budget_scope_known CHECK (scope IN ('organisation', 'agent', 'connection', 'person')),
  CONSTRAINT ai_budget_subject CHECK (CASE scope WHEN 'organisation' THEN subject = ''
    WHEN 'agent' THEN subject ~ '^[a-z][a-z0-9_.]{1,59}$'
    WHEN 'connection' THEN subject ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ELSE subject ~ '^user:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' END),
  -- An exact amount above zero, never NaN (golden rule 1, QA-ENG-05).
  CONSTRAINT ai_budget_amount CHECK (amount <> 'NaN' AND amount > 0),
  CONSTRAINT ai_budget_version_positive CHECK (version >= 1),
  CONSTRAINT ai_budget_alerted_format CHECK (alerted ~ '^[0-9]{4}-[0-9]{2}$'),
  CONSTRAINT ai_budget_set_by_format CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.ai_reservation (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  run_id          uuid           NOT NULL,
  step            integer        NOT NULL,
  agent           text           NOT NULL,
  connection_id   uuid           NOT NULL,
  person          text           NOT NULL, -- who the call ran for: user:<id>, or system:<job>
  month           text           NOT NULL, -- the calendar month in Singapore it counts against, such as 2026-11
  amount          numeric(43,26),          -- the most the attempt could cost, in the base currency, exact: a cost of up to 16
                                           -- places at a rate of up to 10; NULL when it could not be converted
  actual          numeric(43,26),          -- what it cost, in the base currency, once settled
  state           text           NOT NULL DEFAULT 'held',
  reserved_at     timestamptz    NOT NULL DEFAULT now(),
  settled_at      timestamptz,
  CONSTRAINT ai_reservation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_reservation_agent_fkey FOREIGN KEY (agent) REFERENCES fyniq.ai_agent (id),
  CONSTRAINT ai_reservation_connection_fkey FOREIGN KEY (organisation_id, connection_id) REFERENCES fyniq.ai_connection (organisation_id, id),
  CONSTRAINT ai_reservation_step CHECK (step BETWEEN 1 AND 5),
  CONSTRAINT ai_reservation_month_format CHECK (month ~ '^[0-9]{4}-[0-9]{2}$'),
  CONSTRAINT ai_reservation_person_format CHECK (person ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT ai_reservation_state_known CHECK (state IN ('held', 'settled', 'unknown')),
  CONSTRAINT ai_reservation_amounts CHECK (amount <> 'NaN' AND amount >= 0 AND actual <> 'NaN' AND actual >= 0),
  CONSTRAINT ai_reservation_settled CHECK ((state = 'settled') = (actual IS NOT NULL) AND (state = 'held') = (settled_at IS NULL))
);
CREATE INDEX ai_reservation_by_month ON fyniq.ai_reservation (organisation_id, month);
CREATE INDEX ai_reservation_by_run ON fyniq.ai_reservation (organisation_id, run_id);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_budget_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An AI budget and its reservations are kept: a budget is removed by clearing its amount' USING ERRCODE = 'FY336';
  END IF;
  IF TG_TABLE_NAME = 'ai_reservation' THEN
    IF (NEW.organisation_id, NEW.id, NEW.run_id, NEW.step, NEW.agent, NEW.connection_id, NEW.person, NEW.month, NEW.amount, NEW.reserved_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.run_id, OLD.step, OLD.agent, OLD.connection_id, OLD.person, OLD.month, OLD.amount,
                         OLD.reserved_at)
       OR NOT ((OLD.state = 'held' AND NEW.state IN ('settled', 'unknown')) OR (OLD.state = 'unknown' AND NEW.state = 'settled')) THEN
      RAISE EXCEPTION 'A reservation moves only from held to settled or unknown, or from unknown to settled' USING ERRCODE = 'FY336';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.scope, NEW.subject) IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.scope, OLD.subject)
     OR (NEW.version <> OLD.version AND NEW.version <> OLD.version + 1) THEN
    RAISE EXCEPTION 'An AI budget keeps its scope, and each change is its next version' USING ERRCODE = 'FY336';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_budget_guard BEFORE UPDATE OR DELETE ON fyniq.ai_budget
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_budget_guard();
CREATE TRIGGER ai_reservation_guard BEFORE UPDATE OR DELETE ON fyniq.ai_reservation
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_budget_guard();
CREATE TRIGGER ai_budget_no_truncate BEFORE TRUNCATE ON fyniq.ai_budget
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_reservation_no_truncate BEFORE TRUNCATE ON fyniq.ai_reservation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_budget
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_budget', 'id');

ALTER TABLE fyniq.ai_budget ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_budget FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_budget
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_reservation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_reservation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_reservation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_budget, fyniq.ai_reservation FROM PUBLIC;
GRANT SELECT ON fyniq.ai_budget, fyniq.ai_reservation TO fyniq_api, fyniq_worker, fyniq_readonly;
-- The Owner sets a budget through the API.
GRANT INSERT (scope, subject, amount, set_by, set_at), UPDATE (amount, version, set_by, set_at) ON fyniq.ai_budget TO fyniq_api;

-- ai_reserve reserves an attempt's most against every budget that applies, locking them, and returns the reservation and
-- the budgets it took to 80% this month. FY337 is a connection's budget spent, so the next step may still serve; FY338
-- is the organisation's, the agent's or the person's spent, or a cost that could not be put in the base currency while
-- a budget applies, so the call stops.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_reserve(p_run uuid, p_step integer, p_agent text, p_connection uuid, p_person text, p_month text, p_amount numeric)
  RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  b record;
  used numeric;
  out_id uuid;
  alerts jsonb := '[]'::jsonb;
BEGIN
  IF fyniq.current_org() IS NULL THEN
    RAISE EXCEPTION 'A reservation is made only in an organisation' USING ERRCODE = 'FY336';
  END IF;
  FOR b IN SELECT * FROM fyniq.ai_budget
            WHERE organisation_id = fyniq.current_org() AND amount IS NOT NULL
              AND (scope = 'organisation' OR (scope = 'agent' AND subject = p_agent)
                   OR (scope = 'connection' AND subject = p_connection::text) OR (scope = 'person' AND subject = p_person))
            ORDER BY id FOR UPDATE LOOP
    IF p_amount IS NULL THEN
      RAISE EXCEPTION 'The call''s cost could not be put in the base currency, so the % budget cannot be applied', b.scope
        USING ERRCODE = 'FY338', HINT = 'Enter the day''s exchange rate for the provider''s currency.';
    END IF;
    SELECT coalesce(sum(CASE WHEN r.state = 'settled' THEN r.actual ELSE coalesce(r.amount, 0) END), 0) INTO used
      FROM fyniq.ai_reservation r
     WHERE r.organisation_id = b.organisation_id AND r.month = p_month
       AND (b.scope = 'organisation' OR (b.scope = 'agent' AND r.agent = b.subject)
            OR (b.scope = 'connection' AND r.connection_id::text = b.subject) OR (b.scope = 'person' AND r.person = b.subject));
    IF used + p_amount > b.amount THEN
      IF b.scope = 'connection' THEN
        RAISE EXCEPTION 'The connection''s AI budget for % is spent', p_month USING ERRCODE = 'FY337';
      END IF;
      RAISE EXCEPTION 'The % AI budget for % is spent', CASE b.scope WHEN 'organisation' THEN 'organisation''s' WHEN 'agent' THEN 'agent''s'
        ELSE 'person''s' END, p_month USING ERRCODE = 'FY338';
    END IF;
    IF (used + p_amount) * 100 >= b.amount * 80 AND coalesce(b.alerted, '') < p_month THEN
      UPDATE fyniq.ai_budget SET alerted = p_month WHERE organisation_id = b.organisation_id AND id = b.id;
      alerts := alerts || jsonb_build_object('scope', b.scope, 'subject', b.subject, 'amount', b.amount::text, 'used', (used + p_amount)::text);
    END IF;
  END LOOP;
  INSERT INTO fyniq.ai_reservation (organisation_id, run_id, step, agent, connection_id, person, month, amount)
  VALUES (fyniq.current_org(), p_run, p_step, p_agent, p_connection, p_person, p_month, p_amount)
  RETURNING id INTO out_id;
  RETURN jsonb_build_object('id', out_id, 'alerts', alerts);
END
$$;
-- +goose StatementEnd

-- ai_settle settles a reservation to what the attempt cost, or keeps it whole as unknown until the provider's usage is
-- reconciled.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_settle(p_reservation uuid, p_actual numeric, p_known boolean) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.ai_reservation
     SET state = CASE WHEN p_known THEN 'settled' ELSE 'unknown' END, actual = CASE WHEN p_known THEN p_actual END,
         settled_at = now()
   WHERE organisation_id = fyniq.current_org() AND id = p_reservation AND (state = 'held' OR (state = 'unknown' AND p_known));
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The reservation is not one this organisation holds or keeps unknown' USING ERRCODE = 'FY336';
  END IF;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.ai_reserve(uuid, integer, text, uuid, text, text, numeric), fyniq.ai_settle(uuid, numeric, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.ai_reserve(uuid, integer, text, uuid, text, text, numeric), fyniq.ai_settle(uuid, numeric, boolean)
  TO fyniq_api, fyniq_worker;

-- ------------------------------------------------------------------ probes are calls too, outside any route
ALTER TABLE fyniq.ai_call DROP CONSTRAINT ai_call_steps;
ALTER TABLE fyniq.ai_call ADD CONSTRAINT ai_call_steps CHECK (route_version >= 0 AND step BETWEEN 1 AND 5
  AND (fell_back_from IS NULL OR fell_back_from BETWEEN 1 AND step - 1) AND (route_version > 0 OR (agent = 'probe' AND step = 1))) NOT VALID;

-- ------------------------------------------------------------------ each attempt's cost in the base currency
ALTER TABLE fyniq.ai_call ADD COLUMN cost_base numeric(43,26); -- exact: the cost at the day's rate, never rounded
ALTER TABLE fyniq.ai_call ADD CONSTRAINT ai_call_cost_base CHECK (cost_base <> 'NaN' AND cost_base >= 0 AND (cost_base IS NULL OR cost IS NOT NULL)) NOT VALID;
DROP FUNCTION fyniq.record_ai_call(uuid, text, integer, integer, integer, uuid, text, text, text, text, text[], text, text,
  bigint, bigint, bigint, bigint, bigint, numeric, text, uuid, integer, timestamptz);
-- +goose StatementBegin
CREATE FUNCTION fyniq.record_ai_call(p_run uuid, p_agent text, p_route_version integer, p_step integer, p_fell_back_from integer,
  p_connection uuid, p_model_requested text, p_model_reported text, p_version_reported text, p_actor text, p_classes text[],
  p_outcome text, p_detail text, p_input bigint, p_output bigint, p_cache_read bigint, p_cache_write bigint, p_reasoning bigint,
  p_cost numeric, p_currency text, p_price uuid, p_cost_base numeric, p_latency_ms integer, p_started_at timestamptz) RETURNS uuid
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
    cost, currency, price_id, cost_base, latency_ms, started_at)
  VALUES (fyniq.current_org(), p_run, p_agent, p_route_version, p_step, p_fell_back_from, p_connection, p_model_requested, p_model_reported,
    p_version_reported, p_actor, p_classes, p_outcome, p_detail, p_input, p_output, p_cache_read, p_cache_write, p_reasoning,
    p_cost, p_currency, p_price, p_cost_base, p_latency_ms, p_started_at)
  RETURNING id INTO out_id;
  RETURN out_id;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.record_ai_call(uuid, text, integer, integer, integer, uuid, text, text, text, text, text[], text, text,
  bigint, bigint, bigint, bigint, bigint, numeric, text, uuid, numeric, integer, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.record_ai_call(uuid, text, integer, integer, integer, uuid, text, text, text, text, text[], text, text,
  bigint, bigint, bigint, bigint, bigint, numeric, text, uuid, numeric, integer, timestamptz) TO fyniq_api, fyniq_worker;
