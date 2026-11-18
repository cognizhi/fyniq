-- The corporate profile (PRD-001 FR-CS-01, FR-CS-06; p1-company-secretary task 2.1, design D2).
--
-- A corporate_detail document holds the company's name, registered office and SSIC code from a day, and the day the
-- change was lodged with ACRA. A corporate_officer document holds one appointment: a director, the secretary, an auditor
-- or the CEO, by name and identifier (the last 4 characters of an individual's), a member of the organisation when they
-- are one, the days they were appointed and ceased and the days each was lodged, and for a director whether they are a
-- nominee and for whom. Each is drafted, then recorded on another person's approval with its evidence; a correction or a
-- later step (a cessation, a lodgement) is a new version, and the last recorded is in force. Neither ever posts.
--
-- The database is the backstop: a detail keeps its own keys, a day, a name, an office, a five-digit SSIC code and its
-- days in order (FY352); an officer keeps its own keys, a known role, a natural person unless an auditor, an
-- identifier kept as the guard of holder records keeps it, its days in order, a nominee only as a director and with
-- whom they act for, a member of the organisation when it names one, and no NRIC or FIN in full, a word of its own as
-- 000276 reads it, so an ID's hex never trips it (FY353). The widened
-- checks are added NOT VALID and validated by 000393.

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
                  'feasibility_study', 'corporate_detail', 'corporate_officer')) NOT VALID;

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
                               ('corporate_officer', 'corporate_officer.record'))) NOT VALID;

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
                  'corporate_detail', 'corporate_officer')) NOT VALID;

