-- Payment batches (PRD-001 FR-BNK-12, FR-AP-13, SOD-1, SOD-2, FM-1, FM-3, SEC-11; the owner's decisions of 27 and 28 Oct
-- 2026, rev 0.154 and rev 0.156; p1-payments-out task 2.1, design D1, D5, D7).
--
-- A batch is a document of its own type, payment_batch: prepared as a draft that names a money account, a payment date,
-- the method and its items (approved bills and expense claims, each paid whole), approved by a second person through the
-- approval engine, which posts it (numbered PB-) with no entry, and released once. Its release is a record of its own,
-- payment_batch_release, with the file it wrote sealed by the application (SEC-11) and the SHA-256 of the clear file;
-- payment_batch_payment links the release to each payment it posted, a bill payment or a reimbursement recorded as one
-- is by hand. The database is the backstop:
--   - a release is written once, for an approved batch at its current version, and never changed or deleted (FY239); an
--     approved batch is cancelled (reversed) only while it is not released;
--   - a link names a posted bill payment or reimbursement of the batch's money account and payment date, once (FY240);
--   - a payment recorded by hand of a bill or claim in an approved, unreleased batch is refused (FY241): the batch pays it;
--   - a batch is approved only under an approved request that neither its requester nor its preparer signed (FY242).
-- The widened checks are added NOT VALID and validated by 000186 (squawk: constraint-missing-not-valid).

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
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                           'asset_disposal', 'payment_batch')) NOT VALID;

CREATE TABLE fyniq.payment_batch_release (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  batch_id        uuid           NOT NULL,
  batch_version   integer        NOT NULL,
  approval_id     uuid           NOT NULL,  -- the approval it was released under (G4-055)
  released_by     uuid           NOT NULL,
  released_at     timestamptz    NOT NULL,
  writer          text           NOT NULL,  -- the file's writer and its version, such as payment-list@1
  file_name       text           NOT NULL,
  media_type      text           NOT NULL,
  file_sealed     bytea          NOT NULL,  -- sealed by the application (SEC-11), bound to the batch
  file_sha256     bytea          NOT NULL,  -- of the clear file, so a downloaded copy can be checked
  payments        integer        NOT NULL,
  currency        text           NOT NULL,
  minor_units     smallint       NOT NULL,
  total           numeric(20, 4) NOT NULL,
  CONSTRAINT payment_batch_release_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payment_batch_release_once UNIQUE (organisation_id, batch_id),
  CONSTRAINT payment_batch_release_version_fkey FOREIGN KEY (organisation_id, batch_id, batch_version)
    REFERENCES fyniq.document_version (organisation_id, document_id, version),
  CONSTRAINT payment_batch_release_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT payment_batch_release_by_fkey FOREIGN KEY (released_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT payment_batch_release_writer_format CHECK (writer ~ '^[a-z0-9-]+@[0-9]+$'),
  CONSTRAINT payment_batch_release_name_length CHECK (length(file_name) BETWEEN 1 AND 200),
  CONSTRAINT payment_batch_release_media_length CHECK (length(media_type) BETWEEN 3 AND 100),
  CONSTRAINT payment_batch_release_sealed_length CHECK (octet_length(file_sealed) BETWEEN 40 AND 16777216),
  CONSTRAINT payment_batch_release_sha256_length CHECK (octet_length(file_sha256) = 32),
  CONSTRAINT payment_batch_release_payments_positive CHECK (payments > 0),
  CONSTRAINT payment_batch_release_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT payment_batch_release_minor_units CHECK (minor_units BETWEEN 0 AND 4),
  CONSTRAINT payment_batch_release_total_scale CHECK (total <> 'NaN' AND total = trunc(total, minor_units) AND total > 0)
);

CREATE TABLE fyniq.payment_batch_payment (
  organisation_id uuid NOT NULL DEFAULT fyniq.current_org(),
  id              uuid NOT NULL DEFAULT uuidv7(),
  release_id      uuid NOT NULL,
  payment_id      uuid NOT NULL,  -- a posted bill payment or reimbursement the release recorded
  CONSTRAINT payment_batch_payment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payment_batch_payment_once UNIQUE (organisation_id, payment_id),
  CONSTRAINT payment_batch_payment_release_fkey FOREIGN KEY (organisation_id, release_id) REFERENCES fyniq.payment_batch_release (organisation_id, id),
  CONSTRAINT payment_batch_payment_document_fkey FOREIGN KEY (organisation_id, payment_id) REFERENCES fyniq.document (organisation_id, id)
);
CREATE INDEX payment_batch_payment_release ON fyniq.payment_batch_payment (organisation_id, release_id);

-- +goose StatementBegin
-- A release is written once, for an approved (posted) batch at its current version, and never changed, deleted or
-- truncated (FY239).
CREATE FUNCTION fyniq.payment_batch_release_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Releases of payment batches are never deleted' USING ERRCODE = 'FY239';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The release of batch % is never changed or deleted', OLD.batch_id USING ERRCODE = 'FY239',
      HINT = 'A payment the bank rejects is voided on its own; the release stays as it was (FR-BNK-12).';
  END IF;
  IF NEW.total = 'NaN' THEN -- as payment_batch_release_total_scale says, before the guard looks for the batch (§11.3)
    RAISE EXCEPTION 'The release''s total is not a number' USING ERRCODE = 'check_violation';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.payment_batch_release r WHERE r.organisation_id = NEW.organisation_id AND r.batch_id = NEW.batch_id) THEN
    RAISE EXCEPTION 'Batch % is released already', NEW.batch_id USING ERRCODE = 'FY239',
      HINT = 'A batch is released once: its payments are posted and its file written (FR-BNK-12).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.batch_id AND d.type = 'payment_batch'
      AND d.status = 'posted' AND d.current_version = NEW.batch_version
  ) THEN
    RAISE EXCEPTION 'Batch % is released only once approved, at its current version', NEW.batch_id USING ERRCODE = 'FY239',
      HINT = 'Ask for the batch''s approval; another person approves it first (FR-BNK-12, FM-1).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER payment_batch_release_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.payment_batch_release
  FOR EACH ROW EXECUTE FUNCTION fyniq.payment_batch_release_guard();
