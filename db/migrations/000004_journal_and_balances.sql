-- The journal and the balances cache (PRD-001 §10.1, §10.7, §11.3, §11.4; ENG-46, ENG-47,
-- INV-01…05, INV-08, INV-09).
--
-- The domain posts through one path (post_entry, p0-money-posting-engine). These constraints and
-- triggers are the backstop: they refuse what both the engine and the oracle might get wrong, and
-- what a writer that skips the engine might try. Debits are positive and credits negative.
-- Error codes FYnnn are listed in db/README.md; the PostgreSQL adapter maps them to stable codes.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ journal entries
CREATE TABLE fyniq.journal_entry (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  ledger_revision   bigint      NOT NULL,  -- set by journal_entry_assign (ENG-46)
  posting_date      date        NOT NULL,  -- accounting time (§11.6)
  period_id         uuid        NOT NULL,  -- set by journal_entry_assign (INV-03)
  posting_identity  text        NOT NULL,  -- never expires (§11.4, idempotency)
  source_type       text        NOT NULL,  -- provenance (ENG-03)
  source_id         text        NOT NULL,
  source_version    integer     NOT NULL,
  rule_version      text        NOT NULL,
  reverses_entry_id uuid,
  memo              text,
  recorded_at       timestamptz NOT NULL DEFAULT now(),  -- system time: the transaction's start
  created_xact      bigint      NOT NULL,  -- set by journal_entry_assign; lines must share it
  CONSTRAINT journal_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT journal_entry_revision_key UNIQUE (organisation_id, ledger_revision),
  CONSTRAINT journal_entry_posting_identity_key UNIQUE (organisation_id, posting_identity),
  CONSTRAINT journal_entry_reverses_key UNIQUE (organisation_id, reverses_entry_id),
  CONSTRAINT journal_entry_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT journal_entry_period_fkey FOREIGN KEY (organisation_id, period_id)
    REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT journal_entry_reverses_fkey FOREIGN KEY (organisation_id, reverses_entry_id)
    REFERENCES fyniq.journal_entry (organisation_id, id),
  CONSTRAINT journal_entry_revision_positive CHECK (ledger_revision > 0),
  CONSTRAINT journal_entry_identity_length CHECK (length(posting_identity) BETWEEN 1 AND 500),
  CONSTRAINT journal_entry_source_type_format CHECK (source_type ~ '^[a-z][a-z0-9_.]{0,62}$'),
  CONSTRAINT journal_entry_source_id_length CHECK (length(source_id) BETWEEN 1 AND 200),
  CONSTRAINT journal_entry_source_version_positive CHECK (source_version > 0),
  CONSTRAINT journal_entry_rule_version_length CHECK (length(rule_version) BETWEEN 1 AND 100),
  CONSTRAINT journal_entry_not_own_reversal CHECK (reverses_entry_id <> id),
  CONSTRAINT journal_entry_memo_length CHECK (length(memo) <= 1000)
);
CREATE INDEX journal_entry_period_idx ON fyniq.journal_entry (organisation_id, period_id);
CREATE INDEX journal_entry_posting_date_idx ON fyniq.journal_entry (organisation_id, posting_date);

