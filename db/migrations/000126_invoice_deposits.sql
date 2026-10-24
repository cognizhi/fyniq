-- Customer deposits deducted on invoices (PRD-001 FR-AR-09; p1-sales task 3.2, part; the owner's decision of 24 Oct
-- 2026: a deposit is deducted on the final invoice).
--
-- The invoice that deducts a deposit records an allocation from the invoice to the deposit, and a refund of a deposit
-- an allocation from the refund to it. Each use takes its part of the deposit's base amount and of the output tax the
-- deposit bore (RND-DEPOSIT), which the new column tax keeps, in the base currency; tax is not zero only on a use of a
-- deposit, and a reversing allocation negates it with the rest. The widened checks are added NOT VALID and validated
-- by 000127 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.allocation ADD COLUMN tax numeric(20, 4) NOT NULL DEFAULT 0;
ALTER TABLE fyniq.allocation ADD CONSTRAINT allocation_tax CHECK (tax <> 'NaN' AND tax = trunc(tax, base_minor_units)
  AND ((reverses_id IS NULL AND tax >= 0) OR (reverses_id IS NOT NULL AND tax <= 0))) NOT VALID;
GRANT INSERT (tax) ON fyniq.allocation TO fyniq_api;

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
       OR (src = 'customer_receipt' AND dst = 'invoice')
       OR (src = 'write_off' AND dst = 'invoice')
       OR (src = 'customer_credit_application' AND dst IN ('invoice', 'credit_note', 'customer_receipt'))
       OR (src = 'customer_refund' AND dst IN ('credit_note', 'customer_receipt', 'customer_deposit'))
       OR (src = 'invoice' AND dst = 'customer_deposit')) THEN
    RAISE EXCEPTION 'An allocation settles a bill from a bill payment, a bill and a credit from a credit application, a credit from a supplier refund, an invoice from a customer receipt or a write-off, an invoice and a credit from a customer credit application, a credit from a customer refund, or a deposit from the invoice that deducts it, not a % from a %', dst, src
      USING ERRCODE = 'FY171';
  END IF;
  IF NEW.tolerance <> 0 AND src NOT IN ('bill_payment', 'customer_receipt') THEN
    RAISE EXCEPTION 'Only a payment of bills or a customer receipt clears a settlement difference within tolerance, not a %', src
      USING ERRCODE = 'FY171', HINT = 'A larger difference, or one on a credit, is a write-off (FR-BNK-08, FR-AR-17).';
  END IF;
  IF NEW.tax <> 0 AND dst IS DISTINCT FROM 'customer_deposit' THEN
    RAISE EXCEPTION 'Only a use of a customer deposit takes the output tax it bore, not an allocation to a %', dst
      USING ERRCODE = 'FY171', HINT = 'The GST of an invoice or a credit is on its own entry (FR-AR-09).';
  END IF;
  IF NEW.reverses_id IS NOT NULL THEN
    SELECT * INTO orig FROM fyniq.allocation WHERE organisation_id = NEW.organisation_id AND id = NEW.reverses_id;
    IF orig.reverses_id IS NOT NULL OR orig.source_id <> NEW.source_id OR orig.document_id <> NEW.document_id
       OR NEW.on_date < orig.on_date OR NEW.amount <> -orig.amount OR NEW.base_amount <> -orig.base_amount
       OR NEW.realised_fx <> -orig.realised_fx OR NEW.tolerance <> -orig.tolerance OR NEW.tax <> -orig.tax OR NEW.currency <> orig.currency THEN
      RAISE EXCEPTION 'A reversing allocation negates allocation % exactly', NEW.reverses_id USING ERRCODE = 'FY171',
        HINT = 'Reverse an allocation through the application, which negates every amount (FR-AR-18).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
