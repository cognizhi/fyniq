-- Purchase orders (PRD-001 FR-AP-01, FR-APR-01, FM-2, SOD-3; the owner's decisions of 23 Oct 2026, built in rev 0.87;
-- p1-purchases task 8.1).
--
-- A purchase order is a document of its own type, numbered from its own sequence (PO-), whose lines each hold a
-- quantity and a unit price. It posts nothing: approving it moves the document from draft to posted, with who approved
-- it and when (FY053), and no entry; closing it, with a reason, moves it to reversed, again with no entry, which
-- document_reversed_posted (FY056) now allows for a purchase order alone. An approved order is never edited: its
-- version cannot move without an entry (FY055). Approval above the self-approval threshold is an approval request on
-- the object type purchase_order.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order')) NOT VALID;

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
                               ('purchase_order', 'purchase_order.approve'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order')) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.document_reversed_posted() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type = 'purchase_order' THEN
    RETURN NULL; -- a purchase order posts nothing, so closing it reverses nothing
  END IF;
  IF NEW.type = 'credit_application' AND NOT EXISTS (
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
    WHERE e.organisation_id = NEW.organisation_id AND e.source_id = NEW.id::text AND e.source_type <> 'bill_release'
      AND e.source_version = NEW.current_version AND e.reverses_entry_id IS NULL
  ) THEN
    RAISE EXCEPTION 'Document % is reversed only with the reversal of its entry', NEW.id
      USING ERRCODE = 'FY056', HINT = 'Reverse it through the application, which posts the reversing entry in the same transaction (FR-GL-12).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- A purchase order posts nothing: an entry never names one as its source.
-- +goose StatementBegin
CREATE FUNCTION fyniq.journal_entry_not_purchase_order() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  -- Only a source that is a document's ID can name one; looked up by the document's key.
  IF NEW.source_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
    IF EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.source_id::uuid
               AND d.type = 'purchase_order') THEN
      RAISE EXCEPTION 'Entry % names purchase order % as its source', NEW.posting_identity, NEW.source_id
        USING ERRCODE = 'FY173', HINT = 'A purchase order posts nothing: its bills post (FR-AP-01).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER journal_entry_not_purchase_order BEFORE INSERT ON fyniq.journal_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_entry_not_purchase_order();
