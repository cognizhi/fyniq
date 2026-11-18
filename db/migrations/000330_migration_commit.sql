-- The workbook migration's commit (PRD-001 §17.1 M3, FR-MIG-01 rev 0.258; p1-workbook-migration task 4.1, design D3).
-- The import is one approval request over the workbook, its validation report and the signed mapping; executing the
-- approval records the commit. The commit then runs in batches, each record written in its own transaction with its
-- migration record: its business key, its kind, its sheet and row, and the record it made, so each figure traces to
-- its row and a batch resumes at the first key not yet imported. Once a migration has a commit, its workbook and
-- mapping are fixed: no step, file or mapping version is added (FY327). A commit is committed once, when it holds every
-- record it planned, and nothing of it changes or goes. Every change is audited on the run (AUD-01). The approval
-- request's object type migration_commit (operation migration.commit) is widened NOT VALID and validated by 000331.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.migration_commit (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  approval_id     uuid        NOT NULL,  -- the approved request that committed it
  path            text        NOT NULL,  -- history or clean_start (§17.1 M3)
  step_id         uuid        NOT NULL,  -- the M1 step whose workbook it imports
  mapping_id      uuid        NOT NULL,  -- the signed mapping version it imports by
  records         integer     NOT NULL,  -- the records it plans
  status          text        NOT NULL DEFAULT 'running',  -- running; committed once every record is imported
  approved_by     text        NOT NULL,
  approved_at     timestamptz NOT NULL,
  committed_at    timestamptz,
  CONSTRAINT migration_commit_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_commit_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_commit_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT migration_commit_step_fkey FOREIGN KEY (organisation_id, step_id) REFERENCES fyniq.migration_step (organisation_id, id),
  CONSTRAINT migration_commit_mapping_fkey FOREIGN KEY (organisation_id, mapping_id) REFERENCES fyniq.migration_mapping (organisation_id, id),
  CONSTRAINT migration_commit_path_known CHECK (path IN ('history', 'clean_start')),
  CONSTRAINT migration_commit_status_known CHECK (status IN ('running', 'committed')),
  CONSTRAINT migration_commit_records_range CHECK (records BETWEEN 1 AND 100000),
  CONSTRAINT migration_commit_committed CHECK ((status = 'committed') = (committed_at IS NOT NULL)),
  CONSTRAINT migration_commit_approved_by_format CHECK (approved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- One commit per migration: a rejected import starts again in a new organisation (rev 0.254).
CREATE UNIQUE INDEX migration_commit_one ON fyniq.migration_commit (organisation_id, run_id);
CREATE INDEX migration_commit_approval ON fyniq.migration_commit (organisation_id, approval_id);
CREATE INDEX migration_commit_step ON fyniq.migration_commit (organisation_id, step_id);
CREATE INDEX migration_commit_mapping ON fyniq.migration_commit (organisation_id, mapping_id);

CREATE TABLE fyniq.migration_record (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  commit_id       uuid        NOT NULL,
  key             text        NOT NULL,  -- the record's business key from its row, unique in the commit
  kind            text        NOT NULL,  -- what the row became, such as contact or invoice
  sheet           text        NOT NULL,  -- the workbook's sheet
  row_number      integer     NOT NULL,  -- its row; 0 for a sheet's settings as a whole
  record_type     text        NOT NULL,  -- the Fyniq record it made: account, contact, document, …
  record_id       uuid        NOT NULL,
  imported_by     text        NOT NULL,
  imported_at     timestamptz NOT NULL,
  CONSTRAINT migration_record_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT migration_record_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES fyniq.migration_run (organisation_id, id),
  CONSTRAINT migration_record_commit_fkey FOREIGN KEY (organisation_id, commit_id) REFERENCES fyniq.migration_commit (organisation_id, id),
  CONSTRAINT migration_record_key UNIQUE (organisation_id, commit_id, key),
  CONSTRAINT migration_record_key_length CHECK (length(key) BETWEEN 1 AND 300),
  CONSTRAINT migration_record_kind_format CHECK (kind ~ '^[a-z][a-z_]{0,39}$'),
  CONSTRAINT migration_record_sheet_length CHECK (length(btrim(sheet)) BETWEEN 1 AND 40),
  CONSTRAINT migration_record_row_range CHECK (row_number BETWEEN 0 AND 1048576),
  CONSTRAINT migration_record_type_format CHECK (record_type ~ '^[a-z][a-z_]{0,39}$'),
  CONSTRAINT migration_record_imported_by_format CHECK (imported_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX migration_record_run ON fyniq.migration_record (organisation_id, run_id);
-- Which row a record came from (design D3).
CREATE INDEX migration_record_made ON fyniq.migration_record (organisation_id, record_type, record_id);

-- The database is the backstop (FY327): a commit is recorded only under its own approved request of its open migration,
-- of that migration's latest valid workbook and its signed latest mapping; it changes once, from running to committed,
-- when it holds every record it planned; a record joins only a running commit of its own migration; nothing changes
-- or goes. Once a migration has a commit, no step, file or mapping version is added to it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.migration_commit_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME IN ('migration_step', 'migration_file', 'migration_mapping') THEN
    IF EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id AND c.run_id = NEW.run_id) THEN
      RAISE EXCEPTION 'The migration is committed: its workbook and mapping are fixed' USING ERRCODE = 'FY327',
        HINT = 'A rejected import starts again in a new organisation (rev 0.254).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A migration''s commit is kept' USING ERRCODE = 'FY327',
      HINT = 'Posted history is reversed, never deleted: a rejected import is abandoned with its organisation.';
  END IF;
  IF TG_TABLE_NAME = 'migration_record' THEN
    IF TG_OP = 'UPDATE' THEN
      RAISE EXCEPTION 'A migration record is kept as it was imported' USING ERRCODE = 'FY327';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_commit c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.commit_id
        AND c.run_id = NEW.run_id AND c.status = 'running') THEN
      RAISE EXCEPTION 'A record joins a running commit of its own migration' USING ERRCODE = 'FY327';
    END IF;
    RETURN NEW;
  END IF;
  -- migration_commit
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'running' THEN
      RAISE EXCEPTION 'A commit starts running' USING ERRCODE = 'FY327';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.migration_run r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.run_id AND r.status = 'open') THEN
      RAISE EXCEPTION 'The migration is closed' USING ERRCODE = 'FY327';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.approval_request a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.approval_id
        AND a.object_type = 'migration_commit' AND a.operation = 'migration.commit' AND a.object_key = NEW.run_id::text
        AND a.status = 'approved') THEN
      RAISE EXCEPTION 'A commit is recorded only under its own approved request' USING ERRCODE = 'FY327';
    END IF;
    IF NEW.step_id IS DISTINCT FROM (SELECT s.id FROM fyniq.migration_step s WHERE s.organisation_id = NEW.organisation_id AND s.run_id = NEW.run_id
        AND s.step = 'M1' ORDER BY s.started_at DESC, s.id DESC LIMIT 1)
        OR NOT EXISTS (SELECT 1 FROM fyniq.migration_step s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.step_id AND s.state = 'valid') THEN
      RAISE EXCEPTION 'A commit imports its migration''s latest workbook, valid' USING ERRCODE = 'FY327';
    END IF;
    IF NEW.mapping_id IS DISTINCT FROM (SELECT m.id FROM fyniq.migration_mapping m WHERE m.organisation_id = NEW.organisation_id AND m.run_id = NEW.run_id
        ORDER BY m.version DESC LIMIT 1)
        OR NOT EXISTS (SELECT 1 FROM fyniq.migration_mapping m JOIN fyniq.migration_signoff o ON o.organisation_id = m.organisation_id AND o.mapping_id = m.id
          WHERE m.organisation_id = NEW.organisation_id AND m.id = NEW.mapping_id AND m.step_id = NEW.step_id) THEN
      RAISE EXCEPTION 'A commit imports by the latest mapping, of its workbook and signed' USING ERRCODE = 'FY327';
    END IF;
    RETURN NEW;
  END IF;
  IF NOT (OLD.status = 'running' AND NEW.status = 'committed' AND NEW.run_id = OLD.run_id AND NEW.approval_id = OLD.approval_id
      AND NEW.path = OLD.path AND NEW.step_id = OLD.step_id AND NEW.mapping_id = OLD.mapping_id AND NEW.records = OLD.records
      AND NEW.approved_by = OLD.approved_by AND NEW.approved_at = OLD.approved_at) THEN
    RAISE EXCEPTION 'A commit changes once, from running to committed' USING ERRCODE = 'FY327';
  END IF;
  IF (SELECT count(*) FROM fyniq.migration_record x WHERE x.organisation_id = NEW.organisation_id AND x.commit_id = NEW.id) <> NEW.records THEN
    RAISE EXCEPTION 'A commit is committed once it holds every record it planned' USING ERRCODE = 'FY327';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER migration_commit_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_commit
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_commit_guard();
CREATE TRIGGER migration_record_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.migration_record
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_commit_guard();
CREATE TRIGGER migration_step_fixed BEFORE INSERT ON fyniq.migration_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_commit_guard();
CREATE TRIGGER migration_file_fixed BEFORE INSERT ON fyniq.migration_file
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_commit_guard();
CREATE TRIGGER migration_mapping_fixed BEFORE INSERT ON fyniq.migration_mapping
  FOR EACH ROW EXECUTE FUNCTION fyniq.migration_commit_guard();
CREATE TRIGGER migration_commit_no_truncate BEFORE TRUNCATE ON fyniq.migration_commit
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER migration_record_no_truncate BEFORE TRUNCATE ON fyniq.migration_record
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.migration_commit
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.migration_record
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('migration_run', 'run_id');

ALTER TABLE fyniq.migration_commit ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_commit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_commit
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.migration_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.migration_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.migration_record
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.migration_commit, fyniq.migration_record FROM PUBLIC;
GRANT SELECT ON fyniq.migration_commit, fyniq.migration_record TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (run_id, approval_id, path, step_id, mapping_id, records, approved_by, approved_at) ON fyniq.migration_commit TO fyniq_api;
GRANT UPDATE (status, committed_at) ON fyniq.migration_commit TO fyniq_api;
GRANT INSERT (run_id, commit_id, key, kind, sheet, row_number, record_type, record_id, imported_by, imported_at) ON fyniq.migration_record TO fyniq_api;

-- The approval request that commits the import, under Appendix D's "Workbook import (migration)" row.
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
                               ('migration_commit', 'migration.commit'))) NOT VALID;

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
                  'migration_commit')) NOT VALID;
