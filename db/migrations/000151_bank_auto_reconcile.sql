-- Auto-reconcile's undo and its weekly sample (PRD-001 FR-BNK-14, FM-3, FR-AUT-01; rev 0.142; p1-bank-import-reconcile
-- task 5.1, design D8).
--
-- What the system reconciles is undone while its period is open. A transfer is undone once, with a reason: its entry
-- is reversed and its lines' matches undone in the same transaction, so its record is in transit no more and its
-- lines may join another transfer (the unique line indexes leave undone transfers out); otherwise it keeps what was
-- recorded (FY223).
--
-- Each Monday the platform job bank.review-sample, as the principal "scheduler:bank.review-sample" with the worker's
-- role, puts the previous week's 5 largest auto-reconciliations and 5 others at random in the organisation's review
-- log (the owner's decision of 25 Oct 2026). The worker gains no new right: review_log_entry takes the kind
-- auto_reconciled, and scheduled_issue_fence (FY201) lets the worker log it only for a bank line the system reconciled
-- and nobody undid, with no actor or approval of its own. The new checks are added NOT VALID and validated by 000152
-- (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';


-- ------------------------------------------------------------------ a transfer undone
ALTER TABLE fyniq.bank_transfer ADD COLUMN undone_by text, ADD COLUMN undone_at timestamptz, ADD COLUMN undo_reason text;
ALTER TABLE fyniq.bank_transfer ADD CONSTRAINT bank_transfer_undone_whole
  CHECK ((undone_by IS NULL) = (undone_at IS NULL) AND (undone_by IS NULL) = (undo_reason IS NULL)) NOT VALID;
ALTER TABLE fyniq.bank_transfer ADD CONSTRAINT bank_transfer_undo_reason_length CHECK (length(btrim(undo_reason)) BETWEEN 1 AND 500) NOT VALID;
ALTER TABLE fyniq.bank_transfer ADD CONSTRAINT bank_transfer_undone_by_form CHECK (undone_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$') NOT VALID;

-- The line indexes leave undone transfers out. bank_transfer is small and written only by imports and people, so
-- building them in this transaction holds it for moments (squawk: the concurrent forms cannot run in a transaction).
-- squawk-ignore require-concurrent-index-deletion
DROP INDEX fyniq.bank_transfer_from_line;
-- squawk-ignore require-concurrent-index-deletion
DROP INDEX fyniq.bank_transfer_to_line;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX bank_transfer_from_line ON fyniq.bank_transfer (organisation_id, from_line_id) WHERE from_line_id IS NOT NULL AND undone_at IS NULL;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX bank_transfer_to_line ON fyniq.bank_transfer (organisation_id, to_line_id) WHERE to_line_id IS NOT NULL AND undone_at IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.bank_transfer_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  l fyniq.bank_line;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Transfer % is never deleted', OLD.id USING ERRCODE = 'FY223', HINT = 'Reverse its entry and undo its matches instead.';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF OLD.undone_at IS NOT NULL THEN
      RAISE EXCEPTION 'Transfer % was undone and never changes', OLD.id USING ERRCODE = 'FY223', HINT = 'Record the transfer again instead.';
    END IF;
    IF (NEW.from_account_id, NEW.to_account_id, NEW.on_date, NEW.from_currency, NEW.from_amount, NEW.to_currency, NEW.to_amount, NEW.fee,
        NEW.made_by, NEW.made_at)
       IS DISTINCT FROM (OLD.from_account_id, OLD.to_account_id, OLD.on_date, OLD.from_currency, OLD.from_amount, OLD.to_currency,
        OLD.to_amount, OLD.fee, OLD.made_by, OLD.made_at)
       OR (OLD.from_line_id IS NOT NULL AND NEW.from_line_id IS DISTINCT FROM OLD.from_line_id)
       OR (OLD.to_line_id IS NOT NULL AND NEW.to_line_id IS DISTINCT FROM OLD.to_line_id)
       OR (OLD.entry_id IS NOT NULL AND NEW.entry_id IS DISTINCT FROM OLD.entry_id)
       OR NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'Transfer % keeps what was recorded; only a missing side and its entry are added', OLD.id USING ERRCODE = 'FY223';
    END IF;
    IF NEW.undone_at IS NOT NULL AND (NEW.entry_id IS NULL OR NOT EXISTS (
         SELECT 1 FROM fyniq.journal_entry r WHERE r.organisation_id = NEW.organisation_id AND r.reverses_entry_id = NEW.entry_id)) THEN
      RAISE EXCEPTION 'Transfer % is undone only with the reversal of its entry', OLD.id USING ERRCODE = 'FY223',
        HINT = 'Undo it through Banking, which reverses its entry in the same transaction.';
    END IF;
  END IF;
  IF TG_OP = 'INSERT' AND NEW.undone_at IS NOT NULL THEN
    RAISE EXCEPTION 'A new transfer is not undone' USING ERRCODE = 'FY223';
  END IF;
  IF NEW.from_line_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.from_line_id IS NULL) THEN
    SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.from_line_id;
    IF l.money_account_id <> NEW.from_account_id OR l.currency <> NEW.from_currency OR l.amount <> -NEW.from_amount THEN
      RAISE EXCEPTION 'The paying line of transfer % is not its money out', NEW.id USING ERRCODE = 'FY223';
    END IF;
  END IF;
  IF NEW.to_line_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.to_line_id IS NULL) THEN
    SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.to_line_id;
    IF l.money_account_id <> NEW.to_account_id OR l.currency <> NEW.to_currency OR l.amount <> NEW.to_amount THEN
      RAISE EXCEPTION 'The receiving line of transfer % is not its money in', NEW.id USING ERRCODE = 'FY223';
    END IF;
  END IF;
  IF TG_OP = 'INSERT' AND NEW.version <> 1 THEN
    RAISE EXCEPTION 'A new transfer starts at version 1' USING ERRCODE = 'FY223';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT UPDATE (undone_by, undone_at, undo_reason) ON fyniq.bank_transfer TO fyniq_api, fyniq_worker;

-- ------------------------------------------------------------------ the weekly sample
ALTER TABLE fyniq.review_log_entry DROP CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.review_log_entry ADD CONSTRAINT review_log_entry_kind_known CHECK (
  kind IN ('approved_change', 'founder_bootstrap', 'self_approved', 'scheduled_issue', 'auto_reconciled')) NOT VALID;

-- +goose StatementBegin
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
      ok := NEW.source = 'generated' AND fyniq.scheduled_issue_allowed(NEW.document_id);
    WHEN 'document_send', 'document_link' THEN
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
    RAISE EXCEPTION 'The scheduler may change % only for the invoices of an approved recurring invoice set to issue and send, or log an auto-reconciled bank line for review', TG_TABLE_NAME
      USING ERRCODE = 'FY201', HINT = 'A person issues and sends every other invoice (FR-AR-07); the weekly sample logs only lines the system reconciled (FR-BNK-14).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
