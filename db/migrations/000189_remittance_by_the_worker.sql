-- The worker sends remittance advice (PRD-001 FR-BNK-12; rev 0.154; p1-payments-out task 2.7, design D8).
--
-- The remittance job runs as fyniq_worker, whose writes are fenced to the scheduler's own cases (FY201, 000135 and
-- 000151) and whose email to notification, digest and scheduled invoice email. It needs three of those rights for one
-- more case: to keep the advice's PDF as the payment's generated evidence, to queue it as email of purpose remittance,
-- and to record the send. Each is fenced here to remittance_allowed: a posted bill payment a released payment batch
-- recorded, matched to its statement line by a match nobody undid. Remittance email the worker queues must be carried
-- by such a send in the same transaction (FY202, checked at commit). The worker gains no new grant; the API's rights
-- are unchanged.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.remittance_allowed(p_payment uuid) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1
    FROM fyniq.document d
    JOIN fyniq.payment_batch_payment l ON l.organisation_id = d.organisation_id AND l.payment_id = d.id
    JOIN fyniq.bank_match m ON m.organisation_id = d.organisation_id AND m.document_id = d.id AND m.undone_at IS NULL
    WHERE d.organisation_id = fyniq.current_org() AND d.id = p_payment AND d.type = 'bill_payment' AND d.status = 'posted')
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION fyniq.remittance_allowed(uuid) TO fyniq_worker;

-- +goose StatementBegin
-- scheduled_issue_fence as in 000151, and: a batch's matched supplier payment takes its remittance advice as generated
-- evidence and its send.
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
                           AND m.basis <> 'person' AND m.undone_at IS NULL));
    ELSE
      ok := false;
  END CASE;
  IF NOT coalesce(ok, false) THEN
    RAISE EXCEPTION 'The scheduler may change % only for the invoices of an approved recurring invoice set to issue and send, the remittance advice of a matched batch payment, or log an auto-reconciled bank line for review', TG_TABLE_NAME
      USING ERRCODE = 'FY201', HINT = 'A person issues and sends every other invoice (FR-AR-07); remittance advice follows a batch payment''s match (FR-BNK-12); the weekly sample logs only lines the system reconciled (FR-BNK-14).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- email_message_worker_purposes as in 000135, and remittance advice.
CREATE OR REPLACE FUNCTION fyniq.email_message_worker_purposes() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_worker' AND NEW.purpose NOT IN ('notification', 'digest', 'invoice', 'remittance') THEN
    RAISE EXCEPTION 'The worker queues only notification, digest, scheduled invoice and remittance email, not %', NEW.purpose USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.remittance_email_sent() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.document_send s WHERE s.organisation_id = NEW.organisation_id AND s.message_id = NEW.id) THEN
    RAISE EXCEPTION 'The worker queued remittance email % that no send of a matched batch payment carries', NEW.id
      USING ERRCODE = 'FY202', HINT = 'The worker emails only the remittance advice of a batch payment matched to the bank (FR-BNK-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
-- Named to fire before require_audit (triggers fire by name), so a forged message is refused for what it is.
CREATE CONSTRAINT TRIGGER email_message_remittance_send AFTER INSERT ON fyniq.email_message
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (current_user = 'fyniq_worker' AND NEW.purpose = 'remittance')
  EXECUTE FUNCTION fyniq.remittance_email_sent();
