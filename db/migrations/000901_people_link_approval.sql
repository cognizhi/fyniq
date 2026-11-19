-- The link to Nemura's people platform is asked by an Admin or the Owner and approved by the Owner with a second approver (FR-PPL-01,
-- NM-ARC-04, golden rule 6), so the approval engine learns the object type people_link, and the API client table learns the kind link:
-- the one client Nemura calls with, holding Appendix E's scopes and no other (p1-payroll-link design D1). Each widening is NOT VALID here
-- and validated in the next migration.

-- +goose Up
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
                               ('ai_proposal', 'ai_proposal.execute'),
                               ('corporate_detail', 'corporate_detail.record'),
                               ('corporate_officer', 'corporate_officer.record'),
                               ('nominee_entry', 'nominee_entry.record'),
                               ('statements_approval', 'statements_approval.record'),
                               ('corporate_document', 'corporate_document.record'),
                               ('api_client', 'api_client.create'), ('api_client', 'api_client.reclass'),
                               ('kb_publication', 'kb_publication.publish'),
                               ('people_link', 'people_link.link'), ('people_link', 'people_link.unlink'))) NOT VALID;

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
                  'corporate_document', 'api_client', 'kb_publication', 'people_link')) NOT VALID;

ALTER TABLE fyniq.api_client DROP CONSTRAINT api_client_kind_known;
ALTER TABLE fyniq.api_client ADD CONSTRAINT api_client_kind_known CHECK (kind IN ('integration', 'personal', 'link')) NOT VALID;
-- A link client holds Appendix E's scopes only; every other client holds fyniq.read.<area> and fyniq.write.<area> only (no scope approves or releases).
ALTER TABLE fyniq.api_client DROP CONSTRAINT api_client_scopes_format;
ALTER TABLE fyniq.api_client ADD CONSTRAINT api_client_scopes_format CHECK (cardinality(scopes) BETWEEN 1 AND 16 AND (
    (kind <> 'link' AND array_to_string(scopes, ' ') ~ '^fyniq\.(read|write)\.[a-z]+( fyniq\.(read|write)\.[a-z]+)*$')
    OR (kind = 'link' AND array_to_string(scopes, ' ') ~
        '^(people\.projection\.write|people\.availability\.write|people\.holds\.write|people\.holds\.read|payroll\.journal\.submit|payroll\.payments\.submit)( (people\.projection\.write|people\.availability\.write|people\.holds\.write|people\.holds\.read|payroll\.journal\.submit|payroll\.payments\.submit))*$'))) NOT VALID;
ALTER TABLE fyniq.api_client DROP CONSTRAINT api_client_integration_key;
ALTER TABLE fyniq.api_client ADD CONSTRAINT api_client_integration_key CHECK (kind = 'personal' OR (key_thumbprint IS NOT NULL AND approval_id IS NOT NULL)) NOT VALID;