CREATE TRIGGER payment_batch_release_no_truncate BEFORE TRUNCATE ON fyniq.payment_batch_release
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.payment_batch_release_guard();

-- +goose StatementBegin
-- A release's payment is a posted bill payment or reimbursement of the batch's money account and payment date, linked
-- once, and never changed, deleted or truncated (FY240).
CREATE FUNCTION fyniq.payment_batch_payment_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  b jsonb;
  p jsonb;
  t text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The payments of payment batches are never deleted' USING ERRCODE = 'FY240';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The payment % of a batch is never changed or deleted', OLD.payment_id USING ERRCODE = 'FY240';
  END IF;
  SELECT v.payload INTO b FROM fyniq.payment_batch_release r
    JOIN fyniq.document_version v ON v.organisation_id = r.organisation_id AND v.document_id = r.batch_id AND v.version = r.batch_version
   WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.release_id;
  SELECT d.type, v.payload INTO t, p FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.payment_id AND d.status = 'posted';
  IF t IS NULL OR t NOT IN ('bill_payment', 'claim_reimbursement') OR b IS NULL
     OR p->>'money_account_id' IS DISTINCT FROM b->>'money_account_id' OR p->>'date' IS DISTINCT FROM b->>'date' THEN
    RAISE EXCEPTION 'Payment % is not a posted bill payment or reimbursement of the batch''s account and date', NEW.payment_id
      USING ERRCODE = 'FY240', HINT = 'A batch links only the payments its release recorded (FR-BNK-12).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER payment_batch_payment_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.payment_batch_payment
  FOR EACH ROW EXECUTE FUNCTION fyniq.payment_batch_payment_guard();
CREATE TRIGGER payment_batch_payment_no_truncate BEFORE TRUNCATE ON fyniq.payment_batch_payment
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.payment_batch_payment_guard();

ALTER TABLE fyniq.payment_batch_release ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payment_batch_release FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payment_batch_release
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.payment_batch_payment ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payment_batch_payment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payment_batch_payment
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The release and its links are audited on the batch and on each payment in the same transaction (AUD-01); the file's
-- name and hash only, never its content.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payment_batch_release
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'batch_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payment_batch_payment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'payment_id');

REVOKE ALL ON fyniq.payment_batch_release FROM PUBLIC;
REVOKE ALL ON fyniq.payment_batch_payment FROM PUBLIC;
GRANT SELECT ON fyniq.payment_batch_release, fyniq.payment_batch_payment TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (batch_id, batch_version, approval_id, released_by, released_at, writer, file_name, media_type, file_sealed, file_sha256,
              payments, currency, minor_units, total) ON fyniq.payment_batch_release TO fyniq_api;
GRANT INSERT (release_id, payment_id) ON fyniq.payment_batch_payment TO fyniq_api;

-- +goose StatementBegin
-- A payment recorded by hand of a bill or claim that an approved, unreleased batch pays is refused at commit (FY241):
-- the batch pays it, and paying it twice is how money leaves twice. The release's own payments commit with the release.
CREATE FUNCTION fyniq.payment_batch_holds() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.reverses_id IS NOT NULL OR NOT EXISTS (
    SELECT 1 FROM fyniq.document s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.source_id
      AND s.type IN ('bill_payment', 'claim_reimbursement')
  ) THEN
    RETURN NULL;
  END IF;
  IF EXISTS (
    SELECT 1 FROM fyniq.document b
      JOIN fyniq.document_version v ON v.organisation_id = b.organisation_id AND v.document_id = b.id AND v.version = b.current_version
     WHERE b.organisation_id = NEW.organisation_id AND b.type = 'payment_batch' AND b.status = 'posted'
       AND v.payload->'items' @> jsonb_build_array(jsonb_build_object('document_id', NEW.document_id::text))
       AND NOT EXISTS (SELECT 1 FROM fyniq.payment_batch_release r WHERE r.organisation_id = b.organisation_id AND r.batch_id = b.id)
  ) THEN
    RAISE EXCEPTION 'Document % is in an approved payment batch, so it is paid by the batch', NEW.document_id USING ERRCODE = 'FY241',
      HINT = 'Release the batch, or cancel it first and record the payment then (FR-BNK-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER payment_batch_holds AFTER INSERT ON fyniq.allocation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.payment_batch_holds();

