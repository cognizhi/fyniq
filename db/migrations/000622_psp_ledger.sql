-- What the books need beside a provider's statement lines (PRD-001 FR-PSP-01, FR-PSP-04, FR-PSP-05, FR-PSP-06;
-- p1-payment-providers task 2.1, design D1, D4, D5, D7).
--
-- A payment provider is a money account fed by its connector (000144, bankfeed): its balance transactions are the account's
-- statement lines, each a line of the provider's own reference (a balance transaction's ID, with ":fee" for its fee), and a
-- line is reconciled once, so that reference is the posting identity of a money leg. This file adds:
--   psp_account  the setup of one provider money account: the bank account its payouts land in, the accounts its fees and
--                its disputes post to, and how far the daily sweep has made it complete;
--   psp_txn      each provider balance transaction as the provider reports it: its type, its source (a charge, a refund, a
--                dispute, a payout), gross, fee and net, the date it becomes available and the payout that settles it;
--   psp_payout   each payout, its amount and arrival;
--   psp_balance  the provider's own balance not yet paid out, as each sweep read it (check C.2 #50).
-- Every row is a mirror of what the provider reported: it is written once and never rewritten but for the payout a
-- transaction is settled by (set once) and a payout's standing.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.psp_account (
  organisation_id         uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                      uuid        NOT NULL DEFAULT uuidv7(),
  money_account_id        uuid        NOT NULL,
  connector_id            uuid        NOT NULL,
  payout_account_id       uuid,       -- the money account a payout lands in; none: payouts wait for a person
  fee_account_id          uuid        NOT NULL, -- the chart account a fee expenses to, 5040 by default
  disputes_account_id     uuid,       -- the chart account a disputed amount waits in, 1055 by default; none: a dispute waits for a person
  swept_through           date,       -- the day the sweep has made the account complete to
  swept_at                timestamptz,
  sweep_error             text,
  created_by              text        NOT NULL,
  created_at              timestamptz NOT NULL,
  version                 integer     NOT NULL DEFAULT 1,
  CONSTRAINT psp_account_pkey PRIMARY KEY (organisation_id, money_account_id),
  CONSTRAINT psp_account_id_key UNIQUE (organisation_id, id),
  CONSTRAINT psp_account_money_fkey FOREIGN KEY (organisation_id, money_account_id) REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT psp_account_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_account_payout_fkey FOREIGN KEY (organisation_id, payout_account_id) REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT psp_account_fee_fkey FOREIGN KEY (organisation_id, fee_account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT psp_account_disputes_fkey FOREIGN KEY (organisation_id, disputes_account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT psp_account_payout_other CHECK (payout_account_id IS NULL OR payout_account_id <> money_account_id),
  CONSTRAINT psp_account_sweep_pair CHECK ((swept_through IS NULL) = (swept_at IS NULL)),
  CONSTRAINT psp_account_lengths CHECK (length(sweep_error) BETWEEN 1 AND 500),
  CONSTRAINT psp_account_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT psp_account_version_positive CHECK (version >= 1)
);
CREATE INDEX psp_account_connector ON fyniq.psp_account (organisation_id, connector_id);

CREATE TABLE fyniq.psp_txn (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  connector_id    uuid          NOT NULL,
  txn_id          text          NOT NULL,  -- the provider's ID: the reference of the statement line of its gross
  type            text          NOT NULL,  -- the provider's own type: charge, payout, refund, adjustment…
  category        text          NOT NULL,  -- how Fyniq reads it: charge, refund, dispute, dispute_reversal, fee, payout or other
  source_kind     text,                    -- what it is of: charge, refund, dispute, payout
  source_id       text,
  currency        text          NOT NULL,
  gross           numeric(20,4) NOT NULL,  -- signed, money in positive
  fee             numeric(20,4) NOT NULL,  -- what the provider keeps: positive; negative when it gives a fee back
  net             numeric(20,4) NOT NULL,  -- gross less fee
  available_on    date,
  payout_id       text,                    -- the payout that settles it, once the provider says
  provider_at     timestamptz   NOT NULL,
  seen_at         timestamptz   NOT NULL,
  CONSTRAINT psp_txn_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_txn_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_txn_once UNIQUE (organisation_id, connector_id, txn_id),
  CONSTRAINT psp_txn_category_known CHECK (category IN ('charge', 'refund', 'dispute', 'dispute_reversal', 'fee', 'payout', 'other')),
  CONSTRAINT psp_txn_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_txn_amounts CHECK (gross <> 'NaN' AND fee <> 'NaN' AND net <> 'NaN' AND net = gross - fee),
  CONSTRAINT psp_txn_source_pair CHECK ((source_kind IS NULL) = (source_id IS NULL)),
  CONSTRAINT psp_txn_lengths CHECK (length(txn_id) BETWEEN 1 AND 255 AND length(type) BETWEEN 1 AND 80 AND length(source_id) BETWEEN 1 AND 255
    AND length(payout_id) BETWEEN 1 AND 255)
);
CREATE INDEX psp_txn_payout ON fyniq.psp_txn (organisation_id, connector_id, payout_id) WHERE payout_id IS NOT NULL;
CREATE INDEX psp_txn_source ON fyniq.psp_txn (organisation_id, connector_id, source_kind, source_id);

CREATE TABLE fyniq.psp_payout (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  connector_id    uuid          NOT NULL,
  payout_id       text          NOT NULL,
  currency        text          NOT NULL,
  amount          numeric(20,4) NOT NULL,  -- as the provider reports it, positive: money paid out
  status          text          NOT NULL,
  arrival_date    date,
  automatic       boolean       NOT NULL DEFAULT true, -- Stripe itemises automatic payouts only (FR-PSP-06)
  provider_at     timestamptz   NOT NULL,
  seen_at         timestamptz   NOT NULL,
  CONSTRAINT psp_payout_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_payout_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_payout_once UNIQUE (organisation_id, connector_id, payout_id),
  CONSTRAINT psp_payout_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_payout_amount CHECK (amount <> 'NaN'),
  CONSTRAINT psp_payout_lengths CHECK (length(payout_id) BETWEEN 1 AND 255 AND length(status) BETWEEN 1 AND 40)
);

CREATE TABLE fyniq.psp_balance (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  connector_id    uuid          NOT NULL,
  currency        text          NOT NULL,
  amount          numeric(20,4) NOT NULL,  -- the provider's balance not yet paid out: available, pending and reserve
  taken_at        timestamptz   NOT NULL,
  CONSTRAINT psp_balance_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_balance_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_balance_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_balance_amount CHECK (amount <> 'NaN')
);
CREATE INDEX psp_balance_latest ON fyniq.psp_balance (organisation_id, connector_id, currency, taken_at DESC);

-- A transaction is settled by one payout, said once (FY622); a payout's facts never change but its standing (FY623); a
-- balance reading is never changed (FY623).
-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_txn_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.connector_id, NEW.txn_id, NEW.type, NEW.category, NEW.source_kind, NEW.source_id, NEW.currency, NEW.gross, NEW.fee, NEW.net,
         NEW.available_on, NEW.provider_at)
        IS DISTINCT FROM (OLD.connector_id, OLD.txn_id, OLD.type, OLD.category, OLD.source_kind, OLD.source_id, OLD.currency, OLD.gross,
         OLD.fee, OLD.net, OLD.available_on, OLD.provider_at)
     OR (OLD.payout_id IS NOT NULL AND NEW.payout_id IS DISTINCT FROM OLD.payout_id) THEN
    RAISE EXCEPTION 'A provider transaction is kept as the provider reported it' USING ERRCODE = 'FY622',
      HINT = 'Only the payout that settles it is added, once (FR-PSP-06).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.psp_payout_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.connector_id, NEW.payout_id, NEW.currency, NEW.amount, NEW.provider_at)
        IS DISTINCT FROM (OLD.connector_id, OLD.payout_id, OLD.currency, OLD.amount, OLD.provider_at) THEN
    RAISE EXCEPTION 'A provider payout is kept as the provider reported it' USING ERRCODE = 'FY623',
      HINT = 'Only its status, arrival date and whether it is automatic are updated (FR-PSP-06).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.psp_balance_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A provider balance reading is never changed' USING ERRCODE = 'FY623',
    HINT = 'Each sweep adds its own reading (FR-PSP-06).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_txn_guard BEFORE UPDATE OR DELETE ON fyniq.psp_txn FOR EACH ROW EXECUTE FUNCTION fyniq.psp_txn_guard();