-- +goose StatementBegin
-- A corporate detail or officer keeps its rules whoever writes it (FY352, FY353).
CREATE FUNCTION fyniq.corporate_record_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t = 'corporate_detail' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'from', 'name', 'registered_office', 'ssic', 'lodged_on', 'note') LIMIT 1;
    IF extra IS NOT NULL OR coalesce(p->>'from', '') = '' OR length(btrim(coalesce(p->>'name', ''))) NOT BETWEEN 1 AND 200
       OR length(btrim(coalesce(p->>'registered_office', ''))) NOT BETWEEN 1 AND 500
       OR NOT coalesce(p->>'ssic', '') ~ '^[0-9]{5}$' OR length(coalesce(p->>'note', '')) > 1000 THEN
      RAISE EXCEPTION 'A corporate detail keeps its own keys: its day, the name, the registered office and a five-digit SSIC code'
        USING ERRCODE = 'FY352', HINT = 'Record the details as ACRA''s business profile shows them (FR-CS-01).';
    END IF;
    IF p ? 'lodged_on' AND (p->>'lodged_on')::date < (p->>'from')::date THEN
      RAISE EXCEPTION 'A change is lodged on or after the day it takes effect' USING ERRCODE = 'FY352',
        HINT = 'Enter the day BizFile+ acknowledged the change (FR-CS-01).';
    END IF;
    IF p::text ~ '\m[STFGMstfgm][0-9]{7}[A-Za-z]\M' THEN
      RAISE EXCEPTION 'A corporate detail never shows an NRIC or FIN in full' USING ERRCODE = 'FY352',
        HINT = 'Keep the last 4 characters only (CMP-PD-07).';
    END IF;
  ELSIF t = 'corporate_officer' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'officer_id', 'role', 'name', 'member_user_id', 'kind', 'id_type', 'id_last4', 'registration_number',
                     'appointed_on', 'ceased_on', 'appointment_lodged_on', 'cessation_lodged_on', 'nominee', 'nominator', 'note')
     LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(p->>'role', '') IN ('director', 'secretary', 'auditor', 'ceo')
       OR length(btrim(coalesce(p->>'name', ''))) NOT BETWEEN 1 AND 200 OR coalesce(p->>'appointed_on', '') = ''
       OR jsonb_typeof(p->'nominee') IS DISTINCT FROM 'boolean' OR length(coalesce(p->>'note', '')) > 1000 THEN
      RAISE EXCEPTION 'An officer keeps their own keys: a role of director, secretary, auditor or CEO, a name and the day appointed'
        USING ERRCODE = 'FY353', HINT = 'Record the officer as ACRA''s business profile shows them (FR-CS-01).';
    END IF;
    IF p->>'kind' = 'individual' THEN
      IF p ? 'registration_number' OR NOT coalesce(p->>'id_type', 'nric') IN ('nric', 'fin', 'passport')
         OR NOT coalesce(p->>'id_last4', 'X') ~ '^[A-Za-z0-9]{1,4}$' OR (p ? 'id_type') <> (p ? 'id_last4') THEN
        RAISE EXCEPTION 'An individual''s identifier is kept as its type and its last 4 characters only' USING ERRCODE = 'FY353',
          HINT = 'The full number stays in ACRA''s register and the identity document kept as evidence (SEC-15).';
      END IF;
    ELSIF p->>'kind' = 'entity' AND p->>'role' = 'auditor' THEN
      IF p ? 'id_last4' OR p ? 'member_user_id' OR NOT coalesce(p->>'id_type', 'uen') IN ('uen', 'foreign_registration')
         OR length(coalesce(p->>'registration_number', '')) > 30 OR (p ? 'id_type') <> (p ? 'registration_number') THEN
        RAISE EXCEPTION 'An audit firm is identified by its UEN or foreign registration number' USING ERRCODE = 'FY353';
      END IF;
    ELSE
      RAISE EXCEPTION 'A director, secretary or CEO is an individual; an auditor is an individual or a firm' USING ERRCODE = 'FY353',
        HINT = 'Only a natural person may be a director or the secretary (s145, s171).';
    END IF;
    IF (p ? 'ceased_on' AND (p->>'ceased_on')::date < (p->>'appointed_on')::date)
       OR (p ? 'appointment_lodged_on' AND (p->>'appointment_lodged_on')::date < (p->>'appointed_on')::date)
       OR (p ? 'cessation_lodged_on' AND (NOT p ? 'ceased_on' OR (p->>'cessation_lodged_on')::date < (p->>'ceased_on')::date)) THEN
      RAISE EXCEPTION 'An officer ceases on or after their appointment, and each is lodged on or after it' USING ERRCODE = 'FY353',
        HINT = 'Enter the days in order (FR-CS-01).';
    END IF;
    IF (p->>'nominee')::boolean <> (length(btrim(coalesce(p->>'nominator', ''))) BETWEEN 1 AND 200)
       OR ((p->>'nominee')::boolean AND p->>'role' <> 'director') THEN
      RAISE EXCEPTION 'A nominee is a director, and names whom they act for' USING ERRCODE = 'FY353',
        HINT = 'Name the person on whose directions the nominee director acts, or record them as no nominee (s386AL).';
    END IF;
    IF p ? 'member_user_id' AND NOT EXISTS (SELECT 1 FROM fyniq.user_membership m
                                             WHERE m.organisation_id = NEW.organisation_id AND m.user_id::text = p->>'member_user_id') THEN
      RAISE EXCEPTION 'An officer''s member is a member of the organisation' USING ERRCODE = 'FY353',
        HINT = 'Choose a member, or leave it empty for an officer who does not sign in (FR-CS-06).';
    END IF;
    IF p ? 'officer_id' AND NOT EXISTS (SELECT 1 FROM fyniq.document d
                                         WHERE d.organisation_id = NEW.organisation_id AND d.id::text = p->>'officer_id'
                                           AND d.type = 'corporate_officer' AND d.id <> NEW.document_id) THEN
      RAISE EXCEPTION 'A later version names the officer''s first version' USING ERRCODE = 'FY353',
        HINT = 'Revise the officer from their record (FR-CS-01).';
    END IF;
    IF p::text ~ '\m[STFGMstfgm][0-9]{7}[A-Za-z]\M' THEN
      RAISE EXCEPTION 'An officer''s record never shows an NRIC or FIN in full' USING ERRCODE = 'FY353',
        HINT = 'Keep the last 4 characters only (SEC-15, CMP-PD-07).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER corporate_record_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.corporate_record_guard();
