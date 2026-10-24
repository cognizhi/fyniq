-- Settlement tolerances (PRD-001 FR-BNK-08, Appendix B.1 and B.2 "Settlement difference on an invoice or bill"; open
-- question 32, decided by the founders on 16 Oct 2026 with the recommended defaults; p1-sales task 2.2).
--
-- A payment or a receipt that settles a document in full though it pays a little less clears the shortfall to 7030 in
-- its own entry, within the organisation's tolerances: the allocation records the carrying amount the tolerance cleared,
-- in the base currency, so the month's total is read back for the cap per month and the close (C.2 #49). An allocation
-- still never changes; a reversing allocation negates its tolerance too. The sign check is added NOT VALID and validated
-- by 000115 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.allocation ADD COLUMN tolerance numeric(20, 4) NOT NULL DEFAULT 0;
ALTER TABLE fyniq.allocation ADD CONSTRAINT allocation_tolerance CHECK (tolerance <> 'NaN' AND tolerance = trunc(tolerance, base_minor_units)
  AND ((reverses_id IS NULL AND tolerance >= 0) OR (reverses_id IS NOT NULL AND tolerance <= 0))) NOT VALID;
GRANT INSERT (tolerance) ON fyniq.allocation TO fyniq_api;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.allocation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  src text;
  dst text;
  orig fyniq.allocation;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'An allocation never changes or goes' USING ERRCODE = 'FY170',
      HINT = 'Undo it with a reversing allocation (FR-AR-18).';
  END IF;
  SELECT type INTO src FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.source_id;
  SELECT type INTO dst FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF NOT ((src = 'bill_payment' AND dst = 'bill')
       OR (src = 'credit_application' AND dst IN ('bill', 'vendor_credit', 'bill_payment'))
       OR (src = 'supplier_refund' AND dst IN ('vendor_credit', 'bill_payment'))
       OR (src = 'customer_receipt' AND dst = 'invoice')) THEN
    RAISE EXCEPTION 'An allocation settles a bill from a bill payment, a bill and a credit from a credit application, a credit from a supplier refund, or an invoice from a customer receipt, not a % from a %', dst, src
      USING ERRCODE = 'FY171';
  END IF;
  IF NEW.tolerance <> 0 AND src NOT IN ('bill_payment', 'customer_receipt') THEN
    RAISE EXCEPTION 'Only a payment of bills or a customer receipt clears a settlement difference within tolerance, not a %', src
      USING ERRCODE = 'FY171', HINT = 'A larger difference, or one on a credit, is a write-off (FR-BNK-08, FR-AR-17).';
  END IF;
  IF NEW.reverses_id IS NOT NULL THEN
    SELECT * INTO orig FROM fyniq.allocation WHERE organisation_id = NEW.organisation_id AND id = NEW.reverses_id;
    IF orig.reverses_id IS NOT NULL OR orig.source_id <> NEW.source_id OR orig.document_id <> NEW.document_id
       OR NEW.on_date < orig.on_date OR NEW.amount <> -orig.amount OR NEW.base_amount <> -orig.base_amount
       OR NEW.realised_fx <> -orig.realised_fx OR NEW.tolerance <> -orig.tolerance OR NEW.currency <> orig.currency THEN
      RAISE EXCEPTION 'A reversing allocation negates allocation % exactly', NEW.reverses_id USING ERRCODE = 'FY171',
        HINT = 'Reverse an allocation through the application, which negates every amount (FR-AR-18).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
