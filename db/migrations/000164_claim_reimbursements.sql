-- Reimbursements of expense claims, recorded by hand (PRD-001 FR-AP-07; p1-expense-claims task 3.1, design D4 and D7;
-- the owner's decisions of 26 Oct 2026, rev 0.147 and 0.148).
--
-- Until payment batches exist, the money paid back to a claimant is a claim_reimbursement document recorded by hand and
-- posted at once by claim_reimbursement@1 (Dr 2150 · Cr the money account), with an allocation to each claim it
-- settles: allocation_guard lets a reimbursement settle an expense claim (FY171), and bank import matches a bank line
-- to it as money recorded by hand (bank_match_posting, FY228). Voiding one is an approval object: in Founder mode a
-- second person who holds A on "Payment batches" approves claim_reimbursement.void (FM-1). The widened checks are added
-- NOT VALID and validated by 000165 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

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
       OR (src = 'claim_reimbursement' AND dst = 'expense_claim')) THEN
    RAISE EXCEPTION 'An allocation settles a bill from a bill payment, a bill and a credit from a credit application, a credit from a supplier refund, an invoice from a customer receipt or a write-off, an invoice and a credit from a customer credit application, a credit from a customer refund, a deposit from the invoice that deducts it, or an expense claim from a reimbursement, not a % from a %', dst, src
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
    ELSE ARRAY['customer_receipt', 'bill_payment', 'customer_deposit', 'customer_refund', 'supplier_refund', 'claim_reimbursement'] END;
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

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
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
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement')) NOT VALID;
