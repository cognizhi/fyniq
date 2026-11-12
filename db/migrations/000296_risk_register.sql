-- The risk register (PRD-001 CMP-06; p1-compliance-center slice 6, design D7).
--
-- A risk is a row of its own (its reference, such as R1, unique in the organisation), and each saved state of it a
-- version: its title, words, owner role, likelihood and impact, mitigation, review interval and next review date, and
-- whether it is open. A review records who reviewed which version, when, and the next review date it set. Versions and
-- reviews are only ever added (FY309): the register's history is its versions. Each is audited on the risk in the same
-- transaction (AUD-01). The API writes them for the Compliance officer (Appendix D "Compliance Center", C/A); first-run
-- setup writes the operator's starting register (the owner's decision of 9 Nov 2026).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.risk (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  ref             text        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT risk_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT risk_ref_key UNIQUE (organisation_id, ref),
  CONSTRAINT risk_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT risk_ref_format CHECK (ref ~ '^R[0-9]{1,4}$')
);

CREATE TABLE fyniq.risk_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  risk_id         uuid        NOT NULL,
  version         integer     NOT NULL,
  title           text        NOT NULL,
  description     text        NOT NULL DEFAULT '',
  owner_role      text        NOT NULL,
  likelihood      text        NOT NULL,
  impact          text        NOT NULL,
  mitigation      text        NOT NULL,
  review_months   smallint    NOT NULL DEFAULT 3,
  next_review_on  date        NOT NULL,
  state           text        NOT NULL DEFAULT 'open',
  saved_by        text        NOT NULL,  -- the actor, as records name it: user:<id>
  saved_at        timestamptz NOT NULL,
  CONSTRAINT risk_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT risk_version_once UNIQUE (organisation_id, risk_id, version),
  CONSTRAINT risk_version_risk_fkey FOREIGN KEY (organisation_id, risk_id) REFERENCES fyniq.risk (organisation_id, id),
  CONSTRAINT risk_version_positive CHECK (version > 0),
  CONSTRAINT risk_version_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 300),
  CONSTRAINT risk_version_description_length CHECK (length(description) <= 4000),
  CONSTRAINT risk_version_owner_known CHECK (owner_role IN ('owner', 'administrator', 'controller', 'accountant', 'dpo', 'auditor')),
  CONSTRAINT risk_version_likelihood_known CHECK (likelihood IN ('low', 'medium', 'high')),
  CONSTRAINT risk_version_impact_known CHECK (impact IN ('low', 'medium', 'high')),
  CONSTRAINT risk_version_mitigation_length CHECK (length(btrim(mitigation)) BETWEEN 1 AND 4000),
  CONSTRAINT risk_version_review_months CHECK (review_months BETWEEN 1 AND 12),
  CONSTRAINT risk_version_state_known CHECK (state IN ('open', 'closed')),
  CONSTRAINT risk_version_saved_by_format CHECK (saved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.risk_review (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  risk_id         uuid        NOT NULL,
  version         integer     NOT NULL,  -- the version reviewed
  reviewed_by     text        NOT NULL,  -- the actor, as records name it: user:<id>
  reviewed_at     timestamptz NOT NULL,
  note            text        NOT NULL DEFAULT '',
  next_review_on  date        NOT NULL,
  CONSTRAINT risk_review_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT risk_review_version_fkey FOREIGN KEY (organisation_id, risk_id, version)
    REFERENCES fyniq.risk_version (organisation_id, risk_id, version),
  CONSTRAINT risk_review_by_format CHECK (reviewed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT risk_review_note_length CHECK (length(note) <= 2000)
);
CREATE INDEX risk_review_risk_idx ON fyniq.risk_review (organisation_id, risk_id, reviewed_at);

-- A version is the risk's next, and a review is of its latest version and sets a later review date; neither ever
-- changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.risk_register_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A risk''s versions and reviews never change or go' USING ERRCODE = 'FY309',
      HINT = 'Save the risk again: that is its next version (CMP-06).';
  END IF;
  SELECT max(v.version) INTO latest FROM fyniq.risk_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.risk_id = NEW.risk_id;
  IF TG_TABLE_NAME = 'risk_version' THEN
    IF NEW.version <> coalesce(latest, 0) + 1 THEN
      RAISE EXCEPTION 'Version % of a risk follows its version %', NEW.version, coalesce(latest, 0) USING ERRCODE = 'FY309';
    END IF;
    RETURN NEW;
  END IF;
  -- risk_review: a field only it has is read here only
  IF NEW.version IS DISTINCT FROM latest OR NEW.next_review_on <= (NEW.reviewed_at AT TIME ZONE 'UTC')::date THEN
    RAISE EXCEPTION 'A review is of the risk''s latest version, and sets a later review date' USING ERRCODE = 'FY309';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER risk_version_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.risk_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.risk_register_guard();
CREATE TRIGGER risk_review_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.risk_review
  FOR EACH ROW EXECUTE FUNCTION fyniq.risk_register_guard();
CREATE TRIGGER risk_immutable BEFORE UPDATE OR DELETE ON fyniq.risk
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER risk_no_truncate BEFORE TRUNCATE ON fyniq.risk
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER risk_version_no_truncate BEFORE TRUNCATE ON fyniq.risk_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER risk_review_no_truncate BEFORE TRUNCATE ON fyniq.risk_review
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.risk
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('risk', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.risk_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('risk', 'risk_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.risk_review
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('risk', 'risk_id');

ALTER TABLE fyniq.risk ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.risk FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.risk
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.risk_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.risk_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.risk_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.risk_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.risk_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.risk_review
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (ref, created_at) ON fyniq.risk TO fyniq_api;
GRANT INSERT (risk_id, version, title, description, owner_role, likelihood, impact, mitigation, review_months, next_review_on, state,
  saved_by, saved_at) ON fyniq.risk_version TO fyniq_api;
GRANT INSERT (risk_id, version, reviewed_by, reviewed_at, note, next_review_on) ON fyniq.risk_review TO fyniq_api;
GRANT SELECT ON fyniq.risk, fyniq.risk_version, fyniq.risk_review TO fyniq_api, fyniq_worker, fyniq_readonly;
