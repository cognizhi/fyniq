-- Letters, templates and what an issued letter proves (PRD-002 NM-LTR-01, NM-LTR-02, NM-LTR-03, NM-LTR-07; p1-nemura-self-service-letters slice 1).
--
-- A letter type is switched on by the company; its template is text blocks with typed merge fields, a draft until the HR administrator approves it, and then an immutable
-- numbered version. A request fills a version from the person's facts; an HR officer checks it (the PDF is drawn and hashed then); the signatory signs it with a fresh
-- passkey over that hash; the letter is issued once, as evidence nothing can change: the exact bytes' SHA-256, the template version, the data, the signatory, their authority
-- and the envelope the passkey confirmed.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.letter_settings (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  company_name    text        NOT NULL,
  uen             text        NOT NULL,
  address         text        NOT NULL,
  updated_by      uuid        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT letter_settings_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT letter_settings_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT letter_settings_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_settings_lengths CHECK (length(btrim(company_name)) BETWEEN 1 AND 200 AND length(btrim(uen)) BETWEEN 1 AND 30 AND length(btrim(address)) BETWEEN 1 AND 400)
);

CREATE TABLE nemura.letter_type (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  type_key        text        NOT NULL,
  enabled         boolean     NOT NULL,
  due_days        integer     NOT NULL,   -- working days to issue
  updated_by      uuid        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT letter_type_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_type_once UNIQUE (organisation_id, type_key),
  CONSTRAINT letter_type_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_type_key CHECK (type_key ~ '^[a-z][a-z0-9_]{1,60}$'),
  CONSTRAINT letter_type_due CHECK (due_days BETWEEN 1 AND 30)
);

