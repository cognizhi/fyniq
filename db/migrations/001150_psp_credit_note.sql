-- The provider's credit notes (PRD-001 FR-PSP-07; p1-payment-providers task 5, "Left, designed"): a billing provider's credit note against an invoice it
-- billed is mirrored read-only (`psp_credit_note`) and becomes a draft AR credit note of the invoice's document only through
-- create_provider_credit_note_draft, a narrow SECURITY DEFINER function modelled on create_provider_invoice_draft (001076): the worker's only way to this
-- document. The API issues the draft, as the provider's connector, only when its total is the provider's to the cent; any other is parked for a person.
-- The credit note's money is not posted here: a refund's statement line is the bank feed's, and a credit left on the account is the customer's credit.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.psp_credit_note (
  organisation_id            uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                         uuid          NOT NULL DEFAULT uuidv7(),
  connector_id               uuid          NOT NULL,
  provider_credit_note_id    text          NOT NULL,
  provider_invoice_id        text          NOT NULL,
  number                     text,
  status                     text          NOT NULL,  -- the provider's: issued or void
  currency                   text          NOT NULL,
  total                      numeric(20,4) NOT NULL,  -- as the provider reports it, tax included
  reason                     text,
  issued_on                  date,
  draft_lines                jsonb         NOT NULL DEFAULT '[]'::jsonb, -- its lines as the invoice rules read them, without an account
  state                      text          NOT NULL DEFAULT 'mirrored',
  parked_reason              text,
  document_id                uuid,                     -- the AR credit note, once made; one provider credit note is one document
  version                    integer       NOT NULL DEFAULT 1,
  seen_at                    timestamptz   NOT NULL,
  CONSTRAINT psp_credit_note_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_credit_note_provider_once UNIQUE (organisation_id, connector_id, provider_credit_note_id),
  CONSTRAINT psp_credit_note_document_once UNIQUE (organisation_id, document_id),
  CONSTRAINT psp_credit_note_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_credit_note_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT psp_credit_note_status_known CHECK (status IN ('issued', 'void')),
  CONSTRAINT psp_credit_note_state_known CHECK (state IN ('mirrored', 'drafted', 'posted', 'parked')),
  CONSTRAINT psp_credit_note_state_document CHECK ((state IN ('drafted', 'posted')) = (document_id IS NOT NULL) OR state = 'parked'),
  CONSTRAINT psp_credit_note_parked_reason CHECK ((state = 'parked') = (parked_reason IS NOT NULL)),
  CONSTRAINT psp_credit_note_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_credit_note_total CHECK (total <> 'NaN' AND total >= 0),
  CONSTRAINT psp_credit_note_lines_array CHECK (jsonb_typeof(draft_lines) = 'array' AND octet_length(draft_lines::text) <= 262144),
  CONSTRAINT psp_credit_note_lengths CHECK (length(provider_credit_note_id) BETWEEN 1 AND 255 AND length(provider_invoice_id) BETWEEN 1 AND 255
    AND length(number) BETWEEN 1 AND 100 AND length(reason) BETWEEN 1 AND 500 AND length(parked_reason) BETWEEN 1 AND 500),
  CONSTRAINT psp_credit_note_version_positive CHECK (version >= 1)
);
CREATE INDEX psp_credit_note_state ON fyniq.psp_credit_note (organisation_id, state);
CREATE INDEX psp_credit_note_invoice ON fyniq.psp_credit_note (organisation_id, connector_id, provider_invoice_id);

-- A mirror row's identity never changes, its AR document is set once (by the function below or never), nothing is deleted, and the worker may only
-- mirror and park: the draft is made by the function and the issue is the API's.
-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_credit_note_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A mirrored provider credit note is never deleted' USING ERRCODE = 'FYP33', HINT = 'It is a mirror of what the provider reported (FR-PSP-07).';
  END IF;
  IF (NEW.connector_id, NEW.provider_credit_note_id, NEW.provider_invoice_id, NEW.currency) IS DISTINCT FROM
     (OLD.connector_id, OLD.provider_credit_note_id, OLD.provider_invoice_id, OLD.currency) THEN
    RAISE EXCEPTION 'A mirrored credit note keeps its provider, ID, invoice and currency' USING ERRCODE = 'FYP33';
  END IF;
  IF OLD.document_id IS NOT NULL AND NEW.document_id IS DISTINCT FROM OLD.document_id THEN
    RAISE EXCEPTION 'A mirrored credit note has one AR document, made once' USING ERRCODE = 'FYP33';
  END IF;
  IF current_user = 'fyniq_worker' AND NEW.state IS DISTINCT FROM OLD.state AND NEW.state NOT IN ('mirrored', 'parked') THEN
    RAISE EXCEPTION 'The worker mirrors and parks a provider credit note; the draft is made by create_provider_credit_note_draft and issued by the API' USING ERRCODE = 'FYP33';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_credit_note_guard BEFORE UPDATE OR DELETE ON fyniq.psp_credit_note FOR EACH ROW EXECUTE FUNCTION fyniq.psp_credit_note_guard();
