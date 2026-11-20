-- The board pack's PDF kept as evidence (PRD-001 FR-RPT-06, FR-DOC-05, FR-DOC-06; p2-reporting-planning task 3.1, rev 0.607).
--
-- The PDF of a published board pack is a file Fyniq made. Keeping it is a request another person approves (the approval engine's object board_pack_pdf, operation board_pack_pdf.keep;
-- in Founder mode the other founder): the PDF is attached as generated evidence to a document of its own, board_pack_pdf, one per pack, a draft until the approval, and posted by it; once
-- posted its file is committed to the retention-locked store and is append-only (FR-DOC-13). It posts nothing to the ledger and is never reversed. The widened checks are added NOT VALID
-- and validated by 001111.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('account_merge', 'acra_extract', 'asset_disposal', 'bank_agreement', 'bank_transaction', 'bill',
                  'bill_payment', 'board_pack_pdf', 'budget', 'cash_advance', 'claim_reimbursement', 'claimant_bank_change', 'controller_entry',
                  'corporate_detail', 'corporate_document', 'corporate_officer', 'credit_application', 'credit_note', 'customer_credit_application',
                  'customer_deposit', 'customer_receipt', 'customer_refund', 'director_confirmation', 'director_fee', 'director_pay_choice',
                  'director_repayment', 'director_settlement', 'equity_transaction', 'exempt_status', 'expense_claim', 'feasibility_study',
                  'fixed_asset', 'forecast', 'fx_revaluation', 'gst_forecast', 'holder_record', 'instrument',
                  'instrument_template', 'investor_right', 'invoice', 'journal', 'nominee_entry', 'opening_balance',
                  'option_event', 'option_grant', 'option_pool', 'owner_succession', 'payee_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'purchase_order', 'quote', 'round_scenario', 'sales_order', 'share_class',
                  'statement', 'statements_approval', 'statutory_filing', 'subscription_file', 'supplier_refund', 'vendor_credit',
                  'write_off')) NOT VALID;

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
                               ('statements_approval', 'statements_approval.record'),
                               ('corporate_document', 'corporate_document.record'),
                               ('api_client', 'api_client.create'), ('api_client', 'api_client.reclass'),
                               ('kb_publication', 'kb_publication.publish'),
                               ('people_link', 'people_link.link'), ('people_link', 'people_link.unlink'),
                               ('salary_batch', 'salary_batch.approve'),
                               ('people_link', 'people_link.source'),
                               ('board_pack_pdf', 'board_pack_pdf.keep'))) NOT VALID;

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
                  'corporate_detail', 'corporate_officer', 'nominee_entry', 'statements_approval',
                  'corporate_document', 'api_client', 'kb_publication', 'people_link', 'salary_batch', 'board_pack_pdf')) NOT VALID;

-- +goose StatementBegin
-- A kept board pack PDF is posted only under an approved board_pack_pdf.keep request that its requester did not sign alone, with a file attached, and only once for a pack; it is never
-- reversed (FYR12, FYR13; FR-RPT-06, CTL-FIN-07).
CREATE FUNCTION fyniq.board_pack_pdf_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  pack text;
BEGIN
  IF NEW.status = 'reversed' THEN
    RAISE EXCEPTION 'The kept PDF of board pack % stays as it was', NEW.id
      USING ERRCODE = 'FYR12', HINT = 'Evidence is append-only (FR-DOC-13): keep the corrected version''s PDF instead.';
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'board_pack_pdf' AND r.operation = 'board_pack_pdf.keep'
          AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND EXISTS (SELECT 1 FROM fyniq.approval_signature s
                       WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.signer <> r.requested_by AND s.decision = 'approve' AND s.voided_at IS NULL)) THEN
    RAISE EXCEPTION 'The PDF of board pack % is kept only on another person''s approval', NEW.id
      USING ERRCODE = 'FYR12', HINT = 'Ask for approval: someone who did not ask for it approves keeping the PDF (FR-RPT-06).';
  END IF;
  SELECT v.payload->>'pack_id' INTO pack FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF EXISTS (
       SELECT 1 FROM fyniq.document d JOIN fyniq.document_version v
         ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
        WHERE d.organisation_id = NEW.organisation_id AND d.type = 'board_pack_pdf' AND d.status = 'posted' AND d.id <> NEW.id
          AND v.payload->>'pack_id' = pack) THEN
    RAISE EXCEPTION 'The PDF of board pack % is kept already', pack
      USING ERRCODE = 'FYR13', HINT = 'A pack keeps one PDF; a correction is the next version of the pack, with its own.';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER board_pack_pdf_guard AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'board_pack_pdf' AND NEW.status IN ('posted', 'reversed'))
  EXECUTE FUNCTION fyniq.board_pack_pdf_guard();
