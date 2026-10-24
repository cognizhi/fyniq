-- Reminders to customers (PRD-001 FR-AR-08; p1-sales task 7.1; the owner's decision of 24 Oct 2026: through the Inbox
-- until bank import).
--
-- A customer group names a reminder schedule: the days, before (negative) or after the due date, on which an invoice
-- still open is due a reminder. A customer in no group follows the standard schedule (3 days before, on the due date, 7
-- and 30 days after), kept in code; a group with no days sends none. While receipts are recorded by hand, no reminder
-- goes by itself: each due one waits in the Inbox for a person to send or skip, and the decision is recorded once per
-- document and step, never changed (FY203). A quote about to expire is reminded the same way, its step counted from its
-- validity date. A sent reminder is a send of the document's current version (document_send), its email of purpose
-- reminder from the organisation's reminders sender. Automatic reminders, per customer group, come with bank import.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A group is never deleted, as a contact is not: no role holds DELETE; one no longer used is made inactive.
CREATE TABLE fyniq.customer_group (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  reminder_days   integer[]   NOT NULL,
  active          boolean     NOT NULL DEFAULT true,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT customer_group_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT customer_group_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT customer_group_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  CONSTRAINT customer_group_reminder_days CHECK (cardinality(reminder_days) <= 6 AND array_position(reminder_days, NULL) IS NULL
    AND -30 <= ALL (reminder_days) AND 365 >= ALL (reminder_days)),
  CONSTRAINT customer_group_version_positive CHECK (version > 0)
);
CREATE UNIQUE INDEX customer_group_name_key ON fyniq.customer_group (organisation_id, lower(btrim(name)));

ALTER TABLE fyniq.customer_group ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.customer_group FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.customer_group
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.customer_group
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('customer_group', 'id');

-- A customer is in at most one group; the key is checked once validated (000138).
ALTER TABLE fyniq.contact ADD COLUMN customer_group_id uuid;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_customer_group_fkey FOREIGN KEY (organisation_id, customer_group_id)
  REFERENCES fyniq.customer_group (organisation_id, id) NOT VALID;

-- Reminders are email of their own purpose, from the organisation's reminders sender.
ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest', 'invoice', 'quote', 'reminder')) NOT VALID;

CREATE TABLE fyniq.sales_reminder (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  document_id     uuid        NOT NULL,
  step            integer     NOT NULL,
  outcome         text        NOT NULL,
  send_id         uuid,
  reason          text,
  decided_by      text        NOT NULL,
  decided_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT sales_reminder_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT sales_reminder_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT sales_reminder_send_fkey FOREIGN KEY (organisation_id, send_id) REFERENCES fyniq.document_send (organisation_id, id),
  CONSTRAINT sales_reminder_once UNIQUE (organisation_id, document_id, step),
  CONSTRAINT sales_reminder_step_range CHECK (step BETWEEN -30 AND 365),
  CONSTRAINT sales_reminder_outcome_known CHECK ((outcome = 'sent' AND send_id IS NOT NULL AND reason IS NULL)
    OR (outcome = 'skipped' AND send_id IS NULL)),
  CONSTRAINT sales_reminder_reason_length CHECK (reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT sales_reminder_decided_by_format CHECK (decided_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

ALTER TABLE fyniq.sales_reminder ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.sales_reminder FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.sales_reminder
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.sales_reminder
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('sales_reminder', 'id');

-- A decision never changes or goes, whoever tries (FY203); it is of a posted invoice, quote or sales order, and a sent
-- one names a send of that document.
-- +goose StatementBegin
CREATE FUNCTION fyniq.sales_reminder_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  doc fyniq.document;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The reminder % of document % never changes', OLD.id, OLD.document_id
      USING ERRCODE = 'FY203', HINT = 'A reminder is sent or skipped once (FR-AR-08).';
  END IF;
  SELECT * INTO doc FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF doc.type NOT IN ('quote', 'sales_order', 'invoice') OR doc.status <> 'posted' THEN
    RAISE EXCEPTION 'A reminder is of an issued invoice or a sent quote; document % is a % that is %', NEW.document_id, doc.type, doc.status
      USING ERRCODE = 'FY203', HINT = 'Remind of an issued invoice or a sent quote (FR-AR-08).';
  END IF;
  IF NEW.send_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM fyniq.document_send s
       WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.send_id AND s.document_id = NEW.document_id) THEN
    RAISE EXCEPTION 'The send % is not of document %', NEW.send_id, NEW.document_id
      USING ERRCODE = 'FY203', HINT = 'A sent reminder names the send of its document that carried it (FR-AR-08).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER sales_reminder_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.sales_reminder
  FOR EACH ROW EXECUTE FUNCTION fyniq.sales_reminder_guard();
CREATE TRIGGER sales_reminder_no_truncate BEFORE TRUNCATE ON fyniq.sales_reminder
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

GRANT SELECT ON fyniq.customer_group, fyniq.sales_reminder TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (name, reminder_days), UPDATE (name, reminder_days, active, version) ON fyniq.customer_group TO fyniq_api;
GRANT INSERT (customer_group_id), UPDATE (customer_group_id) ON fyniq.contact TO fyniq_api;
GRANT INSERT (document_id, step, outcome, send_id, reason, decided_by) ON fyniq.sales_reminder TO fyniq_api;