CREATE TRIGGER psp_credit_note_no_truncate BEFORE TRUNCATE ON fyniq.psp_credit_note FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- The narrow function: the draft AR credit note of one mirrored provider credit note (an issued one, not yet made into a document) against the
-- document of the provider invoice it credits, which must be issued already; from the mirror rows alone: its lines (the revenue account of the provider
-- account in the credit note's currency added, GST code SR), the invoice's customer, dated on the day the provider issued it, saved by
-- "psp:credit_notes". It records the document on the mirror row; the caller records the audit events. Returns the document.
-- +goose StatementBegin
CREATE FUNCTION fyniq.create_provider_credit_note_draft(p_connector uuid, p_provider_credit_note text)
RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org      uuid := fyniq.require_org();
  cn       fyniq.psp_credit_note;
  inv      fyniq.psp_invoice;
  inv_doc  fyniq.document;
  inv_body jsonb;
  revenue  uuid;
  doc      uuid;
  lines    jsonb;
  body     jsonb;
BEGIN
  SELECT * INTO cn FROM fyniq.psp_credit_note
   WHERE organisation_id = org AND connector_id = p_connector AND provider_credit_note_id = p_provider_credit_note FOR UPDATE;
  IF NOT FOUND OR cn.document_id IS NOT NULL OR cn.state <> 'mirrored' OR cn.status <> 'issued'
     OR cn.issued_on IS NULL OR jsonb_array_length(cn.draft_lines) = 0 THEN
    RAISE EXCEPTION 'Provider credit note % is not one to make a credit note of', p_provider_credit_note
      USING ERRCODE = 'FYP34', HINT = 'Only an issued mirrored credit note with its lines, not yet made, becomes an AR credit note (FR-PSP-07).';
  END IF;
  SELECT * INTO inv FROM fyniq.psp_invoice
   WHERE organisation_id = org AND connector_id = p_connector AND provider_invoice_id = cn.provider_invoice_id;
  IF NOT FOUND OR inv.document_id IS NULL OR inv.state <> 'posted' OR inv.currency <> cn.currency THEN
    RAISE EXCEPTION 'The invoice of provider credit note % is not an issued invoice here in its currency', p_provider_credit_note
      USING ERRCODE = 'FYP34', HINT = 'The credit note waits until the provider''s invoice is issued as an AR invoice (FR-PSP-07).';
  END IF;
  SELECT * INTO inv_doc FROM fyniq.document WHERE organisation_id = org AND id = inv.document_id;
  IF inv_doc.type <> 'invoice' OR inv_doc.status <> 'posted' THEN
    RAISE EXCEPTION 'The AR invoice of provider credit note % is not issued', p_provider_credit_note USING ERRCODE = 'FYP34';
  END IF;
  SELECT v.payload INTO inv_body FROM fyniq.document_version v
   WHERE v.organisation_id = org AND v.document_id = inv_doc.id ORDER BY v.version DESC LIMIT 1;
  SELECT a.revenue_account_id INTO revenue FROM fyniq.psp_account a
    JOIN fyniq.money_account m ON m.organisation_id = a.organisation_id AND m.id = a.money_account_id
   WHERE a.organisation_id = org AND a.connector_id = p_connector AND a.revenue_account_id IS NOT NULL AND m.currency = cn.currency
   ORDER BY a.created_at LIMIT 1;
  IF revenue IS NULL THEN
    RAISE EXCEPTION 'No provider account of this connector books % revenue', cn.currency
      USING ERRCODE = 'FYP34', HINT = 'Name the revenue account on the provider account first (FR-PSP-03).';
  END IF;
  SELECT jsonb_agg((l - 'spread_months' - 'spread_from') || jsonb_build_object('account_id', revenue, 'gst_code', 'SR')) INTO lines FROM jsonb_array_elements(cn.draft_lines) l;
  body := jsonb_build_object('customer_id', inv_body->>'customer_id', 'invoice_id', inv_doc.id, 'reference', coalesce(cn.number, cn.provider_credit_note_id),
            'date', to_char(cn.issued_on, 'YYYY-MM-DD'), 'currency', cn.currency, 'lines', lines,
            'notes', 'Credited through the payment provider: ' || cn.provider_credit_note_id || coalesce('. ' || cn.reason, ''));
  INSERT INTO fyniq.document (organisation_id, type) VALUES (org, 'credit_note') RETURNING id INTO doc;
  INSERT INTO fyniq.document_version (organisation_id, document_id, version, payload, created_by) VALUES (org, doc, 1, body, 'psp:credit_notes');
  UPDATE fyniq.psp_credit_note SET document_id = doc, state = 'drafted', version = version + 1 WHERE organisation_id = org AND id = cn.id;
  RETURN doc;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.create_provider_credit_note_draft(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.create_provider_credit_note_draft(uuid, text) TO fyniq_worker;

ALTER TABLE fyniq.psp_credit_note ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_credit_note FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_credit_note USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_credit_note
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_credit_note', 'id');

GRANT SELECT ON fyniq.psp_credit_note TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector_id, provider_credit_note_id, provider_invoice_id, number, status, currency, total, reason, issued_on, draft_lines, seen_at),
  UPDATE (number, status, total, reason, issued_on, draft_lines, state, parked_reason, version, seen_at) ON fyniq.psp_credit_note TO fyniq_worker;
-- The API moves a drafted credit note to posted, or parks it, when it issues the draft (the worker never posts).
GRANT UPDATE (state, parked_reason, version) ON fyniq.psp_credit_note TO fyniq_api;
