-- Restricted people data (PRD-002 NM-PPL-02 to NM-PPL-05, NM-PPL-07; p1-nemura-records-packs slice 2).
--
-- NRIC or FIN, the salary bank account and a work pass's number are envelope-encrypted at the application level (the cipher port) and kept with a
-- masked form and a keyed fingerprint: the database never holds the plain value. The bank account changes only through a change that someone else
-- approves and the employee confirms (D5). Dependants are effective-dated; documents are append-only evidence with a retention class.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.person_secret (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  person_id        uuid        NOT NULL,
  nric_sealed      bytea,
  nric_masked      text,
  nric_fingerprint bytea,
  bank_sealed      bytea,
  bank_masked      text,
  bank_fingerprint bytea,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_secret_pkey PRIMARY KEY (organisation_id, person_id),
  CONSTRAINT person_secret_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_secret_nric_whole CHECK ((nric_sealed IS NULL) = (nric_masked IS NULL) AND (nric_sealed IS NULL) = (nric_fingerprint IS NULL)),
  CONSTRAINT person_secret_bank_whole CHECK ((bank_sealed IS NULL) = (bank_masked IS NULL) AND (bank_sealed IS NULL) = (bank_fingerprint IS NULL)),
  CONSTRAINT person_secret_masked_hides CHECK ((nric_masked IS NULL OR nric_masked ~ '\*') AND (bank_masked IS NULL OR bank_masked ~ '\*'))
);

CREATE TABLE nemura.work_pass (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  pass_type       text        NOT NULL,
  number_sealed   bytea       NOT NULL,
  number_masked   text        NOT NULL,
  issued_on       date        NOT NULL,
  expires_on      date        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT work_pass_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT work_pass_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT work_pass_type_known CHECK (pass_type IN ('employment_pass', 's_pass', 'work_permit', 'dp_work_permit')),
  CONSTRAINT work_pass_dates CHECK (expires_on > issued_on),
  CONSTRAINT work_pass_masked_hides CHECK (number_masked ~ '\*')
);
CREATE INDEX work_pass_person_idx ON nemura.work_pass (organisation_id, person_id, expires_on);