-- ------------------------------------------------------------------ journal lines
-- Each line carries its currency's minor units, so the scale checks are row constraints (§11.3):
-- the trigger fills minor_units from the currency and the base currency from the organisation.
CREATE TABLE fyniq.journal_line (
  organisation_id  uuid           NOT NULL DEFAULT fyniq.current_org(),
  id               uuid           NOT NULL DEFAULT uuidv7(),
  entry_id         uuid           NOT NULL,
  line_no          integer        NOT NULL,
  account_id       uuid           NOT NULL,
  posting_date     date           NOT NULL,  -- the entry's, set by journal_line_check
  currency         text           NOT NULL,
  minor_units      smallint       NOT NULL,  -- the currency's, set by journal_line_check
  amount           numeric(20, 4) NOT NULL,  -- transaction currency; debit +, credit −
  fx_rate          numeric(20, 10) NOT NULL, -- base per unit of the transaction currency
  fx_rate_source   text           NOT NULL,
  base_currency    text           NOT NULL,  -- the organisation's, set by journal_line_check
  base_minor_units smallint       NOT NULL,
  base_amount      numeric(20, 4) NOT NULL,  -- RND-FX, computed once at posting (ENG-14)
  description      text,
  recorded_at      timestamptz    NOT NULL DEFAULT now(),
  CONSTRAINT journal_line_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT journal_line_entry_line_key UNIQUE (organisation_id, entry_id, line_no),
  CONSTRAINT journal_line_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT journal_line_entry_fkey FOREIGN KEY (organisation_id, entry_id)
    REFERENCES fyniq.journal_entry (organisation_id, id),
  CONSTRAINT journal_line_account_fkey FOREIGN KEY (organisation_id, account_id)
    REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT journal_line_currency_fkey FOREIGN KEY (currency, minor_units)
    REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT journal_line_base_currency_fkey FOREIGN KEY (base_currency, base_minor_units)
    REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT journal_line_line_no_positive CHECK (line_no > 0),
  -- INV-05: amounts at the currency's minor units, never NaN (§11.3, rev 0.8)
  CONSTRAINT journal_line_amount_scale CHECK (amount <> 'NaN' AND amount = trunc(amount, minor_units)),
  CONSTRAINT journal_line_base_amount_scale
    CHECK (base_amount <> 'NaN' AND base_amount = trunc(base_amount, base_minor_units)),
  CONSTRAINT journal_line_fx_rate_positive CHECK (fx_rate <> 'NaN' AND fx_rate > 0),
  CONSTRAINT journal_line_base_currency_rate CHECK (currency <> base_currency OR fx_rate = 1),
  -- INV-05: the base amount follows RND-FX (round half away from zero to the base minor units).
  -- Base-only lines (amount 0, base not 0) belong to FX revaluation (FR-GL-04, P1) and are refused
  -- until that source and its own trigger exist.
  CONSTRAINT journal_line_base_amount_rnd_fx
    CHECK (base_amount = round(amount * fx_rate, base_minor_units)),
  CONSTRAINT journal_line_fx_rate_source_length CHECK (length(fx_rate_source) BETWEEN 1 AND 100),
  CONSTRAINT journal_line_description_length CHECK (length(description) <= 500)
);
CREATE INDEX journal_line_account_idx ON fyniq.journal_line (organisation_id, account_id, posting_date);

-- ------------------------------------------------------------------ balances (materialised)
-- A cache of the lines per account, period and currency, kept in the posting transaction by
-- journal_line_balances and rebuilt by fyniq.rebuild_balances(). Nothing else writes it (INV-08).
-- Dimension sets join the key with the P1 dimensions (FR-DIM).
CREATE TABLE fyniq.balance (
  organisation_id uuid           NOT NULL,
  id              uuid           NOT NULL DEFAULT uuidv7(),
  account_id      uuid           NOT NULL,
  period_id       uuid           NOT NULL,
  currency        text           NOT NULL,
  minor_units     smallint       NOT NULL,
  amount          numeric(20, 4) NOT NULL,
  base_amount     numeric(20, 4) NOT NULL,
  line_count      bigint         NOT NULL,
  CONSTRAINT balance_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT balance_key UNIQUE (organisation_id, account_id, period_id, currency),
  CONSTRAINT balance_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT balance_account_fkey FOREIGN KEY (organisation_id, account_id)
    REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT balance_period_fkey FOREIGN KEY (organisation_id, period_id)
    REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT balance_currency_fkey FOREIGN KEY (currency, minor_units)
    REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT balance_amount_scale CHECK (amount <> 'NaN' AND amount = trunc(amount, minor_units)),
  CONSTRAINT balance_base_amount_scale CHECK (base_amount <> 'NaN'),
  CONSTRAINT balance_line_count_positive CHECK (line_count > 0)
);
CREATE INDEX balance_period_idx ON fyniq.balance (organisation_id, period_id);

-- ------------------------------------------------------------------ entry: revision and period
-- ENG-47 step 2: the next ledger revision, under the revision row's lock until commit (ENG-46).
-- Step 3: the period covering the posting date, read FOR SHARE after that lock, so a close or a
-- reopen (which take the same revision lock, then the period FOR UPDATE) never interleaves with a
-- posting (INV-03). The trigger also sets the system columns, whatever the writer sent.
-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_assign() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  p   record;
BEGIN
  UPDATE fyniq.ledger_revision SET revision = revision + 1
   WHERE organisation_id = org
   RETURNING revision INTO NEW.ledger_revision;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The organisation of this transaction does not exist' USING ERRCODE = 'FY006';
  END IF;

  SELECT id, status, starts_on, ends_on INTO p FROM fyniq.period
   WHERE organisation_id = org AND daterange(starts_on, ends_on, '[]') @> NEW.posting_date
   FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No period covers the posting date %', NEW.posting_date
      USING ERRCODE = 'FY030', HINT = 'Create the period first, or post on a date inside one.';
  END IF;
  IF p.status = 'closed' THEN
    RAISE EXCEPTION 'The period % to % is closed, so nothing can be posted on %',
                    p.starts_on, p.ends_on, NEW.posting_date
      USING ERRCODE = 'FY003',
            HINT = 'Post in an open period, or ask an Owner to reopen this one (INV-03).';
  END IF;

  NEW.organisation_id := org;
  NEW.period_id := p.id;
  NEW.recorded_at := now();
  NEW.created_xact := pg_current_xact_id()::text::bigint;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_assign BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_entry_assign();

