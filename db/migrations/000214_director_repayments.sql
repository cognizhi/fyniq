-- Directors' repayments through payment batches (PRD-001 FR-EQ-05, FR-BNK-12, FM-1, FM-9; p1-founders-money task 4.3,
-- design D10).
--
-- A settlement approved to be repaid is paid by a payment batch: an item of the kind director names it, and the batch's
-- release records what it pays each director as a director_repayment document, numbered DR- (Dr 2500/2120 naming the
-- person · Cr the money account), with an allocation to each settlement it repays, and links it to the release. Bank
-- import matches it to the statement line as other money recorded for a batch (FY228); one the bank rejected is voided
-- on another person's approval (director_repayment.void), never the person's own. The database is the backstop:
--   - an allocation settles a director's settlement only from a repayment (FY171);
--   - a release links a repayment of its account and date as it does a bill payment or a reimbursement (FY240);
--   - a repayment is posted only with a release linking it, so nobody records one by hand (FY254, at commit).
-- The widened checks are added NOT VALID and validated by 000215 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'director_pay_choice', 'director_fee', 'exempt_status',
                  'director_confirmation', 'director_settlement', 'director_repayment')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                           'asset_disposal', 'payment_batch', 'pre_incorporation_cost', 'director_fee',
                           'director_settlement', 'director_repayment')) NOT VALID;

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('membership', 'member.remove'),
                               ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'),
                               ('credit_application', 'credit_application.void'),
                               ('supplier_refund', 'supplier_refund.void'),
                               ('purchase_order', 'purchase_order.approve'),
                               ('document_template', 'document_template.publish'),
                               ('invoice', 'invoice.post'), ('invoice', 'invoice.void'), ('invoice', 'invoice.edit'),
                               ('customer_receipt', 'customer_receipt.void'),
                               ('write_off', 'write_off.post'), ('write_off', 'write_off.void'),
                               ('customer_credit_application', 'customer_credit_application.void'),
                               ('customer_refund', 'customer_refund.void'),
                               ('customer_deposit', 'customer_deposit.void'),
                               ('recurring_invoice', 'recurring_invoice.approve'),
                               ('connector', 'connector.change'), ('bank_rule', 'bank_rule.auto_apply'),
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'),
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'),
                               ('contact_member_link', 'contact_member_link.change'),
                               ('director_fee', 'director_fee.post'),
                               ('director_settlement', 'director_settlement.approve'),
                               ('director_repayment', 'director_repayment.void'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment')) NOT VALID;

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
       OR (src = 'invoice' AND dst = 'customer_deposit')
       OR (src = 'claim_reimbursement' AND dst = 'expense_claim')
       OR (src IN ('expense_claim', 'cash_advance') AND dst = 'cash_advance')
       OR (src = 'director_repayment' AND dst = 'director_settlement')) THEN
    RAISE EXCEPTION 'An allocation settles a bill from a bill payment, a bill and a credit from a credit application, a credit from a supplier refund, an invoice from a customer receipt or a write-off, an invoice and a credit from a customer credit application, a credit from a customer refund, a deposit from the invoice that deducts it, an expense claim from a reimbursement, a cash advance from a claim or a refund, or a director''s settlement from a repayment, not a % from a %', dst, src
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.bank_match_posting() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  l     fyniq.bank_line;
  kinds text[];
BEGIN
  SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.line_id;
  kinds := CASE NEW.kind
    WHEN 'categorised' THEN ARRAY['bank_transaction']
    WHEN 'suspense' THEN ARRAY['bank_transaction']
    WHEN 'receipt' THEN ARRAY['customer_receipt']
    WHEN 'bill_payment' THEN ARRAY['bill_payment']
    ELSE ARRAY['customer_receipt', 'bill_payment', 'customer_deposit', 'customer_refund', 'supplier_refund', 'claim_reimbursement', 'cash_advance',
               'asset_disposal', 'director_repayment'] END;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id AND d.type = ANY (kinds) AND d.status = 'posted'
      AND (d.type <> 'bank_transaction' OR EXISTS (
        SELECT 1 FROM fyniq.document_version v WHERE v.organisation_id = d.organisation_id AND v.document_id = d.id
          AND v.version = d.current_version AND v.payload->>'bank_line_id' = NEW.line_id::text))
      AND EXISTS (
        SELECT 1 FROM fyniq.journal_entry e
        JOIN fyniq.money_account m ON m.organisation_id = e.organisation_id AND m.id = l.money_account_id
        JOIN fyniq.journal_line jl ON jl.organisation_id = e.organisation_id AND jl.entry_id = e.id AND jl.account_id = m.account_id
        WHERE e.organisation_id = NEW.organisation_id AND e.source_id = d.id::text AND e.source_version = d.current_version
          AND e.reverses_entry_id IS NULL AND jl.currency = l.currency AND jl.amount = l.amount
          AND (NEW.entry_id IS NULL OR (e.id = NEW.entry_id AND e.posting_date = l.booking_date))
          AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id))
  ) THEN
    RAISE EXCEPTION 'The % match of bank line % names no document of that kind, or one that does not post the line''s amount through its account', NEW.kind, NEW.line_id
      USING ERRCODE = 'FY228', HINT = 'Match the line through Banking, which records its document with the line''s amount and date (FR-BNK-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- A release's payment is a posted bill payment, reimbursement or director's repayment of the batch's money account and
-- payment date, linked once, and never changed, deleted or truncated (FY240).
CREATE OR REPLACE FUNCTION fyniq.payment_batch_payment_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  b jsonb;
  p jsonb;
  t text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The payments of payment batches are never deleted' USING ERRCODE = 'FY240';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The payment % of a batch is never changed or deleted', OLD.payment_id USING ERRCODE = 'FY240';
  END IF;
  SELECT v.payload INTO b FROM fyniq.payment_batch_release r
    JOIN fyniq.document_version v ON v.organisation_id = r.organisation_id AND v.document_id = r.batch_id AND v.version = r.batch_version
   WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.release_id;
  SELECT d.type, v.payload INTO t, p FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.payment_id AND d.status = 'posted';
  IF t IS NULL OR t NOT IN ('bill_payment', 'claim_reimbursement', 'director_repayment') OR b IS NULL
     OR p->>'money_account_id' IS DISTINCT FROM b->>'money_account_id' OR p->>'date' IS DISTINCT FROM b->>'date' THEN
    RAISE EXCEPTION 'Payment % is not a posted bill payment, reimbursement or director''s repayment of the batch''s account and date', NEW.payment_id
      USING ERRCODE = 'FY240', HINT = 'A batch links only the payments its release recorded (FR-BNK-12).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- A director's repayment is posted only with a release of a payment batch linking it, in the same transaction (FY254;
-- FR-EQ-05, FM-1): what the company repays a director is paid by a batch another person approved.
CREATE FUNCTION fyniq.director_repayment_released() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.payment_batch_payment p WHERE p.organisation_id = NEW.organisation_id AND p.payment_id = NEW.id) THEN
    RAISE EXCEPTION 'Repayment % to a director is posted only by the release of a payment batch', NEW.id
      USING ERRCODE = 'FY254', HINT = 'Add the settlement to a payment batch: another person approves the batch, and its release pays it (FR-EQ-05, FM-1).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER director_repayment_released AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'director_repayment' AND NEW.status = 'posted')
  EXECUTE FUNCTION fyniq.director_repayment_released();