CREATE TRIGGER psp_payout_guard BEFORE UPDATE OR DELETE ON fyniq.psp_payout FOR EACH ROW EXECUTE FUNCTION fyniq.psp_payout_guard();
CREATE TRIGGER psp_balance_guard BEFORE UPDATE OR DELETE ON fyniq.psp_balance FOR EACH ROW EXECUTE FUNCTION fyniq.psp_balance_guard();
CREATE TRIGGER psp_txn_no_truncate BEFORE TRUNCATE ON fyniq.psp_txn FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER psp_payout_no_truncate BEFORE TRUNCATE ON fyniq.psp_payout FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER psp_balance_no_truncate BEFORE TRUNCATE ON fyniq.psp_balance FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.psp_account ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_account FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_account USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.psp_txn ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_txn FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_txn USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.psp_payout ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_payout FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_payout USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.psp_balance ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_balance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_balance USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_account
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_account', 'money_account_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_txn
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_txn', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_payout
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_payout', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.psp_balance
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_balance', 'id');

-- A person sets an account up (API); the worker mirrors the provider's data and records how far the sweep has got.
GRANT SELECT ON fyniq.psp_account, fyniq.psp_txn, fyniq.psp_payout, fyniq.psp_balance TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (money_account_id, connector_id, payout_account_id, fee_account_id, disputes_account_id, created_by, created_at)
  ON fyniq.psp_account TO fyniq_api;
GRANT UPDATE (payout_account_id, fee_account_id, disputes_account_id, version) ON fyniq.psp_account TO fyniq_api;
GRANT UPDATE (swept_through, swept_at, sweep_error) ON fyniq.psp_account TO fyniq_worker;
GRANT INSERT (connector_id, txn_id, type, category, source_kind, source_id, currency, gross, fee, net, available_on, payout_id, provider_at, seen_at)
  ON fyniq.psp_txn TO fyniq_worker;
GRANT UPDATE (payout_id) ON fyniq.psp_txn TO fyniq_worker;
GRANT INSERT (connector_id, payout_id, currency, amount, status, arrival_date, automatic, provider_at, seen_at) ON fyniq.psp_payout TO fyniq_worker;
GRANT UPDATE (status, arrival_date, automatic) ON fyniq.psp_payout TO fyniq_worker;
GRANT INSERT (connector_id, currency, amount, taken_at) ON fyniq.psp_balance TO fyniq_worker;
