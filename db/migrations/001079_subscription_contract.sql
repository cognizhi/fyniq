-- Subscription contracts and their dated amendments (PRD-001 FR-AR-15, rev 0.550; p2-customer-portal-subscriptions task 3.1, design D3).
--
-- A contract is a customer's subscription with a start, an optional agreed end and a monthly value (MRR) in one currency; an amendment (upgrade,
-- downgrade, cancel, renew) is dated and sets the monthly value from its day. Both are records, not postings: invoices and credit notes still
-- post through the existing rules, and the schedule and the contracted MRR are derived from the contract and its amendments. A contract never
-- changes (only its version moves, when an amendment is added); an amendment is append-only (FYP21, FYP22).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.subscription_contract (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  contact_id      uuid          NOT NULL,
  name            text          NOT NULL,
  currency        text          NOT NULL,
  starts_on       date          NOT NULL,
  ends_on         date,                    -- the agreed end, when there is one
  mrr             numeric(20,4) NOT NULL,  -- the monthly value at the start, in the contract's currency
  version         integer       NOT NULL DEFAULT 1,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT subscription_contract_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT subscription_contract_contact_fkey FOREIGN KEY (organisation_id, contact_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT subscription_contract_name_form CHECK (length(name) BETWEEN 1 AND 140),
  CONSTRAINT subscription_contract_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT subscription_contract_mrr_form CHECK (mrr <> 'NaN' AND mrr >= 0),
  CONSTRAINT subscription_contract_term_form CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CONSTRAINT subscription_contract_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX subscription_contract_contact ON fyniq.subscription_contract (organisation_id, contact_id, starts_on);

CREATE TABLE fyniq.subscription_amendment (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  contract_id     uuid          NOT NULL,
  kind            text          NOT NULL,
  effective_on    date          NOT NULL,
  mrr             numeric(20,4) NOT NULL,  -- the monthly value from the effective day; 0 for a cancellation
  reason          text          NOT NULL,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT subscription_amendment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT subscription_amendment_contract_fkey FOREIGN KEY (organisation_id, contract_id) REFERENCES fyniq.subscription_contract (organisation_id, id),
  CONSTRAINT subscription_amendment_kind_known CHECK (kind IN ('upgrade', 'downgrade', 'cancel', 'renew')),
  CONSTRAINT subscription_amendment_mrr_form CHECK (mrr <> 'NaN' AND mrr >= 0 AND (kind <> 'cancel' OR mrr = 0)),
  CONSTRAINT subscription_amendment_reason_form CHECK (length(reason) BETWEEN 1 AND 500),
  CONSTRAINT subscription_amendment_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- One amendment per contract and day: two for the same day would not say which monthly value holds.
CREATE UNIQUE INDEX subscription_amendment_day ON fyniq.subscription_amendment (organisation_id, contract_id, effective_on);

-- A contract changes only in its version, when an amendment is added; it is never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.subscription_contract_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.organisation_id, NEW.id, NEW.contact_id, NEW.name, NEW.currency, NEW.starts_on, NEW.ends_on, NEW.mrr, NEW.created_by, NEW.created_at)
        IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.contact_id, OLD.name, OLD.currency, OLD.starts_on, OLD.ends_on, OLD.mrr, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A subscription contract never changes, and is never deleted' USING ERRCODE = 'FYP21',
      HINT = 'Record an amendment dated from the day the change takes effect (FR-AR-15).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER subscription_contract_guard BEFORE UPDATE OR DELETE ON fyniq.subscription_contract FOR EACH ROW EXECUTE FUNCTION fyniq.subscription_contract_guard();
CREATE TRIGGER subscription_contract_no_truncate BEFORE TRUNCATE ON fyniq.subscription_contract FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- +goose StatementBegin
CREATE FUNCTION fyniq.subscription_amendment_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A subscription amendment is never changed or deleted' USING ERRCODE = 'FYP22',
    HINT = 'Record a new amendment dated from the day the change takes effect (FR-AR-15).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER subscription_amendment_guard BEFORE UPDATE OR DELETE ON fyniq.subscription_amendment FOR EACH ROW EXECUTE FUNCTION fyniq.subscription_amendment_guard();
CREATE TRIGGER subscription_amendment_no_truncate BEFORE TRUNCATE ON fyniq.subscription_amendment FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.subscription_contract ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.subscription_contract FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.subscription_contract USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.subscription_contract
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('subscription_contract', 'id');
ALTER TABLE fyniq.subscription_amendment ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.subscription_amendment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.subscription_amendment USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.subscription_amendment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('subscription_amendment', 'id');

GRANT SELECT ON fyniq.subscription_contract, fyniq.subscription_amendment TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (contact_id, name, currency, starts_on, ends_on, mrr, created_by, created_at) ON fyniq.subscription_contract TO fyniq_api;
GRANT UPDATE (version) ON fyniq.subscription_contract TO fyniq_api;
GRANT INSERT (contract_id, kind, effective_on, mrr, reason, created_by, created_at) ON fyniq.subscription_amendment TO fyniq_api;
