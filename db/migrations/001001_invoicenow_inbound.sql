-- Documents received through InvoiceNow (PRD-001 FR-AP-05; p2-invoicenow task 4.3): each is kept as received with its SHA-256 and the access point's cursor, and becomes a draft
-- bill when its supplier is known; a second document of the same sender and number is refused, naming the first. What was received never changes: only its status,
-- its bill and its note do (FYI05).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.invoicenow_inbound (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  message_id      text        NOT NULL,   -- the access point's ID of the message
  cursor          text        NOT NULL,   -- where to read from next after it
  sender          text        NOT NULL,   -- the sender's Peppol participant ID
  number          text        NOT NULL,   -- the invoice number it states ('' when it has none)
  received_at     timestamptz NOT NULL,
  sha256          bytea       NOT NULL,
  payload         bytea       NOT NULL,
  status          text        NOT NULL DEFAULT 'received',
  bill_id         uuid,
  note            text,
  created_by      text        NOT NULL,
  CONSTRAINT invoicenow_inbound_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invoicenow_inbound_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT invoicenow_inbound_bill_fkey FOREIGN KEY (organisation_id, bill_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT invoicenow_inbound_message_key UNIQUE (organisation_id, message_id),
  CONSTRAINT invoicenow_inbound_status_known CHECK (status IN ('received', 'drafted', 'refused')),
  CONSTRAINT invoicenow_inbound_drafted_has_bill CHECK ((status = 'drafted') = (bill_id IS NOT NULL)),
  CONSTRAINT invoicenow_inbound_sha CHECK (octet_length(sha256) = 32),
  CONSTRAINT invoicenow_inbound_size CHECK (octet_length(payload) BETWEEN 1 AND 10485760),
  CONSTRAINT invoicenow_inbound_sender_form CHECK (sender ~ '^[0-9]{4}:[!-~]{1,100}$' OR sender = ''),
  CONSTRAINT invoicenow_inbound_note_length CHECK (note IS NULL OR length(note) <= 1000),
  CONSTRAINT invoicenow_inbound_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX invoicenow_inbound_sender_number ON fyniq.invoicenow_inbound (organisation_id, sender, number);

-- +goose StatementBegin
CREATE FUNCTION fyniq.invoicenow_inbound_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.status <> 'received'
     OR (NEW.message_id, NEW.cursor, NEW.sender, NEW.number, NEW.received_at, NEW.sha256, NEW.payload, NEW.created_by)
        IS DISTINCT FROM (OLD.message_id, OLD.cursor, OLD.sender, OLD.number, OLD.received_at, OLD.sha256, OLD.payload, OLD.created_by) THEN
    RAISE EXCEPTION 'A received document is kept as received: it is drafted or refused once' USING ERRCODE = 'FYI05', HINT = 'Make the draft from it, or leave it refused (FR-AP-05).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invoicenow_inbound_guard BEFORE UPDATE OR DELETE ON fyniq.invoicenow_inbound
  FOR EACH ROW EXECUTE FUNCTION fyniq.invoicenow_inbound_guard();
CREATE TRIGGER invoicenow_inbound_no_truncate BEFORE TRUNCATE ON fyniq.invoicenow_inbound
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.invoicenow_inbound ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.invoicenow_inbound FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.invoicenow_inbound
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.invoicenow_inbound
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('invoicenow_inbound', 'id');

GRANT SELECT ON fyniq.invoicenow_inbound TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (message_id, cursor, sender, number, received_at, sha256, payload, status, bill_id, note, created_by) ON fyniq.invoicenow_inbound TO fyniq_api;
GRANT UPDATE (status, bill_id, note) ON fyniq.invoicenow_inbound TO fyniq_api;