CREATE TABLE nemura.person_dependant (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  relationship    text        NOT NULL,
  birth_date      date,
  adoption_intent boolean     NOT NULL DEFAULT false,
  citizenship     text        NOT NULL,
  declared_on     date        NOT NULL,
  evidence_id     uuid,
  effective_from  date        NOT NULL,
  effective_to    date,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_dependant_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_dependant_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_dependant_relationship_known CHECK (relationship IN ('child', 'adopted_child', 'stepchild')),
  CONSTRAINT person_dependant_citizenship_known CHECK (citizenship IN ('citizen', 'permanent_resident', 'other')),
  CONSTRAINT person_dependant_birth_or_intent CHECK (birth_date IS NOT NULL OR adoption_intent),
  CONSTRAINT person_dependant_dates CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE INDEX person_dependant_person_idx ON nemura.person_dependant (organisation_id, person_id, effective_from);

CREATE TABLE nemura.person_document (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  doc_type        text        NOT NULL,
  title           text        NOT NULL,
  retention_class text        NOT NULL,
  blob_key        text        NOT NULL,
  content_type    text        NOT NULL,
  size_bytes      bigint      NOT NULL,
  sha256          bytea       NOT NULL,
  viewers         text[]      NOT NULL,
  uploaded_by     uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_document_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_document_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_document_uploaded_by_fkey FOREIGN KEY (organisation_id, uploaded_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT person_document_type_known CHECK (doc_type IN ('contract', 'kets', 'letter', 'pass_copy', 'certificate', 'other')),
  CONSTRAINT person_document_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT person_document_sha256 CHECK (length(sha256) = 32),
  CONSTRAINT person_document_size CHECK (size_bytes BETWEEN 1 AND 20971520),
  CONSTRAINT person_document_viewers_some CHECK (cardinality(viewers) BETWEEN 1 AND 10)
);
CREATE INDEX person_document_person_idx ON nemura.person_document (organisation_id, person_id, created_at);

CREATE TABLE nemura.bank_account_change (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  new_sealed      bytea       NOT NULL,
  new_masked      text        NOT NULL,
  new_fingerprint bytea       NOT NULL,
  proposed_by     uuid        NOT NULL,
  approval_id     uuid        NOT NULL,
  state           text        NOT NULL DEFAULT 'proposed',
  confirmed_at    timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT bank_account_change_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_account_change_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT bank_account_change_proposed_by_fkey FOREIGN KEY (organisation_id, proposed_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT bank_account_change_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES nemura.approval_request (organisation_id, id),
  CONSTRAINT bank_account_change_state_known CHECK (state IN ('proposed', 'approved', 'refused', 'confirmed')),
  CONSTRAINT bank_account_change_confirmed_whole CHECK ((state = 'confirmed') = (confirmed_at IS NOT NULL)),
  CONSTRAINT bank_account_change_masked_hides CHECK (new_masked ~ '\*')
);

-- The account a change proposes never changes; it moves forward through its states only (proposed, approved or refused, then confirmed), never back.
-- +goose StatementBegin
CREATE FUNCTION nemura.bank_account_change_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.new_sealed IS DISTINCT FROM OLD.new_sealed OR NEW.new_masked IS DISTINCT FROM OLD.new_masked
     OR NEW.proposed_by IS DISTINCT FROM OLD.proposed_by OR NEW.approval_id IS DISTINCT FROM OLD.approval_id
     OR NOT ((OLD.state = 'proposed' AND NEW.state IN ('approved', 'refused')) OR (OLD.state = 'approved' AND NEW.state = 'confirmed')) THEN
    RAISE EXCEPTION 'A bank account change moves forward only: proposed, approved or refused, then confirmed' USING ERRCODE = 'NM126';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_account_change_guard BEFORE UPDATE OR DELETE ON nemura.bank_account_change FOR EACH ROW EXECUTE FUNCTION nemura.bank_account_change_guard();

-- Documents and passes are evidence: written once, never changed, never deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.append_only_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% is append-only: a correction is a new record', TG_ARGV[0] USING ERRCODE = 'NM127';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_document_append_only BEFORE UPDATE OR DELETE ON nemura.person_document FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A document');
CREATE TRIGGER work_pass_append_only BEFORE UPDATE OR DELETE ON nemura.work_pass FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A work pass');
-- +goose StatementBegin
CREATE FUNCTION nemura.person_dependant_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.relationship IS DISTINCT FROM OLD.relationship OR NEW.birth_date IS DISTINCT FROM OLD.birth_date
     OR NEW.adoption_intent IS DISTINCT FROM OLD.adoption_intent OR NEW.citizenship IS DISTINCT FROM OLD.citizenship OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
     OR NEW.evidence_id IS DISTINCT FROM OLD.evidence_id OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to) THEN
    RAISE EXCEPTION 'A dependant record is history: it can only be ended, once' USING ERRCODE = 'NM128';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_dependant_guard BEFORE UPDATE OR DELETE ON nemura.person_dependant FOR EACH ROW EXECUTE FUNCTION nemura.person_dependant_guard();

-- ------------------------------------------------------------------ audit, row-level security, grants
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_secret DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_secret', 'person_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.work_pass DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('work_pass', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_dependant DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_dependant', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_document DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_document', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.bank_account_change DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('bank_account_change', 'id');

ALTER TABLE nemura.person_secret ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_secret FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_secret USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.work_pass ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.work_pass FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.work_pass USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.person_dependant ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_dependant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_dependant USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.person_document ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_document FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_document USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.bank_account_change ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.bank_account_change FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.bank_account_change USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.person_secret, nemura.work_pass, nemura.person_dependant, nemura.person_document, nemura.bank_account_change TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (person_id, nric_sealed, nric_masked, nric_fingerprint, bank_sealed, bank_masked, bank_fingerprint),
      UPDATE (nric_sealed, nric_masked, nric_fingerprint, bank_sealed, bank_masked, bank_fingerprint, updated_at) ON nemura.person_secret TO nemura_api;
GRANT INSERT (id, person_id, pass_type, number_sealed, number_masked, issued_on, expires_on) ON nemura.work_pass TO nemura_api;
GRANT INSERT (id, person_id, relationship, birth_date, adoption_intent, citizenship, declared_on, evidence_id, effective_from), UPDATE (effective_to) ON nemura.person_dependant TO nemura_api;
GRANT INSERT (id, person_id, doc_type, title, retention_class, blob_key, content_type, size_bytes, sha256, viewers, uploaded_by) ON nemura.person_document TO nemura_api;
GRANT INSERT (id, person_id, new_sealed, new_masked, new_fingerprint, proposed_by, approval_id), UPDATE (state, confirmed_at) ON nemura.bank_account_change TO nemura_api;
