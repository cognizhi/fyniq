-- Recurring invoices the scheduler issues and sends (PRD-001 FR-AR-07, FR-AUT-02, FM-3, SOD-3; p1-sales task 6.1; the
-- owner's decisions of 24 Oct 2026; the threat model in docs/security/threat-models/scheduled-invoices.md).
--
-- A recurring invoice is set to make a draft (the default) or to issue and send. Issue and send takes effect only for
-- the content version a second person approved through the approval engine (recurring_invoice.approve): the approval
-- names the version, so a change of content withdraws it. The worker then issues each scheduled draft of that version
-- and emails it, which needs rights it never held: to number and post an invoice, attach its PDF as evidence, queue
-- invoice email, record the send and the online link, and list it for the weekly review (scheduled_issue). Each of those
-- rights is fenced here to the one case, a scheduled draft (version 1, made by scheduler:invoices.recurring) of a
-- recurring invoice set to issue and send whose approved version it came from (FY201); and invoice email the worker
-- queues must be carried by such a send in the same transaction (FY202). The API's own rights are unchanged.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.recurring_invoice ADD COLUMN mode text NOT NULL DEFAULT 'draft';
ALTER TABLE fyniq.recurring_invoice ADD COLUMN approved_version integer;
ALTER TABLE fyniq.recurring_invoice ADD COLUMN approved_by text;
ALTER TABLE fyniq.recurring_invoice ADD COLUMN approved_at timestamptz;
ALTER TABLE fyniq.recurring_invoice ADD CONSTRAINT recurring_invoice_mode_known CHECK (mode IN ('draft', 'issue_and_send')) NOT VALID;
ALTER TABLE fyniq.recurring_invoice ADD CONSTRAINT recurring_invoice_approval_whole CHECK (
  (approved_version IS NULL) = (approved_by IS NULL) AND (approved_by IS NULL) = (approved_at IS NULL)
  AND (approved_by IS NULL OR approved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
  AND (approved_version IS NULL OR approved_version BETWEEN 1 AND content_version)) NOT VALID;
GRANT INSERT (mode), UPDATE (mode, approved_version, approved_by, approved_at) ON fyniq.recurring_invoice TO fyniq_api;

-- The approval of a recurring invoice's content for issue and send is an approval object of its own.
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
                               ('recurring_invoice', 'recurring_invoice.approve'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice')) NOT VALID;

-- Each invoice the scheduler issues is listed for the weekly review (FM-3).
ALTER TABLE fyniq.review_log_entry DROP CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.review_log_entry ADD CONSTRAINT review_log_entry_kind_known CHECK (
  kind IN ('approved_change', 'founder_bootstrap', 'self_approved', 'scheduled_issue')) NOT VALID;

-- Whether the worker may issue and send document p_document: a draft or posted invoice still at version 1, made by
-- the scheduler from a recurring invoice set to issue and send whose approved version is the version it came from.
-- +goose StatementBegin
CREATE FUNCTION fyniq.scheduled_issue_allowed(p_document uuid) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1
    FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = 1
    JOIN fyniq.recurring_invoice r ON r.organisation_id = d.organisation_id
      AND r.id = (v.payload->'recurring_invoice'->>'id')::uuid
    WHERE d.organisation_id = fyniq.current_org() AND d.id = p_document AND d.type = 'invoice' AND d.current_version = 1
      AND v.created_by = 'scheduler:invoices.recurring' AND r.mode = 'issue_and_send'
      AND r.approved_version IS NOT NULL AND r.approved_version = (v.payload->'recurring_invoice'->>'version')::integer)
$$;
-- +goose StatementEnd

-- The fence on each right the worker gains (FY201): it holds only for scheduled_issue_allowed, and for nothing when
-- another role acts.
-- +goose StatementBegin
CREATE FUNCTION fyniq.scheduled_issue_fence() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  doc uuid;
  ok  boolean;
BEGIN
  IF current_user <> 'fyniq_worker' THEN
    RETURN NEW;
  END IF;
  CASE TG_TABLE_NAME
    WHEN 'document' THEN
      -- Its old rights (a reversal's status) stand; posting, and setting who posted, only an allowed invoice.
      IF (NEW.posted_by, NEW.posted_at) IS NOT DISTINCT FROM (OLD.posted_by, OLD.posted_at) AND NEW.status IS DISTINCT FROM 'posted' THEN
        RETURN NEW;
      END IF;
      ok := fyniq.scheduled_issue_allowed(NEW.id) AND NEW.posted_by = 'scheduler:invoices.recurring';
    WHEN 'document_number' THEN
      ok := NEW.document_type = 'invoice' AND fyniq.scheduled_issue_allowed(NEW.id);
    WHEN 'document_sequence' THEN
      ok := NEW.document_type = 'invoice' AND (TG_OP = 'INSERT' OR OLD.document_type = 'invoice');
    WHEN 'evidence' THEN
      ok := NEW.source = 'generated' AND fyniq.scheduled_issue_allowed(NEW.document_id);
    WHEN 'document_send', 'document_link' THEN
      ok := fyniq.scheduled_issue_allowed(NEW.document_id);
    WHEN 'review_log_entry' THEN
      ok := NEW.kind = 'scheduled_issue' AND NEW.object_type = 'invoice' AND NEW.object_key ~ '^[0-9a-f-]{36}$'
        AND fyniq.scheduled_issue_allowed(NEW.object_key::uuid);
    ELSE
      ok := false;
  END CASE;
  IF NOT coalesce(ok, false) THEN
    RAISE EXCEPTION 'The scheduler may change % only for the invoices of an approved recurring invoice set to issue and send', TG_TABLE_NAME
      USING ERRCODE = 'FY201', HINT = 'A person issues and sends every other invoice (FR-AR-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER scheduled_issue_fence BEFORE UPDATE ON fyniq.document
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();
CREATE TRIGGER scheduled_issue_fence BEFORE INSERT ON fyniq.document_number
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();
CREATE TRIGGER scheduled_issue_fence BEFORE INSERT OR UPDATE ON fyniq.document_sequence
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();
CREATE TRIGGER scheduled_issue_fence BEFORE INSERT ON fyniq.evidence
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();
CREATE TRIGGER scheduled_issue_fence BEFORE INSERT ON fyniq.document_send
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();
CREATE TRIGGER scheduled_issue_fence BEFORE INSERT ON fyniq.document_link
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();
CREATE TRIGGER scheduled_issue_fence BEFORE INSERT ON fyniq.review_log_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.scheduled_issue_fence();

-- The worker queues notification and digest email, and invoice email carried by a send of an allowed invoice in the
-- same transaction (FY202, checked at commit).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.email_message_worker_purposes() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_worker' AND NEW.purpose NOT IN ('notification', 'digest', 'invoice') THEN
    RAISE EXCEPTION 'The worker queues only notification, digest and scheduled invoice email, not %', NEW.purpose USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION fyniq.scheduled_invoice_email_sent() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.document_send s WHERE s.organisation_id = NEW.organisation_id AND s.message_id = NEW.id) THEN
    RAISE EXCEPTION 'The scheduler queued invoice email % that no send of an approved recurring invoice carries', NEW.id
      USING ERRCODE = 'FY202', HINT = 'The scheduler emails only the invoices it issues (FR-AR-07).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
-- Named to fire before require_audit (triggers fire by name), so a forged message is refused for what it is.
CREATE CONSTRAINT TRIGGER email_message_scheduled_send AFTER INSERT ON fyniq.email_message
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (current_user = 'fyniq_worker' AND NEW.purpose = 'invoice')
  EXECUTE FUNCTION fyniq.scheduled_invoice_email_sent();

-- The worker's new rights, each fenced above.
GRANT EXECUTE ON FUNCTION fyniq.scheduled_issue_allowed(uuid) TO fyniq_worker;
GRANT UPDATE (posted_by, posted_at) ON fyniq.document TO fyniq_worker;
GRANT INSERT (document_type, id, number) ON fyniq.document_number TO fyniq_worker;
GRANT INSERT (digits, document_type, gap_free, next_number, prefix), UPDATE (next_number) ON fyniq.document_sequence TO fyniq_worker;
GRANT INSERT (data_class, document_id, evidence_type, file_name, media_type, note, original_sha256, relates_on, sha256, size_bytes, source,
  staging_key, uploaded_by) ON fyniq.evidence TO fyniq_worker;
GRANT INSERT (document_id, document_version, evidence_id, message_id, pdf_sha256, recipient, sent_by, template_hash, template_id, template_source,
  template_version) ON fyniq.document_send TO fyniq_worker;
GRANT INSERT (created_by, document_id, document_version, expires_at, message_id, token_hash) ON fyniq.document_link TO fyniq_worker;
GRANT INSERT (actor, approval_id, kind, logged_at, object_key, object_type, summary) ON fyniq.review_log_entry TO fyniq_worker;
