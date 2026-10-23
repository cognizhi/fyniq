-- Which template renders a document (PRD-001 FR-TPL-05; the owner's decisions of 23 Oct 2026, rev 0.98; built in rev
-- 0.99; p1-templates-email task 1.3).
--
-- A document type's default for each kind is chosen among its published templates by a person who holds A on "Document
-- templates", with no separate request; a customer's or supplier's override is set on its contact by whoever edits
-- contacts. Both are append-only: the latest choice for a kind and type (and contact) is in force, and one naming no
-- template returns to the default, or to the platform's classic starter (version 0) when there is no default. A choice
-- never changes or goes (FY177), and names only a published template of its own kind and type (FY178).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.document_template_default (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,
  document_type   text        NOT NULL,
  template_id     uuid,                  -- none: back to the platform's classic starter
  set_by          text        NOT NULL,
  set_at          timestamptz NOT NULL,
  CONSTRAINT document_template_default_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_template_default_template_fkey FOREIGN KEY (organisation_id, template_id)
    REFERENCES fyniq.document_template (organisation_id, id),
  CONSTRAINT document_template_default_kind_known CHECK (kind IN ('document', 'email')),
  CONSTRAINT document_template_default_type_known CHECK (document_type IN ('invoice', 'quote', 'credit_note', 'receipt', 'statement',
    'reminder', 'remittance', 'purchase_order')),
  CONSTRAINT document_template_default_set_by_format CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX document_template_default_latest ON fyniq.document_template_default (organisation_id, kind, document_type, set_at);

CREATE TABLE fyniq.contact_document_template (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  contact_id      uuid        NOT NULL,
  kind            text        NOT NULL,
  document_type   text        NOT NULL,
  template_id     uuid,                  -- none: back to the type's default
  set_by          text        NOT NULL,
  set_at          timestamptz NOT NULL,
  CONSTRAINT contact_document_template_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT contact_document_template_contact_fkey FOREIGN KEY (organisation_id, contact_id)
    REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT contact_document_template_template_fkey FOREIGN KEY (organisation_id, template_id)
    REFERENCES fyniq.document_template (organisation_id, id),
  CONSTRAINT contact_document_template_kind_known CHECK (kind IN ('document', 'email')),
  CONSTRAINT contact_document_template_type_known CHECK (document_type IN ('invoice', 'quote', 'credit_note', 'receipt', 'statement',
    'reminder', 'remittance', 'purchase_order')),
  CONSTRAINT contact_document_template_set_by_format CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX contact_document_template_latest ON fyniq.contact_document_template (organisation_id, contact_id, kind, document_type, set_at);

-- A choice never changes or goes (FY177), and names only a published template of its own kind and type (FY178).
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_template_choice_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t record;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A template choice never changes or goes' USING ERRCODE = 'FY177',
      HINT = 'Choose again: the latest choice is in force (FR-TPL-05).';
  END IF;
  IF NEW.template_id IS NOT NULL THEN
    SELECT kind, document_type, published INTO t FROM fyniq.document_template
     WHERE organisation_id = NEW.organisation_id AND id = NEW.template_id
     FOR SHARE;
    IF t.kind IS DISTINCT FROM NEW.kind OR t.document_type IS DISTINCT FROM NEW.document_type OR t.published IS NULL THEN
      RAISE EXCEPTION 'Template % is not a published % template of a %', NEW.template_id, NEW.kind, NEW.document_type
        USING ERRCODE = 'FY178', HINT = 'Choose a published template of the same kind and document type.';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_template_default_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.document_template_default
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_template_choice_guard();
CREATE TRIGGER contact_document_template_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.contact_document_template
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_template_choice_guard();
CREATE TRIGGER document_template_default_no_truncate BEFORE TRUNCATE ON fyniq.document_template_default
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER contact_document_template_no_truncate BEFORE TRUNCATE ON fyniq.contact_document_template
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.document_template_default ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_template_default FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_template_default
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.contact_document_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.contact_document_template FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.contact_document_template
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.document_template_default
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_template_default', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.contact_document_template
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('contact', 'contact_id');

GRANT SELECT ON fyniq.document_template_default, fyniq.contact_document_template TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (kind, document_type, template_id, set_by, set_at) ON fyniq.document_template_default TO fyniq_api;
GRANT INSERT (contact_id, kind, document_type, template_id, set_by, set_at) ON fyniq.contact_document_template TO fyniq_api;
