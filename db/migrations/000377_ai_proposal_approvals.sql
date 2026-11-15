-- An AI proposal is an approval request (PRD-001 AI-89, AI-P4, AI-P5, AI-10; p1-ai-proposals-core task 4.1, design D4; the
-- owner's decision of 15 Nov 2026, rev 0.280).
--
-- A proposal that becomes Pending is at once a request of object type ai_proposal, whose operation ai_proposal.execute
-- queues the approved proposal for the executor. Two exceptions to the approval rules hold for ai_proposal alone: its
-- requester, the person who asked, is authenticated by their live turn (ai_turn, within the delegated token's 5 minutes)
-- instead of a fresh step-up, and that person may sign their own tier-1 proposal (AI-10). Every signer still passes
-- step-up, the signature still signs the envelope's hash, and every other rule of 000030 to 000047 stands.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

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
                               ('ai_proposal', 'ai_proposal.execute'))) NOT VALID;
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
                  'migration_commit', 'tenant_export', 'ai_connection', 'ai_data_policy', 'ai_route', 'ai_proposal')) NOT VALID;

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_mfa_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_mfa_known CHECK (
  requester_mfa_method IN ('session_step_up', 'passkey', 'totp') OR (object_type = 'ai_proposal' AND requester_mfa_method = 'ai_turn')
) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.approval_signature_check() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r fyniq.approval_request;
  c record;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL
       OR (NEW.request_id, NEW.step, NEW.signer, NEW.decision, NEW.envelope_hash, NEW.mfa_method, NEW.mfa_at, NEW.signed_at, NEW.reason,
           NEW.passkey_challenge_id, NEW.passkey_id, NEW.authenticator_data, NEW.client_data, NEW.assertion_signature)
          IS DISTINCT FROM (OLD.request_id, OLD.step, OLD.signer, OLD.decision, OLD.envelope_hash, OLD.mfa_method, OLD.mfa_at, OLD.signed_at,
           OLD.reason, OLD.passkey_challenge_id, OLD.passkey_id, OLD.authenticator_data, OLD.client_data, OLD.assertion_signature) THEN
      RAISE EXCEPTION 'A signed approval is never changed, only voided once' USING ERRCODE = 'FY102';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  IF r.envelope_hash IS DISTINCT FROM NEW.envelope_hash THEN
    RAISE EXCEPTION 'The signed hash is not the hash of the request''s envelope' USING ERRCODE = 'FY100',
      HINT = 'Sign the envelope the request holds, exactly (FR-APR-03, FR-APR-10).';
  END IF;
  -- The person who asked may sign their own tier-1 AI proposal (AI-10, AI-89): ai_proposal's alone.
  IF r.requested_by = NEW.signer AND NOT (r.object_type = 'ai_proposal' AND EXISTS (
       SELECT 1 FROM fyniq.ai_proposal p
        WHERE p.organisation_id = r.organisation_id AND p.approval_request_id = r.id AND p.risk_tier = 1
          AND p.requested_by = NEW.signer)) THEN
    RAISE EXCEPTION 'The requester never signs their own request' USING ERRCODE = 'FY101',
      HINT = 'Another member who holds the approval right signs it (FM-1).';
  END IF;
  IF r.status <> 'pending' OR NEW.signed_at >= r.expires_at OR NEW.signed_at < r.requested_at OR NEW.voided_at IS NOT NULL THEN
    RAISE EXCEPTION 'The request is not pending, or it expired at %', r.expires_at USING ERRCODE = 'FY103',
      HINT = 'Ask for the change again (FR-APR-05).';
  END IF;
  IF NEW.step > jsonb_array_length(r.policy_steps)
     OR NOT fyniq.approval_steps_met(r.organisation_id, r.id, r.envelope_hash, r.policy_steps, NEW.step - 1)
     OR (SELECT count(*) FROM fyniq.approval_signature s
          WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.step = NEW.step AND s.decision = 'approve'
            AND s.voided_at IS NULL AND s.envelope_hash = r.envelope_hash) >= (r.policy_steps -> (NEW.step - 1) ->> 'quorum')::integer THEN
    RAISE EXCEPTION 'Step % is not the request''s current step', NEW.step USING ERRCODE = 'FY106',
      HINT = 'Sign the first step whose quorum is not met yet; the steps are signed in order (FR-APR-01).';
  END IF;
  IF NEW.mfa_method = 'passkey' THEN
    SELECT * INTO c FROM fyniq.webauthn_approval_proof(NEW.passkey_challenge_id);
    IF c.organisation_id IS DISTINCT FROM NEW.organisation_id OR c.request_id IS DISTINCT FROM NEW.request_id
       OR c.envelope_hash IS DISTINCT FROM NEW.envelope_hash OR c.user_id IS DISTINCT FROM NEW.signer
       OR c.answered_by IS DISTINCT FROM NEW.passkey_id OR c.taken_at IS NULL OR NEW.signed_at - c.taken_at > interval '5 minutes'
       OR EXISTS (SELECT 1 FROM fyniq.approval_signature s
                   WHERE s.organisation_id = NEW.organisation_id AND s.passkey_challenge_id = NEW.passkey_challenge_id) THEN
      RAISE EXCEPTION 'A passkey-signed approval is the signer''s answer to a challenge over this request''s envelope'
        USING ERRCODE = 'FY142', HINT = 'Ask for a new challenge for this request and confirm it''s you again (FR-APR-03).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

