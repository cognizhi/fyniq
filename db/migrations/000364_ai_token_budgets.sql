-- Token budgets (PRD-001 AI-13, AI-86; p1-ai-providers task 6.2, design D8, D9).
--
-- AI-13 keeps a token budget for the organisation and for each person, beside the budgets in the base currency: a month's
-- tokens, which bound a subscription or a self-hosted server as well as a priced connection. They are ai_budget's scopes
-- tokens_organisation and tokens_person, whole numbers of tokens, set by the Owner as the others are. Each reservation
-- now holds the most tokens its attempt could use and, once settled, the tokens it used; ai_reserve reserves them under
-- the same locks as the cost, refusing what a token budget cannot cover (FY338) and raising its 80% alert once a month,
-- and ai_settle settles them with the cost. An unknown outcome keeps its tokens whole until the provider's usage
-- reconciles it. The functions stay the API's and the worker's, never the AI's (CMP-AI-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_budget DROP CONSTRAINT ai_budget_scope_known;
ALTER TABLE fyniq.ai_budget ADD CONSTRAINT ai_budget_scope_known
  CHECK (scope IN ('organisation', 'agent', 'connection', 'person', 'tokens_organisation', 'tokens_person')) NOT VALID;
ALTER TABLE fyniq.ai_budget DROP CONSTRAINT ai_budget_subject;
ALTER TABLE fyniq.ai_budget ADD CONSTRAINT ai_budget_subject CHECK (CASE scope WHEN 'organisation' THEN subject = ''
    WHEN 'tokens_organisation' THEN subject = ''
    WHEN 'agent' THEN subject ~ '^[a-z][a-z0-9_.]{1,59}$'
    WHEN 'connection' THEN subject ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ELSE subject ~ '^user:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' END) NOT VALID;
-- A token budget is a whole number of tokens.
ALTER TABLE fyniq.ai_budget ADD CONSTRAINT ai_budget_tokens_whole
  CHECK (scope NOT IN ('tokens_organisation', 'tokens_person') OR amount = trunc(amount)) NOT VALID;

ALTER TABLE fyniq.ai_reservation ADD COLUMN tokens bigint;        -- the most tokens the attempt could use; NULL before 000364
ALTER TABLE fyniq.ai_reservation ADD COLUMN actual_tokens bigint; -- the tokens it used, once settled
ALTER TABLE fyniq.ai_reservation ADD CONSTRAINT ai_reservation_tokens
  CHECK (tokens >= 0 AND actual_tokens >= 0 AND (actual_tokens IS NULL OR state = 'settled')) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.ai_budget_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An AI budget and its reservations are kept: a budget is removed by clearing its amount' USING ERRCODE = 'FY336';
  END IF;
  IF TG_TABLE_NAME = 'ai_reservation' THEN
    IF (NEW.organisation_id, NEW.id, NEW.run_id, NEW.step, NEW.agent, NEW.connection_id, NEW.person, NEW.month, NEW.amount, NEW.tokens,
        NEW.reserved_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.run_id, OLD.step, OLD.agent, OLD.connection_id, OLD.person, OLD.month, OLD.amount,
                         OLD.tokens, OLD.reserved_at)
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

DROP FUNCTION fyniq.ai_reserve(uuid, integer, text, uuid, text, text, numeric);
DROP FUNCTION fyniq.ai_settle(uuid, numeric, boolean);

-- ai_reserve reserves an attempt's most cost and most tokens against every budget that applies, locking them, and
-- returns the reservation and the budgets it took to 80% this month. FY337 is a connection's budget spent, so the next
-- step may still serve; FY338 is the organisation's, the agent's or the person's budget spent, in money or in tokens, or
-- a cost or token count not known while such a budget applies, so the call stops.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_reserve(p_run uuid, p_step integer, p_agent text, p_connection uuid, p_person text, p_month text, p_amount numeric,
  p_tokens bigint) RETURNS jsonb
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
              AND (scope IN ('organisation', 'tokens_organisation') OR (scope = 'agent' AND subject = p_agent)
                   OR (scope = 'connection' AND subject = p_connection::text) OR (scope IN ('person', 'tokens_person') AND subject = p_person))
            ORDER BY id FOR UPDATE LOOP
    IF b.scope IN ('tokens_organisation', 'tokens_person') THEN
      IF p_tokens IS NULL THEN
        RAISE EXCEPTION 'The call''s tokens are not known, so the % token budget cannot be applied', b.scope USING ERRCODE = 'FY338';
      END IF;
      SELECT coalesce(sum(CASE WHEN r.state = 'settled' THEN coalesce(r.actual_tokens, 0) ELSE coalesce(r.tokens, 0) END), 0) INTO used
        FROM fyniq.ai_reservation r
       WHERE r.organisation_id = b.organisation_id AND r.month = p_month AND (b.scope = 'tokens_organisation' OR r.person = b.subject);
      IF used + p_tokens > b.amount THEN
        RAISE EXCEPTION 'The % AI token budget for % is spent', CASE b.scope WHEN 'tokens_organisation' THEN 'organisation''s'
          ELSE 'person''s' END, p_month USING ERRCODE = 'FY338';
      END IF;
      IF (used + p_tokens) * 100 >= b.amount * 80 AND coalesce(b.alerted, '') < p_month THEN
        UPDATE fyniq.ai_budget SET alerted = p_month WHERE organisation_id = b.organisation_id AND id = b.id;
        alerts := alerts || jsonb_build_object('scope', b.scope, 'subject', b.subject, 'amount', b.amount::text, 'used', (used + p_tokens)::text);
      END IF;
      CONTINUE;
    END IF;
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
  INSERT INTO fyniq.ai_reservation (organisation_id, run_id, step, agent, connection_id, person, month, amount, tokens)
  VALUES (fyniq.current_org(), p_run, p_step, p_agent, p_connection, p_person, p_month, p_amount, p_tokens)
  RETURNING id INTO out_id;
  RETURN jsonb_build_object('id', out_id, 'alerts', alerts);
END
$$;
-- +goose StatementEnd

-- ai_settle settles a reservation to what the attempt cost and the tokens it used, or keeps it whole as unknown until the
-- provider's usage is reconciled.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_settle(p_reservation uuid, p_actual numeric, p_actual_tokens bigint, p_known boolean) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.ai_reservation
     SET state = CASE WHEN p_known THEN 'settled' ELSE 'unknown' END, actual = CASE WHEN p_known THEN p_actual END,
         actual_tokens = CASE WHEN p_known THEN p_actual_tokens END, settled_at = now()
   WHERE organisation_id = fyniq.current_org() AND id = p_reservation AND (state = 'held' OR (state = 'unknown' AND p_known));
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The reservation is not one this organisation holds or keeps unknown' USING ERRCODE = 'FY336';
  END IF;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.ai_reserve(uuid, integer, text, uuid, text, text, numeric, bigint),
  fyniq.ai_settle(uuid, numeric, bigint, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.ai_reserve(uuid, integer, text, uuid, text, text, numeric, bigint),
  fyniq.ai_settle(uuid, numeric, bigint, boolean) TO fyniq_api, fyniq_worker;
