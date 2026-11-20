-- A financial year's tax working paper kept as a document of its own (PRD-001 FR-TAX-06, rev 0.608; p2-tax-filings task 2.3): the adjustments the tax agent enters with their reasons,
-- drafted so evidence can attach (the evidence table holds files of documents only, FR-DOC-05), changed while a draft, then recorded with the figures it worked out, from when it
-- changes no more and its files are committed to the retention-locked store (FR-DOC-13). Nothing but the document-type check changes: the document, its versions and its evidence
-- are the tables every record already uses, with the grants they already have. The widened check is added NOT VALID and validated by 001123.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('account_merge', 'acra_extract', 'asset_disposal', 'bad_debt_relief', 'bank_agreement', 'bank_transaction', 'bill',
                  'bill_payment', 'board_pack_pdf', 'budget', 'cash_advance', 'claim_reimbursement', 'claimant_bank_change', 'controller_entry',
                  'corporate_detail', 'corporate_document', 'corporate_officer', 'credit_application', 'credit_note', 'customer_credit_application',
                  'customer_deposit', 'customer_receipt', 'customer_refund', 'director_confirmation', 'director_fee', 'director_pay_choice',
                  'director_repayment', 'director_settlement', 'equity_transaction', 'exempt_status', 'expense_claim', 'feasibility_study',
                  'fixed_asset', 'forecast', 'fx_revaluation', 'gst_forecast', 'holder_record', 'instrument',
                  'instrument_template', 'investor_right', 'invoice', 'journal', 'nominee_entry', 'opening_balance',
                  'option_event', 'option_grant', 'option_pool', 'owner_succession', 'payee_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'purchase_order', 'quote', 'round_scenario', 'sales_order', 'share_class',
                  'statement', 'statements_approval', 'statutory_filing', 'subscription_file', 'supplier_refund', 'tax_working_paper', 'vendor_credit',
                  'write_off')) NOT VALID;
