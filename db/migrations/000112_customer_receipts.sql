-- Customer receipts recorded by hand (PRD-001 FR-AR-18, ENG-41, FM-1; the owner's decision of 24 Oct 2026; p1-sales
-- task 2.1).
--
-- Until bank import exists, money a customer paid is recorded by hand as a document of its own, customer_receipt,
-- numbered from its own sequence (RC-, gap-free) and posted by the rule customer_receipt@1: a payment of bills mirrored
-- onto accounts receivable. What it settles is recorded as allocations (FR-AR-18), one per invoice, as a payment's are
-- per bill; allocation_guard now accepts an allocation from a customer_receipt to an invoice. The part that settles what
-- is left of an invoice in another currency clears the residual RND-FX left on accounts receivable: journal_line_base_only
-- accepts such a line from a customer_receipt entry, on the account with the accounts-receivable role, of at most 1.00
-- in the base currency (FY008).
--
-- Voiding a receipt is FM-1's "voiding a posted document": in Founder mode it waits for a second person, so the approval
-- engine covers the object type customer_receipt with its operation customer_receipt.void. The widened checks are added
-- NOT VALID and validated by 000113 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt')) NOT VALID;

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
                               ('customer_receipt', 'customer_receipt.void'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt')) NOT VALID;

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
  IF NEW.reverses_id IS NOT NULL THEN
    SELECT * INTO orig FROM fyniq.allocation WHERE organisation_id = NEW.organisation_id AND id = NEW.reverses_id;
    IF orig.reverses_id IS NOT NULL OR orig.source_id <> NEW.source_id OR orig.document_id <> NEW.document_id
       OR NEW.on_date < orig.on_date OR NEW.amount <> -orig.amount OR NEW.base_amount <> -orig.base_amount
       OR NEW.realised_fx <> -orig.realised_fx OR NEW.currency <> orig.currency THEN
      RAISE EXCEPTION 'A reversing allocation negates allocation % exactly', NEW.reverses_id USING ERRCODE = 'FY171',
        HINT = 'Reverse an allocation through the application, which negates every amount (FR-AR-18).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_line_base_only() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  source text;
  stray  integer;
  role   text;
BEGIN
  SELECT source_type INTO source FROM fyniq.journal_entry WHERE organisation_id = NEW.organisation_id AND id = NEW.entry_id;
  IF source IN ('bill_payment', 'credit_application', 'supplier_refund') THEN
    SELECT a.role INTO role FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.account_id;
    IF role IS DISTINCT FROM 'accounts-payable' OR abs(NEW.base_amount) > 1 THEN
      RAISE EXCEPTION 'A % changes a base value alone only to clear the residual RND-FX left of a document on accounts payable, at most 1.00 %', source, NEW.base_currency
        USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; a larger difference is a write-off (INV-05, FR-BNK-08).';
    END IF;
    RETURN NULL;
  END IF;
  IF source = 'customer_receipt' THEN
    SELECT a.role INTO role FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.account_id;
    IF role IS DISTINCT FROM 'accounts-receivable' OR abs(NEW.base_amount) > 1 THEN
      RAISE EXCEPTION 'A customer receipt changes a base value alone only to clear the residual RND-FX left of an invoice on accounts receivable, at most 1.00 %', NEW.base_currency
        USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; a larger difference is a write-off (INV-05, FR-AR-17).';
    END IF;
    RETURN NULL;
  END IF;
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