CREATE TABLE nemura.letter_template_version (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  type_key        text        NOT NULL,
  version         integer,                -- numbered when published
  blocks          jsonb       NOT NULL,
  note            text,
  state           text        NOT NULL DEFAULT 'draft',
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  approved_by     uuid,
  approved_at     timestamptz,
  CONSTRAINT letter_template_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_template_version_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_template_version_approved_fkey FOREIGN KEY (organisation_id, approved_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_template_version_type_key CHECK (type_key ~ '^[a-z][a-z0-9_]{1,60}$'),
  CONSTRAINT letter_template_version_blocks CHECK (jsonb_typeof(blocks) = 'array' AND jsonb_array_length(blocks) BETWEEN 1 AND 40 AND octet_length(blocks::text) <= 65536),
  CONSTRAINT letter_template_version_state CHECK (state IN ('draft', 'published', 'superseded')),
  CONSTRAINT letter_template_version_published CHECK ((state = 'draft') = (version IS NULL) AND (state = 'draft') = (approved_by IS NULL) AND (state = 'draft') = (approved_at IS NULL)),
  CONSTRAINT letter_template_version_not_own_approval CHECK (approved_by IS NULL OR approved_by <> created_by),
  CONSTRAINT letter_template_version_note_length CHECK (note IS NULL OR length(note) <= 500),
  CONSTRAINT letter_template_version_numbered UNIQUE (organisation_id, type_key, version)
);

-- A draft's blocks and note may be edited; a published version never changes except to be superseded by a newer one; the approver is not the author (the check above).
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_template_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A template version is never deleted' USING ERRCODE = 'NM160';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.type_key IS DISTINCT FROM OLD.type_key OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
      RAISE EXCEPTION 'A template version keeps its type and its author' USING ERRCODE = 'NM160';
    END IF;
    IF OLD.state = 'draft' AND NEW.state = 'draft' THEN
      RETURN NEW;
    END IF;
    IF OLD.state = 'draft' AND NEW.state = 'published' THEN
      IF NEW.blocks IS DISTINCT FROM OLD.blocks OR NEW.note IS DISTINCT FROM OLD.note THEN
        RAISE EXCEPTION 'A draft is published as it is' USING ERRCODE = 'NM160';
      END IF;
      RETURN NEW;
    END IF;
    IF OLD.state = 'published' AND NEW.state = 'superseded' AND NEW.blocks IS NOT DISTINCT FROM OLD.blocks AND NEW.version IS NOT DISTINCT FROM OLD.version
       AND NEW.approved_by IS NOT DISTINCT FROM OLD.approved_by AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at AND NEW.note IS NOT DISTINCT FROM OLD.note THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'A published template version is immutable: change it with a new version' USING ERRCODE = 'NM160';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER letter_template_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.letter_template_version FOR EACH ROW EXECUTE FUNCTION nemura.letter_template_guard();

CREATE TABLE nemura.letter_request (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  type_key        text        NOT NULL,
  template_id     uuid        NOT NULL,   -- the published version it is filled from
  purpose         text        NOT NULL,
  data            jsonb       NOT NULL,   -- the values of the template's fields, written once when the request is made
  state           text        NOT NULL DEFAULT 'requested',
  due_on          date        NOT NULL,
  requested_by    uuid        NOT NULL,
  requested_at    timestamptz NOT NULL,
  reference       text,                   -- given when it is checked, as the PDF carries it
  render_blob     text,
  render_sha256   bytea,
  checked_by      uuid,
  checked_at      timestamptz,
  decision_reason text,
  decided_at      timestamptz,
  CONSTRAINT letter_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_request_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT letter_request_template_fkey FOREIGN KEY (organisation_id, template_id) REFERENCES nemura.letter_template_version (organisation_id, id),
  CONSTRAINT letter_request_by_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_request_checked_by_fkey FOREIGN KEY (organisation_id, checked_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_request_state CHECK (state IN ('requested', 'checked', 'refused', 'issued', 'withdrawn')),
  CONSTRAINT letter_request_purpose_length CHECK (length(btrim(purpose)) BETWEEN 1 AND 300),
  CONSTRAINT letter_request_checked_whole CHECK ((render_sha256 IS NULL) = (render_blob IS NULL) AND (render_sha256 IS NULL) = (reference IS NULL) AND (render_sha256 IS NULL) = (checked_by IS NULL) AND (render_sha256 IS NULL) = (checked_at IS NULL)),
  CONSTRAINT letter_request_checked_state CHECK (state NOT IN ('checked', 'issued') OR render_sha256 IS NOT NULL),
  CONSTRAINT letter_request_refusal_reason CHECK (state <> 'refused' OR length(btrim(coalesce(decision_reason, ''))) > 0),
  CONSTRAINT letter_request_reason_length CHECK (decision_reason IS NULL OR length(decision_reason) <= 500),
  CONSTRAINT letter_request_reference_once UNIQUE (organisation_id, reference)
);
CREATE INDEX letter_request_state_idx ON nemura.letter_request (organisation_id, state, due_on);
CREATE INDEX letter_request_person_idx ON nemura.letter_request (organisation_id, person_id, requested_at);

-- A request keeps its words and its data; it moves requested to checked, refused or withdrawn, and checked to issued, refused or withdrawn; a check is made by someone other
-- than the person; nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A letter request is evidence: it is withdrawn, never deleted' USING ERRCODE = 'NM161';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'requested' THEN
      RAISE EXCEPTION 'A letter request starts requested' USING ERRCODE = 'NM161';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.type_key IS DISTINCT FROM OLD.type_key OR NEW.template_id IS DISTINCT FROM OLD.template_id OR NEW.purpose IS DISTINCT FROM OLD.purpose
     OR NEW.data IS DISTINCT FROM OLD.data OR NEW.due_on IS DISTINCT FROM OLD.due_on OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.requested_at IS DISTINCT FROM OLD.requested_at THEN
    RAISE EXCEPTION 'A letter request keeps its words and its data' USING ERRCODE = 'NM161';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT ((OLD.state = 'requested' AND NEW.state IN ('checked', 'refused', 'withdrawn')) OR (OLD.state = 'checked' AND NEW.state IN ('issued', 'refused', 'withdrawn'))) THEN
    RAISE EXCEPTION 'A % letter request cannot become %', OLD.state, NEW.state USING ERRCODE = 'NM161';
  END IF;
  IF OLD.render_sha256 IS NOT NULL AND (NEW.render_sha256 IS DISTINCT FROM OLD.render_sha256 OR NEW.render_blob IS DISTINCT FROM OLD.render_blob OR NEW.reference IS DISTINCT FROM OLD.reference) THEN
    RAISE EXCEPTION 'The checked letter is the letter that is signed: its bytes and reference never change' USING ERRCODE = 'NM161';
  END IF;
  IF NEW.state = 'checked' AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.member_id = NEW.checked_by) THEN
    RAISE EXCEPTION 'Nobody checks their own letter' USING ERRCODE = 'NM162';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER letter_request_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.letter_request FOR EACH ROW EXECUTE FUNCTION nemura.letter_request_guard();

CREATE TABLE nemura.letter_issued (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  request_id      uuid        NOT NULL,
  person_id       uuid        NOT NULL,
  type_key        text        NOT NULL,
  reference       text        NOT NULL,
  blob_key        text        NOT NULL,
  sha256          bytea       NOT NULL,   -- of the exact PDF bytes issued
  template_id     uuid        NOT NULL,
  data            jsonb       NOT NULL,
  signatory       uuid        NOT NULL,
  authority       text        NOT NULL,   -- the signing role or the delegation that gave the right
  envelope        jsonb       NOT NULL,   -- what the passkey confirmed: the letter, its hash, the template version, when
  issued_at       timestamptz NOT NULL,
  replaces        uuid,
  CONSTRAINT letter_issued_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_issued_request_fkey FOREIGN KEY (organisation_id, request_id) REFERENCES nemura.letter_request (organisation_id, id),
  CONSTRAINT letter_issued_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT letter_issued_template_fkey FOREIGN KEY (organisation_id, template_id) REFERENCES nemura.letter_template_version (organisation_id, id),
  CONSTRAINT letter_issued_signatory_fkey FOREIGN KEY (organisation_id, signatory) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_issued_sha CHECK (octet_length(sha256) = 32),
  CONSTRAINT letter_issued_request_once UNIQUE (organisation_id, request_id),
  CONSTRAINT letter_issued_reference_once UNIQUE (organisation_id, reference)
);
CREATE TRIGGER letter_issued_append_only BEFORE UPDATE OR DELETE ON nemura.letter_issued FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('An issued letter');

-- A delivery link: the token is kept as a hash and expires; it delivers the one letter and nothing else.
CREATE TABLE nemura.letter_link (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  issued_id       uuid        NOT NULL,
  token_hash      bytea       NOT NULL,
  expires_at      timestamptz NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT letter_link_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_link_issued_fkey FOREIGN KEY (organisation_id, issued_id) REFERENCES nemura.letter_issued (organisation_id, id),
  CONSTRAINT letter_link_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_link_token CHECK (octet_length(token_hash) = 32),
  CONSTRAINT letter_link_token_once UNIQUE (token_hash)
);
CREATE TRIGGER letter_link_append_only BEFORE UPDATE OR DELETE ON nemura.letter_link FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A letter link');

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.letter_settings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_settings', 'organisation_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.letter_type DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_type', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.letter_template_version DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_template_version', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.letter_request DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_request', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.letter_issued DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_issued', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.letter_link DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_link', 'id');

ALTER TABLE nemura.letter_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_settings USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.letter_type ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_type FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_type USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.letter_template_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_template_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_template_version USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.letter_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_request USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.letter_issued ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_issued FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_issued USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.letter_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_link USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.letter_settings, nemura.letter_type, nemura.letter_template_version, nemura.letter_request, nemura.letter_issued, nemura.letter_link TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (company_name, uen, address, updated_by, updated_at), UPDATE (company_name, uen, address, updated_by, updated_at) ON nemura.letter_settings TO nemura_api;
GRANT INSERT (id, type_key, enabled, due_days, updated_by, updated_at), UPDATE (enabled, due_days, updated_by, updated_at) ON nemura.letter_type TO nemura_api;
GRANT INSERT (id, type_key, blocks, note, created_by, created_at), UPDATE (blocks, note, state, version, approved_by, approved_at) ON nemura.letter_template_version TO nemura_api;
GRANT INSERT (id, person_id, type_key, template_id, purpose, data, due_on, requested_by, requested_at), UPDATE (state, reference, render_blob, render_sha256, checked_by, checked_at, decision_reason, decided_at) ON nemura.letter_request TO nemura_api;
GRANT INSERT (id, request_id, person_id, type_key, reference, blob_key, sha256, template_id, data, signatory, authority, envelope, issued_at, replaces) ON nemura.letter_issued TO nemura_api;
GRANT INSERT (id, issued_id, token_hash, expires_at, created_by, created_at) ON nemura.letter_link TO nemura_api;
