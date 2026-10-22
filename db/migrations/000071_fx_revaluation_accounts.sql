-- Month-end FX revaluation: the accounts it revalues and its base-only lines (PRD-001 FR-GL-04, INV-05; rev 0.72;
-- p1-multi-currency-fx tasks 3.1 and 3.2).
--
-- account.revalue marks an account whose foreign-currency balances are revalued at month end: a monetary item, such as
-- a bank, card or wallet account, AR, AP, staff and director advances, the director's loan and deferred remuneration.
-- Only an asset or a liability is one, and never prepayments or deferred revenue, which are non-monetary (FR-GL-04);
-- the check is added NOT VALID and validated by 000072 (squawk: constraint-missing-not-valid). Accounts loaded before
-- this migration keep false until the chart says otherwise: an account changes only with its audit event.
--
-- A base-only line has a foreign currency, amount 0 and a base amount: the revaluation's change to the line's base
-- value, at the rate it was revalued at. INV-05's RND-FX check now accepts it, and journal_line_base_only, deferred to
-- commit, accepts it only from the fx_revaluation source (an entry and its reversal both carry it), on an account
-- marked revalue, in an entry whose every line is such a line or a base-currency line on the unrealised-FX account,
-- which offsets them (FY008), whoever writes it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account ADD COLUMN revalue boolean NOT NULL DEFAULT false;
ALTER TABLE fyniq.account ADD CONSTRAINT account_revalue_monetary
  CHECK (NOT revalue OR (type IN ('asset', 'liability') AND role IS DISTINCT FROM 'prepayments' AND role IS DISTINCT FROM 'deferred-revenue'))
  NOT VALID;
GRANT INSERT (revalue), UPDATE (revalue) ON fyniq.account TO fyniq_api;

ALTER TABLE fyniq.journal_line DROP CONSTRAINT journal_line_base_amount_rnd_fx;
ALTER TABLE fyniq.journal_line ADD CONSTRAINT journal_line_base_amount_rnd_fx
  CHECK (base_amount = round(amount * fx_rate, base_minor_units) OR (amount = 0 AND base_amount <> 0 AND currency <> base_currency))
  NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_line_base_only() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  source text;
  stray  integer;
BEGIN
  SELECT source_type INTO source FROM fyniq.journal_entry WHERE organisation_id = NEW.organisation_id AND id = NEW.entry_id;
  IF source IS DISTINCT FROM 'fx_revaluation' THEN
    RAISE EXCEPTION 'A line with a base amount and no % amount comes only from FX revaluation, not from %', NEW.currency, source
      USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; only the month-end revaluation changes a base value alone (INV-05, FR-GL-04).';
  END IF;
  SELECT count(*) INTO stray
    FROM fyniq.journal_line l
    JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
   WHERE l.organisation_id = NEW.organisation_id AND l.entry_id = NEW.entry_id
     AND NOT ((l.amount = 0 AND l.base_amount <> 0 AND l.currency <> l.base_currency AND a.revalue)
           OR (l.currency = l.base_currency AND a.role IS NOT DISTINCT FROM 'unrealised-fx'));
  IF stray > 0 THEN
    RAISE EXCEPTION 'A revaluation revalues only accounts marked for it, against the unrealised-FX account: % line(s) of entry % are neither', stray, NEW.entry_id
      USING ERRCODE = 'FY008', HINT = 'Mark the account for revaluation in the chart, or leave its balance out (INV-05, FR-GL-04).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER journal_line_base_only AFTER INSERT ON fyniq.journal_line
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW WHEN (NEW.amount = 0 AND NEW.base_amount <> 0)
  EXECUTE FUNCTION fyniq.journal_line_base_only();

-- The revaluation of a month is a document of its own (FR-GL-04): its version records the balances, rates and ledger
-- revision it read; its entry is dated the month's last day and reversed on the next period's first.
ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation')) NOT VALID;
