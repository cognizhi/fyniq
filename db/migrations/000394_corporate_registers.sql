-- The registers of nominee directors and nominee shareholders, and the directors' approval of the financial statements
-- (PRD-001 FR-CS-01, FR-CS-03; p1-company-secretary task 2.2, design D2 and D4).
--
-- A nominee_entry document is one entry of the register of nominee directors or of nominee shareholders (s386AL): a
-- director among the officers, or a holder of the equity register, who acts or holds for another; since when, the day
-- they informed the company (30 days), the day the company's register entered it (7 days) and the day ACRA's central
-- register was told (2 business days after), and the day they ceased. A statements_approval document is the directors'
-- approval of a financial year's statements: the year's end, the day they approved them and the day they were sent to the
-- members. Each is drafted, then recorded on another person's approval with its evidence; a later step or a correction is
-- a new version, and the last recorded is in force. Neither ever posts.
--
-- The database is the backstop: a nominee entry keeps its own keys, a known kind, a director among the officers or a
-- contact marked shareholder or investor, whom they act for, and its days in order (FY354); an approval keeps its own
-- keys, a year's end and the days approved and sent after it, in order (FY355). The widened checks are added NOT VALID
-- and validated by 000395.

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
                  'instrument_template', 'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right',
                  'controller_entry', 'acra_extract', 'gst_forecast', 'statutory_filing', 'forecast', 'budget',
                  'feasibility_study', 'corporate_detail', 'corporate_officer',
                  'nominee_entry', 'statements_approval')) NOT VALID;

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
                               ('controller_entry', 'controller_entry.record'),
                               ('budget', 'budget.record'),
                               ('migration_commit', 'migration.commit'),
                               ('tenant_export', 'tenant_export.prepare'),
                               ('ai_connection', 'ai_connection.create'), ('ai_connection', 'ai_connection.change'),
                               ('ai_connection', 'ai_connection.credential'), ('ai_connection', 'ai_connection.enable'),
                               ('ai_data_policy', 'ai_data_policy.change'),
                               ('ai_connection', 'ai_connection.allow_model'),
                               ('ai_connection', 'ai_connection.price'),
                               ('ai_route', 'ai_route.change'),
                               ('ai_proposal', 'ai_proposal.execute'),
                               ('corporate_detail', 'corporate_detail.record'),
                               ('corporate_officer', 'corporate_officer.record'),
                               ('nominee_entry', 'nominee_entry.record'),
                               ('statements_approval', 'statements_approval.record'))) NOT VALID;

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
                  'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry', 'budget',
                  'migration_commit', 'tenant_export', 'ai_connection', 'ai_data_policy', 'ai_route', 'ai_proposal',
                  'corporate_detail', 'corporate_officer', 'nominee_entry', 'statements_approval')) NOT VALID;

-- +goose StatementBegin
-- A nominee entry or a statements approval keeps its rules whoever writes it (FY354, FY355).
CREATE FUNCTION fyniq.corporate_register_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t = 'nominee_entry' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'entry_id', 'kind', 'officer_id', 'holder_id', 'nominator', 'since', 'informed_on', 'register_on',
                     'central_on', 'ceased_on', 'note') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(p->>'kind', '') IN ('director', 'shareholder') OR coalesce(p->>'since', '') = ''
       OR length(btrim(coalesce(p->>'nominator', ''))) NOT BETWEEN 1 AND 200 OR length(coalesce(p->>'note', '')) > 1000
       OR (p->>'kind' = 'director') <> (p ? 'officer_id') OR (p->>'kind' = 'shareholder') <> (p ? 'holder_id') THEN
      RAISE EXCEPTION 'A nominee entry keeps its own keys: a director or a shareholder, whom they act for, and since when'
        USING ERRCODE = 'FY354', HINT = 'Record the entry as the register of nominee directors or shareholders holds it (s386AL).';
    END IF;
    IF (p ? 'informed_on' AND (p->>'informed_on')::date < (p->>'since')::date)
       OR (p ? 'register_on' AND (p->>'register_on')::date < (p->>'since')::date)
       OR (p ? 'central_on' AND (NOT p ? 'register_on' OR (p->>'central_on')::date < (p->>'register_on')::date))
       OR (p ? 'ceased_on' AND (p->>'ceased_on')::date < (p->>'since')::date) THEN
      RAISE EXCEPTION 'A nominee entry''s days are in order: informed and entered on or after they became one, ACRA told after the register'
        USING ERRCODE = 'FY354', HINT = 'Enter the days in order (s386AL).';
    END IF;
    IF p->>'kind' = 'director' AND NOT EXISTS (
         SELECT 1 FROM fyniq.document d
          WHERE d.organisation_id = NEW.organisation_id AND d.id::text = p->>'officer_id' AND d.type = 'corporate_officer') THEN
      RAISE EXCEPTION 'A nominee director is an officer of the corporate profile' USING ERRCODE = 'FY354',
        HINT = 'Record the director among the officers first (FR-CS-01).';
    END IF;
    IF p->>'kind' = 'shareholder' AND NOT EXISTS (
         SELECT 1 FROM fyniq.contact c
          WHERE c.organisation_id = NEW.organisation_id AND c.id::text = p->>'holder_id' AND (c.is_shareholder OR c.is_investor)) THEN
      RAISE EXCEPTION 'A nominee shareholder is a contact marked shareholder or investor' USING ERRCODE = 'FY354',
        HINT = 'Mark the contact a shareholder in Contacts first (FR-EQ-15).';
    END IF;
    IF p ? 'entry_id' AND NOT EXISTS (SELECT 1 FROM fyniq.document d
                                       WHERE d.organisation_id = NEW.organisation_id AND d.id::text = p->>'entry_id'
                                         AND d.type = 'nominee_entry' AND d.id <> NEW.document_id) THEN
      RAISE EXCEPTION 'A later version names the entry''s first version' USING ERRCODE = 'FY354',
        HINT = 'Revise the entry from its record (FR-CS-01).';
    END IF;
    IF p::text ~ '\m[STFGMstfgm][0-9]{7}[A-Za-z]\M' THEN
      RAISE EXCEPTION 'A nominee entry never shows an NRIC or FIN in full' USING ERRCODE = 'FY354',
        HINT = 'Keep the last 4 characters only (SEC-15, CMP-PD-07).';
    END IF;
  ELSIF t = 'statements_approval' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k WHERE k NOT IN ('date', 'fy_end', 'approved_on', 'sent_on', 'note') LIMIT 1;
    IF extra IS NOT NULL OR coalesce(p->>'fy_end', '') = '' OR coalesce(p->>'approved_on', '') = ''
       OR length(coalesce(p->>'note', '')) > 1000 THEN
      RAISE EXCEPTION 'A statements approval keeps its own keys: the financial year''s end and the day the directors approved them'
        USING ERRCODE = 'FY355', HINT = 'Record the directors'' approval of a year''s financial statements (FR-CS-03).';
    END IF;
    IF (p->>'approved_on')::date <= (p->>'fy_end')::date
       OR (p ? 'sent_on' AND (p->>'sent_on')::date < (p->>'approved_on')::date) THEN
      RAISE EXCEPTION 'Statements are approved after their year ends, and sent to the members on or after they are approved'
        USING ERRCODE = 'FY355', HINT = 'Enter the days in order (s201).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER corporate_register_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.corporate_register_guard();
