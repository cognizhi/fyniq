-- Quotes and invoices sent by email (PRD-001 FR-AR-06, FR-AR-16; p1-sales task 5.2).
--
-- A send records which version of a sent quote or sales order, or of an issued invoice, went to whom, through which
-- email message, with the PDF Fyniq rendered for it (an evidence file of the document, of the new source 'generated')
-- and the template version that rendered it, so what the customer received can be shown and proved. A send never
-- changes or goes (FY195), and is only of a posted quote, sales order or invoice at its current version (FY196). Its
-- delivery comes from the email message's outcome and the provider's events (FR-INT-21).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The PDF of a document sent is evidence Fyniq generated.
ALTER TABLE fyniq.evidence DROP CONSTRAINT evidence_source_known;
ALTER TABLE fyniq.evidence ADD CONSTRAINT evidence_source_known CHECK (source IN ('upload', 'paste', 'email', 'api', 'generated')) NOT VALID;

-- Invoices and quotes are email of their own purposes, sent from the organisation's invoices sender.
ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest', 'invoice', 'quote')) NOT VALID;

CREATE TABLE fyniq.document_send (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  document_id      uuid        NOT NULL,
  document_version integer     NOT NULL,
  message_id       uuid        NOT NULL,
  evidence_id      uuid        NOT NULL,
  recipient        text        NOT NULL,
  template_source  text        NOT NULL,
  template_id      uuid,
  template_version integer     NOT NULL,
  template_hash    text        NOT NULL,
  pdf_sha256       text        NOT NULL,
  sent_by          text        NOT NULL,
  sent_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_send_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_send_version_fkey FOREIGN KEY (organisation_id, document_id, document_version)
    REFERENCES fyniq.document_version (organisation_id, document_id, version),
  CONSTRAINT document_send_message_fkey FOREIGN KEY (organisation_id, message_id) REFERENCES fyniq.email_message (organisation_id, id),
  CONSTRAINT document_send_evidence_fkey FOREIGN KEY (organisation_id, evidence_id) REFERENCES fyniq.evidence (organisation_id, id),
  CONSTRAINT document_send_template_fkey FOREIGN KEY (organisation_id, template_id) REFERENCES fyniq.document_template (organisation_id, id),
  CONSTRAINT document_send_message_once UNIQUE (organisation_id, message_id),
  CONSTRAINT document_send_template_source CHECK (template_source IN ('contact', 'default', 'platform')
    AND (template_source = 'platform') = (template_id IS NULL) AND template_version >= 0),
  CONSTRAINT document_send_hashes CHECK (template_hash ~ '^[0-9a-f]{64}$' AND pdf_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT document_send_recipient_length CHECK (length(recipient) BETWEEN 3 AND 254),
  CONSTRAINT document_send_sent_by_format CHECK (sent_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX document_send_document ON fyniq.document_send (organisation_id, document_id, sent_at);

ALTER TABLE fyniq.document_send ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_send FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_send
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.document_send
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_send', 'id');

-- A send never changes or goes, whoever tries (FY195), and is only of a posted quote, sales order or invoice at its
-- current version (FY196): a draft is never sent.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_send_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  doc fyniq.document;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The send % of document % never changes', OLD.id, OLD.document_id
      USING ERRCODE = 'FY195', HINT = 'Send the document again instead (FR-AR-06).';
  END IF;
  SELECT * INTO doc FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF doc.type NOT IN ('quote', 'sales_order', 'invoice') OR doc.status <> 'posted' OR doc.current_version <> NEW.document_version THEN
    RAISE EXCEPTION 'Only the current version of a sent quote or an issued invoice is sent, not version % of a % that is %',
      NEW.document_version, doc.type, doc.status
      USING ERRCODE = 'FY196', HINT = 'Issue or send it first; a draft is never sent (FR-AR-06).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_send_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.document_send
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_send_guard();
CREATE TRIGGER document_send_no_truncate BEFORE TRUNCATE ON fyniq.document_send
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

GRANT SELECT ON fyniq.document_send TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (document_id, document_version, message_id, evidence_id, recipient, template_source, template_id, template_version,
  template_hash, pdf_sha256, sent_by) ON fyniq.document_send TO fyniq_api;
