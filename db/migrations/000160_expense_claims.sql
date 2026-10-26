-- Expense claims (PRD-001 FR-AP-07, FR-AP-15, FR-AP-17, §10.2; p1-expense-claims task 1.1, design D1 and D6).
--
-- Three documents: expense_claim (a member's claim, posted by expense_claim@1 once approved: Dr each line's expense
-- and the claimable input tax, Cr 2150 Expense claims payable, or the advances account for what it settles of an
-- advance), claim_reimbursement (Dr 2150 · Cr the money account, recorded by hand until payment batches) and
-- cash_advance (Dr the advances account · Cr the money account). Each is numbered from its own gap-free sequence.
--
-- Claim categories are the expense policy's lines (FR-AP-15): a name, the account a claim line of it costs, and its
-- limits in the base currency (per claim line, a receipt required above, attendees required above); one category may
-- be the mileage category. The starter set is made by the API, audited, when the standard chart loads.
-- The widened checks are added NOT VALID and validated by 000161 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance')) NOT VALID;

CREATE TABLE fyniq.claim_category (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  name            text          NOT NULL,
  account_id      uuid          NOT NULL,
  mileage         boolean       NOT NULL DEFAULT false,
  currency        text          NOT NULL,
  minor_units     smallint      NOT NULL,
  line_limit      numeric(20,4),
  receipt_above   numeric(20,4),
  attendees_above numeric(20,4),
  active          boolean       NOT NULL DEFAULT true,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  changed_by      text          NOT NULL,
  changed_at      timestamptz   NOT NULL,
  version         integer       NOT NULL DEFAULT 1,
  CONSTRAINT claim_category_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT claim_category_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT claim_category_account_fkey FOREIGN KEY (organisation_id, account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT claim_category_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT claim_category_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  CONSTRAINT claim_category_limits_scale CHECK (
    (line_limit IS NULL OR (line_limit <> 'NaN' AND line_limit > 0 AND line_limit = trunc(line_limit, minor_units)))
    AND (receipt_above IS NULL OR (receipt_above <> 'NaN' AND receipt_above >= 0 AND receipt_above = trunc(receipt_above, minor_units)))
    AND (attendees_above IS NULL OR (attendees_above <> 'NaN' AND attendees_above >= 0 AND attendees_above = trunc(attendees_above, minor_units)))),
  CONSTRAINT claim_category_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT claim_category_version_positive CHECK (version >= 1)
);
-- One live category of a name, and at most one live mileage category.
CREATE UNIQUE INDEX claim_category_live_name ON fyniq.claim_category (organisation_id, lower(btrim(name))) WHERE active;
CREATE UNIQUE INDEX claim_category_live_mileage ON fyniq.claim_category (organisation_id) WHERE active AND mileage;

-- +goose StatementBegin
-- A category costs an active, postable expense or asset account that is not a money, control or system account, in the
-- base currency; it keeps its creation, currency and mileage flag, changes one version at a time and is never deleted
-- (deactivate it instead: claims name it) (FY232).
CREATE FUNCTION fyniq.claim_category_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  a fyniq.account;
  base text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Claim category % is never deleted', OLD.id USING ERRCODE = 'FY232', HINT = 'Deactivate it instead: claims name it.';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF (NEW.mileage, NEW.currency, NEW.minor_units, NEW.created_by, NEW.created_at)
       IS DISTINCT FROM (OLD.mileage, OLD.currency, OLD.minor_units, OLD.created_by, OLD.created_at)
       OR NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'Claim category % keeps its creation, currency and mileage flag, one version at a time', OLD.id USING ERRCODE = 'FY232';
    END IF;
  ELSIF NEW.version <> 1 THEN
    RAISE EXCEPTION 'A new claim category starts at version 1' USING ERRCODE = 'FY232';
  END IF;
  IF TG_OP = 'INSERT' OR NEW.account_id IS DISTINCT FROM OLD.account_id THEN
    SELECT * INTO a FROM fyniq.account WHERE organisation_id = NEW.organisation_id AND id = NEW.account_id;
    IF a.id IS NULL OR a.is_group OR NOT a.is_active OR a.is_money_account OR a.is_control OR a.role IS NOT NULL OR a.type NOT IN ('expense', 'asset') THEN
      RAISE EXCEPTION 'Account % is not an active, postable expense or asset account a claim can cost', a.code USING ERRCODE = 'FY232',
        HINT = 'Pick an expense or asset account that is not a money, control or system account (FR-AP-15).';
    END IF;
  END IF;
  SELECT base_currency INTO base FROM fyniq.organisation WHERE organisation_id = NEW.organisation_id;
  IF NEW.currency <> base THEN
    RAISE EXCEPTION 'A claim category''s limits are in the base currency %', base USING ERRCODE = 'FY232';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER claim_category_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.claim_category
  FOR EACH ROW EXECUTE FUNCTION fyniq.claim_category_guard();

ALTER TABLE fyniq.claim_category ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.claim_category FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.claim_category
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.claim_category
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('claim_category', 'id');

REVOKE ALL ON fyniq.claim_category FROM PUBLIC;
GRANT SELECT ON fyniq.claim_category TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (name, account_id, mileage, currency, minor_units, line_limit, receipt_above, attendees_above, created_by, created_at,
  changed_by, changed_at) ON fyniq.claim_category TO fyniq_api;
GRANT UPDATE (name, account_id, line_limit, receipt_above, attendees_above, active, changed_by, changed_at, version)
  ON fyniq.claim_category TO fyniq_api;
