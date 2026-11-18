-- Unusual items in the weekly review (PRD-001 FM-6, rev 0.287): in Founder mode, the journals, payments and payees'
-- bank-detail changes that rules find unusual are logged for the weekly review (FM-3), each with its reason, by the
-- worker, whose fence lets it log one only for a posted journal or payment, named for who posted it. Nothing about them
-- is approved: a founder other than who posted it reviews each.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.review_log_entry DROP CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.review_log_entry ADD CONSTRAINT review_log_entry_kind_known CHECK (
  kind IN ('approved_change', 'founder_bootstrap', 'self_approved', 'scheduled_issue', 'auto_reconciled', 'release_conflict',
           'break_glass', 'account_recovery', 'unusual')) NOT VALID;

-- +goose StatementBegin
-- scheduled_issue_fence as in 000189, and: the nightly review logs an unusual item only for a posted journal or payment of
-- the organisation, tied to no approval, naming as its actor nobody but who posted it (FM-6, rev 0.287).
CREATE OR REPLACE FUNCTION fyniq.scheduled_issue_fence() RETURNS trigger
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
      ok := NEW.source = 'generated' AND (fyniq.scheduled_issue_allowed(NEW.document_id) OR fyniq.remittance_allowed(NEW.document_id));
    WHEN 'document_send' THEN
      ok := fyniq.scheduled_issue_allowed(NEW.document_id) OR fyniq.remittance_allowed(NEW.document_id);
    WHEN 'document_link' THEN
      ok := fyniq.scheduled_issue_allowed(NEW.document_id);
    WHEN 'review_log_entry' THEN
      ok := (NEW.kind = 'scheduled_issue' AND NEW.object_type = 'invoice' AND NEW.object_key ~ '^[0-9a-f-]{36}$'
             AND fyniq.scheduled_issue_allowed(NEW.object_key::uuid))
         OR (NEW.kind = 'auto_reconciled' AND NEW.object_type = 'bank_line' AND NEW.object_key ~ '^[0-9a-f-]{36}$'
             AND NEW.actor IS NULL AND NEW.approval_id IS NULL
             AND EXISTS (SELECT 1 FROM fyniq.bank_match m WHERE m.organisation_id = NEW.organisation_id AND m.line_id = NEW.object_key::uuid
                           AND m.basis <> 'person' AND m.undone_at IS NULL))
         OR (NEW.kind = 'unusual' AND NEW.object_type IN ('journal', 'bill_payment') AND NEW.object_key ~ '^[0-9a-f-]{36}$'
             AND NEW.approval_id IS NULL
             AND EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.object_key::uuid
                           AND d.type = NEW.object_type AND d.status = 'posted'
                           AND (NEW.actor IS NULL OR d.posted_by = 'user:' || NEW.actor::text)));
    ELSE
      ok := false;
  END CASE;
  IF NOT coalesce(ok, false) THEN
    RAISE EXCEPTION 'The scheduler may change % only for the invoices of an approved recurring invoice set to issue and send, the remittance advice of a matched batch payment, or log an auto-reconciled bank line or an unusual journal or payment for review', TG_TABLE_NAME
      USING ERRCODE = 'FY201', HINT = 'A person issues and sends every other invoice (FR-AR-07); remittance advice follows a batch payment''s match (FR-BNK-12); the weekly sample logs only lines the system reconciled (FR-BNK-14); unusual items are posted journals and payments, named for who posted them (FM-6).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