-- +goose StatementBegin
-- A batch is approved, by posting it, only under an approved payment_batch.approve request that neither its requester nor
-- the person who saved its current version signed (FY242; FR-BNK-12, FM-1, SOD-2: in every team mode).
CREATE FUNCTION fyniq.payment_batch_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  preparer text;
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  SELECT v.created_by INTO preparer FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'payment_batch' AND r.operation = 'payment_batch.approve'
          AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                             AND (s.signer = r.requested_by OR 'user:' || s.signer::text = preparer))) THEN
    RAISE EXCEPTION 'Batch % is approved only by a second person', NEW.id USING ERRCODE = 'FY242',
      HINT = 'Ask for approval: someone who did not prepare or ask for it approves it (FR-BNK-12, FM-1).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER payment_batch_approved AFTER INSERT OR UPDATE OF status ON fyniq.document
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.type = 'payment_batch' AND NEW.status = 'posted')
  EXECUTE FUNCTION fyniq.payment_batch_approved();

-- document_reversed_posted as in 000177, and: a payment batch posts nothing, so cancelling an approved one reverses
-- nothing; but a released batch is never cancelled (FY239).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_reversed_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type IN ('purchase_order', 'quote', 'sales_order') THEN
    RETURN NULL; -- a purchase order, a quote or a sales order posts nothing, so closing or cancelling it reverses nothing
  END IF;
  IF NEW.type = 'payment_batch' THEN -- a batch posts nothing; once released, its payments are voided on their own
    IF EXISTS (SELECT 1 FROM fyniq.payment_batch_release r WHERE r.organisation_id = NEW.organisation_id AND r.batch_id = NEW.id) THEN
      RAISE EXCEPTION 'Batch % is released, so it is never cancelled', NEW.id USING ERRCODE = 'FY239',
        HINT = 'Void a payment the bank rejected on its own (FR-BNK-12).';
    END IF;
    RETURN NULL;
  END IF;
  IF NEW.type = 'fixed_asset' THEN -- registering posts nothing; a void reverses every month of its depreciation, if any posted
    IF EXISTS (
      SELECT 1 FROM fyniq.journal_entry e
      WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text AND e.source_type = 'asset_depreciation'
        AND e.reverses_entry_id IS NULL
        AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id)
    ) THEN
      RAISE EXCEPTION 'Asset % is voided only with the reversal of every month of its depreciation', NEW.id
        USING ERRCODE = 'FY056', HINT = 'Void it through the application, which reverses its months in the same transaction (FR-FA-01, FR-GL-12).';
    END IF;
    RETURN NULL;
  END IF;
  IF NEW.type IN ('credit_application', 'customer_credit_application') AND NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text AND e.source_version = NEW.current_version
  ) THEN
    IF EXISTS (
      SELECT 1 FROM fyniq.allocation a
      WHERE a.organisation_id = NEW.organisation_id AND a.source_id = NEW.id AND a.reverses_id IS NULL
        AND NOT EXISTS (SELECT 1 FROM fyniq.allocation r WHERE r.organisation_id = a.organisation_id AND r.reverses_id = a.id)
    ) THEN
      RAISE EXCEPTION 'Credit application % is reversed only with the reversal of its allocations', NEW.id
        USING ERRCODE = 'FY056', HINT = 'Void it through the application, which reverses its allocations in the same transaction (FR-AR-18).';
    END IF;
    RETURN NULL;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.journal_entry e
    JOIN fyniq.journal_entry r ON r.organisation_id = e.organisation_id AND r.reverses_entry_id = e.id
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text
      AND e.source_type NOT IN ('bill_release', 'invoice_release', 'bank_out_release', 'bank_in_release')
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'Document % is reversed only with the reversal of its entry', NEW.id
      USING ERRCODE = 'FY056', HINT = 'Reverse it through the application, which posts the reversing entry in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
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
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'),
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch')) NOT VALID;

-- A release whose releaser prepared or approved the batch, or the account change of a payee it pays, is allowed in
-- Founder mode and logged for the weekly review (SOD-1, SOD-2, FM-3).
ALTER TABLE fyniq.review_log_entry DROP CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.review_log_entry ADD CONSTRAINT review_log_entry_kind_known CHECK (
  kind IN ('approved_change', 'founder_bootstrap', 'self_approved', 'scheduled_issue', 'auto_reconciled', 'release_conflict')) NOT VALID;
