-- Exchange rates (PRD-001 FR-GL-05, ENG-40, RND-RATE; rev 0.70; p1-multi-currency-fx tasks 1.1 to 1.4).
--
-- Two places hold rates, both as base currency per one unit of the foreign currency, converted from the quote under
-- RND-RATE: round(quote ÷ 100, 10) for a quote per 100 units, round(1 ÷ quote, 10) for a quote of foreign currency per
-- one unit of the base, half away from zero. The quote and its form stay beside the rate, and a CHECK keeps the three
-- consistent whoever writes them.
--
-- fx_feed_rate is the daily rate feed: one rate per base currency, currency and date, the same for every organisation.
-- It is a global table: no organisation_id and no grant. Services read it through the view fx_feed_rates, and only the
-- feed job writes it, through record_feed_rate. A feed rate never changes or goes (FY168): a second, different rate for
-- the same date is refused, so a changed source shows up instead of rewriting the past.
--
-- fx_rate is the organisation's own rates: a day's rate, which overrides the feed for that date, and the monthly table,
-- whose duplicates the cascade averages (ENG-40). A rate is never changed or deleted, only withdrawn once, with who did
-- it, when and why (FY167); at most one day's rate per currency and date is current. A rate is never of the base
-- currency, and a monthly rate is dated on its month's first day (FY166).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ the daily feed (global)
CREATE TABLE fyniq.fx_feed_rate (
  base_currency text            NOT NULL,
  currency      text            NOT NULL,
  on_date       date            NOT NULL,
  rate          numeric(20, 10) NOT NULL,  -- base per one unit of currency, under RND-RATE
  quote         numeric(20, 10) NOT NULL,  -- as the feed published it
  quote_form    text            NOT NULL,
  feed          text            NOT NULL,  -- the feed's name, such as MAS
  recorded_at   timestamptz     NOT NULL DEFAULT now(),
  CONSTRAINT fx_feed_rate_pkey PRIMARY KEY (base_currency, currency, on_date),
  CONSTRAINT fx_feed_rate_base_currency_fkey FOREIGN KEY (base_currency) REFERENCES fyniq.currency (code),
  CONSTRAINT fx_feed_rate_currency_fkey FOREIGN KEY (currency) REFERENCES fyniq.currency (code),
  CONSTRAINT fx_feed_rate_not_base CHECK (currency <> base_currency),
  CONSTRAINT fx_feed_rate_positive CHECK (rate <> 'NaN' AND rate > 0 AND quote <> 'NaN' AND quote > 0),
  CONSTRAINT fx_feed_rate_rnd_rate CHECK (
    (quote_form = 'base-per-unit' AND rate = quote) OR
    (quote_form = 'base-per-100' AND rate = round(quote / 100, 10)) OR
    (quote_form = 'foreign-per-base' AND rate = round(1::numeric(40, 30) / quote, 10))),
  CONSTRAINT fx_feed_rate_feed_format CHECK (feed ~ '^[A-Za-z][A-Za-z0-9 ._-]{0,39}$')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.fx_feed_rate_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A rate of the daily feed never changes or goes' USING ERRCODE = 'FY168',
    HINT = 'The feed keeps what it published; an organisation overrides a day''s rate with its own (FR-GL-05).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER fx_feed_rate_immutable BEFORE UPDATE OR DELETE ON fyniq.fx_feed_rate
  FOR EACH ROW EXECUTE FUNCTION fyniq.fx_feed_rate_immutable();
CREATE TRIGGER fx_feed_rate_no_truncate BEFORE TRUNCATE ON fyniq.fx_feed_rate
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.fx_feed_rate_immutable();

CREATE VIEW fyniq.fx_feed_rates AS
  SELECT base_currency, currency, on_date, rate, quote, quote_form, feed, recorded_at FROM fyniq.fx_feed_rate;
GRANT SELECT ON fyniq.fx_feed_rates TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;

-- record_feed_rate stores one rate of the feed: true when it is new, false when the same rate is there already. A
-- different rate for a date the feed already has is refused (FY168).
-- +goose StatementBegin
CREATE FUNCTION fyniq.record_feed_rate(p_base text, p_currency text, p_on date, p_rate numeric, p_quote numeric,
  p_quote_form text, p_feed text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  had record;
BEGIN
  INSERT INTO fyniq.fx_feed_rate (base_currency, currency, on_date, rate, quote, quote_form, feed)
  VALUES (p_base, p_currency, p_on, p_rate, p_quote, p_quote_form, p_feed)
  ON CONFLICT (base_currency, currency, on_date) DO NOTHING;
  IF FOUND THEN
    RETURN true;
  END IF;
  SELECT rate, quote, quote_form, feed INTO had FROM fyniq.fx_feed_rate
   WHERE base_currency = p_base AND currency = p_currency AND on_date = p_on;
  IF had.rate <> p_rate OR had.quote <> p_quote OR had.quote_form <> p_quote_form OR had.feed <> p_feed THEN
    RAISE EXCEPTION 'The feed has % per % on % at %, from %; it is not changed to %', p_base, p_currency, p_on, had.rate,
      had.feed, p_rate USING ERRCODE = 'FY168',
      HINT = 'A feed rate never changes: check the source, and override the day''s rate in the organisation if it must differ.';
  END IF;
  RETURN false;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.record_feed_rate(text, text, date, numeric, numeric, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.record_feed_rate(text, text, date, numeric, numeric, text, text) TO fyniq_worker;

-- ------------------------------------------------------------------ the organisation's rates
CREATE TABLE fyniq.fx_rate (
  organisation_id  uuid            NOT NULL DEFAULT fyniq.current_org(),
  id               uuid            NOT NULL DEFAULT uuidv7(),
  kind             text            NOT NULL,  -- day: overrides the feed on on_date; month: the monthly table
  currency         text            NOT NULL,
  on_date          date            NOT NULL,  -- the day, or the month's first day
  rate             numeric(20, 10) NOT NULL,  -- base per one unit of currency, under RND-RATE
  quote            numeric(20, 10) NOT NULL,  -- as entered
  quote_form       text            NOT NULL,
  note             text,                      -- where the rate came from, in the person's words
  entered_by       text            NOT NULL,  -- "type:id", as the audit trail names the actor
  entered_at       timestamptz     NOT NULL,
  withdrawn_by     text,
  withdrawn_at     timestamptz,
  withdrawn_reason text,
  CONSTRAINT fx_rate_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT fx_rate_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT fx_rate_currency_fkey FOREIGN KEY (currency) REFERENCES fyniq.currency (code),
  CONSTRAINT fx_rate_kind_known CHECK (kind IN ('day', 'month')),
  CONSTRAINT fx_rate_positive CHECK (rate <> 'NaN' AND rate > 0 AND quote <> 'NaN' AND quote > 0),
  CONSTRAINT fx_rate_rnd_rate CHECK (
    (quote_form = 'base-per-unit' AND rate = quote) OR
    (quote_form = 'base-per-100' AND rate = round(quote / 100, 10)) OR
    (quote_form = 'foreign-per-base' AND rate = round(1::numeric(40, 30) / quote, 10))),
  CONSTRAINT fx_rate_note_length CHECK (length(note) BETWEEN 1 AND 200),
  CONSTRAINT fx_rate_entered_by_format CHECK (entered_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT fx_rate_withdrawn_whole CHECK (
    (withdrawn_by IS NULL) = (withdrawn_at IS NULL) AND (withdrawn_by IS NULL) = (withdrawn_reason IS NULL)),
  CONSTRAINT fx_rate_withdrawn_by_format CHECK (withdrawn_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT fx_rate_withdrawn_reason_length CHECK (length(btrim(withdrawn_reason)) BETWEEN 1 AND 500)
);
CREATE INDEX fx_rate_lookup ON fyniq.fx_rate (organisation_id, currency, kind, on_date);
CREATE UNIQUE INDEX fx_rate_day_current ON fyniq.fx_rate (organisation_id, currency, on_date)
  WHERE kind = 'day' AND withdrawn_at IS NULL;

-- A rate is of a currency other than the base, a monthly rate is dated on its month's first day, and once entered only
-- its withdrawal is ever recorded, once.
-- +goose StatementBegin
CREATE FUNCTION fyniq.fx_rate_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  base text;
BEGIN
  IF TG_OP = 'INSERT' THEN
    SELECT base_currency INTO base FROM fyniq.organisation WHERE organisation_id = NEW.organisation_id;
    IF NEW.currency = base THEN
      RAISE EXCEPTION 'A rate of % is not needed: it is the base currency', NEW.currency USING ERRCODE = 'FY166',
        HINT = 'Base-currency amounts take rate 1 (ENG-03).';
    END IF;
    IF NEW.kind = 'month' AND NEW.on_date <> date_trunc('month', NEW.on_date)::date THEN
      RAISE EXCEPTION 'A monthly rate is dated on its month''s first day, not %', NEW.on_date USING ERRCODE = 'FY166',
        HINT = 'Date it on the first day of the month it is for (ENG-40).';
    END IF;
    IF NEW.withdrawn_at IS NOT NULL THEN
      RAISE EXCEPTION 'A rate is entered current, then withdrawn' USING ERRCODE = 'FY167';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'UPDATE'
     AND OLD.withdrawn_at IS NULL AND NEW.withdrawn_at IS NOT NULL
     AND (to_jsonb(NEW) - '{withdrawn_by,withdrawn_at,withdrawn_reason}'::text[])
       = (to_jsonb(OLD) - '{withdrawn_by,withdrawn_at,withdrawn_reason}'::text[]) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'An exchange rate never changes or goes; it is withdrawn once' USING ERRCODE = 'FY167',
    HINT = 'Withdraw the rate and enter a new one (FR-GL-05).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER fx_rate_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.fx_rate
  FOR EACH ROW EXECUTE FUNCTION fyniq.fx_rate_guard();
CREATE TRIGGER fx_rate_no_truncate BEFORE TRUNCATE ON fyniq.fx_rate
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.fx_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.fx_rate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.fx_rate
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.fx_rate
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('fx_rate', 'id');

GRANT SELECT ON fyniq.fx_rate TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (kind, currency, on_date, rate, quote, quote_form, note, entered_by, entered_at),
  UPDATE (withdrawn_by, withdrawn_at, withdrawn_reason) ON fyniq.fx_rate TO fyniq_api;