-- ------------------------------------------------------------------ line: entry, account, currencies
-- INV-02: lines join their entry only in the transaction that posted it. INV-04: the account is an
-- active leaf of the same organisation, read FOR SHARE so it cannot be deactivated mid-posting.
-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_line_check() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  e   record;
  a   record;
  o   record;
BEGIN
  SELECT posting_date, created_xact INTO e FROM fyniq.journal_entry
   WHERE organisation_id = org AND id = NEW.entry_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Journal entry % does not exist in this organisation', NEW.entry_id
      USING ERRCODE = 'FY021';
  END IF;
  IF e.created_xact <> pg_current_xact_id()::text::bigint THEN
    RAISE EXCEPTION 'Journal entry % is posted, so no line can be added to it', NEW.entry_id
      USING ERRCODE = 'FY002',
            HINT = 'Correct posted history with a reversing entry (INV-02, FR-GL-12).';
  END IF;

  SELECT code, is_group, is_active INTO a FROM fyniq.account
   WHERE organisation_id = org AND id = NEW.account_id
   FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Account % does not exist in this organisation', NEW.account_id
      USING ERRCODE = 'FY004';
  END IF;
  IF a.is_group THEN
    RAISE EXCEPTION 'Account % is a group account, so it cannot take postings', a.code
      USING ERRCODE = 'FY004', HINT = 'Post to an account under it (INV-04, FR-COA-05).';
  END IF;
  IF NOT a.is_active THEN
    RAISE EXCEPTION 'Account % is inactive, so it cannot take postings', a.code
      USING ERRCODE = 'FY004', HINT = 'Reactivate it or post to another account (INV-04).';
  END IF;

  SELECT base_currency, base_minor_units INTO o FROM fyniq.organisation WHERE organisation_id = org;
  NEW.organisation_id := org;
  NEW.posting_date := e.posting_date;
  NEW.minor_units := (SELECT c.minor_units FROM fyniq.currency c WHERE c.code = NEW.currency);
  IF NEW.minor_units IS NULL THEN
    RAISE EXCEPTION 'Currency % is not an ISO 4217 code Fyniq knows', NEW.currency
      USING ERRCODE = 'FY005';
  END IF;
  NEW.base_currency := o.base_currency;
  NEW.base_minor_units := o.base_minor_units;
  NEW.recorded_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_line_check BEFORE INSERT ON fyniq.journal_line
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_line_check();

-- ------------------------------------------------------------------ INV-01: balanced at commit
-- Checked at commit for every entry and again for every line, so an entry can be built over several
-- statements in its transaction, and no line can unbalance it afterwards.
-- +goose StatementBegin
CREATE FUNCTION fyniq.check_entry_balanced() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  eid       uuid;
  n         bigint;
  base_sum  numeric;
  amt_sum   numeric;
  ncur      bigint;
  cur       text;
BEGIN
  IF TG_TABLE_NAME = 'journal_entry' THEN
    eid := NEW.id;
  ELSE
    eid := NEW.entry_id;
  END IF;
  SELECT count(*), coalesce(sum(base_amount), 0), coalesce(sum(amount), 0),
         count(DISTINCT currency), min(currency)
    INTO n, base_sum, amt_sum, ncur, cur
    FROM fyniq.journal_line
   WHERE organisation_id = NEW.organisation_id AND entry_id = eid;
  IF n < 2 THEN
    RAISE EXCEPTION 'Journal entry % has % line(s); an entry needs at least two', eid, n
      USING ERRCODE = 'FY001';
  END IF;
  IF base_sum <> 0 THEN
    RAISE EXCEPTION 'Journal entry % does not balance: debits exceed credits by % in the base currency',
                    eid, base_sum
      USING ERRCODE = 'FY001', HINT = 'Debits and credits must be equal (INV-01).';
  END IF;
  IF ncur = 1 AND amt_sum <> 0 THEN
    RAISE EXCEPTION 'Journal entry % does not balance: debits exceed credits by % %', eid, amt_sum, cur
      USING ERRCODE = 'FY001', HINT = 'Debits and credits must be equal (INV-01).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER journal_entry_balanced AFTER INSERT ON fyniq.journal_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.check_entry_balanced();
CREATE CONSTRAINT TRIGGER journal_line_balanced AFTER INSERT ON fyniq.journal_line
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.check_entry_balanced();

