-- Tracking and paying bills (PRD-001 FR-AP-13; the owner's decisions of 23 Oct 2026, built in rev 0.93; p1-purchases
-- task 5.2).
--
-- A supplier keeps its early-payment discount terms: a percentage off if paid within a number of days of the bill's
-- date, shown on its bills; taking one is a supplier credit note. A posted bill may have a planned payment date, kept
-- beside it: a plan posts nothing and changes no version of the bill. Plans are append-only (FY175): the latest for a
-- bill is in force, and one with no date unschedules it. Whoever prepares bills plans them; recording the payment
-- clears the plan by settling the bill, and the payment batch (FR-BNK-12) reads the dates when it comes.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact ADD COLUMN discount_percent numeric(5, 2);
ALTER TABLE fyniq.contact ADD COLUMN discount_days smallint;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_discount_terms CHECK ((discount_percent IS NULL) = (discount_days IS NULL)
  AND (discount_percent IS NULL OR (discount_percent <> 'NaN' AND discount_percent > 0 AND discount_percent < 100))
  AND (discount_days IS NULL OR discount_days BETWEEN 1 AND 365)) NOT VALID;
GRANT INSERT (discount_percent, discount_days), UPDATE (discount_percent, discount_days) ON fyniq.contact TO fyniq_api;

CREATE TABLE fyniq.bill_payment_plan (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  bill_id         uuid        NOT NULL,
  planned_on      date,
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT bill_payment_plan_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bill_payment_plan_bill_fkey FOREIGN KEY (organisation_id, bill_id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT bill_payment_plan_recorded_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX bill_payment_plan_bill ON fyniq.bill_payment_plan (organisation_id, bill_id, recorded_at);

ALTER TABLE fyniq.bill_payment_plan ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bill_payment_plan FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bill_payment_plan
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.bill_payment_plan
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'bill_id');

-- A plan never changes or goes, whoever tries (FY175): a later one replaces it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.bill_payment_plan_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Payment plan % never changes', OLD.id
    USING ERRCODE = 'FY175', HINT = 'Plan the bill again: the latest plan is in force (FR-AP-13).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bill_payment_plan_immutable BEFORE UPDATE OR DELETE ON fyniq.bill_payment_plan
  FOR EACH ROW EXECUTE FUNCTION fyniq.bill_payment_plan_immutable();

GRANT SELECT ON fyniq.bill_payment_plan TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (bill_id, planned_on, recorded_by) ON fyniq.bill_payment_plan TO fyniq_api;
