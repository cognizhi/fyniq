-- Document and email templates, their drafts and published versions (PRD-001 FR-TPL-02, FR-TPL-04 and FR-TPL-05, P1
-- parts; built in rev 0.95; p1-templates-email task 1.1).
--
-- A template is data: the content of FR-TPL-01's blocks, theme and texts, with fields from a typed catalogue, which the
-- render service maps to Typst sources it owns (the domain's doctemplate package checks it). Each template has one
-- draft, saved over in place at the next version (the ETag), and its published versions: numbered from 1, each the whole
-- content with its SHA-256, who published it, when and why, and, for a rollback, the earlier version it republishes.
-- A version never changes or goes, and a template is never deleted (FY176); the template's latest published number
-- moves forward by one, only with the version it names. Publishing is an approval object (document_template.publish):
-- in Founder mode an approver publishes alone and the weekly review sees it; otherwise another person approves.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.document_template (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,  -- a document rendered to PDF, or the email that carries it
  document_type   text        NOT NULL,
  name            text        NOT NULL,
  draft           jsonb       NOT NULL,  -- the draft's content
  version         integer     NOT NULL DEFAULT 1,
  published       integer,               -- the latest published version's number; none until the first
  saved_by        text        NOT NULL,
  saved_at        timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_template_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_template_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT document_template_kind_known CHECK (kind IN ('document', 'email')),
  CONSTRAINT document_template_type_known CHECK (document_type IN ('invoice', 'quote', 'credit_note', 'receipt', 'statement',
    'reminder', 'remittance', 'purchase_order')),
  CONSTRAINT document_template_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT document_template_draft_object CHECK (jsonb_typeof(draft) = 'object' AND octet_length(draft::text) <= 65536),
  CONSTRAINT document_template_version_positive CHECK (version > 0),
  CONSTRAINT document_template_published_positive CHECK (published IS NULL OR published > 0),
  CONSTRAINT document_template_saved_by_format CHECK (saved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX document_template_type ON fyniq.document_template (organisation_id, document_type, kind, id);

CREATE TABLE fyniq.document_template_version (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  template_id      uuid        NOT NULL,
  number           integer     NOT NULL,
  content          jsonb       NOT NULL,
  content_hash     bytea       NOT NULL,  -- SHA-256 of the content's RFC 8785 JSON
  note             text,
  republished_from integer,               -- a rollback: the earlier version whose content this is
  published_by     text        NOT NULL,
  published_at     timestamptz NOT NULL,
  CONSTRAINT document_template_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_template_version_number_key UNIQUE (organisation_id, template_id, number),
  CONSTRAINT document_template_version_template_fkey FOREIGN KEY (organisation_id, template_id)
    REFERENCES fyniq.document_template (organisation_id, id),
  CONSTRAINT document_template_version_number_positive CHECK (number > 0),
  CONSTRAINT document_template_version_content_object CHECK (jsonb_typeof(content) = 'object' AND octet_length(content::text) <= 65536),
  CONSTRAINT document_template_version_hash_length CHECK (octet_length(content_hash) = 32),
  CONSTRAINT document_template_version_note_length CHECK (note IS NULL OR length(btrim(note)) BETWEEN 1 AND 500),
  CONSTRAINT document_template_version_republished CHECK (republished_from IS NULL OR (republished_from > 0 AND republished_from < number)),
  CONSTRAINT document_template_version_published_by_format CHECK (published_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- A template keeps its kind and type and is never deleted; its draft moves its version forward by one; its latest
-- published number moves forward by one, only to a version that exists. A version is the next number of its template,
-- read under the template's lock, republishes only an earlier one, and never changes or goes (FY176).
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_template_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest integer;
BEGIN
  IF TG_TABLE_NAME = 'document_template' THEN
    IF TG_OP = 'DELETE' THEN
      RAISE EXCEPTION 'Template % is never deleted', OLD.id USING ERRCODE = 'FY176',
        HINT = 'Its versions stay with the documents they rendered (FR-TPL-04).';
    ELSIF TG_OP = 'INSERT' THEN
      IF NEW.version <> 1 OR NEW.published IS NOT NULL THEN
        RAISE EXCEPTION 'A new template starts as a draft at version 1' USING ERRCODE = 'FY176';
      END IF;
    ELSIF NEW.id <> OLD.id OR NEW.kind <> OLD.kind OR NEW.document_type <> OLD.document_type OR NEW.created_at <> OLD.created_at THEN
      RAISE EXCEPTION 'Template % keeps what it is', OLD.id USING ERRCODE = 'FY176',
        HINT = 'Make a new template for another kind or document type.';
    ELSIF NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'Template % moves from version % to the next only', OLD.id, OLD.version USING ERRCODE = 'FY176';
    ELSIF NEW.published IS DISTINCT FROM OLD.published THEN
      IF NEW.published IS DISTINCT FROM coalesce(OLD.published, 0) + 1 OR NOT EXISTS (
          SELECT 1 FROM fyniq.document_template_version v
           WHERE v.organisation_id = NEW.organisation_id AND v.template_id = NEW.id AND v.number = NEW.published) THEN
        RAISE EXCEPTION 'Template % publishes its next version, %, only with that version', OLD.id, coalesce(OLD.published, 0) + 1
          USING ERRCODE = 'FY176';
      END IF;
    END IF;
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A published template version never changes or goes' USING ERRCODE = 'FY176',
      HINT = 'Publish a new version, or roll back by republishing an earlier one (FR-TPL-04).';
  END IF;
  SELECT coalesce(published, 0) INTO latest FROM fyniq.document_template
   WHERE organisation_id = NEW.organisation_id AND id = NEW.template_id
   FOR UPDATE;
  IF NEW.number <> latest + 1 THEN
    RAISE EXCEPTION 'Template % publishes version % next, not %', NEW.template_id, latest + 1, NEW.number USING ERRCODE = 'FY176';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_template_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.document_template
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_template_guard();
CREATE TRIGGER document_template_version_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.document_template_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_template_guard();
CREATE TRIGGER document_template_no_truncate BEFORE TRUNCATE ON fyniq.document_template
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER document_template_version_no_truncate BEFORE TRUNCATE ON fyniq.document_template_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.document_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_template FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_template
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.document_template_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_template_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_template_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.document_template
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_template', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.document_template_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_template', 'template_id');

GRANT SELECT ON fyniq.document_template, fyniq.document_template_version TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (kind, document_type, name, draft, saved_by, saved_at), UPDATE (name, draft, version, published, saved_by, saved_at)
  ON fyniq.document_template TO fyniq_api;
GRANT INSERT (template_id, number, content, content_hash, note, republished_from, published_by, published_at)
  ON fyniq.document_template_version TO fyniq_api;

-- Publishing a template is an approval object (FR-TPL-04, FR-APR-01).
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
                               ('document_template', 'document_template.publish'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template')) NOT VALID;
