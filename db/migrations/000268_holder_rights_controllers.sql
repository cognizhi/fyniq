-- Investor rights and registrable controllers (PRD-001 FR-EQ-22, FR-EQ-25; p1-equity-register slice 7, task 8.1,
-- design D10).
--
-- An investor_right document holds a holder's rights from the agreements: board seats, an observer, a board majority,
-- information rights, pro-rata and consent matters, recorded on another person's approval with the agreements attached.
-- A controller_entry document holds the company's conclusion about whether a holder is a registrable controller: the
-- grounds, the days of the company's register and ACRA's central register, the yearly notices, or the reason they are
-- not one, recorded with the company secretary's confirmation. Neither ever posts.
--
-- The database is the backstop: investor rights keep their own keys, a holder marked shareholder or investor, board
-- seats from 0 to 20 and a list of consent matters (FY295); a controller entry keeps its own keys, a holder marked
-- shareholder or investor, grounds for a controller and a reason and no register days for one who is not, and its days
-- in order (FY296). The widened checks are added NOT VALID and validated by 000269.

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
                  'director_confirmation', 'director_settlement', 'director_repayment', 'account_merge',
                  'owner_succession', 'holder_record', 'share_class', 'equity_transaction', 'instrument',
                  'instrument_template', 'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry')) NOT VALID;

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
                               ('director_repayment', 'director_repayment.void'),
                               ('account', 'account.merge'),
                               ('bank_reconciliation', 'bank_reconciliation.sign'),
                               ('holder_record', 'holder_record.record'), ('share_class', 'share_class.record'),
                               ('equity_transaction', 'equity_transaction.approve'),
                               ('instrument', 'instrument.record'),
                               ('instrument_template', 'instrument_template.enable'),
                               ('option_pool', 'option_pool.record'), ('option_grant', 'option_grant.record'),
                               ('option_event', 'option_event.record'), ('round_scenario', 'round_scenario.approve'),
                               ('investor_right', 'investor_right.record'),
                               ('controller_entry', 'controller_entry.record'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction', 'instrument', 'instrument_template',
                  'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry')) NOT VALID;

-- +goose StatementBegin
-- Investor rights keep their rules whoever writes them (FY295).
CREATE FUNCTION fyniq.investor_right_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'investor_right' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k
   WHERE k NOT IN ('date', 'holder_id', 'from', 'board_seats', 'observer', 'board_majority', 'information', 'pro_rata', 'consents',
                   'agreements', 'note') LIMIT 1;
  IF extra IS NOT NULL OR jsonb_typeof(p->'board_seats') IS DISTINCT FROM 'number' OR jsonb_typeof(p->'consents') IS DISTINCT FROM 'array'
     OR jsonb_typeof(p->'observer') IS DISTINCT FROM 'boolean' OR jsonb_typeof(p->'board_majority') IS DISTINCT FROM 'boolean'
     OR jsonb_typeof(p->'pro_rata') IS DISTINCT FROM 'boolean' THEN
    RAISE EXCEPTION 'Investor rights keep their own keys: board seats, an observer, a board majority, pro-rata and consent matters'
      USING ERRCODE = 'FY295', HINT = 'Record the rights the agreements give (FR-EQ-22).';
  END IF;
  IF (p->>'board_seats')::numeric NOT BETWEEN 0 AND 20 OR (p->>'board_seats')::numeric <> trunc((p->>'board_seats')::numeric) THEN
    RAISE EXCEPTION 'Investor rights appoint from 0 to 20 directors' USING ERRCODE = 'FY295',
      HINT = 'Enter the directors the holder may appoint (FR-EQ-22).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.contact c
                  WHERE c.organisation_id = NEW.organisation_id AND c.id::text = p->>'holder_id'
                    AND (c.is_shareholder OR c.is_investor)) THEN
    RAISE EXCEPTION 'Investor rights are of a contact marked shareholder or investor' USING ERRCODE = 'FY295',
      HINT = 'Mark the contact an investor in Contacts first (FR-EQ-15, FR-EQ-22).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER investor_right_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.investor_right_guard();

-- +goose StatementBegin
-- A controller entry keeps its rules whoever writes it (FY296).
CREATE FUNCTION fyniq.controller_entry_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  ctl   boolean;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'controller_entry' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k
   WHERE k NOT IN ('date', 'holder_id', 'controller', 'grounds', 'reason', 'since', 'company_register_on', 'central_register_on',
                   'ceased_on', 'notices', 'note') LIMIT 1;
  IF extra IS NOT NULL OR jsonb_typeof(p->'controller') IS DISTINCT FROM 'boolean' OR jsonb_typeof(p->'grounds') IS DISTINCT FROM 'array'
     OR jsonb_typeof(p->'notices') IS DISTINCT FROM 'array' OR coalesce(p->>'since', '') = '' THEN
    RAISE EXCEPTION 'A controller entry keeps its own keys: the conclusion, its grounds, its day and its notices'
      USING ERRCODE = 'FY296', HINT = 'Record the company''s conclusion about the holder (FR-EQ-25).';
  END IF;
  ctl := (p->>'controller')::boolean;
  IF ctl AND (jsonb_array_length(p->'grounds') = 0 OR coalesce(p->>'reason', '') <> ''
              OR EXISTS (SELECT 1 FROM jsonb_array_elements_text(p->'grounds') g WHERE g NOT IN ('shares', 'votes', 'board', 'influence'))) THEN
    RAISE EXCEPTION 'A registrable controller has known grounds and no reason for not being one' USING ERRCODE = 'FY296',
      HINT = 'Name the grounds: shares, votes, board or influence (FR-EQ-25).';
  END IF;
  IF NOT ctl AND (jsonb_array_length(p->'grounds') > 0 OR btrim(coalesce(p->>'reason', '')) = '' OR p ? 'company_register_on'
                  OR p ? 'central_register_on' OR p ? 'ceased_on' OR jsonb_array_length(p->'notices') > 0) THEN
    RAISE EXCEPTION 'A holder who is not a registrable controller has a reason, and no grounds, register days or notices'
      USING ERRCODE = 'FY296', HINT = 'Say why their rights do not confer control (FR-EQ-25).';
  END IF;
  IF (p ? 'company_register_on' AND (p->>'company_register_on')::date < (p->>'since')::date)
     OR (p ? 'central_register_on' AND (NOT p ? 'company_register_on' OR (p->>'central_register_on')::date < (p->>'company_register_on')::date)) THEN
    RAISE EXCEPTION 'A controller is entered in the company''s register on or after becoming one, and in ACRA''s central register after it'
      USING ERRCODE = 'FY296', HINT = 'Enter the days in order (FR-EQ-25).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.contact c
                  WHERE c.organisation_id = NEW.organisation_id AND c.id::text = p->>'holder_id'
                    AND (c.is_shareholder OR c.is_investor)) THEN
    RAISE EXCEPTION 'A controller entry is of a contact marked shareholder or investor' USING ERRCODE = 'FY296',
      HINT = 'Mark the contact a shareholder or an investor in Contacts first (FR-EQ-15, FR-EQ-25).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER controller_entry_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.controller_entry_guard();