-- ------------------------------------------------------------------ INV-02: posted means immutable
-- No application role has UPDATE, DELETE or TRUNCATE on the journal; these triggers refuse them
-- from every role, the owner included, so a mistaken grant cannot open history.
-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Posted journal entries and lines cannot be changed or deleted (% on %)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'FY002',
          HINT = 'Correct posted history with a reversing entry (INV-02, FR-GL-12).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_immutable BEFORE UPDATE OR DELETE ON fyniq.journal_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_immutable();
CREATE TRIGGER journal_entry_no_truncate BEFORE TRUNCATE ON fyniq.journal_entry
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.journal_immutable();
CREATE TRIGGER journal_line_immutable BEFORE UPDATE OR DELETE ON fyniq.journal_line
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_immutable();
CREATE TRIGGER journal_line_no_truncate BEFORE TRUNCATE ON fyniq.journal_line
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.journal_immutable();

-- ------------------------------------------------------------------ INV-08: the balances cache
-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_line_balances() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  INSERT INTO fyniq.balance AS b
         (organisation_id, account_id, period_id, currency, minor_units, amount, base_amount, line_count)
  SELECT l.organisation_id, l.account_id, e.period_id, l.currency, l.minor_units,
         sum(l.amount), sum(l.base_amount), count(*)
    FROM new_lines l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
   GROUP BY l.organisation_id, l.account_id, e.period_id, l.currency, l.minor_units
  ON CONFLICT (organisation_id, account_id, period_id, currency) DO UPDATE
     SET amount = b.amount + excluded.amount,
         base_amount = b.base_amount + excluded.base_amount,
         line_count = b.line_count + excluded.line_count;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_line_balances AFTER INSERT ON fyniq.journal_line
  REFERENCING NEW TABLE AS new_lines
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.journal_line_balances();

-- The cache against a recomputation from the lines, for the current organisation: no rows means
-- they agree (INV-08). The nightly verification (p0-outbox-background-work) runs it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.balance_differences()
RETURNS TABLE (account_id uuid, period_id uuid, currency text,
               cached_amount numeric, cached_base_amount numeric,
               lines_amount numeric, lines_base_amount numeric)
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  WITH lines AS (
    SELECT l.account_id, e.period_id, l.currency,
           sum(l.amount) AS amount, sum(l.base_amount) AS base_amount, count(*) AS line_count
      FROM fyniq.journal_line l
      JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
     WHERE l.organisation_id = fyniq.current_org()
     GROUP BY l.account_id, e.period_id, l.currency)
  SELECT coalesce(b.account_id, l.account_id), coalesce(b.period_id, l.period_id),
         coalesce(b.currency, l.currency), b.amount, b.base_amount, l.amount, l.base_amount
    FROM (SELECT * FROM fyniq.balance WHERE organisation_id = fyniq.current_org()) b
    FULL JOIN lines l
      ON l.account_id = b.account_id AND l.period_id = b.period_id AND l.currency = b.currency
   WHERE b.amount IS DISTINCT FROM l.amount
      OR b.base_amount IS DISTINCT FROM l.base_amount
      OR b.line_count IS DISTINCT FROM l.line_count
$$;
-- +goose StatementEnd

-- Rebuild the cache for the current organisation from the lines, under the ledger lock (ENG-47),
-- so no posting runs meanwhile. Returns the number of balance rows written.
-- +goose StatementBegin
CREATE FUNCTION fyniq.rebuild_balances() RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  n   bigint;
BEGIN
  PERFORM fyniq.lock_ledger();
  DELETE FROM fyniq.balance WHERE organisation_id = org;
  INSERT INTO fyniq.balance
         (organisation_id, account_id, period_id, currency, minor_units, amount, base_amount, line_count)
  SELECT l.organisation_id, l.account_id, e.period_id, l.currency, l.minor_units,
         sum(l.amount), sum(l.base_amount), count(*)
    FROM fyniq.journal_line l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
   WHERE l.organisation_id = org
   GROUP BY l.organisation_id, l.account_id, e.period_id, l.currency, l.minor_units;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ row-level security
ALTER TABLE fyniq.journal_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.journal_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.journal_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.journal_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.journal_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.journal_line
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.balance ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.balance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.balance
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
GRANT SELECT ON fyniq.journal_entry, fyniq.journal_line, fyniq.balance
  TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (posting_date, posting_identity, source_type, source_id, source_version, rule_version,
              reverses_entry_id, memo)
  ON fyniq.journal_entry TO fyniq_api, fyniq_worker, fyniq_executor;
GRANT INSERT (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount,
              description)
  ON fyniq.journal_line TO fyniq_api, fyniq_worker, fyniq_executor;
GRANT EXECUTE ON FUNCTION fyniq.balance_differences() TO fyniq_worker, fyniq_readonly;
GRANT EXECUTE ON FUNCTION fyniq.rebuild_balances() TO fyniq_worker;
